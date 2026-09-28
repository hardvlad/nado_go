package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"nado_go/internal/database"
)

// Редактирование каталога из кабинета продавца: категории и товары. В отличие от
// построения из зеркала (BuildProduct), эти методы отражают ручные правки и
// работают со всеми языками витрины. Изоляция арендатора: каждый метод фильтрует
// по store_id И account_id (products/categories/variants денормализуют account_id).

// --- Категории ---

// CabinetCategory — строка списка категорий в кабинете.
type CabinetCategory struct {
	ID           int64
	ParentID     int64
	ParentName   string
	Name         string
	Sort         int
	ProductCount int
	FromKaspi    bool // есть external_code — категория пришла из Kaspi
}

// CategoryTr — перевод категории для одного языка.
type CategoryTr struct {
	Name string
	Slug string
}

// CategoryEdit — данные категории для формы редактирования.
type CategoryEdit struct {
	ID        int64
	ParentID  int64
	Sort      int
	FromKaspi bool
	Trs       map[string]CategoryTr // язык → перевод
}

// CategoryInput — создание/обновление категории (ID=0 — создание).
type CategoryInput struct {
	StoreID   int64
	AccountID int64
	ID        int64
	ParentID  int64
	Sort      int
	Trs       map[string]CategoryTr
}

// CabinetCategories возвращает все категории магазина с именем на языке по
// умолчанию, именем родителя и числом товаров (любого статуса).
func (r *CatalogRepository) CabinetCategories(ctx context.Context, storeID, accountID int64, defLang string) ([]CabinetCategory, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT c.id, ISNULL(c.parent_id, 0),
		       COALESCE(tl.name, td.name, ''),
		       ISNULL(pt.name, ''),
		       c.sort,
		       (SELECT COUNT(*) FROM dbo.products p WHERE p.category_id = c.id) AS cnt,
		       CASE WHEN c.external_code IS NULL THEN 0 ELSE 1 END
		FROM dbo.categories c
		LEFT JOIN dbo.category_translations tl ON tl.category_id = c.id AND tl.lang = @def
		OUTER APPLY (SELECT TOP 1 name FROM dbo.category_translations t2 WHERE t2.category_id = c.id ORDER BY t2.lang) td
		OUTER APPLY (SELECT TOP 1 name FROM dbo.category_translations pt2 WHERE pt2.category_id = c.parent_id AND pt2.lang = @def) pt
		WHERE c.store_id = @store AND c.account_id = @acc
		ORDER BY c.sort, COALESCE(tl.name, td.name);`
	rows, err := r.db.QueryContext(ctx, query,
		sql.Named("store", storeID), sql.Named("acc", accountID), sql.Named("def", defLang))
	if err != nil {
		return nil, fmt.Errorf("repository: категории кабинета %d: %w", storeID, database.MapError(err))
	}
	defer rows.Close()

	var out []CabinetCategory
	for rows.Next() {
		var (
			c        CabinetCategory
			fromKasp int
		)
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Name, &c.ParentName, &c.Sort, &c.ProductCount, &fromKasp); err != nil {
			return nil, fmt.Errorf("repository: чтение категории: %w", err)
		}
		c.FromKaspi = fromKasp == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCategoryForEdit возвращает категорию с переводами всех языков.
func (r *CatalogRepository) GetCategoryForEdit(ctx context.Context, storeID, accountID, id int64) (*CategoryEdit, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const head = `
		SELECT ISNULL(parent_id, 0), sort, CASE WHEN external_code IS NULL THEN 0 ELSE 1 END
		FROM dbo.categories WHERE id = @id AND store_id = @store AND account_id = @acc;`
	var (
		e        CategoryEdit
		fromKasp int
	)
	e.ID = id
	e.Trs = map[string]CategoryTr{}
	err := r.db.QueryRowContext(ctx, head,
		sql.Named("id", id), sql.Named("store", storeID), sql.Named("acc", accountID)).
		Scan(&e.ParentID, &e.Sort, &fromKasp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: категория %d: %w", id, database.MapError(err))
	}
	e.FromKaspi = fromKasp == 1

	rows, err := r.db.QueryContext(ctx,
		`SELECT lang, name, slug FROM dbo.category_translations WHERE category_id = @id;`, sql.Named("id", id))
	if err != nil {
		return nil, fmt.Errorf("repository: переводы категории %d: %w", id, database.MapError(err))
	}
	defer rows.Close()
	for rows.Next() {
		var lang string
		var tr CategoryTr
		if err := rows.Scan(&lang, &tr.Name, &tr.Slug); err != nil {
			return nil, fmt.Errorf("repository: чтение перевода категории: %w", err)
		}
		e.Trs[lang] = tr
	}
	return &e, rows.Err()
}

// CreateCategory создаёт категорию магазина (ручную, без external_code) с
// переводами. Возвращает id.
func (r *CatalogRepository) CreateCategory(ctx context.Context, in CategoryInput) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	var id int64
	err := r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		const ins = `
			INSERT INTO dbo.categories (store_id, account_id, parent_id, sort)
			OUTPUT INSERTED.id
			VALUES (@store, @acc, @parent, @sort);`
		if err := tx.QueryRowContext(ctx, ins,
			sql.Named("store", in.StoreID), sql.Named("acc", in.AccountID),
			sql.Named("parent", nullInt64(in.ParentID)), sql.Named("sort", in.Sort)).Scan(&id); err != nil {
			return database.MapError(err)
		}
		return insertCategoryTranslations(ctx, tx, id, in.StoreID, in.Trs)
	})
	if err != nil {
		return 0, fmt.Errorf("repository: создание категории: %w", err)
	}
	return id, nil
}

// UpdateCategory обновляет родителя, сортировку и переводы категории.
func (r *CatalogRepository) UpdateCategory(ctx context.Context, in CategoryInput) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	err := r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE dbo.categories SET parent_id = @parent, sort = @sort
			 WHERE id = @id AND store_id = @store AND account_id = @acc;`,
			sql.Named("id", in.ID), sql.Named("store", in.StoreID), sql.Named("acc", in.AccountID),
			sql.Named("parent", nullInt64(in.ParentID)), sql.Named("sort", in.Sort))
		if err != nil {
			return database.MapError(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return database.ErrNotFound
		}
		return insertCategoryTranslations(ctx, tx, in.ID, in.StoreID, in.Trs)
	})
	if err != nil {
		return fmt.Errorf("repository: обновление категории %d: %w", in.ID, err)
	}
	return nil
}

// insertCategoryTranslations пишет переводы (MERGE по (category_id, lang)).
func insertCategoryTranslations(ctx context.Context, tx *sql.Tx, catID, storeID int64, trs map[string]CategoryTr) error {
	const merge = `
		MERGE dbo.category_translations WITH (HOLDLOCK) AS t
		USING (SELECT @cat AS category_id, @lang AS lang) AS s
		ON t.category_id = s.category_id AND t.lang = s.lang
		WHEN MATCHED THEN UPDATE SET name = @name, slug = @slug
		WHEN NOT MATCHED THEN INSERT (category_id, store_id, lang, name, slug)
			VALUES (@cat, @store, @lang, @name, @slug);`
	for lang, tr := range trs {
		if _, err := tx.ExecContext(ctx, merge,
			sql.Named("cat", catID), sql.Named("store", storeID), sql.Named("lang", lang),
			sql.Named("name", tr.Name), sql.Named("slug", tr.Slug)); err != nil {
			return database.MapError(err)
		}
	}
	return nil
}

// DeleteCategory удаляет категорию, если у неё нет подкатегорий и товаров.
// Иначе возвращает database.ErrConflict (продавец сначала переносит содержимое).
func (r *CatalogRepository) DeleteCategory(ctx context.Context, storeID, accountID, id int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	err := r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		// Владение + существование.
		var exists int
		err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM dbo.categories WHERE id = @id AND store_id = @store AND account_id = @acc;`,
			sql.Named("id", id), sql.Named("store", storeID), sql.Named("acc", accountID)).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return database.ErrNotFound
		}
		if err != nil {
			return database.MapError(err)
		}

		var children, products int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM dbo.categories WHERE parent_id = @id;`, sql.Named("id", id)).Scan(&children); err != nil {
			return database.MapError(err)
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM dbo.products WHERE category_id = @id;`, sql.Named("id", id)).Scan(&products); err != nil {
			return database.MapError(err)
		}
		if children > 0 || products > 0 {
			return database.ErrConflict
		}

		if _, err := tx.ExecContext(ctx,
			`DELETE FROM dbo.category_translations WHERE category_id = @id;`, sql.Named("id", id)); err != nil {
			return database.MapError(err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM dbo.categories WHERE id = @id AND store_id = @store;`,
			sql.Named("id", id), sql.Named("store", storeID)); err != nil {
			return database.MapError(err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("repository: удаление категории %d: %w", id, err)
	}
	return nil
}

// --- Товары ---

// CabinetProductQuery — параметры списка товаров в кабинете.
type CabinetProductQuery struct {
	StoreID    int64
	AccountID  int64
	Lang       string
	DefLang    string
	CategoryID int64
	Search     string
	Status     string // "" — любой
	Limit      int
	Offset     int
}

// CabinetProduct — строка списка товаров в кабинете.
type CabinetProduct struct {
	ID           int64
	Title        string
	CategoryName string
	Status       string
	PriceMinor   int64
	Currency     string
	PriceMode    string
	IsVisible    bool
	Qty          int
	Image        string
}

// ProductTr — перевод товара для одного языка.
type ProductTr struct {
	Title       string
	Description string
	Slug        string
	SEOTitle    string
	SEODesc     string
}

// ProductEdit — данные товара для формы редактирования.
type ProductEdit struct {
	ID               int64
	CategoryID       int64
	Brand            string
	Status           string
	SourceSKU        string // непусто — товар из Kaspi (действует защита правок)
	Overridden       map[string]bool
	Trs              map[string]ProductTr
	Images           []string
	VariantID        int64
	SKU              string
	PriceMode        string
	ManualPriceMinor int64
	OfferPriceMinor  int64
	OldPriceMinor    int64
	SourcePriceMinor int64
	Currency         string
	IsVisible        bool
	Qty              int
}

// ProductUpdate — сохранение контента товара из кабинета.
type ProductUpdate struct {
	StoreID    int64
	AccountID  int64
	ID         int64
	CategoryID int64
	Brand      string
	Status     string
	Trs        map[string]ProductTr
	Images     []string
	Overrides  []string // итоговый список переопределённых полей (для товаров Kaspi)
}

// CabinetProducts возвращает страницу товаров магазина (любого статуса) с
// офферами (в т.ч. скрытыми) и общим числом.
func (r *CatalogRepository) CabinetProducts(ctx context.Context, q CabinetProductQuery) ([]CabinetProduct, int, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT p.id, COALESCE(tl.title, tr.title, ''), ISNULL(ct.name, ''), p.status,
		       ISNULL(o.price_minor, 0), ISNULL(o.currency, ''), ISNULL(o.price_mode, ''), ISNULL(o.is_visible, 0),
		       ISNULL(img.url, ''),
		       ISNULL((SELECT SUM(qty) FROM dbo.variant_stocks vs JOIN dbo.variants v ON v.id = vs.variant_id WHERE v.product_id = p.id), 0) AS qty,
		       COUNT(*) OVER() AS total
		FROM dbo.products p
		LEFT JOIN dbo.product_translations tl ON tl.product_id = p.id AND tl.lang = @lang
		LEFT JOIN dbo.product_translations tr ON tr.product_id = p.id AND tr.lang = @def
		LEFT JOIN dbo.category_translations ct ON ct.category_id = p.category_id AND ct.lang = @def
		OUTER APPLY (
			SELECT TOP 1 so.price_minor, so.currency, so.price_mode, so.is_visible
			FROM dbo.store_offers so JOIN dbo.variants v ON v.id = so.variant_id
			WHERE v.product_id = p.id ORDER BY so.price_minor ASC
		) o
		OUTER APPLY (SELECT TOP 1 url FROM dbo.product_images pi WHERE pi.product_id = p.id ORDER BY sort) img
		WHERE p.store_id = @store AND p.account_id = @acc
		  AND (@cat = 0 OR p.category_id = @cat)
		  AND (@status = '' OR p.status = @status)
		  AND (@q = '' OR COALESCE(tl.title, tr.title) LIKE @like)
		ORDER BY p.id DESC
		OFFSET @off ROWS FETCH NEXT @lim ROWS ONLY;`

	rows, err := r.db.QueryContext(ctx, query,
		sql.Named("store", q.StoreID), sql.Named("acc", q.AccountID),
		sql.Named("lang", q.Lang), sql.Named("def", q.DefLang),
		sql.Named("cat", q.CategoryID), sql.Named("status", q.Status),
		sql.Named("q", q.Search), sql.Named("like", "%"+escapeLike(q.Search)+"%"),
		sql.Named("off", q.Offset), sql.Named("lim", q.Limit))
	if err != nil {
		return nil, 0, fmt.Errorf("repository: товары кабинета %d: %w", q.StoreID, database.MapError(err))
	}
	defer rows.Close()

	var (
		out   []CabinetProduct
		total int
	)
	for rows.Next() {
		var (
			p       CabinetProduct
			visible int
		)
		if err := rows.Scan(&p.ID, &p.Title, &p.CategoryName, &p.Status,
			&p.PriceMinor, &p.Currency, &p.PriceMode, &visible, &p.Image, &p.Qty, &total); err != nil {
			return nil, 0, fmt.Errorf("repository: чтение товара кабинета: %w", err)
		}
		p.IsVisible = visible == 1
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// GetProductForEdit возвращает товар со всеми переводами, изображениями и
// оффером основного варианта. Не найден — database.ErrNotFound.
func (r *CatalogRepository) GetProductForEdit(ctx context.Context, storeID, accountID, id int64) (*ProductEdit, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const head = `
		SELECT ISNULL(p.category_id, 0), ISNULL(p.brand, ''), p.status, ISNULL(p.source_sku, ''),
		       ISNULL(p.overridden_fields, '[]')
		FROM dbo.products p WHERE p.id = @id AND p.store_id = @store AND p.account_id = @acc;`
	e := &ProductEdit{ID: id, Trs: map[string]ProductTr{}}
	var overridden string
	err := r.db.QueryRowContext(ctx, head,
		sql.Named("id", id), sql.Named("store", storeID), sql.Named("acc", accountID)).
		Scan(&e.CategoryID, &e.Brand, &e.Status, &e.SourceSKU, &overridden)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: товар %d: %w", id, database.MapError(err))
	}
	e.Overridden = parseOverrides(overridden)

	// Переводы.
	trRows, err := r.db.QueryContext(ctx,
		`SELECT lang, title, ISNULL(description, ''), slug, ISNULL(seo_title, ''), ISNULL(seo_description, '')
		 FROM dbo.product_translations WHERE product_id = @id;`, sql.Named("id", id))
	if err != nil {
		return nil, fmt.Errorf("repository: переводы товара %d: %w", id, database.MapError(err))
	}
	for trRows.Next() {
		var lang string
		var tr ProductTr
		if err := trRows.Scan(&lang, &tr.Title, &tr.Description, &tr.Slug, &tr.SEOTitle, &tr.SEODesc); err != nil {
			trRows.Close()
			return nil, fmt.Errorf("repository: чтение перевода товара: %w", err)
		}
		e.Trs[lang] = tr
	}
	trRows.Close()
	if err := trRows.Err(); err != nil {
		return nil, err
	}

	imgs, err := r.productImages(ctx, id)
	if err != nil {
		return nil, err
	}
	e.Images = imgs

	// Основной вариант и его оффер (у зеркала Kaspi он один).
	const variant = `
		SELECT TOP 1 v.id, v.sku,
		       ISNULL(o.price_mode, 'rule'), ISNULL(o.manual_price_minor, 0), ISNULL(o.price_minor, 0),
		       ISNULL(o.old_price_minor, 0), ISNULL(o.currency, ''), ISNULL(o.is_visible, 1),
		       ISNULL((SELECT SUM(qty) FROM dbo.variant_stocks vs WHERE vs.variant_id = v.id), 0),
		       ISNULL((SELECT MIN(price_minor) FROM dbo.variant_marketplace_prices mp WHERE mp.variant_id = v.id), 0)
		FROM dbo.variants v
		LEFT JOIN dbo.store_offers o ON o.variant_id = v.id
		WHERE v.product_id = @id ORDER BY v.id;`
	var visible int
	err = r.db.QueryRowContext(ctx, variant, sql.Named("id", id)).
		Scan(&e.VariantID, &e.SKU, &e.PriceMode, &e.ManualPriceMinor, &e.OfferPriceMinor,
			&e.OldPriceMinor, &e.Currency, &visible, &e.Qty, &e.SourcePriceMinor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("repository: вариант товара %d: %w", id, database.MapError(err))
	}
	e.IsVisible = visible == 1
	return e, nil
}

// UpdateProduct сохраняет контент товара: категорию, бренд, статус, переводы всех
// языков, изображения и список переопределённых полей — одной транзакцией.
func (r *CatalogRepository) UpdateProduct(ctx context.Context, in ProductUpdate) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	overrides := "[]"
	if len(in.Overrides) > 0 {
		b, _ := json.Marshal(in.Overrides)
		overrides = string(b)
	}

	err := r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE dbo.products SET category_id = @cat, brand = @brand, status = @status,
			        overridden_fields = @ov, updated_at = SYSUTCDATETIME()
			 WHERE id = @id AND store_id = @store AND account_id = @acc;`,
			sql.Named("id", in.ID), sql.Named("store", in.StoreID), sql.Named("acc", in.AccountID),
			sql.Named("cat", nullInt64(in.CategoryID)), sql.Named("brand", nullString(in.Brand)),
			sql.Named("status", in.Status), sql.Named("ov", overrides))
		if err != nil {
			return database.MapError(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return database.ErrNotFound
		}

		const mergeTr = `
			MERGE dbo.product_translations WITH (HOLDLOCK) AS t
			USING (SELECT @pid AS product_id, @lang AS lang) AS s
			ON t.product_id = s.product_id AND t.lang = s.lang
			WHEN MATCHED THEN UPDATE SET title = @title, description = @descr, slug = @slug, seo_title = @seot, seo_description = @seod
			WHEN NOT MATCHED THEN INSERT (product_id, store_id, lang, title, description, slug, seo_title, seo_description)
				VALUES (@pid, @store, @lang, @title, @descr, @slug, @seot, @seod);`
		for lang, tr := range in.Trs {
			if _, err := tx.ExecContext(ctx, mergeTr,
				sql.Named("pid", in.ID), sql.Named("store", in.StoreID), sql.Named("lang", lang),
				sql.Named("title", tr.Title), sql.Named("descr", nullString(tr.Description)),
				sql.Named("slug", tr.Slug), sql.Named("seot", nullString(tr.SEOTitle)),
				sql.Named("seod", nullString(tr.SEODesc))); err != nil {
				return database.MapError(err)
			}
		}

		// Изображения — перезаписываем целиком в переданном порядке.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM dbo.product_images WHERE product_id = @pid;`, sql.Named("pid", in.ID)); err != nil {
			return database.MapError(err)
		}
		for i, url := range in.Images {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO dbo.product_images (product_id, store_id, url, sort) VALUES (@pid, @store, @url, @sort);`,
				sql.Named("pid", in.ID), sql.Named("store", in.StoreID),
				sql.Named("url", url), sql.Named("sort", i)); err != nil {
				return database.MapError(err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("repository: обновление товара %d: %w", in.ID, err)
	}
	return nil
}

// SetProductOffer задаёт цену и видимость оффера для всех вариантов товара.
// mode='manual' фиксирует ручную цену; mode='rule' записывает расчётную цену и
// правило. Изоляция — через вариант товара (проверка владения по account_id).
func (r *CatalogRepository) SetProductOffer(ctx context.Context, storeID, accountID, productID int64, mode string, priceMinor, oldPriceMinor, ruleID int64, currency string, visible bool) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	vis := 0
	if visible {
		vis = 1
	}
	manual := "manual" == mode

	err := r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		// Проверяем владение и берём варианты товара.
		rows, err := tx.QueryContext(ctx,
			`SELECT id FROM dbo.variants WHERE product_id = @pid AND store_id = @store AND account_id = @acc;`,
			sql.Named("pid", productID), sql.Named("store", storeID), sql.Named("acc", accountID))
		if err != nil {
			return database.MapError(err)
		}
		var variantIDs []int64
		for rows.Next() {
			var vid int64
			if err := rows.Scan(&vid); err != nil {
				rows.Close()
				return err
			}
			variantIDs = append(variantIDs, vid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(variantIDs) == 0 {
			return database.ErrNotFound
		}

		const upsert = `
			MERGE dbo.store_offers WITH (HOLDLOCK) AS t
			USING (SELECT @store AS store_id, @vid AS variant_id) AS s
			ON t.store_id = s.store_id AND t.variant_id = s.variant_id
			WHEN MATCHED THEN UPDATE SET
				price_mode = @mode, price_minor = @price, old_price_minor = @old,
				manual_price_minor = @manual, applied_rule_id = @rule,
				currency = @cur, is_visible = @vis, computed_at = SYSUTCDATETIME()
			WHEN NOT MATCHED THEN INSERT (store_id, variant_id, price_minor, old_price_minor, currency, is_visible, price_mode, manual_price_minor, applied_rule_id)
				VALUES (@store, @vid, @price, @old, @cur, @vis, @mode, @manual, @rule);`
		for _, vid := range variantIDs {
			var manualParam any
			if manual {
				manualParam = priceMinor
			} else {
				manualParam = nil
			}
			if _, err := tx.ExecContext(ctx, upsert,
				sql.Named("store", storeID), sql.Named("vid", vid), sql.Named("mode", mode),
				sql.Named("price", priceMinor), sql.Named("old", nullInt64(oldPriceMinor)),
				sql.Named("manual", manualParam), sql.Named("rule", nullInt64(ruleID)),
				sql.Named("cur", currency), sql.Named("vis", vis)); err != nil {
				return database.MapError(err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("repository: оффер товара %d: %w", productID, err)
	}
	return nil
}

// ClearOverride убирает поле из списка переопределённых у товара (кнопка «вернуть
// значение с маркетплейса»). Значение восстановит следующая сборка каталога.
func (r *CatalogRepository) ClearOverride(ctx context.Context, storeID, accountID, id int64, field string) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	err := r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		var raw string
		err := tx.QueryRowContext(ctx,
			`SELECT ISNULL(overridden_fields, '[]') FROM dbo.products WHERE id = @id AND store_id = @store AND account_id = @acc;`,
			sql.Named("id", id), sql.Named("store", storeID), sql.Named("acc", accountID)).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return database.ErrNotFound
		}
		if err != nil {
			return database.MapError(err)
		}
		set := parseOverrides(raw)
		delete(set, field)
		list := make([]string, 0, len(set))
		for f := range set {
			list = append(list, f)
		}
		next := "[]"
		if len(list) > 0 {
			b, _ := json.Marshal(list)
			next = string(b)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE dbo.products SET overridden_fields = @ov WHERE id = @id AND store_id = @store;`,
			sql.Named("ov", next), sql.Named("id", id), sql.Named("store", storeID)); err != nil {
			return database.MapError(err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("repository: сброс правки товара %d: %w", id, err)
	}
	return nil
}
