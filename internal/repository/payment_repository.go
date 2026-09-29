package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"nado_go/internal/database"
)

// PaymentRepository — платежи и настройки приёма оплат магазином, а также
// подписка аккаунта. Изоляция арендатора — по account_id (и store_id, где есть).
type PaymentRepository struct {
	db *database.DB
}

func NewPaymentRepository(db *database.DB) *PaymentRepository { return &PaymentRepository{db: db} }

// --- Платежи ---

// NewPayment — данные для создания платежа.
type NewPayment struct {
	AccountID   int64
	StoreID     int64 // 0 — подписка платформе
	Purpose     string
	OrderID     int64 // 0 — нет заказа
	Provider    string
	RefToken    string
	AmountMinor int64
	Currency    string
	PlanCode    string
	PeriodDays  int
}

// PaymentRow — платёж из БД.
type PaymentRow struct {
	ID          int64
	AccountID   int64
	StoreID     int64
	Purpose     string
	OrderID     int64
	Provider    string
	ProviderRef string
	RefToken    string
	AmountMinor int64
	Currency    string
	Status      string
	PlanCode    string
	PeriodDays  int
}

// CreatePayment создаёт платёж в статусе pending и возвращает его id.
func (r *PaymentRepository) CreatePayment(ctx context.Context, in NewPayment) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const q = `
		INSERT INTO dbo.payments (account_id, store_id, purpose, order_id, provider, ref_token, amount_minor, currency, status, plan_code, period_days)
		OUTPUT INSERTED.id
		VALUES (@acc, @store, @purpose, @order, @provider, @ref, @amount, @cur, 'pending', @plan, @period);`
	var id int64
	err := r.db.QueryRowContext(ctx, q,
		sql.Named("acc", in.AccountID), sql.Named("store", nullInt64(in.StoreID)),
		sql.Named("purpose", in.Purpose), sql.Named("order", nullInt64(in.OrderID)),
		sql.Named("provider", in.Provider), sql.Named("ref", in.RefToken),
		sql.Named("amount", in.AmountMinor), sql.Named("cur", in.Currency),
		sql.Named("plan", nullString(in.PlanCode)), sql.Named("period", nullInt(in.PeriodDays)),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("repository: создание платежа: %w", database.MapError(err))
	}
	return id, nil
}

// SetProviderRef сохраняет идентификатор платежа у провайдера.
func (r *PaymentRepository) SetProviderRef(ctx context.Context, id int64, ref string) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	_, err := r.db.ExecContext(ctx,
		`UPDATE dbo.payments SET provider_ref = @ref, updated_at = SYSUTCDATETIME() WHERE id = @id;`,
		sql.Named("ref", nullString(ref)), sql.Named("id", id))
	if err != nil {
		return fmt.Errorf("repository: сохранение provider_ref: %w", database.MapError(err))
	}
	return nil
}

// GetPaymentByRef возвращает платёж по нашему ref_token.
func (r *PaymentRepository) GetPaymentByRef(ctx context.Context, ref string) (*PaymentRow, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const q = `
		SELECT id, account_id, ISNULL(store_id, 0), purpose, ISNULL(order_id, 0), provider,
		       ISNULL(provider_ref, ''), ref_token, amount_minor, currency, status,
		       ISNULL(plan_code, ''), ISNULL(period_days, 0)
		FROM dbo.payments WHERE ref_token = @ref;`
	p, err := scanPayment(r.db.QueryRowContext(ctx, q, sql.Named("ref", ref)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: платёж %s: %w", ref, database.MapError(err))
	}
	return p, nil
}

// MarkPaymentStatus обновляет статус платежа по ref_token и возвращает платёж.
// Идемпотентно: повторный вебхук на уже succeeded не меняет статус.
func (r *PaymentRepository) MarkPaymentStatus(ctx context.Context, ref, status, providerRef string) (*PaymentRow, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const q = `
		UPDATE dbo.payments
		SET status = CASE WHEN status = 'succeeded' THEN status ELSE @status END,
		    provider_ref = COALESCE(NULLIF(@pref, ''), provider_ref),
		    updated_at = SYSUTCDATETIME()
		OUTPUT INSERTED.id, INSERTED.account_id, ISNULL(INSERTED.store_id, 0), INSERTED.purpose,
		       ISNULL(INSERTED.order_id, 0), INSERTED.provider, ISNULL(INSERTED.provider_ref, ''),
		       INSERTED.ref_token, INSERTED.amount_minor, INSERTED.currency, INSERTED.status,
		       ISNULL(INSERTED.plan_code, ''), ISNULL(INSERTED.period_days, 0)
		WHERE ref_token = @ref;`
	p, err := scanPayment(r.db.QueryRowContext(ctx, q,
		sql.Named("status", status), sql.Named("pref", providerRef), sql.Named("ref", ref)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: смена статуса платежа %s: %w", ref, database.MapError(err))
	}
	return p, nil
}

func scanPayment(row *sql.Row) (*PaymentRow, error) {
	var p PaymentRow
	if err := row.Scan(&p.ID, &p.AccountID, &p.StoreID, &p.Purpose, &p.OrderID, &p.Provider,
		&p.ProviderRef, &p.RefToken, &p.AmountMinor, &p.Currency, &p.Status, &p.PlanCode, &p.PeriodDays); err != nil {
		return nil, err
	}
	return &p, nil
}

// --- Методы оплаты магазина (несколько провайдеров одновременно) ---

// StorePaymentMethod — один подключённый метод оплаты магазина.
type StorePaymentMethod struct {
	StoreID          int64
	AccountID        int64
	Provider         string
	IsEnabled        bool
	MerchantID       string
	Terminal         string
	SecretCiphertext []byte
	Testing          bool
	WebhookToken     string
	Sort             int
	HasSecret        bool
}

const storeMethodCols = `store_id, account_id, provider, is_enabled, ISNULL(merchant_id, ''), ISNULL(terminal_id, ''),
	secret_ciphertext, testing_mode, ISNULL(webhook_token, ''), sort`

func scanStoreMethod(sc interface{ Scan(...any) error }) (*StorePaymentMethod, error) {
	var (
		m      StorePaymentMethod
		secret []byte
	)
	if err := sc.Scan(&m.StoreID, &m.AccountID, &m.Provider, &m.IsEnabled, &m.MerchantID, &m.Terminal,
		&secret, &m.Testing, &m.WebhookToken, &m.Sort); err != nil {
		return nil, err
	}
	m.SecretCiphertext = secret
	m.HasSecret = len(secret) > 0
	return &m, nil
}

// ListStorePaymentMethods возвращает все методы оплаты магазина. Изоляция по account_id.
func (r *PaymentRepository) ListStorePaymentMethods(ctx context.Context, accountID, storeID int64) ([]StorePaymentMethod, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	q := `SELECT ` + storeMethodCols + `
		FROM dbo.store_payment_methods WHERE store_id = @store AND account_id = @acc ORDER BY sort, provider;`
	rows, err := r.db.QueryContext(ctx, q, sql.Named("store", storeID), sql.Named("acc", accountID))
	if err != nil {
		return nil, fmt.Errorf("repository: методы оплаты магазина %d: %w", storeID, database.MapError(err))
	}
	defer rows.Close()
	var out []StorePaymentMethod
	for rows.Next() {
		m, err := scanStoreMethod(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: чтение метода оплаты: %w", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetStorePaymentMethod возвращает метод оплаты магазина по провайдеру (или nil).
func (r *PaymentRepository) GetStorePaymentMethod(ctx context.Context, accountID, storeID int64, provider string) (*StorePaymentMethod, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	q := `SELECT ` + storeMethodCols + `
		FROM dbo.store_payment_methods WHERE store_id = @store AND account_id = @acc AND provider = @provider;`
	m, err := scanStoreMethod(r.db.QueryRowContext(ctx, q, sql.Named("store", storeID), sql.Named("acc", accountID), sql.Named("provider", provider)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("repository: метод оплаты %s магазина %d: %w", provider, storeID, database.MapError(err))
	}
	return m, nil
}

// ResolveStoreMethodByToken находит метод оплаты по провайдеру и токену вебхука.
// Нет совпадения — database.ErrNotFound.
func (r *PaymentRepository) ResolveStoreMethodByToken(ctx context.Context, provider, token string) (*StorePaymentMethod, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	q := `SELECT ` + storeMethodCols + `
		FROM dbo.store_payment_methods WHERE provider = @provider AND webhook_token = @token;`
	m, err := scanStoreMethod(r.db.QueryRowContext(ctx, q, sql.Named("provider", provider), sql.Named("token", token)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: метод оплаты по токену: %w", database.MapError(err))
	}
	return m, nil
}

// SaveStorePaymentMethodInput — создание/обновление метода оплаты (по store+provider).
type SaveStorePaymentMethodInput struct {
	StoreID          int64
	AccountID        int64
	Provider         string
	IsEnabled        bool
	MerchantID       string
	Terminal         string
	Testing          bool
	SecretCiphertext []byte // nil — не менять секрет
	UpdateSecret     bool
	WebhookToken     string // задаётся при UpdateToken
	UpdateToken      bool
}

// SaveStorePaymentMethod создаёт или обновляет метод (MERGE по store_id+provider).
func (r *PaymentRepository) SaveStorePaymentMethod(ctx context.Context, in SaveStorePaymentMethodInput) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const q = `
		MERGE dbo.store_payment_methods WITH (HOLDLOCK) AS t
		USING (SELECT @store AS store_id, @provider AS provider) AS s
		ON t.store_id = s.store_id AND t.provider = s.provider
		WHEN MATCHED THEN UPDATE SET
			is_enabled = @enabled, merchant_id = @merchant, terminal_id = @terminal, testing_mode = @testing,
			secret_ciphertext = CASE WHEN @update_secret = 1 THEN CONVERT(VARBINARY(4000), @secret) ELSE t.secret_ciphertext END,
			webhook_token = CASE WHEN @update_token = 1 THEN @token ELSE t.webhook_token END,
			updated_at = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN INSERT (store_id, account_id, provider, is_enabled, merchant_id, terminal_id, testing_mode, secret_ciphertext, webhook_token)
			VALUES (@store, @acc, @provider, @enabled, @merchant, @terminal, @testing, CONVERT(VARBINARY(4000), @secret), @token);`
	_, err := r.db.ExecContext(ctx, q,
		sql.Named("store", in.StoreID), sql.Named("acc", in.AccountID),
		sql.Named("provider", in.Provider), sql.Named("enabled", boolBit(in.IsEnabled)),
		sql.Named("merchant", nullString(in.MerchantID)), sql.Named("terminal", nullString(in.Terminal)),
		sql.Named("testing", boolBit(in.Testing)),
		sql.Named("update_secret", boolBit(in.UpdateSecret)), sql.Named("secret", nullBytes(in.SecretCiphertext)),
		sql.Named("update_token", boolBit(in.UpdateToken)), sql.Named("token", nullString(in.WebhookToken)),
	)
	if err != nil {
		return fmt.Errorf("repository: сохранение метода оплаты %s магазина %d: %w", in.Provider, in.StoreID, database.MapError(err))
	}
	return nil
}

// DeleteStorePaymentMethod удаляет метод оплаты магазина.
func (r *PaymentRepository) DeleteStorePaymentMethod(ctx context.Context, accountID, storeID int64, provider string) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM dbo.store_payment_methods WHERE store_id = @store AND account_id = @acc AND provider = @provider;`,
		sql.Named("store", storeID), sql.Named("acc", accountID), sql.Named("provider", provider))
	if err != nil {
		return fmt.Errorf("repository: удаление метода оплаты %s: %w", provider, database.MapError(err))
	}
	return nil
}

// --- Подписка аккаунта ---

// Subscription — состояние подписки аккаунта на платформу.
type Subscription struct {
	Status   string
	Until    time.Time
	PlanCode string
}

// GetSubscription возвращает статус подписки аккаунта.
func (r *PaymentRepository) GetSubscription(ctx context.Context, accountID int64) (*Subscription, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const q = `SELECT subscription_status, subscription_until, plan_code FROM dbo.accounts WHERE id = @id;`
	var (
		s     Subscription
		until sql.NullTime
	)
	err := r.db.QueryRowContext(ctx, q, sql.Named("id", accountID)).Scan(&s.Status, &until, &s.PlanCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: подписка аккаунта %d: %w", accountID, database.MapError(err))
	}
	s.Until = until.Time
	return &s, nil
}

// ActivateSubscription продлевает подписку: статус active и until (максимум из
// текущего и нового, чтобы оплата продлевала, а не сбрасывала остаток).
func (r *PaymentRepository) ActivateSubscription(ctx context.Context, accountID int64, planCode string, until time.Time) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const q = `
		UPDATE dbo.accounts
		SET subscription_status = 'active', plan_code = @plan,
		    subscription_until = CASE WHEN subscription_until IS NULL OR subscription_until < @until THEN @until ELSE subscription_until END
		WHERE id = @id;`
	_, err := r.db.ExecContext(ctx, q,
		sql.Named("plan", planCode), sql.Named("until", until), sql.Named("id", accountID))
	if err != nil {
		return fmt.Errorf("repository: активация подписки аккаунта %d: %w", accountID, database.MapError(err))
	}
	return nil
}

func boolBit(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullInt(v int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(v), Valid: v != 0}
}
