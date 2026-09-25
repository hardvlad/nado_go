package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// KaspiOnboardingRepository — состояние кабинетного онбординга Kaspi (миграция
// 0008). Секреты (сессия входа владельца, пароль сотрудника) уже зашифрованы
// сервисом и хранятся как VARBINARY.
type KaspiOnboardingRepository struct {
	db *database.DB
}

func NewKaspiOnboardingRepository(db *database.DB) *KaspiOnboardingRepository {
	return &KaspiOnboardingRepository{db: db}
}

// Create заводит запись онбординга в статусе 'started' и возвращает её id.
func (r *KaspiOnboardingRepository) Create(ctx context.Context, o *model.KaspiOnboarding) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		INSERT INTO dbo.kaspi_onboarding (account_id, store_name, phone, employee_name, status)
		OUTPUT INSERTED.id
		VALUES (@account_id, @store_name, @phone, @employee_name, 'started');`
	var id int64
	err := r.db.QueryRowContext(ctx, query,
		sql.Named("account_id", o.AccountID),
		sql.Named("store_name", o.StoreName),
		sql.Named("phone", o.Phone),
		sql.Named("employee_name", o.EmployeeName),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("repository: создание онбординга Kaspi: %w", database.MapError(err))
	}
	return id, nil
}

// SetEmployeeEmail задаёт адрес служебного сотрудника (генерируется из id).
func (r *KaspiOnboardingRepository) SetEmployeeEmail(ctx context.Context, id int64, email string) error {
	return r.exec(ctx, `UPDATE dbo.kaspi_onboarding SET employee_email = @email, updated_at = SYSUTCDATETIME() WHERE id = @id;`,
		sql.Named("id", id), sql.Named("email", email))
}

// Get возвращает онбординг аккаунта (для кабинета). Merchants разбирается из JSON.
func (r *KaspiOnboardingRepository) Get(ctx context.Context, accountID, id int64) (*model.KaspiOnboarding, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT id, account_id, store_name, phone, employee_name, ISNULL(employee_email, ''),
		       status, merchants_json, ISNULL(merchant_id, ''), ISNULL(store_id, 0),
		       ISNULL(connection_id, 0), ISNULL(error, ''), created_at, updated_at
		FROM dbo.kaspi_onboarding
		WHERE id = @id AND account_id = @account_id;`

	var (
		o         model.KaspiOnboarding
		merchants sql.NullString
	)
	err := r.db.QueryRowContext(ctx, query, sql.Named("id", id), sql.Named("account_id", accountID)).
		Scan(&o.ID, &o.AccountID, &o.StoreName, &o.Phone, &o.EmployeeName, &o.EmployeeEmail,
			&o.Status, &merchants, &o.MerchantID, &o.StoreID, &o.ConnectionID, &o.Error, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: чтение онбординга %d: %w", id, database.MapError(err))
	}
	if merchants.Valid && merchants.String != "" {
		_ = json.Unmarshal([]byte(merchants.String), &o.Merchants)
	}
	return &o, nil
}

// VerifyData — данные для постановки задачи проверки кода: зашифрованная сессия
// и реквизиты сотрудника.
type VerifyData struct {
	SessionCipher []byte
	EmployeeEmail string
	EmployeeName  string
	MerchantID    string // не пусто, если кабинет уже выбран
}

// LoadForVerify возвращает данные для проверки кода/выбора кабинета.
func (r *KaspiOnboardingRepository) LoadForVerify(ctx context.Context, id int64) (*VerifyData, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT session_ciphertext, ISNULL(employee_email, ''), employee_name, ISNULL(merchant_id, '')
		FROM dbo.kaspi_onboarding WHERE id = @id;`
	var v VerifyData
	err := r.db.QueryRowContext(ctx, query, sql.Named("id", id)).
		Scan(&v.SessionCipher, &v.EmployeeEmail, &v.EmployeeName, &v.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: данные проверки онбординга %d: %w", id, database.MapError(err))
	}
	return &v, nil
}

// Scope возвращает аккаунт и имя магазина онбординга без проверки владельца —
// для серверных шагов (создание магазина по токену из кабинета).
func (r *KaspiOnboardingRepository) Scope(ctx context.Context, id int64) (accountID int64, storeName string, err error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `SELECT account_id, store_name FROM dbo.kaspi_onboarding WHERE id = @id;`
	err = r.db.QueryRowContext(ctx, query, sql.Named("id", id)).Scan(&accountID, &storeName)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", database.ErrNotFound
	}
	if err != nil {
		return 0, "", fmt.Errorf("repository: область онбординга %d: %w", id, database.MapError(err))
	}
	return accountID, storeName, nil
}

// SaveSession сохраняет зашифрованную сессию входа владельца и переводит статус.
func (r *KaspiOnboardingRepository) SaveSession(ctx context.Context, id int64, cipher []byte, status string) error {
	return r.exec(ctx, `UPDATE dbo.kaspi_onboarding SET session_ciphertext = @c, status = @s, updated_at = SYSUTCDATETIME() WHERE id = @id;`,
		sql.Named("id", id), sql.Named("c", cipher), sql.Named("s", status))
}

// SaveMerchants сохраняет обновлённую сессию (с mc-sid) и список кабинетов для
// выбора продавцом.
func (r *KaspiOnboardingRepository) SaveMerchants(ctx context.Context, id int64, cipher []byte, merchantsJSON string) error {
	return r.exec(ctx, `UPDATE dbo.kaspi_onboarding SET session_ciphertext = @c, merchants_json = @m, status = 'need_merchant', updated_at = SYSUTCDATETIME() WHERE id = @id;`,
		sql.Named("id", id), sql.Named("c", cipher), sql.Named("m", merchantsJSON))
}

// CompleteEmployee фиксирует созданного сотрудника, подключённый магазин и
// переводит онбординг в ожидание пароля.
func (r *KaspiOnboardingRepository) CompleteEmployee(ctx context.Context, id int64, merchantID string, storeID, connID int64) error {
	return r.exec(ctx, `UPDATE dbo.kaspi_onboarding SET merchant_id = @m, store_id = @st, connection_id = @cn, status = 'employee_created', error = NULL, updated_at = SYSUTCDATETIME() WHERE id = @id;`,
		sql.Named("id", id), sql.Named("m", merchantID), sql.Named("st", storeID), sql.Named("cn", connID))
}

// SetStatus обновляет только статус (например, verifying при постановке задачи).
func (r *KaspiOnboardingRepository) SetStatus(ctx context.Context, id int64, status string) error {
	return r.exec(ctx, `UPDATE dbo.kaspi_onboarding SET status = @s, updated_at = SYSUTCDATETIME() WHERE id = @id;`,
		sql.Named("id", id), sql.Named("s", status))
}

// Fail помечает онбординг ошибкой (текст показывается продавцу).
func (r *KaspiOnboardingRepository) Fail(ctx context.Context, id int64, msg string) error {
	return r.exec(ctx, `UPDATE dbo.kaspi_onboarding SET status = 'failed', error = @e, updated_at = SYSUTCDATETIME() WHERE id = @id;`,
		sql.Named("id", id), sql.Named("e", truncate(msg, 1000)))
}

// PendingByEmail — онбординг, ожидающий пароль сотрудника, по адресу сотрудника.
type PendingByEmail struct {
	ID            int64
	ConnectionID  int64
	MerchantID    string
	EmployeeEmail string
}

// FindPendingByEmail ищет онбординг в статусе ожидания пароля по адресу
// сотрудника (письмо с паролем пришло на этот адрес). Нет — database.ErrNotFound.
func (r *KaspiOnboardingRepository) FindPendingByEmail(ctx context.Context, email string) (*PendingByEmail, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT id, ISNULL(connection_id, 0), ISNULL(merchant_id, ''), ISNULL(employee_email, '')
		FROM dbo.kaspi_onboarding
		WHERE employee_email = @email AND status = 'employee_created';`
	var p PendingByEmail
	err := r.db.QueryRowContext(ctx, query, sql.Named("email", email)).
		Scan(&p.ID, &p.ConnectionID, &p.MerchantID, &p.EmployeeEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: поиск онбординга по адресу: %w", database.MapError(err))
	}
	return &p, nil
}

// SetPassword сохраняет зашифрованный пароль сотрудника и переводит статус в
// catalog_queued (импорт каталога поставлен).
func (r *KaspiOnboardingRepository) SetPassword(ctx context.Context, id int64, cipher []byte) error {
	return r.exec(ctx, `UPDATE dbo.kaspi_onboarding SET password_ciphertext = @c, status = 'catalog_queued', updated_at = SYSUTCDATETIME() WHERE id = @id;`,
		sql.Named("id", id), sql.Named("c", cipher))
}

// exec — общий помощник для UPDATE-ов.
func (r *KaspiOnboardingRepository) exec(ctx context.Context, query string, args ...any) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("repository: обновление онбординга: %w", database.MapError(err))
	}
	return nil
}
