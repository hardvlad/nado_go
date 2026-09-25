package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"nado_go/internal/database"
	"nado_go/internal/integration/messaging"
	"nado_go/internal/integration/messaging/greenapi"
	"nado_go/internal/model"
	"nado_go/internal/secrets"
)

// WhatsAppRepository — пул инстансов WhatsApp (общий для платформы, только OTP)
// и журнал вызовов API. Реализует greenapi.Pool.
type WhatsAppRepository struct {
	db  *database.DB
	box *secrets.Box
}

func NewWhatsAppRepository(db *database.DB, box *secrets.Box) *WhatsAppRepository {
	return &WhatsAppRepository{db: db, box: box}
}

// Pick выбирает активный незаблокированный инстанс, дольше всех не отправлявший
// (равномерное распределение). Токен расшифровывается здесь.
func (r *WhatsAppRepository) Pick(ctx context.Context) (*greenapi.Instance, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT TOP (1) id, api_domain, instance_id, token_ciphertext
		FROM dbo.whatsapp_instances
		WHERE disabled = 0 AND state = 'authorized'
		ORDER BY CASE WHEN last_sent_at IS NULL THEN 0 ELSE 1 END, last_sent_at, id;`

	var (
		inst   greenapi.Instance
		cipher []byte
	)
	err := r.db.QueryRowContext(ctx, query).Scan(&inst.ID, &inst.APIDomain, &inst.InstanceID, &cipher)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, messaging.ErrNoInstance
	}
	if err != nil {
		return nil, fmt.Errorf("repository: выбор инстанса: %w", database.MapError(err))
	}

	token, err := r.box.DecryptString(cipher)
	if err != nil {
		// Инстанс с нечитаемым токеном отключаем, чтобы не выбирался снова.
		_ = r.MarkFailed(ctx, inst.ID, "не удалось расшифровать токен")
		return nil, fmt.Errorf("repository: токен инстанса %d: %w", inst.ID, err)
	}
	inst.Token = token
	return &inst, nil
}

// MarkSent отмечает успешную отправку и сбрасывает счётчик ошибок.
func (r *WhatsAppRepository) MarkSent(ctx context.Context, id int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		UPDATE dbo.whatsapp_instances
		SET last_sent_at = SYSUTCDATETIME(), fail_count = 0, last_error = NULL
		WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", id)); err != nil {
		return fmt.Errorf("repository: отметка отправки инстанса %d: %w", id, database.MapError(err))
	}
	return nil
}

// MarkFailed увеличивает счётчик ошибок и после порога отключает инстанс,
// чтобы неисправный номер не выбирался снова.
func (r *WhatsAppRepository) MarkFailed(ctx context.Context, id int64, reason string) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		UPDATE dbo.whatsapp_instances
		SET fail_count = fail_count + 1,
		    last_error = @reason,
		    disabled = CASE WHEN fail_count + 1 >= 5 THEN 1 ELSE disabled END
		WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", id), sql.Named("reason", nullString(truncate(reason, 1000)))); err != nil {
		return fmt.Errorf("repository: отметка ошибки инстанса %d: %w", id, database.MapError(err))
	}
	return nil
}

// LogCall пишет запись в журнал вызовов. Ошибку журналирования не пробрасываем:
// не удалось записать лог — отправку это срывать не должно.
func (r *WhatsAppRepository) LogCall(ctx context.Context, c greenapi.Call) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		INSERT INTO dbo.whatsapp_api_calls (instance_id, method, status_code, duration_ms, request, response)
		VALUES (@instance_id, @method, @status_code, @duration_ms, @request, @response);`
	_, _ = r.db.ExecContext(ctx, query,
		sql.Named("instance_id", nullInt64(c.InstanceID)),
		sql.Named("method", c.Method),
		sql.Named("status_code", nullIntZero(c.StatusCode)),
		sql.Named("duration_ms", nullIntZero(c.DurationMS)),
		sql.Named("request", nullString(truncate(c.Request, 2000))),
		sql.Named("response", nullString(truncate(c.Response, 2000))),
	)
}

// SetState обновляет состояние инстанса (по данным вебхука или getStateInstance).
func (r *WhatsAppRepository) SetState(ctx context.Context, provider, instanceID, state, phone string) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		UPDATE dbo.whatsapp_instances
		SET state = @state, state_changed_at = SYSUTCDATETIME(),
		    phone = COALESCE(@phone, phone)
		WHERE provider = @provider AND instance_id = @instance_id;`
	if _, err := r.db.ExecContext(ctx, query,
		sql.Named("provider", provider), sql.Named("instance_id", instanceID),
		sql.Named("state", nullString(state)), sql.Named("phone", nullString(phone))); err != nil {
		return fmt.Errorf("repository: обновление состояния инстанса: %w", database.MapError(err))
	}
	return nil
}

// AddInstance добавляет инстанс в пул: шифрует токен, ставит state='notAuthorized'
// (авторизация — сканированием QR). Возвращает id новой строки.
func (r *WhatsAppRepository) AddInstance(ctx context.Context, provider, name, apiDomain, instanceID, token string) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	cipher, err := r.box.EncryptString(token)
	if err != nil {
		return 0, fmt.Errorf("repository: шифрование токена инстанса: %w", err)
	}

	const query = `
		INSERT INTO dbo.whatsapp_instances (provider, name, api_domain, instance_id, token_ciphertext, state)
		OUTPUT INSERTED.id
		VALUES (@provider, @name, @api_domain, @instance_id, @token, 'notAuthorized');`

	var id int64
	err = r.db.QueryRowContext(ctx, query,
		sql.Named("provider", provider),
		sql.Named("name", nullString(name)),
		sql.Named("api_domain", apiDomain),
		sql.Named("instance_id", instanceID),
		sql.Named("token", cipher),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("repository: добавление инстанса: %w", database.MapError(err))
	}
	return id, nil
}

// GetInstance возвращает инстанс с расшифрованным токеном по id пула.
func (r *WhatsAppRepository) GetInstance(ctx context.Context, id int64) (*greenapi.Instance, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `SELECT id, api_domain, instance_id, token_ciphertext FROM dbo.whatsapp_instances WHERE id = @id;`
	var (
		inst   greenapi.Instance
		cipher []byte
	)
	err := r.db.QueryRowContext(ctx, query, sql.Named("id", id)).
		Scan(&inst.ID, &inst.APIDomain, &inst.InstanceID, &cipher)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: чтение инстанса %d: %w", id, database.MapError(err))
	}
	token, err := r.box.DecryptString(cipher)
	if err != nil {
		return nil, fmt.Errorf("repository: токен инстанса %d: %w", id, err)
	}
	inst.Token = token
	return &inst, nil
}

// SetStateByID обновляет состояние инстанса по id пула (после проверки State).
func (r *WhatsAppRepository) SetStateByID(ctx context.Context, id int64, state, phone string) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		UPDATE dbo.whatsapp_instances
		SET state = @state, state_changed_at = SYSUTCDATETIME(), phone = COALESCE(@phone, phone)
		WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query,
		sql.Named("id", id), sql.Named("state", nullString(state)), sql.Named("phone", nullString(phone))); err != nil {
		return fmt.Errorf("repository: состояние инстанса %d: %w", id, database.MapError(err))
	}
	return nil
}

// ListInstances возвращает все инстансы пула (без токенов) для админ-команды.
func (r *WhatsAppRepository) ListInstances(ctx context.Context) ([]model.WhatsAppInstance, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT id, ISNULL(name, ''), provider, instance_id, ISNULL(phone, ''), ISNULL(state, ''), disabled, last_sent_at
		FROM dbo.whatsapp_instances ORDER BY id;`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("repository: список инстансов: %w", database.MapError(err))
	}
	defer rows.Close()

	var out []model.WhatsAppInstance
	for rows.Next() {
		var (
			i        model.WhatsAppInstance
			lastSent sql.NullTime
		)
		if err := rows.Scan(&i.ID, &i.Name, &i.Provider, &i.InstanceID, &i.Phone, &i.State, &i.Disabled, &lastSent); err != nil {
			return nil, fmt.Errorf("repository: чтение инстанса: %w", err)
		}
		i.LastSentAt = lastSent.Time
		out = append(out, i)
	}
	return out, rows.Err()
}

func nullIntZero(v int) sql.NullInt64 { return sql.NullInt64{Int64: int64(v), Valid: v != 0} }
