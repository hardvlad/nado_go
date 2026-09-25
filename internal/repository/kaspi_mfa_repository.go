package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"nado_go/internal/database"
)

// KaspiMFARepository — коды подтверждения входа в кабинет Kaspi, прочитанные из
// почтового ящика служебных сотрудников. Код хранится открытым текстом
// (воркер предъявляет его форме Kaspi) и живёт недолго — см. миграцию 0007.
type KaspiMFARepository struct {
	db *database.DB
}

func NewKaspiMFARepository(db *database.DB) *KaspiMFARepository {
	return &KaspiMFARepository{db: db}
}

// Insert сохраняет новый код для адреса-получателя.
func (r *KaspiMFARepository) Insert(ctx context.Context, email, code string) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `INSERT INTO dbo.kaspi_mfa_codes (email, code) VALUES (@email, @code);`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("email", email), sql.Named("code", code)); err != nil {
		return fmt.Errorf("repository: сохранение кода MFA Kaspi: %w", database.MapError(err))
	}
	return nil
}

// Latest возвращает свежий неиспользованный код для адреса, полученный не раньше
// (now - ttl), и сразу помечает его использованным, чтобы повторный опрос не
// выдал тот же код дважды. Нет подходящего кода — ("", false, nil).
//
// Помечаем в том же запросе (UPDATE ... OUTPUT), а не двумя обращениями: между
// SELECT и UPDATE другой воркер мог бы забрать тот же код.
func (r *KaspiMFARepository) Latest(ctx context.Context, email string, ttl time.Duration) (string, bool, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		UPDATE TOP (1) dbo.kaspi_mfa_codes
		SET used_at = SYSUTCDATETIME()
		OUTPUT DELETED.code
		FROM dbo.kaspi_mfa_codes
		WHERE id = (
			SELECT TOP (1) id
			FROM dbo.kaspi_mfa_codes
			WHERE email = @email AND used_at IS NULL AND received_at >= @since
			ORDER BY id DESC
		);`

	var code string
	err := r.db.QueryRowContext(ctx, query,
		sql.Named("email", email),
		sql.Named("since", time.Now().UTC().Add(-ttl)),
	).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("repository: чтение кода MFA Kaspi: %w", database.MapError(err))
	}
	return code, true, nil
}

// Cleanup удаляет коды старше ttl (использованные и просроченные), чтобы открытый
// текст не хранился дольше необходимого. Возвращает число удалённых строк.
func (r *KaspiMFARepository) Cleanup(ctx context.Context, ttl time.Duration) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `DELETE FROM dbo.kaspi_mfa_codes WHERE received_at < @cutoff;`
	res, err := r.db.ExecContext(ctx, query, sql.Named("cutoff", time.Now().UTC().Add(-ttl)))
	if err != nil {
		return 0, fmt.Errorf("repository: очистка кодов MFA Kaspi: %w", database.MapError(err))
	}
	n, _ := res.RowsAffected()
	return n, nil
}
