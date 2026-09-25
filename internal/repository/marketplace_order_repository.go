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

func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}
