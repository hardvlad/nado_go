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

// StoreRepository — магазины, учётные данные провайдеров и подключения к
// маркетплейсам (D-23: магазин ↔ подключение один-к-одному).
type StoreRepository struct {
	db *database.DB
}

func NewStoreRepository(db *database.DB) *StoreRepository {
	return &StoreRepository{db: db}
}

// NewConnection — данные для создания магазина с подключением к маркетплейсу.
type NewConnection struct {
	AccountID    int64
	Name         string // имя магазина
	Slug         string
	Country      string
	BaseCurrency string
	PlanCode     string
	Marketplace  string
	// Секрет провайдера уже зашифрован сервисом.
	SecretCiphertext []byte
	PublicMeta       string // JSON, что можно показать (merchantId, имя кабинета)
	ConnectionName   string
}

// CreateStoreWithConnection создаёт магазин, учётные данные и подключение одной
// транзакцией. Занятый slug возвращается как database.ErrConflict.
func (r *StoreRepository) CreateStoreWithConnection(ctx context.Context, in NewConnection) (storeID, connectionID int64, err error) {
	err = r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		const insertStore = `
			INSERT INTO dbo.stores (account_id, slug, name, country, base_currency, plan_code)
			OUTPUT INSERTED.id
			VALUES (@account_id, @slug, @name, @country, @base_currency, @plan_code);`
		if err := tx.QueryRowContext(ctx, insertStore,
			sql.Named("account_id", in.AccountID),
			sql.Named("slug", in.Slug),
			sql.Named("name", in.Name),
			sql.Named("country", in.Country),
			sql.Named("base_currency", in.BaseCurrency),
			sql.Named("plan_code", nullString(in.PlanCode)),
		).Scan(&storeID); err != nil {
			return database.MapError(err)
		}

		const insertCred = `
			INSERT INTO dbo.provider_credentials (account_id, store_id, kind, provider_code, secret_ciphertext, public_meta, last_verified_at)
			OUTPUT INSERTED.id
			VALUES (@account_id, @store_id, 'marketplace', @provider, @secret, @meta, SYSUTCDATETIME());`
		var credID int64
		if err := tx.QueryRowContext(ctx, insertCred,
			sql.Named("account_id", in.AccountID),
			sql.Named("store_id", storeID),
			sql.Named("provider", in.Marketplace),
			sql.Named("secret", in.SecretCiphertext),
			sql.Named("meta", nullString(in.PublicMeta)),
		).Scan(&credID); err != nil {
			return database.MapError(err)
		}

		const insertConn = `
			INSERT INTO dbo.marketplace_connections (store_id, account_id, marketplace, credentials_id, name)
			OUTPUT INSERTED.id
			VALUES (@store_id, @account_id, @marketplace, @credentials_id, @name);`
		if err := tx.QueryRowContext(ctx, insertConn,
			sql.Named("store_id", storeID),
			sql.Named("account_id", in.AccountID),
			sql.Named("marketplace", in.Marketplace),
			sql.Named("credentials_id", credID),
			sql.Named("name", nullString(in.ConnectionName)),
		).Scan(&connectionID); err != nil {
			return database.MapError(err)
		}
		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("repository: создание магазина с подключением: %w", err)
	}
	return storeID, connectionID, nil
}

// KaspiConnection — подключение с зашифрованным токеном для фоновой задачи.
type KaspiConnection struct {
	ConnectionID     int64
	StoreID          int64
	AccountID        int64
	SecretCiphertext []byte
	OrdersSyncAt     time.Time
}

// GetConnectionForSync возвращает подключение и его секрет для синхронизации.
func (r *StoreRepository) GetConnectionForSync(ctx context.Context, connectionID int64) (*KaspiConnection, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT c.id, c.store_id, c.account_id, pc.secret_ciphertext, c.orders_sync_at
		FROM dbo.marketplace_connections c
		JOIN dbo.provider_credentials pc ON pc.id = c.credentials_id
		WHERE c.id = @id AND c.status = 'active';`

	var (
		conn   KaspiConnection
		syncAt sql.NullTime
	)
	err := r.db.QueryRowContext(ctx, query, sql.Named("id", connectionID)).
		Scan(&conn.ConnectionID, &conn.StoreID, &conn.AccountID, &conn.SecretCiphertext, &syncAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: подключение %d: %w", connectionID, database.MapError(err))
	}
	conn.OrdersSyncAt = syncAt.Time
	return &conn, nil
}

// GetConnectionScope возвращает магазин и аккаунт активного подключения —
// для приёма импортированного каталога. Нет/неактивно — database.ErrNotFound.
func (r *StoreRepository) GetConnectionScope(ctx context.Context, connectionID int64) (storeID, accountID int64, err error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `SELECT store_id, account_id FROM dbo.marketplace_connections WHERE id = @id AND status = 'active';`
	err = r.db.QueryRowContext(ctx, query, sql.Named("id", connectionID)).Scan(&storeID, &accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, database.ErrNotFound
	}
	if err != nil {
		return 0, 0, fmt.Errorf("repository: область подключения %d: %w", connectionID, database.MapError(err))
	}
	return storeID, accountID, nil
}

// TouchContentSync запоминает время последней синхронизации каталога.
func (r *StoreRepository) TouchContentSync(ctx context.Context, connectionID int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `UPDATE dbo.marketplace_connections SET content_sync_at = SYSUTCDATETIME() WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", connectionID)); err != nil {
		return fmt.Errorf("repository: отметка синхронизации каталога %d: %w", connectionID, database.MapError(err))
	}
	return nil
}

// TouchOrdersSync запоминает время последней синхронизации заказов.
func (r *StoreRepository) TouchOrdersSync(ctx context.Context, connectionID int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `UPDATE dbo.marketplace_connections SET orders_sync_at = SYSUTCDATETIME() WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", connectionID)); err != nil {
		return fmt.Errorf("repository: отметка синхронизации %d: %w", connectionID, database.MapError(err))
	}
	return nil
}

// MarkConnectionInvalid переводит подключение в invalid (токен отозван).
func (r *StoreRepository) MarkConnectionInvalid(ctx context.Context, connectionID int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `UPDATE dbo.marketplace_connections SET status = 'invalid' WHERE id = @id;`
	if _, err := r.db.ExecContext(ctx, query, sql.Named("id", connectionID)); err != nil {
		return fmt.Errorf("repository: пометка подключения %d invalid: %w", connectionID, database.MapError(err))
	}
	return nil
}

// ListStores возвращает магазины аккаунта с их подключением (для кабинета).
func (r *StoreRepository) ListStores(ctx context.Context, accountID int64) ([]model.Store, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT s.id, s.name, s.slug, s.status, s.base_currency,
		       ISNULL(c.marketplace, ''), ISNULL(c.status, ''), c.orders_sync_at
		FROM dbo.stores s
		LEFT JOIN dbo.marketplace_connections c ON c.store_id = s.id
		WHERE s.account_id = @account_id AND s.status <> 'archived'
		ORDER BY s.id DESC;`
	rows, err := r.db.QueryContext(ctx, query, sql.Named("account_id", accountID))
	if err != nil {
		return nil, fmt.Errorf("repository: список магазинов: %w", database.MapError(err))
	}
	defer rows.Close()

	var out []model.Store
	for rows.Next() {
		var (
			s      model.Store
			syncAt sql.NullTime
		)
		if err := rows.Scan(&s.ID, &s.Name, &s.Slug, &s.Status, &s.BaseCurrency,
			&s.Marketplace, &s.ConnectionStatus, &syncAt); err != nil {
			return nil, fmt.Errorf("repository: чтение магазина: %w", err)
		}
		s.OrdersSyncAt = syncAt.Time
		out = append(out, s)
	}
	return out, rows.Err()
}
