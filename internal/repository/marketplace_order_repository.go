package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// MarketplaceOrderRepository — зеркало заказов маркетплейса (только чтение с
// маркетплейса, запись к нам).
type MarketplaceOrderRepository struct {
	db *database.DB
}

func NewMarketplaceOrderRepository(db *database.DB) *MarketplaceOrderRepository {
	return &MarketplaceOrderRepository{db: db}
}

// Upsert вставляет или обновляет заказ по (connection_id, external_id).
// Возвращает true, если заказ был создан (новый).
func (r *MarketplaceOrderRepository) Upsert(ctx context.Context, o *model.MarketplaceOrder) (created bool, err error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	// MERGE с HOLDLOCK: без него параллельные импорты дают дубли или нарушение
	// уникального ключа.
	const query = `
		MERGE dbo.marketplace_orders WITH (HOLDLOCK) AS t
		USING (SELECT @connection_id AS connection_id, @external_id AS external_id) AS s
		ON t.connection_id = s.connection_id AND t.external_id = s.external_id
		WHEN MATCHED THEN UPDATE SET
			code = @code, state = @state, status = @status, total_minor = @total_minor,
			currency = @currency, customer_name = @customer_name, customer_phone = @customer_phone,
			ordered_at = @ordered_at, raw_json = @raw, imported_at = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN INSERT
			(connection_id, store_id, account_id, external_id, code, state, status, total_minor,
			 currency, customer_name, customer_phone, ordered_at, raw_json)
			VALUES (@connection_id, @store_id, @account_id, @external_id, @code, @state, @status, @total_minor,
			 @currency, @customer_name, @customer_phone, @ordered_at, @raw)
		OUTPUT $action;`

	var action string
	err = r.db.QueryRowContext(ctx, query,
		sql.Named("connection_id", o.ConnectionID),
		sql.Named("store_id", o.StoreID),
		sql.Named("account_id", o.AccountID),
		sql.Named("external_id", o.ExternalID),
		sql.Named("code", nullString(o.Code)),
		sql.Named("state", nullString(o.State)),
		sql.Named("status", nullString(o.Status)),
		sql.Named("total_minor", o.TotalMinor),
		sql.Named("currency", o.Currency),
		sql.Named("customer_name", nullString(o.CustomerName)),
		sql.Named("customer_phone", nullString(o.CustomerPhone)),
		sql.Named("ordered_at", nullTime(o.OrderedAt)),
		sql.Named("raw", nullString(string(o.Raw))),
	).Scan(&action)
	if err != nil {
		return false, fmt.Errorf("repository: сохранение заказа %s: %w", o.ExternalID, database.MapError(err))
	}
	return action == "INSERT", nil
}

// OrderQuery — параметры выборки заказов для кабинета.
type OrderQuery struct {
	AccountID int64
	StoreID   int64  // 0 — все магазины аккаунта
	Search    string // по коду заказа, имени и телефону покупателя
	Limit     int
	Offset    int
}

// List возвращает страницу заказов аккаунта и общее число подходящих.
// Изоляция арендатора: всегда фильтр по account_id.
func (r *MarketplaceOrderRepository) List(ctx context.Context, q OrderQuery) ([]model.MarketplaceOrder, int, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT o.id, o.store_id, ISNULL(s.name, ''), o.external_id, ISNULL(o.code, ''),
		       ISNULL(o.state, ''), ISNULL(o.status, ''), ISNULL(o.total_minor, 0), o.currency,
		       ISNULL(o.customer_name, ''), ISNULL(o.customer_phone, ''), o.ordered_at, o.imported_at,
		       COUNT(*) OVER() AS total
		FROM dbo.marketplace_orders o
		JOIN dbo.stores s ON s.id = o.store_id
		WHERE o.account_id = @acc
		  AND (@store = 0 OR o.store_id = @store)
		  AND (@q = '' OR o.code LIKE @like OR o.customer_name LIKE @like OR o.customer_phone LIKE @like)
		ORDER BY o.ordered_at DESC, o.id DESC
		OFFSET @off ROWS FETCH NEXT @lim ROWS ONLY;`

	rows, err := r.db.QueryContext(ctx, query,
		sql.Named("acc", q.AccountID), sql.Named("store", q.StoreID),
		sql.Named("q", q.Search), sql.Named("like", "%"+escapeLike(q.Search)+"%"),
		sql.Named("off", q.Offset), sql.Named("lim", q.Limit))
	if err != nil {
		return nil, 0, fmt.Errorf("repository: список заказов: %w", database.MapError(err))
	}
	defer rows.Close()

	var (
		out   []model.MarketplaceOrder
		total int
	)
	for rows.Next() {
		var (
			o        model.MarketplaceOrder
			ordered  sql.NullTime
			imported sql.NullTime
		)
		o.AccountID = q.AccountID
		if err := rows.Scan(&o.ID, &o.StoreID, &o.StoreName, &o.ExternalID, &o.Code,
			&o.State, &o.Status, &o.TotalMinor, &o.Currency, &o.CustomerName, &o.CustomerPhone,
			&ordered, &imported, &total); err != nil {
			return nil, 0, fmt.Errorf("repository: чтение заказа: %w", err)
		}
		o.OrderedAt = ordered.Time
		o.ImportedAt = imported.Time
		out = append(out, o)
	}
	return out, total, rows.Err()
}

func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}
