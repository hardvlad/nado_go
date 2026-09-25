// Package repository — доступ к данным. Здесь и только здесь пишется SQL.
//
// Все параметры передаются через sql.Named: строка запроса статична, значения
// уходят отдельно, поэтому SQL-инъекция невозможна в принципе. Конкатенация
// пользовательского ввода в текст запроса недопустима.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// UserRepository — хранилище пользователей в SQL Server.
type UserRepository struct {
	db *database.DB
}

func NewUserRepository(db *database.DB) *UserRepository {
	return &UserRepository{db: db}
}

// userColumns перечисляются явно: SELECT * ломается при изменении схемы.
const userColumns = `id, email, name, status, created_at, updated_at`

// GetByID возвращает пользователя или database.ErrNotFound.
func (r *UserRepository) GetByID(ctx context.Context, id int64) (*model.User, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT ` + userColumns + `
		FROM dbo.users
		WHERE id = @id;`

	row := r.db.QueryRowContext(ctx, query, sql.Named("id", id))

	user, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("repository: получение пользователя %d: %w", id, database.MapError(err))
	}
	return user, nil
}

// GetByEmail используется при регистрации и входе.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT ` + userColumns + `
		FROM dbo.users
		WHERE email = @email;`

	user, err := scanUser(r.db.QueryRowContext(ctx, query, sql.Named("email", strings.ToLower(email))))
	if err != nil {
		return nil, fmt.Errorf("repository: получение пользователя по email: %w", database.MapError(err))
	}
	return user, nil
}

// List возвращает страницу пользователей и общее количество по фильтру.
// Пагинация — OFFSET/FETCH; ORDER BY обязателен, иначе SQL Server отклонит запрос.
func (r *UserRepository) List(ctx context.Context, f model.UserFilter) ([]model.User, int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	// COUNT(*) OVER() отдаёт общее число строк тем же запросом — без второго
	// обращения к базе и без рассинхронизации между списком и счётчиком.
	const query = `
		SELECT ` + userColumns + `, COUNT(*) OVER() AS total
		FROM dbo.users
		WHERE (@search = '' OR name LIKE '%' + @search + '%' OR email LIKE '%' + @search + '%')
		  AND (@status = '' OR status = @status)
		ORDER BY created_at DESC, id DESC
		OFFSET @offset ROWS FETCH NEXT @limit ROWS ONLY;`

	rows, err := r.db.QueryContext(ctx, query,
		sql.Named("search", f.Search),
		sql.Named("status", f.Status),
		sql.Named("offset", f.Offset),
		sql.Named("limit", f.Limit),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("repository: выборка пользователей: %w", database.MapError(err))
	}
	defer rows.Close()

	users := make([]model.User, 0, f.Limit)
	var total int64

	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Status, &u.CreatedAt, &u.UpdatedAt, &total); err != nil {
			return nil, 0, fmt.Errorf("repository: чтение строки пользователя: %w", err)
		}
		users = append(users, u)
	}
	// Ошибку итерации проверяем обязательно: разрыв соединения посреди
	// выборки иначе выглядит как пустой результат.
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("repository: обход результата: %w", database.MapError(err))
	}

	return users, total, nil
}

// Create вставляет пользователя и возвращает его вместе со сгенерированными полями.
// OUTPUT INSERTED отдаёт их в том же запросе — без повторного SELECT.
func (r *UserRepository) Create(ctx context.Context, u *model.User) (*model.User, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		INSERT INTO dbo.users (email, name, status)
		OUTPUT INSERTED.id, INSERTED.email, INSERTED.name,
		       INSERTED.status, INSERTED.created_at, INSERTED.updated_at
		VALUES (@email, @name, @status);`

	created, err := scanUser(r.db.QueryRowContext(ctx, query,
		sql.Named("email", strings.ToLower(u.Email)),
		sql.Named("name", u.Name),
		sql.Named("status", u.Status),
	))
	if err != nil {
		return nil, fmt.Errorf("repository: создание пользователя: %w", database.MapError(err))
	}
	return created, nil
}

// Update изменяет изменяемые поля пользователя.
func (r *UserRepository) Update(ctx context.Context, u *model.User) (*model.User, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		UPDATE dbo.users
		SET name = @name,
		    status = @status,
		    updated_at = SYSUTCDATETIME()
		OUTPUT INSERTED.id, INSERTED.email, INSERTED.name,
		       INSERTED.status, INSERTED.created_at, INSERTED.updated_at
		WHERE id = @id;`

	updated, err := scanUser(r.db.QueryRowContext(ctx, query,
		sql.Named("id", u.ID),
		sql.Named("name", u.Name),
		sql.Named("status", u.Status),
	))
	if err != nil {
		return nil, fmt.Errorf("repository: обновление пользователя %d: %w", u.ID, database.MapError(err))
	}
	return updated, nil
}

// Delete удаляет пользователя; отсутствие строки — database.ErrNotFound.
func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `DELETE FROM dbo.users WHERE id = @id;`

	res, err := r.db.ExecContext(ctx, query, sql.Named("id", id))
	if err != nil {
		return fmt.Errorf("repository: удаление пользователя %d: %w", id, database.MapError(err))
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: результат удаления: %w", err)
	}
	if affected == 0 {
		return database.ErrNotFound
	}
	return nil
}

// CreateWithAudit — пример операции, атомарно меняющей несколько таблиц.
// Обе вставки видны только после коммита; при любой ошибке транзакция
// откатывается целиком.
func (r *UserRepository) CreateWithAudit(ctx context.Context, u *model.User, actor string) (*model.User, error) {
	var created *model.User

	err := r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		const insertUser = `
			INSERT INTO dbo.users (email, name, status)
			OUTPUT INSERTED.id, INSERTED.email, INSERTED.name,
			       INSERTED.status, INSERTED.created_at, INSERTED.updated_at
			VALUES (@email, @name, @status);`

		row := tx.QueryRowContext(ctx, insertUser,
			sql.Named("email", strings.ToLower(u.Email)),
			sql.Named("name", u.Name),
			sql.Named("status", u.Status),
		)

		var err error
		if created, err = scanUser(row); err != nil {
			return database.MapError(err)
		}

		const insertAudit = `
			INSERT INTO dbo.audit_log (entity, entity_id, action, actor)
			VALUES ('user', @entity_id, 'create', @actor);`

		if _, err := tx.ExecContext(ctx, insertAudit,
			sql.Named("entity_id", created.ID),
			sql.Named("actor", actor),
		); err != nil {
			return database.MapError(err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("repository: создание пользователя с аудитом: %w", err)
	}
	return created, nil
}

// rowScanner объединяет *sql.Row и *sql.Rows — чтобы не дублировать разбор.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(s rowScanner) (*model.User, error) {
	var u model.User
	// email теперь может быть NULL (регистрация по телефону).
	var email sql.NullString
	if err := s.Scan(&u.ID, &email, &u.Name, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	u.Email = email.String
	return &u, nil
}
