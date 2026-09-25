package repository

import (
	"context"
	"database/sql"
	"fmt"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// MarketplaceProductRepository — зеркало каталога маркетплейса (импорт из Kaspi).
type MarketplaceProductRepository struct {
	db *database.DB
}

func NewMarketplaceProductRepository(db *database.DB) *MarketplaceProductRepository {
	return &MarketplaceProductRepository{db: db}
}

// Upsert вставляет или обновляет товар и его остатки одной транзакцией.
// Возвращает true, если товар был создан.
func (r *MarketplaceProductRepository) Upsert(ctx context.Context, p *model.MarketplaceProduct) (created bool, err error) {
	err = r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		const mergeProduct = `
			MERGE dbo.marketplace_products WITH (HOLDLOCK) AS t
			USING (SELECT @connection_id AS connection_id, @sku AS sku) AS s
			ON t.connection_id = s.connection_id AND t.sku = s.sku
			WHEN MATCHED THEN UPDATE SET
				master_sku = @master_sku, title = @title, brand = @brand, category_ext = @category_ext,
				price_minor = @price_minor, currency = @currency, available = @available, images = @images,
				content_hash = @hash, raw_json = @raw, removed_at = NULL, synced_at = SYSUTCDATETIME()
			WHEN NOT MATCHED THEN INSERT
				(connection_id, store_id, account_id, sku, master_sku, title, brand, category_ext,
				 price_minor, currency, available, images, content_hash, raw_json)
				VALUES (@connection_id, @store_id, @account_id, @sku, @master_sku, @title, @brand, @category_ext,
				 @price_minor, @currency, @available, @images, @hash, @raw)
			OUTPUT INSERTED.id, $action;`

		var (
			productID int64
			action    string
		)
		err := tx.QueryRowContext(ctx, mergeProduct,
			sql.Named("connection_id", p.ConnectionID),
			sql.Named("store_id", p.StoreID),
			sql.Named("account_id", p.AccountID),
			sql.Named("sku", p.SKU),
			sql.Named("master_sku", nullString(p.MasterSKU)),
			sql.Named("title", nullString(p.Title)),
			sql.Named("brand", nullString(p.Brand)),
			sql.Named("category_ext", nullString(p.CategoryExt)),
			sql.Named("price_minor", p.PriceMinor),
			sql.Named("currency", p.Currency),
			sql.Named("available", p.Available),
			sql.Named("images", nullString(p.ImagesJSON)),
			sql.Named("hash", p.ContentHash),
			sql.Named("raw", nullString(string(p.Raw))),
		).Scan(&productID, &action)
		if err != nil {
			return database.MapError(err)
		}
		created = action == "INSERT"

		// Остатки перезаписываем целиком: список точек мог измениться.
		if _, err := tx.ExecContext(ctx, `DELETE FROM dbo.marketplace_product_stocks WHERE product_id = @id;`,
			sql.Named("id", productID)); err != nil {
			return database.MapError(err)
		}
		for _, st := range p.Stocks {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO dbo.marketplace_product_stocks (product_id, store_code, qty, specified, preorder)
				VALUES (@id, @code, @qty, @specified, @preorder);`,
				sql.Named("id", productID),
				sql.Named("code", st.StoreCode),
				sql.Named("qty", st.Qty),
				sql.Named("specified", st.Specified),
				sql.Named("preorder", st.PreOrder),
			); err != nil {
				return database.MapError(err)
			}
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("repository: сохранение товара %s: %w", p.SKU, err)
	}
	return created, nil
}
