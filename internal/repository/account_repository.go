package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// AccountRepository — аккаунты продавцов и их участники.
type AccountRepository struct {
	db *database.DB
}

func NewAccountRepository(db *database.DB) *AccountRepository {
	return &AccountRepository{db: db}
}

// CreateWithOwner атомарно создаёт пользователя, аккаунт и членство владельца.
// Занятый email возвращается как database.ErrConflict (уникальный индекс
// UX_users_email): проверка в сервисе не спасает от гонки двух регистраций.
func (r *AccountRepository) CreateWithOwner(ctx context.Context, acc *model.Account, owner *model.NewUser) (*model.Account, *model.User, error) {
	var (
		createdAcc  model.Account
		createdUser *model.User
	)

	err := r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		const insertUser = `
			INSERT INTO dbo.users (email, name, status, password_hash, phone, locale)
			OUTPUT INSERTED.id, INSERTED.email, INSERTED.name,
			       INSERTED.status, INSERTED.created_at, INSERTED.updated_at
			VALUES (@email, @name, @status, @password_hash, @phone, @locale);`

		var err error
		createdUser, err = scanUser(tx.QueryRowContext(ctx, insertUser,
			sql.Named("email", strings.ToLower(owner.Email)),
			sql.Named("name", owner.Name),
			sql.Named("status", model.UserStatusActive),
			sql.Named("password_hash", owner.PasswordHash),
			sql.Named("phone", nullString(owner.Phone)),
			sql.Named("locale", nullString(owner.Locale)),
		))
		if err != nil {
			return database.MapError(err)
		}

		const insertAccount = `
			INSERT INTO dbo.accounts (name, country, plan_code, status)
			OUTPUT INSERTED.id, INSERTED.name, INSERTED.country,
			       INSERTED.plan_code, INSERTED.status, INSERTED.created_at
			VALUES (@name, @country, @plan_code, @status);`

		if err := tx.QueryRowContext(ctx, insertAccount,
			sql.Named("name", acc.Name),
			sql.Named("country", acc.Country),
			sql.Named("plan_code", acc.PlanCode),
			sql.Named("status", acc.Status),
		).Scan(&createdAcc.ID, &createdAcc.Name, &createdAcc.Country,
			&createdAcc.PlanCode, &createdAcc.Status, &createdAcc.CreatedAt); err != nil {
			return database.MapError(err)
		}

		const insertMember = `
			INSERT INTO dbo.account_members (account_id, user_id, role)
			VALUES (@account_id, @user_id, @role);`

		if _, err := tx.ExecContext(ctx, insertMember,
			sql.Named("account_id", createdAcc.ID),
			sql.Named("user_id", createdUser.ID),
			sql.Named("role", model.RoleOwner),
		); err != nil {
			return database.MapError(err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("repository: создание аккаунта с владельцем: %w", err)
	}
	return &createdAcc, createdUser, nil
}

// nullString превращает пустую строку в NULL: «не указано» и «пустое значение»
// в БД должны различаться одинаково для всех необязательных полей.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
