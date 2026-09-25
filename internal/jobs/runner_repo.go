package jobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"nado_go/internal/database"
)

// Runner — удалённый воркер, аутентифицированный постоянным токеном.
type RunnerInfo struct {
	ID    int64
	Code  string
	Kinds []string // разрешённые типы задач; пусто — любые remote
}

// Allows сообщает, вправе ли раннер брать задачу этого типа.
func (r RunnerInfo) Allows(kind string) bool {
	if len(r.Kinds) == 0 {
		return true
	}
	for _, k := range r.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// RunnerRepository — учётные записи удалённых воркеров.
type RunnerRepository struct {
	db *database.DB
}

func NewRunnerRepository(db *database.DB) *RunnerRepository {
	return &RunnerRepository{db: db}
}

// HashToken возвращает SHA-256 токена: в БД хранится только он.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Authenticate находит активного раннера по токену и отмечает его онлайн.
// Неизвестный или отключённый токен — database.ErrNotFound.
func (r *RunnerRepository) Authenticate(ctx context.Context, token, ip string) (*RunnerInfo, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		UPDATE dbo.job_runners
		SET last_seen_at = SYSUTCDATETIME(), last_ip = @ip
		OUTPUT INSERTED.id, INSERTED.code, INSERTED.kinds
		WHERE token_hash = @hash AND disabled = 0;`

	var (
		info  RunnerInfo
		kinds sql.NullString
	)
	err := r.db.QueryRowContext(ctx, query, sql.Named("hash", HashToken(token)), sql.Named("ip", nullString(ip))).
		Scan(&info.ID, &info.Code, &kinds)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("jobs: аутентификация воркера: %w", database.MapError(err))
	}
	if kinds.Valid {
		for _, k := range strings.Split(kinds.String, ",") {
			if k = strings.TrimSpace(k); k != "" {
				info.Kinds = append(info.Kinds, k)
			}
		}
	}
	return &info, nil
}
