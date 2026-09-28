package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// StoreRepository — магазины, учётные данные провайдеров и подключения к
// маркетплейсам (D-23: магазин ↔ подключение один-к-одному).
type StoreRepository struct {
	db *database.DB
}

func NewStoreRepository(db *database.DB) *StoreRepository {
	return &StoreRepository{db: db}
}

// NewConnection — данные для создания магазина с подключением к маркетплейсу.
type NewConnection struct {
	AccountID    int64
	Name         string // имя магазина
	Slug         string
	Country      string
	BaseCurrency string
	PlanCode     string
	Marketplace  string
	// Секрет провайдера уже зашифрован сервисом.
	SecretCiphertext []byte
	PublicMeta       string // JSON, что можно показать (merchantId, имя кабинета)
	ConnectionName   string
	// ShopSuffix — домен платформы для поддомена витрины (<slug>.<suffix>).
	// Пусто — поддомен не регистрируется (локальная разработка).
	ShopSuffix string
	// TrialDays — бесплатный пробный период магазина в днях (0 — без пробного).
	TrialDays int
	// Реквизиты кабинета для повторной проверки и редактирования (кабинетное
	// подключение): логин (e-mail сотрудника), зашифрованный пароль, uid кабинета.
	CabinetLogin          string
	CabinetPasswordCipher []byte
	MerchantUID           string
}

// CreateStoreWithConnection создаёт магазин, учётные данные и подключение одной
// транзакцией. Занятый slug возвращается как database.ErrConflict.
func (r *StoreRepository) CreateStoreWithConnection(ctx context.Context, in NewConnection) (storeID, connectionID int64, err error) {
	err = r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		const insertStore = `
			INSERT INTO dbo.stores (account_id, slug, name, country, base_currency, plan_code, trial_ends_at)
			OUTPUT INSERTED.id
			VALUES (@account_id, @slug, @name, @country, @base_currency, @plan_code,
			        CASE WHEN @trial_days > 0 THEN DATEADD(day, @trial_days, SYSUTCDATETIME()) ELSE NULL END);`
		if err := tx.QueryRowContext(ctx, insertStore,
			sql.Named("account_id", in.AccountID),
			sql.Named("slug", in.Slug),
			sql.Named("name", in.Name),
			sql.Named("country", in.Country),
			sql.Named("base_currency", in.BaseCurrency),
			sql.Named("plan_code", nullString(in.PlanCode)),
			sql.Named("trial_days", in.TrialDays),
		).Scan(&storeID); err != nil {
			return database.MapError(err)
		}

		const insertCred = `
			INSERT INTO dbo.provider_credentials (account_id, store_id, kind, provider_code, secret_ciphertext, public_meta, last_verified_at)
			OUTPUT INSERTED.id
			VALUES (@account_id, @store_id, 'marketplace', @provider, @secret, @meta, SYSUTCDATETIME());`
		var credID int64
		if err := tx.QueryRowContext(ctx, insertCred,
			sql.Named("account_id", in.AccountID),
			sql.Named("store_id", storeID),
			sql.Named("provider", in.Marketplace),
			sql.Named("secret", in.SecretCiphertext),
			sql.Named("meta", nullString(in.PublicMeta)),
		).Scan(&credID); err != nil {
			return database.MapError(err)
		}

		const insertConn = `
			INSERT INTO dbo.marketplace_connections (store_id, account_id, marketplace, credentials_id, name, cabinet_login, cabinet_password_ciphertext, merchant_uid)
			OUTPUT INSERTED.id
			VALUES (@store_id, @account_id, @marketplace, @credentials_id, @name, @cab_login, @cab_pass, @merchant_uid);`
		if err := tx.QueryRowContext(ctx, insertConn,
			sql.Named("store_id", storeID),
			sql.Named("account_id", in.AccountID),
			sql.Named("marketplace", in.Marketplace),
			sql.Named("credentials_id", credID),
			sql.Named("name", nullString(in.ConnectionName)),
			sql.Named("cab_login", nullString(in.CabinetLogin)),
			sql.Named("cab_pass", nullBytes(in.CabinetPasswordCipher)),
			sql.Named("merchant_uid", nullString(in.MerchantUID)),
		).Scan(&connectionID); err != nil {
			return database.MapError(err)
		}

		// Поддомен витрины <slug>.<suffix> — по нему магазин находится по Host.
		if in.ShopSuffix != "" {
			const insertDomain = `
				INSERT INTO dbo.store_domains (store_id, host, kind, is_primary)
				VALUES (@store_id, @host, 'subdomain', 1);`
			if _, err := tx.ExecContext(ctx, insertDomain,
				sql.Named("store_id", storeID),
				sql.Named("host", in.Slug+"."+in.ShopSuffix),
			); err != nil {
				return database.MapError(err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("repository: создание магазина с подключением: %w", err)
	}
	return storeID, connectionID, nil
}

// KaspiConnection — подключение с зашифрованным токеном для фоновой задачи.
type KaspiConnection struct {
	ConnectionID     int64
	StoreID          int64
	AccountID        int64
	SecretCiphertext []byte
	OrdersSyncAt     time.Time
}

// GetConnectionForSync возвращает подключение и его секрет для синхронизации.
func (r *StoreRepository) GetConnectionForSync(ctx context.Context, connectionID int64) (*KaspiConnection, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT c.id, c.store_id, c.account_id, pc.secret_ciphertext, c.orders_sync_at
		FROM dbo.marketplace_connections c
		JOIN dbo.provider_credentials pc ON pc.id = c.credentials_id
		WHERE c.id = @id AND c.status = 'active';`

	var (
		conn   KaspiConnection
		syncAt sql.NullTime
	)
	err := r.db.QueryRowContext(ctx, query, sql.Named("id", connectionID)).
		Scan(&conn.ConnectionID, &conn.StoreID, &conn.AccountID, &conn.SecretCiphertext, &syncAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: подключение %d: %w", connectionID, database.MapError(err))
	}
	conn.OrdersSyncAt = syncAt.Time
	return &conn, nil
}

// GetConnectionScope возвращает магазин и аккаунт активного подключения —
// для приёма импортированного каталога. Нет/неактивно — database.ErrNotFound.
func (r *StoreRepository) GetConnectionScope(ctx context.Context, connectionID int64) (storeID, accountID int64, err error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `SELECT store_id, account_id FROM dbo.marketplace_connections WHERE id = @id AND status = 'active';`
	err = r.db.QueryRowContext(ctx, query, sql.Named("id", connectionID)).Scan(&storeID, &accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, database.ErrNotFound
	}
	if err != nil {
		return 0, 0, fmt.Errorf("repository: область подключения %d: %w", connectionID, database.MapError(err))
	}
	return storeID, accountID, nil
}

// ConnectionIDForStore возвращает id активного подключения магазина (для
// перестроения каталога). Нет подключения — database.ErrNotFound.
func (r *StoreRepository) ConnectionIDForStore(ctx context.Context, accountID, storeID int64) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT TOP 1 id FROM dbo.marketplace_connections
		WHERE store_id = @store AND account_id = @acc AND status = 'active'
		ORDER BY id;`
	var id int64
	err := r.db.QueryRowContext(ctx, query, sql.Named("store", storeID), sql.Named("acc", accountID)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, database.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("repository: подключение магазина %d: %w", storeID, database.MapError(err))
	}
	return id, nil
}

// TouchContentSync запоминает время последней синхронизации каталога.
func (r *StoreRepository) TouchContentSync(ctx context.Context, connectionID int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `UPDATE dbo.marketplace_connections SET content_sync_at = SYSUTCDATETIME() WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", connectionID)); err != nil {
		return fmt.Errorf("repository: отметка синхронизации каталога %d: %w", connectionID, database.MapError(err))
	}
	return nil
}

// TouchOrdersSync запоминает время последней синхронизации заказов.
func (r *StoreRepository) TouchOrdersSync(ctx context.Context, connectionID int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `UPDATE dbo.marketplace_connections SET orders_sync_at = SYSUTCDATETIME() WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", connectionID)); err != nil {
		return fmt.Errorf("repository: отметка синхронизации %d: %w", connectionID, database.MapError(err))
	}
	return nil
}

// MarkConnectionInvalid переводит подключение в invalid (токен отозван).
func (r *StoreRepository) MarkConnectionInvalid(ctx context.Context, connectionID int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `UPDATE dbo.marketplace_connections SET status = 'invalid' WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", connectionID)); err != nil {
		return fmt.Errorf("repository: пометка подключения %d invalid: %w", connectionID, database.MapError(err))
	}
	return nil
}

// storefrontColumns — общий список колонок для разрешения витрины.
const storefrontColumns = `s.id, s.account_id, s.slug, s.name, s.status, s.default_lang, s.base_currency,
	s.theme_code, ISNULL(s.theme_settings, ''),
	ISNULL((SELECT TOP 1 host FROM dbo.store_domains d WHERE d.store_id = s.id AND d.is_primary = 1 ORDER BY d.id), '')`

func scanStorefront(row interface{ Scan(...any) error }) (*model.StorefrontStore, error) {
	var s model.StorefrontStore
	err := row.Scan(&s.ID, &s.AccountID, &s.Slug, &s.Name, &s.Status, &s.DefaultLang, &s.BaseCurrency,
		&s.ThemeCode, &s.ThemeSettings, &s.PrimaryHost)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: разрешение магазина: %w", database.MapError(err))
	}
	return &s, nil
}

// ResolveBySlug находит магазин витрины по slug (локальная разработка, /shop/{slug}).
func (r *StoreRepository) ResolveBySlug(ctx context.Context, slug string) (*model.StorefrontStore, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `SELECT ` + storefrontColumns + ` FROM dbo.stores s WHERE s.slug = @slug;`
	return scanStorefront(r.db.QueryRowContext(ctx, query, sql.Named("slug", slug)))
}

// ResolveByHost находит магазин витрины по домену (прод, выбор по Host).
func (r *StoreRepository) ResolveByHost(ctx context.Context, host string) (*model.StorefrontStore, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `SELECT ` + storefrontColumns + `
		FROM dbo.store_domains d JOIN dbo.stores s ON s.id = d.store_id
		WHERE d.host = @host;`
	return scanStorefront(r.db.QueryRowContext(ctx, query, sql.Named("host", host)))
}

// CatalogSyncTarget — активное подключение Kaspi с реквизитами кабинета для
// регулярного импорта каталога.
type CatalogSyncTarget struct {
	ConnectionID          int64
	AccountID             int64
	CabinetLogin          string
	CabinetPasswordCipher []byte
	MerchantUID           string
}

// OrdersSyncTarget — активное подключение Kaspi для регулярного импорта заказов
// по официальному API (нужен только токен подключения).
type OrdersSyncTarget struct {
	ConnectionID int64
	AccountID    int64
}

// OrdersSyncTargets возвращает активные подключения Kaspi активных магазинов —
// их заказы можно импортировать по расписанию (официальным API).
func (r *StoreRepository) OrdersSyncTargets(ctx context.Context) ([]OrdersSyncTarget, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT c.id, c.account_id
		FROM dbo.marketplace_connections c
		JOIN dbo.stores s ON s.id = c.store_id
		WHERE c.status = 'active' AND s.status = 'active' AND c.marketplace = 'kaspi';`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("repository: подключения для импорта заказов: %w", database.MapError(err))
	}
	defer rows.Close()

	var out []OrdersSyncTarget
	for rows.Next() {
		var t OrdersSyncTarget
		if err := rows.Scan(&t.ConnectionID, &t.AccountID); err != nil {
			return nil, fmt.Errorf("repository: чтение подключения для заказов: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CatalogSyncTargets возвращает активные подключения Kaspi у активных магазинов,
// у которых сохранены реквизиты кабинета (логин + пароль) — их каталог можно
// импортировать повторно по расписанию.
func (r *StoreRepository) CatalogSyncTargets(ctx context.Context) ([]CatalogSyncTarget, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT c.id, c.account_id, ISNULL(c.cabinet_login, ''), c.cabinet_password_ciphertext, ISNULL(c.merchant_uid, '')
		FROM dbo.marketplace_connections c
		JOIN dbo.stores s ON s.id = c.store_id
		WHERE c.status = 'active' AND s.status = 'active' AND c.marketplace = 'kaspi'
		  AND c.cabinet_login IS NOT NULL AND c.cabinet_login <> ''
		  AND c.cabinet_password_ciphertext IS NOT NULL;`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("repository: подключения для импорта каталога: %w", database.MapError(err))
	}
	defer rows.Close()

	var out []CatalogSyncTarget
	for rows.Next() {
		var t CatalogSyncTarget
		if err := rows.Scan(&t.ConnectionID, &t.AccountID, &t.CabinetLogin, &t.CabinetPasswordCipher, &t.MerchantUID); err != nil {
			return nil, fmt.Errorf("repository: чтение подключения для импорта: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TrialCandidate — магазин с пробным периодом и контактом владельца для
// напоминаний об оплате.
type TrialCandidate struct {
	StoreID     int64
	StoreName   string
	TrialEndsAt time.Time
	OwnerPhone  string
	OwnerLocale string
}

// DueTrials возвращает активные магазины, чей пробный период заканчивается в
// окне [from, to], вместе с телефоном и языком владельца (для WhatsApp).
func (r *StoreRepository) DueTrials(ctx context.Context, from, to time.Time) ([]TrialCandidate, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT s.id, s.name, s.trial_ends_at, u.phone, ISNULL(u.locale, '')
		FROM dbo.stores s
		JOIN dbo.account_members m ON m.account_id = s.account_id AND m.role = 'owner'
		JOIN dbo.users u ON u.id = m.user_id
		WHERE s.status = 'active' AND s.trial_ends_at IS NOT NULL
		  AND s.trial_ends_at >= @from AND s.trial_ends_at <= @to
		  AND u.phone IS NOT NULL AND u.phone <> '';`
	rows, err := r.db.QueryContext(ctx, query, sql.Named("from", from.UTC()), sql.Named("to", to.UTC()))
	if err != nil {
		return nil, fmt.Errorf("repository: магазины с истекающим пробным периодом: %w", database.MapError(err))
	}
	defer rows.Close()

	var out []TrialCandidate
	for rows.Next() {
		var c TrialCandidate
		if err := rows.Scan(&c.StoreID, &c.StoreName, &c.TrialEndsAt, &c.OwnerPhone, &c.OwnerLocale); err != nil {
			return nil, fmt.Errorf("repository: чтение кандидата напоминания: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClaimTrialReminder помечает веху напоминания отправленной. Возвращает true,
// если пометка поставлена сейчас (значит, напоминание ещё не отправляли).
func (r *StoreRepository) ClaimTrialReminder(ctx context.Context, storeID int64, daysBefore int) (bool, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		INSERT INTO dbo.trial_reminders (store_id, days_before)
		SELECT @store, @days
		WHERE NOT EXISTS (SELECT 1 FROM dbo.trial_reminders WHERE store_id = @store AND days_before = @days);`
	res, err := r.db.ExecContext(ctx, query, sql.Named("store", storeID), sql.Named("days", daysBefore))
	if err != nil {
		return false, fmt.Errorf("repository: пометка напоминания: %w", database.MapError(err))
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ReleaseTrialReminder снимает пометку (если отправка не удалась) — напоминание
// уйдёт при следующем сканировании.
func (r *StoreRepository) ReleaseTrialReminder(ctx context.Context, storeID int64, daysBefore int) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `DELETE FROM dbo.trial_reminders WHERE store_id = @store AND days_before = @days;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("store", storeID), sql.Named("days", daysBefore)); err != nil {
		return fmt.Errorf("repository: снятие пометки напоминания: %w", database.MapError(err))
	}
	return nil
}

// StoreLocale возвращает язык по умолчанию и базовую валюту магазина.
func (r *StoreRepository) StoreLocale(ctx context.Context, storeID int64) (lang, currency string, err error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `SELECT default_lang, base_currency FROM dbo.stores WHERE id = @id;`
	err = r.db.QueryRowContext(ctx, query, sql.Named("id", storeID)).Scan(&lang, &currency)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", database.ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("repository: локаль магазина %d: %w", storeID, database.MapError(err))
	}
	return lang, currency, nil
}

// ListStores возвращает магазины аккаунта с их подключением (для кабинета).
func (r *StoreRepository) ListStores(ctx context.Context, accountID int64) ([]model.Store, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT s.id, s.name, s.slug, s.status, s.base_currency,
		       ISNULL(c.marketplace, ''), ISNULL(c.status, ''), c.orders_sync_at, s.trial_ends_at
		FROM dbo.stores s
		LEFT JOIN dbo.marketplace_connections c ON c.store_id = s.id
		WHERE s.account_id = @account_id AND s.status <> 'archived'
		ORDER BY s.id DESC;`
	rows, err := r.db.QueryContext(ctx, query, sql.Named("account_id", accountID))
	if err != nil {
		return nil, fmt.Errorf("repository: список магазинов: %w", database.MapError(err))
	}
	defer rows.Close()

	var out []model.Store
	for rows.Next() {
		var (
			s       model.Store
			syncAt  sql.NullTime
			trialAt sql.NullTime
		)
		if err := rows.Scan(&s.ID, &s.Name, &s.Slug, &s.Status, &s.BaseCurrency,
			&s.Marketplace, &s.ConnectionStatus, &syncAt, &trialAt); err != nil {
			return nil, fmt.Errorf("repository: чтение магазина: %w", err)
		}
		s.OrdersSyncAt = syncAt.Time
		s.TrialEndsAt = trialAt.Time
		out = append(out, s)
	}
	return out, rows.Err()
}

// StoreForEdit — данные магазина для формы редактирования (кабинетное подключение).
type StoreForEdit struct {
	StoreID          int64
	AccountID        int64
	ConnectionID     int64
	CredentialsID    int64
	Name             string
	SecretCiphertext []byte // токен API (зашифрован)
	CabinetLogin     string
	MerchantUID      string
}

// GetStoreForEdit возвращает магазин аккаунта с реквизитами подключения для
// редактирования. Нет/чужой — database.ErrNotFound.
func (r *StoreRepository) GetStoreForEdit(ctx context.Context, accountID, storeID int64) (*StoreForEdit, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		SELECT s.id, s.account_id, c.id, c.credentials_id, s.name, pc.secret_ciphertext,
		       ISNULL(c.cabinet_login, ''), ISNULL(c.merchant_uid, '')
		FROM dbo.stores s
		JOIN dbo.marketplace_connections c ON c.store_id = s.id
		JOIN dbo.provider_credentials pc ON pc.id = c.credentials_id
		WHERE s.id = @id AND s.account_id = @acc AND s.status <> 'archived';`
	var e StoreForEdit
	err := r.db.QueryRowContext(ctx, query, sql.Named("id", storeID), sql.Named("acc", accountID)).
		Scan(&e.StoreID, &e.AccountID, &e.ConnectionID, &e.CredentialsID, &e.Name, &e.SecretCiphertext,
			&e.CabinetLogin, &e.MerchantUID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: магазин %d для редактирования: %w", storeID, database.MapError(err))
	}
	return &e, nil
}

// UpdateStoreForEdit — обновление магазина при редактировании (проверено сервисом).
type UpdateStoreForEdit struct {
	StoreID       int64
	ConnectionID  int64
	CredentialsID int64
	Name          string
	TokenCipher   []byte // новый токен API (зашифрован)
	CabinetLogin  string
	// CabinetPasswordCipher — новый пароль (nil — не менять).
	CabinetPasswordCipher []byte
	MerchantUID           string
}

// UpdateStore обновляет имя магазина, токен и реквизиты кабинета одной транзакцией.
func (r *StoreRepository) UpdateStore(ctx context.Context, in UpdateStoreForEdit) error {
	return r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE dbo.stores SET name = @name, updated_at = SYSUTCDATETIME() WHERE id = @id;`,
			sql.Named("name", in.Name), sql.Named("id", in.StoreID)); err != nil {
			return database.MapError(err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE dbo.provider_credentials SET secret_ciphertext = @secret, last_verified_at = SYSUTCDATETIME() WHERE id = @cid;`,
			sql.Named("secret", in.TokenCipher), sql.Named("cid", in.CredentialsID)); err != nil {
			return database.MapError(err)
		}
		// Пароль кабинета меняем только если задан новый.
		if len(in.CabinetPasswordCipher) > 0 {
			if _, err := tx.ExecContext(ctx,
				`UPDATE dbo.marketplace_connections SET cabinet_login = @login, cabinet_password_ciphertext = @pass, merchant_uid = @m, status = 'active' WHERE id = @conn;`,
				sql.Named("login", nullString(in.CabinetLogin)), sql.Named("pass", nullBytes(in.CabinetPasswordCipher)),
				sql.Named("m", nullString(in.MerchantUID)), sql.Named("conn", in.ConnectionID)); err != nil {
				return database.MapError(err)
			}
		} else {
			if _, err := tx.ExecContext(ctx,
				`UPDATE dbo.marketplace_connections SET cabinet_login = @login, merchant_uid = @m, status = 'active' WHERE id = @conn;`,
				sql.Named("login", nullString(in.CabinetLogin)), sql.Named("m", nullString(in.MerchantUID)),
				sql.Named("conn", in.ConnectionID)); err != nil {
				return database.MapError(err)
			}
		}
		return nil
	})
}
