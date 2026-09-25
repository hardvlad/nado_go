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

// OTPRepository — одноразовые коды подтверждения телефона.
type OTPRepository struct {
	db *database.DB
}

func NewOTPRepository(db *database.DB) *OTPRepository {
	return &OTPRepository{db: db}
}

// Create сохраняет код (уже в виде HMAC) и возвращает его id.
func (r *OTPRepository) Create(ctx context.Context, c *model.OTPCode) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		INSERT INTO dbo.otp_codes (purpose, phone, code_hash, channel, instance_id, message_id, max_attempts, expires_at, ip)
		OUTPUT INSERTED.id
		VALUES (@purpose, @phone, @code_hash, @channel, @instance_id, @message_id, @max_attempts, @expires_at, @ip);`

	var id int64
	err := r.db.QueryRowContext(ctx, query,
		sql.Named("purpose", c.Purpose),
		sql.Named("phone", c.Phone),
		sql.Named("code_hash", c.CodeHash),
		sql.Named("channel", c.Channel),
		sql.Named("instance_id", nullInt64(c.InstanceID)),
		sql.Named("message_id", nullString(c.MessageID)),
		sql.Named("max_attempts", c.MaxAttempts),
		sql.Named("expires_at", c.ExpiresAt.UTC()),
		sql.Named("ip", nullString(c.IP)),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("repository: сохранение кода: %w", database.MapError(err))
	}
	return id, nil
}

// Latest возвращает последний неиспользованный код для номера и цели.
// Нет такого — database.ErrNotFound.
func (r *OTPRepository) Latest(ctx context.Context, phone, purpose string) (*model.OTPCode, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT TOP (1) id, purpose, phone, code_hash, channel, attempts, max_attempts, expires_at
		FROM dbo.otp_codes
		WHERE phone = @phone AND purpose = @purpose AND used_at IS NULL
		ORDER BY id DESC;`

	var c model.OTPCode
	err := r.db.QueryRowContext(ctx, query, sql.Named("phone", phone), sql.Named("purpose", purpose)).
		Scan(&c.ID, &c.Purpose, &c.Phone, &c.CodeHash, &c.Channel, &c.Attempts, &c.MaxAttempts, &c.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: чтение кода: %w", database.MapError(err))
	}
	return &c, nil
}

// LastSentAt — время создания последнего кода для номера (для паузы повтора).
// Нет кодов — нулевое время.
func (r *OTPRepository) LastSentAt(ctx context.Context, phone string) (time.Time, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `SELECT TOP (1) created_at FROM dbo.otp_codes WHERE phone = @phone ORDER BY id DESC;`
	var t time.Time
	err := r.db.QueryRowContext(ctx, query, sql.Named("phone", phone)).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("repository: время последнего кода: %w", database.MapError(err))
	}
	return t, nil
}

// IncAttempt увеличивает число попыток ввода кода.
func (r *OTPRepository) IncAttempt(ctx context.Context, id int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `UPDATE dbo.otp_codes SET attempts = attempts + 1 WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", id)); err != nil {
		return fmt.Errorf("repository: учёт попытки кода %d: %w", id, database.MapError(err))
	}
	return nil
}

// MarkUsed помечает код использованным. Гонку двух проверок отсекает условие
// used_at IS NULL: применится только первая.
func (r *OTPRepository) MarkUsed(ctx context.Context, id int64) (bool, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `UPDATE dbo.otp_codes SET used_at = SYSUTCDATETIME() WHERE id = @id AND used_at IS NULL;`
	res, err := r.db.ExecContext(ctx, query, sql.Named("id", id))
	if err != nil {
		return false, fmt.Errorf("repository: пометка кода %d использованным: %w", id, database.MapError(err))
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
