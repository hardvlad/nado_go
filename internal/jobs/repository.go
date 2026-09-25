package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"nado_go/internal/database"
)

// Repository — доступ к очереди задач в MSSQL.
type Repository struct {
	db *database.DB
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

// Enqueue ставит задачу вне транзакции.
func (r *Repository) Enqueue(ctx context.Context, e Enqueue) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	return r.enqueue(ctx, r.db, e)
}

// EnqueueTx ставит задачу в переданной транзакции — чтобы задача и изменение
// данных фиксировались вместе (transactional outbox): задача не потеряется при
// падении между коммитом и постановкой.
func (r *Repository) EnqueueTx(ctx context.Context, tx *sql.Tx, e Enqueue) (int64, error) {
	return r.enqueue(ctx, tx, e)
}

// execer — общий интерфейс *sql.DB-обёртки и *sql.Tx.
type execer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (r *Repository) enqueue(ctx context.Context, ex execer, e Enqueue) (int64, error) {
	payload, err := payloadJSON(e.Payload)
	if err != nil {
		return 0, err
	}
	exec := e.Execution
	if exec == "" {
		exec = Local
	}
	priority := e.Priority
	if priority == 0 {
		priority = 100
	}
	maxAttempts := e.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 10
	}
	runAt := e.RunAt
	if runAt.IsZero() {
		runAt = time.Now()
	}

	const query = `
		INSERT INTO dbo.jobs (kind, execution, account_id, payload, run_at, priority, max_attempts, dedup_key)
		OUTPUT INSERTED.id
		VALUES (@kind, @execution, @account_id, @payload, @run_at, @priority, @max_attempts, @dedup_key);`

	var id int64
	err = ex.QueryRowContext(ctx, query,
		sql.Named("kind", e.Kind),
		sql.Named("execution", string(exec)),
		sql.Named("account_id", nullInt64(e.AccountID)),
		sql.Named("payload", string(payload)),
		sql.Named("run_at", runAt.UTC()),
		sql.Named("priority", priority),
		sql.Named("max_attempts", maxAttempts),
		sql.Named("dedup_key", nullString(e.DedupKey)),
	).Scan(&id)
	if err != nil {
		// Нарушение UX_jobs_dedup: задача уже в очереди — это не ошибка.
		if errors.Is(database.MapError(err), database.ErrConflict) {
			return 0, nil
		}
		return 0, fmt.Errorf("jobs: постановка задачи %q: %w", e.Kind, database.MapError(err))
	}
	return id, nil
}

// Lease захватывает одну готовую задачу указанного типа выполнения.
// worker — идентификатор воркера (для отладки и повторного захвата зависших).
// kinds — необязательный белый список типов; пусто — любые.
// Возвращает (nil, nil), если готовых задач нет.
func (r *Repository) Lease(ctx context.Context, worker string, exec Execution, kinds []string, leaseFor time.Duration) (*Job, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	args := []any{
		sql.Named("worker", worker),
		sql.Named("execution", string(exec)),
		sql.Named("lease", int(leaseFor.Seconds())),
	}
	kindFilter := ""
	if len(kinds) > 0 {
		placeholders := make([]string, len(kinds))
		for i, k := range kinds {
			name := fmt.Sprintf("k%d", i)
			placeholders[i] = "@" + name
			args = append(args, sql.Named(name, k))
		}
		kindFilter = " AND kind IN (" + strings.Join(placeholders, ", ") + ")"
	}

	// UPDLOCK не даёт двум воркерам взять одну строку, READPAST пропускает уже
	// захваченные строки без ожидания. Берём готовые к запуску и «зависшие»
	// running с истёкшей арендой (упавший воркер).
	query := `
		WITH next AS (
			SELECT TOP (1) *
			FROM dbo.jobs WITH (UPDLOCK, READPAST, ROWLOCK)
			WHERE execution = @execution` + kindFilter + `
			  AND (
			        (status = '` + StatusQueued + `' AND run_at <= SYSUTCDATETIME())
			     OR (status = '` + StatusRunning + `' AND locked_until < SYSUTCDATETIME())
			  )
			ORDER BY run_at, priority, id
		)
		UPDATE next
		SET status = '` + StatusRunning + `',
		    locked_by = @worker,
		    locked_until = DATEADD(SECOND, @lease, SYSUTCDATETIME()),
		    attempts = attempts + 1
		OUTPUT INSERTED.id, INSERTED.kind, INSERTED.execution, INSERTED.account_id,
		       INSERTED.payload, INSERTED.attempts, INSERTED.max_attempts;`

	var (
		j       Job
		account sql.NullInt64
		payload string
	)
	err := r.db.QueryRowContext(ctx, query, args...).
		Scan(&j.ID, &j.Kind, &j.Execution, &account, &payload, &j.Attempts, &j.MaxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("jobs: захват задачи: %w", database.MapError(err))
	}
	j.AccountID = account.Int64
	j.Payload = []byte(payload)
	return &j, nil
}

// Complete помечает задачу выполненной и сохраняет результат.
func (r *Repository) Complete(ctx context.Context, id int64, result []byte) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		UPDATE dbo.jobs
		SET status = '` + StatusDone + `', result = @result, finished_at = SYSUTCDATETIME(),
		    locked_by = NULL, locked_until = NULL, last_error = NULL
		WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", id), sql.Named("result", nullString(string(result)))); err != nil {
		return fmt.Errorf("jobs: завершение задачи %d: %w", id, database.MapError(err))
	}
	return nil
}

// LoadOwned возвращает выполняющуюся задачу, только если она числится за этим
// воркером. Так удалённый воркер не может завершить или провалить чужую задачу
// по её id. Нет такой задачи — database.ErrNotFound.
func (r *Repository) LoadOwned(ctx context.Context, id int64, worker string) (*Job, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT id, kind, execution, account_id, payload, attempts, max_attempts
		FROM dbo.jobs
		WHERE id = @id AND locked_by = @worker AND status = '` + StatusRunning + `';`

	var (
		j       Job
		account sql.NullInt64
		payload string
	)
	err := r.db.QueryRowContext(ctx, query, sql.Named("id", id), sql.Named("worker", worker)).
		Scan(&j.ID, &j.Kind, &j.Execution, &account, &payload, &j.Attempts, &j.MaxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("jobs: загрузка задачи %d воркера %q: %w", id, worker, database.MapError(err))
	}
	j.AccountID = account.Int64
	j.Payload = []byte(payload)
	return &j, nil
}

// Fail обрабатывает неуспех: повтор с задержкой, перенос по retryAfter или
// окончательный провал при исчерпании попыток либо permanent.
func (r *Repository) Fail(ctx context.Context, j *Job, errMsg string, permanent bool, retryAfter time.Duration) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	// Перенос без повтора-штрафа: возвращаем в очередь, попытку откатываем.
	if retryAfter > 0 && !permanent {
		const query = `
			UPDATE dbo.jobs
			SET status = '` + StatusQueued + `', run_at = DATEADD(SECOND, @delay, SYSUTCDATETIME()),
			    attempts = attempts - 1, locked_by = NULL, locked_until = NULL, last_error = @err
			WHERE id = @id;`
		_, err := r.db.ExecContext(ctx, query,
			sql.Named("id", j.ID), sql.Named("delay", int(retryAfter.Seconds())), sql.Named("err", truncate(errMsg, 2000)))
		return wrapFail(j.ID, err)
	}

	if permanent || j.Attempts >= j.MaxAttempts {
		const query = `
			UPDATE dbo.jobs
			SET status = '` + StatusFailed + `', finished_at = SYSUTCDATETIME(),
			    locked_by = NULL, locked_until = NULL, last_error = @err
			WHERE id = @id;`
		_, err := r.db.ExecContext(ctx, query, sql.Named("id", j.ID), sql.Named("err", truncate(errMsg, 2000)))
		return wrapFail(j.ID, err)
	}

	const query = `
		UPDATE dbo.jobs
		SET status = '` + StatusQueued + `', run_at = DATEADD(SECOND, @delay, SYSUTCDATETIME()),
		    locked_by = NULL, locked_until = NULL, last_error = @err
		WHERE id = @id;`
	_, err := r.db.ExecContext(ctx, query,
		sql.Named("id", j.ID), sql.Named("delay", int(backoff(j.Attempts).Seconds())), sql.Named("err", truncate(errMsg, 2000)))
	return wrapFail(j.ID, err)
}

// ExtendLease продлевает аренду долгой задачи (heartbeat), только если она всё
// ещё числится за этим воркером.
func (r *Repository) ExtendLease(ctx context.Context, id int64, worker string, leaseFor time.Duration) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		UPDATE dbo.jobs
		SET locked_until = DATEADD(SECOND, @lease, SYSUTCDATETIME())
		WHERE id = @id AND locked_by = @worker AND status = '` + StatusRunning + `';`
	if _, err := r.db.ExecContext(ctx, query,
		sql.Named("id", id), sql.Named("worker", worker), sql.Named("lease", int(leaseFor.Seconds()))); err != nil {
		return fmt.Errorf("jobs: продление аренды задачи %d: %w", id, database.MapError(err))
	}
	return nil
}

// Cleanup удаляет старые завершённые и проваленные задачи.
func (r *Repository) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		DELETE FROM dbo.jobs
		WHERE status IN ('` + StatusDone + `', '` + StatusFailed + `')
		  AND finished_at < DATEADD(SECOND, -@age, SYSUTCDATETIME());`
	res, err := r.db.ExecContext(ctx, query, sql.Named("age", int(olderThan.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("jobs: очистка очереди: %w", database.MapError(err))
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func wrapFail(id int64, err error) error {
	if err != nil {
		return fmt.Errorf("jobs: обработка неуспеха задачи %d: %w", id, database.MapError(err))
	}
	return nil
}

func nullInt64(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: v != 0} }
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func truncate(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes])
}
