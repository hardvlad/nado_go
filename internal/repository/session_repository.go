package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// SessionRepository — серверные сессии кабинета и данные для входа.
//
// Сессии ищутся по хешу токена, а не по аккаунту: на этом шаге аккаунт ещё
// неизвестен. Это единственное место, где запрос не фильтруется по account_id.
type SessionRepository struct {
	db *database.DB
}

func NewSessionRepository(db *database.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// GetCredentialsByEmail возвращает данные для проверки пароля и основной
// аккаунт пользователя (сначала тот, где он владелец).
func (r *SessionRepository) GetCredentialsByEmail(ctx context.Context, email string) (*model.UserCredentials, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT TOP (1) u.id, u.name, u.email, u.status,
		       ISNULL(u.password_hash, ''), ISNULL(m.account_id, 0)
		FROM dbo.users u
		LEFT JOIN dbo.account_members m ON m.user_id = u.id
		WHERE u.email = @email
		ORDER BY CASE m.role WHEN 'owner' THEN 0 ELSE 1 END, m.account_id;`

	var c model.UserCredentials
	err := r.db.QueryRowContext(ctx, query, sql.Named("email", strings.ToLower(email))).
		Scan(&c.UserID, &c.Name, &c.Email, &c.Status, &c.PasswordHash, &c.AccountID)
	if err != nil {
		return nil, fmt.Errorf("repository: учётные данные по email: %w", database.MapError(err))
	}
	return &c, nil
}

// GetCredentialsByPhone возвращает данные пользователя с подтверждённым
// телефоном — для входа по коду. Только подтверждённый номер (phone_verified_at)
// однозначно определяет пользователя.
func (r *SessionRepository) GetCredentialsByPhone(ctx context.Context, phone string) (*model.UserCredentials, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT TOP (1) u.id, u.name, ISNULL(u.email, ''), u.status,
		       ISNULL(u.password_hash, ''), ISNULL(m.account_id, 0)
		FROM dbo.users u
		LEFT JOIN dbo.account_members m ON m.user_id = u.id
		WHERE u.phone = @phone AND u.phone_verified_at IS NOT NULL
		ORDER BY CASE m.role WHEN 'owner' THEN 0 ELSE 1 END, m.account_id;`

	var c model.UserCredentials
	err := r.db.QueryRowContext(ctx, query, sql.Named("phone", phone)).
		Scan(&c.UserID, &c.Name, &c.Email, &c.Status, &c.PasswordHash, &c.AccountID)
	if err != nil {
		return nil, fmt.Errorf("repository: учётные данные по телефону: %w", database.MapError(err))
	}
	return &c, nil
}

// TouchLogin запоминает время последнего входа.
func (r *SessionRepository) TouchLogin(ctx context.Context, userID int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `UPDATE dbo.users SET last_login_at = SYSUTCDATETIME() WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", userID)); err != nil {
		return fmt.Errorf("repository: отметка входа: %w", database.MapError(err))
	}
	return nil
}

// Create сохраняет новую сессию.
func (r *SessionRepository) Create(ctx context.Context, s *model.Session) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		INSERT INTO dbo.sessions (id, user_id, account_id, expires_at, ip, user_agent)
		VALUES (@id, @user_id, @account_id, @expires_at, @ip, @user_agent);`

	if _, err := r.db.ExecContext(ctx, query,
		sql.Named("id", s.IDHash),
		sql.Named("user_id", s.UserID),
		sql.Named("account_id", s.AccountID),
		sql.Named("expires_at", s.ExpiresAt.UTC()),
		sql.Named("ip", nullString(s.IP)),
		sql.Named("user_agent", nullString(truncate(s.UserAgent, 400))),
	); err != nil {
		return fmt.Errorf("repository: создание сессии: %w", database.MapError(err))
	}
	return nil
}

// GetPrincipal возвращает пользователя и аккаунт действующей сессии.
// Сессия заблокированного пользователя или приостановленного аккаунта
// считается недействительной сразу, без ожидания истечения.
func (r *SessionRepository) GetPrincipal(ctx context.Context, idHash []byte) (*model.Principal, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT u.id, u.name, u.email, a.id, a.name, a.plan_code, m.role
		FROM dbo.sessions s
		JOIN dbo.users u ON u.id = s.user_id
		JOIN dbo.accounts a ON a.id = s.account_id
		JOIN dbo.account_members m ON m.account_id = s.account_id AND m.user_id = s.user_id
		WHERE s.id = @id
		  AND s.expires_at > SYSUTCDATETIME()
		  AND u.status = 'active'
		  AND a.status = 'active';`

	var p model.Principal
	err := r.db.QueryRowContext(ctx, query, sql.Named("id", idHash)).
		Scan(&p.UserID, &p.UserName, &p.Email, &p.AccountID, &p.AccountName, &p.PlanCode, &p.Role)
	if err != nil {
		return nil, fmt.Errorf("repository: сессия: %w", database.MapError(err))
	}
	return &p, nil
}

// Delete удаляет сессию (выход). Отсутствие строки — не ошибка: повторный
// выход должен проходить молча.
func (r *SessionRepository) Delete(ctx context.Context, idHash []byte) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `DELETE FROM dbo.sessions WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", idHash)); err != nil {
		return fmt.Errorf("repository: удаление сессии: %w", database.MapError(err))
	}
	return nil
}

// truncate обрезает строку по числу символов (не байт), чтобы не разрезать
// многобайтовую букву и не упереться в размер колонки.
func truncate(s string, maxRunes int) string {
	if len([]rune(s)) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes])
}
