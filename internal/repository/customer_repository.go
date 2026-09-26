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

// CustomerRepository — покупатели магазина, их сессии и коды входа (D-17).
type CustomerRepository struct {
	db *database.DB
}

func NewCustomerRepository(db *database.DB) *CustomerRepository {
	return &CustomerRepository{db: db}
}

// --- Одноразовые коды входа (otp_challenges) ---

// CreateChallenge сохраняет HMAC кода для пары магазин+телефон.
func (r *CustomerRepository) CreateChallenge(ctx context.Context, c *model.OTPChallenge, ip string) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		INSERT INTO dbo.otp_challenges (store_id, phone_e164, channel, code_hash, max_attempts, expires_at, ip)
		VALUES (@store, @phone, @channel, @hash, @max, @exp, @ip);`
	_, err := r.db.ExecContext(ctx, query,
		sql.Named("store", c.StoreID), sql.Named("phone", c.PhoneE164), sql.Named("channel", c.Channel),
		sql.Named("hash", c.CodeHash), sql.Named("max", c.MaxAttempts), sql.Named("exp", c.ExpiresAt.UTC()),
		sql.Named("ip", nullString(ip)))
	if err != nil {
		return fmt.Errorf("repository: код входа покупателя: %w", database.MapError(err))
	}
	return nil
}

// LatestChallenge возвращает последний непроверенный код магазина для номера.
func (r *CustomerRepository) LatestChallenge(ctx context.Context, storeID int64, phone string) (*model.OTPChallenge, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		SELECT TOP (1) id, store_id, phone_e164, channel, code_hash, attempts, max_attempts, expires_at
		FROM dbo.otp_challenges
		WHERE store_id = @store AND phone_e164 = @phone AND verified_at IS NULL
		ORDER BY id DESC;`
	var c model.OTPChallenge
	err := r.db.QueryRowContext(ctx, query, sql.Named("store", storeID), sql.Named("phone", phone)).
		Scan(&c.ID, &c.StoreID, &c.PhoneE164, &c.Channel, &c.CodeHash, &c.Attempts, &c.MaxAttempts, &c.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: чтение кода входа: %w", database.MapError(err))
	}
	return &c, nil
}

// LastChallengeAt — время последнего кода (для паузы повтора).
func (r *CustomerRepository) LastChallengeAt(ctx context.Context, storeID int64, phone string) (time.Time, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `SELECT TOP (1) created_at FROM dbo.otp_challenges WHERE store_id = @store AND phone_e164 = @phone ORDER BY id DESC;`
	var t time.Time
	err := r.db.QueryRowContext(ctx, query, sql.Named("store", storeID), sql.Named("phone", phone)).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("repository: время последнего кода: %w", database.MapError(err))
	}
	return t, nil
}

// IncChallengeAttempt увеличивает счётчик попыток ввода.
func (r *CustomerRepository) IncChallengeAttempt(ctx context.Context, id int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	if _, err := r.db.ExecContext(ctx, `UPDATE dbo.otp_challenges SET attempts = attempts + 1 WHERE id = @id;`, sql.Named("id", id)); err != nil {
		return fmt.Errorf("repository: попытка кода %d: %w", id, database.MapError(err))
	}
	return nil
}

// MarkChallengeVerified помечает код проверенным (гонку отсекает verified_at IS NULL).
func (r *CustomerRepository) MarkChallengeVerified(ctx context.Context, id int64) (bool, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	res, err := r.db.ExecContext(ctx, `UPDATE dbo.otp_challenges SET verified_at = SYSUTCDATETIME() WHERE id = @id AND verified_at IS NULL;`, sql.Named("id", id))
	if err != nil {
		return false, fmt.Errorf("repository: пометка кода %d: %w", id, database.MapError(err))
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// --- Покупатели ---

// UpsertCustomer создаёт покупателя или возвращает существующего по телефону,
// проставляя подтверждение телефона и согласие. Имя задаётся при создании.
func (r *CustomerRepository) UpsertCustomer(ctx context.Context, storeID, accountID int64, phone, name string) (*model.Customer, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		MERGE dbo.customers WITH (HOLDLOCK) AS t
		USING (SELECT @store AS store_id, @phone AS phone_e164) AS s
		ON t.store_id = s.store_id AND t.phone_e164 = s.phone_e164
		WHEN MATCHED THEN UPDATE SET
			phone_verified_at = SYSUTCDATETIME(),
			consent_at = ISNULL(t.consent_at, SYSUTCDATETIME()),
			name = COALESCE(NULLIF(t.name, N''), @name, t.name),
			status = 'active'
		WHEN NOT MATCHED THEN INSERT (store_id, account_id, phone_e164, phone_verified_at, name, consent_at, status)
			VALUES (@store, @acc, @phone, SYSUTCDATETIME(), @name, SYSUTCDATETIME(), 'active')
		OUTPUT INSERTED.id, INSERTED.store_id, INSERTED.account_id, INSERTED.phone_e164,
		       ISNULL(INSERTED.name, ''), ISNULL(INSERTED.email, ''), INSERTED.status;`
	var c model.Customer
	err := r.db.QueryRowContext(ctx, query,
		sql.Named("store", storeID), sql.Named("acc", accountID),
		sql.Named("phone", phone), sql.Named("name", nullString(name))).
		Scan(&c.ID, &c.StoreID, &c.AccountID, &c.PhoneE164, &c.Name, &c.Email, &c.Status)
	if err != nil {
		return nil, fmt.Errorf("repository: покупатель: %w", database.MapError(err))
	}
	return &c, nil
}

// --- Сессии ---

// CreateSession сохраняет сессию покупателя по хешу токена.
func (r *CustomerRepository) CreateSession(ctx context.Context, tokenHash []byte, customerID, storeID int64, ua string, expiresAt time.Time) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		INSERT INTO dbo.customer_sessions (token_hash, customer_id, store_id, user_agent, expires_at)
		VALUES (@hash, @cid, @store, @ua, @exp);`
	_, err := r.db.ExecContext(ctx, query,
		sql.Named("hash", tokenHash), sql.Named("cid", customerID), sql.Named("store", storeID),
		sql.Named("ua", nullString(truncate(ua, 400))), sql.Named("exp", expiresAt.UTC()))
	if err != nil {
		return fmt.Errorf("repository: сессия покупателя: %w", database.MapError(err))
	}
	return nil
}

// CustomerBySession возвращает активного покупателя по хешу токена сессии.
// Сессия должна принадлежать магазину и не истечь.
func (r *CustomerRepository) CustomerBySession(ctx context.Context, tokenHash []byte, storeID int64) (*model.Customer, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		SELECT c.id, c.store_id, c.account_id, c.phone_e164, ISNULL(c.name, ''), ISNULL(c.email, ''), c.status
		FROM dbo.customer_sessions s JOIN dbo.customers c ON c.id = s.customer_id
		WHERE s.token_hash = @hash AND s.store_id = @store AND s.expires_at > SYSUTCDATETIME() AND c.status = 'active';`
	var c model.Customer
	err := r.db.QueryRowContext(ctx, query, sql.Named("hash", tokenHash), sql.Named("store", storeID)).
		Scan(&c.ID, &c.StoreID, &c.AccountID, &c.PhoneE164, &c.Name, &c.Email, &c.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: покупатель по сессии: %w", database.MapError(err))
	}
	return &c, nil
}

// DeleteSession удаляет сессию (выход).
func (r *CustomerRepository) DeleteSession(ctx context.Context, tokenHash []byte) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	if _, err := r.db.ExecContext(ctx, `DELETE FROM dbo.customer_sessions WHERE token_hash = @hash;`, sql.Named("hash", tokenHash)); err != nil {
		return fmt.Errorf("repository: удаление сессии: %w", database.MapError(err))
	}
	return nil
}

// DeleteCustomer обезличивает покупателя и убирает его сессии (правило App Store
// 5.1.1: удаление аккаунта). Заказы сохраняются со снимком данных.
func (r *CustomerRepository) DeleteCustomer(ctx context.Context, storeID, customerID int64) error {
	return r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM dbo.customer_sessions WHERE customer_id = @cid AND store_id = @store;`,
			sql.Named("cid", customerID), sql.Named("store", storeID)); err != nil {
			return database.MapError(err)
		}
		const anon = `
			UPDATE dbo.customers
			SET status = 'deleted', name = NULL, email = NULL,
			    phone_e164 = CONCAT('deleted:', id)
			WHERE id = @cid AND store_id = @store;`
		if _, err := tx.ExecContext(ctx, anon, sql.Named("cid", customerID), sql.Named("store", storeID)); err != nil {
			return database.MapError(err)
		}
		return nil
	})
}
