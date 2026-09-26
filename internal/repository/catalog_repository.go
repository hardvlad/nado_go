package repository

import (
	"context"
	"database/sql"
	"fmt"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// CatalogRepository — продаваемый каталог витрины (products/variants/offers).
// Пишется построением из зеркала (BuildProduct) и читается витриной.
type CatalogRepository struct {
	db *database.DB
}

func NewCatalogRepository(db *database.DB) *CatalogRepository {
	return &CatalogRepository{db: db}
}

// PriceRules возвращает правила цен магазина (для расчёта офферов).
func (r *CatalogRepository) PriceRules(ctx context.Context, storeID int64) ([]model.PriceRule, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT id, store_id, scope, ISNULL(scope_id, 0), adjust_kind, adjust_value, round_to, round_mode, is_enabled
		FROM dbo.store_price_rules WHERE store_id = @store;`
	rows, err := r.db.QueryContext(ctx, query, sql.Named("store", storeID))
	if err != nil {
		return nil, fmt.Errorf("repository: правила цен магазина %d: %w", storeID, database.MapError(err))
	}
	defer rows.Close()

	var out []model.PriceRule
	for rows.Next() {
		var pr model.PriceRule
		if err := rows.Scan(&pr.ID, &pr.StoreID, &pr.Scope, &pr.ScopeID, &pr.AdjustKind,
			&pr.AdjustValue, &pr.RoundTo, &pr.RoundMode, &pr.Enabled); err != nil {
			return nil, fmt.Errorf("repository: чтение правила цены: %w", err)
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

// EnsureCategory создаёт или находит категорию по внешнему коду и задаёт её
// перевод для языка. Возвращает id категории.
func (r *CatalogRepository) EnsureCategory(ctx context.Context, storeID, accountID int64, extCode, name, lang, slug string) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	var id int64
	err := r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		const mergeCat = `
			MERGE dbo.categories WITH (HOLDLOCK) AS t
			USING (SELECT @store AS store_id, @code AS external_code) AS s
			ON t.store_id = s.store_id AND t.external_code = s.external_code
			WHEN NOT MATCHED THEN INSERT (store_id, account_id, external_code)
				VALUES (@store, @acc, @code)
			OUTPUT INSERTED.id;`
		// MERGE с OUTPUT возвращает строку только при вставке; при совпадении
		// добираем id обычным SELECT.
		err := tx.QueryRowContext(ctx, mergeCat,
			sql.Named("store", storeID), sql.Named("acc", accountID), sql.Named("code", extCode)).Scan(&id)
		if err == sql.ErrNoRows {
			if err = tx.QueryRowContext(ctx,
				`SELECT id FROM dbo.categories WHERE store_id = @store AND external_code = @code;`,
				sql.Named("store", storeID), sql.Named("code", extCode)).Scan(&id); err != nil {
				return database.MapError(err)
			}
		} else if err != nil {
			return database.MapError(err)
		}

		const mergeTr = `
			MERGE dbo.category_translations WITH (HOLDLOCK) AS t
			USING (SELECT @cat AS category_id, @lang AS lang) AS s
			ON t.category_id = s.category_id AND t.lang = s.lang
			WHEN MATCHED THEN UPDATE SET name = @name, slug = @slug
			WHEN NOT MATCHED THEN INSERT (category_id, store_id, lang, name, slug)
				VALUES (@cat, @store, @lang, @name, @slug);`
		if _, err := tx.ExecContext(ctx, mergeTr,
			sql.Named("cat", id), sql.Named("store", storeID), sql.Named("lang", lang),
			sql.Named("name", name), sql.Named("slug", slug)); err != nil {
			return database.MapError(err)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("repository: категория %q: %w", extCode, err)
	}
	return id, nil
}

// BuildProductInput — нормализованный товар для построения витринного каталога.
type BuildProductInput struct {
	StoreID          int64
	AccountID        int64
	ConnectionID     int64
	CategoryID       int64 // 0 = без категории
	SourceSKU        string
	Brand            string
	Lang             string
	Title            string
	Description      string
	Slug             string
	SEOTitle         string
	SEODesc          string
	Images           []string
	SKU              string
	SourcePriceMinor int64
	Currency         string
	Qty              int
	Available        bool
	// Рассчитанные сервисом по правилам цены.
	OfferPriceMinor int64
	OldPriceMinor   int64
	AppliedRuleID   int64
}

// BuildProduct создаёт/обновляет товар витрины с переводом, изображениями,
// вариантом, остатками, ценой источника и оффером — одной транзакцией и
// идемпотентно (по source_sku и sku). Ручной оффер (price_mode='manual') по цене
// не перезаписывается.
func (r *CatalogRepository) BuildProduct(ctx context.Context, in BuildProductInput) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	err := r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		// Товар.
		const mergeProduct = `
			MERGE dbo.products WITH (HOLDLOCK) AS t
			USING (SELECT @store AS store_id, @sku AS source_sku) AS s
			ON t.store_id = s.store_id AND t.source_sku = s.source_sku
			WHEN MATCHED THEN UPDATE SET category_id = @cat, brand = @brand, status = 'active', updated_at = SYSUTCDATETIME()
			WHEN NOT MATCHED THEN INSERT (store_id, account_id, category_id, brand, source_sku, status)
				VALUES (@store, @acc, @cat, @brand, @sku, 'active')
			OUTPUT INSERTED.id;`
		var productID int64
		err := tx.QueryRowContext(ctx, mergeProduct,
			sql.Named("store", in.StoreID), sql.Named("acc", in.AccountID),
			sql.Named("cat", nullInt64(in.CategoryID)), sql.Named("brand", nullString(in.Brand)),
			sql.Named("sku", in.SourceSKU)).Scan(&productID)
		if err == sql.ErrNoRows {
			if err = tx.QueryRowContext(ctx,
				`SELECT id FROM dbo.products WHERE store_id = @store AND source_sku = @sku;`,
				sql.Named("store", in.StoreID), sql.Named("sku", in.SourceSKU)).Scan(&productID); err != nil {
				return database.MapError(err)
			}
		} else if err != nil {
			return database.MapError(err)
		}

		// Перевод.
		const mergeTr = `
			MERGE dbo.product_translations WITH (HOLDLOCK) AS t
			USING (SELECT @pid AS product_id, @lang AS lang) AS s
			ON t.product_id = s.product_id AND t.lang = s.lang
			WHEN MATCHED THEN UPDATE SET title = @title, description = @descr, slug = @slug, seo_title = @seot, seo_description = @seod
			WHEN NOT MATCHED THEN INSERT (product_id, store_id, lang, title, description, slug, seo_title, seo_description)
				VALUES (@pid, @store, @lang, @title, @descr, @slug, @seot, @seod);`
		if _, err := tx.ExecContext(ctx, mergeTr,
			sql.Named("pid", productID), sql.Named("store", in.StoreID), sql.Named("lang", in.Lang),
			sql.Named("title", in.Title), sql.Named("descr", nullString(in.Description)),
			sql.Named("slug", in.Slug), sql.Named("seot", nullString(in.SEOTitle)),
			sql.Named("seod", nullString(in.SEODesc))); err != nil {
			return database.MapError(err)
		}

		// Изображения: перезаписываем целиком.
		if _, err := tx.ExecContext(ctx, `DELETE FROM dbo.product_images WHERE product_id = @pid;`,
			sql.Named("pid", productID)); err != nil {
			return database.MapError(err)
		}
		for i, url := range in.Images {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO dbo.product_images (product_id, store_id, url, sort) VALUES (@pid, @store, @url, @sort);`,
				sql.Named("pid", productID), sql.Named("store", in.StoreID),
				sql.Named("url", url), sql.Named("sort", i)); err != nil {
				return database.MapError(err)
			}
		}

		// Вариант (у зеркала Kaspi один на товар).
		const mergeVariant = `
			MERGE dbo.variants WITH (HOLDLOCK) AS t
			USING (SELECT @store AS store_id, @sku AS sku) AS s
			ON t.store_id = s.store_id AND t.sku = s.sku
			WHEN MATCHED THEN UPDATE SET product_id = @pid, status = 'active'
			WHEN NOT MATCHED THEN INSERT (store_id, account_id, product_id, sku, status)
				VALUES (@store, @acc, @pid, @sku, 'active')
			OUTPUT INSERTED.id;`
		var variantID int64
		err = tx.QueryRowContext(ctx, mergeVariant,
			sql.Named("store", in.StoreID), sql.Named("acc", in.AccountID),
			sql.Named("pid", productID), sql.Named("sku", in.SKU)).Scan(&variantID)
		if err == sql.ErrNoRows {
			if err = tx.QueryRowContext(ctx,
				`SELECT id FROM dbo.variants WHERE store_id = @store AND sku = @sku;`,
				sql.Named("store", in.StoreID), sql.Named("sku", in.SKU)).Scan(&variantID); err != nil {
				return database.MapError(err)
			}
		} else if err != nil {
			return database.MapError(err)
		}

		// Остатки варианта из этого подключения: перезаписываем.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM dbo.variant_stocks WHERE variant_id = @vid AND source_key LIKE @prefix;`,
			sql.Named("vid", variantID), sql.Named("prefix", fmt.Sprintf("conn:%d:%%", in.ConnectionID))); err != nil {
			return database.MapError(err)
		}
		if in.Qty > 0 {
			sourceKey := fmt.Sprintf("conn:%d:all", in.ConnectionID)
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO dbo.variant_stocks (variant_id, source_key, store_id, qty, fulfillment) VALUES (@vid, @sk, @store, @qty, 'fbs');`,
				sql.Named("vid", variantID), sql.Named("sk", sourceKey),
				sql.Named("store", in.StoreID), sql.Named("qty", in.Qty)); err != nil {
				return database.MapError(err)
			}
		}

		// Цена источника.
		const mergePrice = `
			MERGE dbo.variant_marketplace_prices WITH (HOLDLOCK) AS t
			USING (SELECT @vid AS variant_id, @conn AS connection_id) AS s
			ON t.variant_id = s.variant_id AND t.connection_id = s.connection_id
			WHEN MATCHED THEN UPDATE SET price_minor = @price, currency = @cur, updated_at = SYSUTCDATETIME()
			WHEN NOT MATCHED THEN INSERT (variant_id, connection_id, store_id, price_minor, currency)
				VALUES (@vid, @conn, @store, @price, @cur);`
		if _, err := tx.ExecContext(ctx, mergePrice,
			sql.Named("vid", variantID), sql.Named("conn", in.ConnectionID), sql.Named("store", in.StoreID),
			sql.Named("price", in.SourcePriceMinor), sql.Named("cur", in.Currency)); err != nil {
			return database.MapError(err)
		}

		// Оффер магазина. Ручную цену не трогаем, обновляем только видимость/старую цену.
		visible := 0
		if in.Available {
			visible = 1
		}
		const mergeOffer = `
			MERGE dbo.store_offers WITH (HOLDLOCK) AS t
			USING (SELECT @store AS store_id, @vid AS variant_id) AS s
			ON t.store_id = s.store_id AND t.variant_id = s.variant_id
			WHEN MATCHED AND t.price_mode = 'rule' THEN UPDATE SET
				price_minor = @price, old_price_minor = @old, currency = @cur, is_visible = @vis,
				applied_rule_id = @rule, computed_at = SYSUTCDATETIME()
			WHEN MATCHED AND t.price_mode = 'manual' THEN UPDATE SET
				old_price_minor = @old, currency = @cur, is_visible = @vis, computed_at = SYSUTCDATETIME()
			WHEN NOT MATCHED THEN INSERT (store_id, variant_id, price_minor, old_price_minor, currency, is_visible, price_mode, applied_rule_id)
				VALUES (@store, @vid, @price, @old, @cur, @vis, 'rule', @rule);`
		if _, err := tx.ExecContext(ctx, mergeOffer,
			sql.Named("store", in.StoreID), sql.Named("vid", variantID),
			sql.Named("price", in.OfferPriceMinor), sql.Named("old", nullInt64(in.OldPriceMinor)),
			sql.Named("cur", in.Currency), sql.Named("vis", visible),
			sql.Named("rule", nullInt64(in.AppliedRuleID))); err != nil {
			return database.MapError(err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("repository: построение товара %s: %w", in.SourceSKU, err)
	}
	return nil
}
