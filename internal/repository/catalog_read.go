package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// Чтение каталога витриной. Перевод берётся для языка страницы с откатом на язык
// магазина по умолчанию (COALESCE). Цена — из store_offers (её считают правила,
// шаблон не вычисляет). В листинги попадают только видимые офферы активных
// товаров; наличие (qty) отдаётся отдельно — товар без остатка остаётся доступен
// по URL со статусом «нет в наличии».

// ListCategories возвращает категории магазина с числом видимых товаров.
func (r *CatalogRepository) ListCategories(ctx context.Context, storeID int64, lang, defLang string) ([]model.Category, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT c.id, ISNULL(c.parent_id, 0), COALESCE(tl.name, tr.name, ''), COALESCE(tl.slug, tr.slug, ''), c.sort,
		       (SELECT COUNT(*) FROM dbo.products p
		        WHERE p.category_id = c.id AND p.status = 'active'
		          AND EXISTS (SELECT 1 FROM dbo.store_offers so JOIN dbo.variants v ON v.id = so.variant_id
		                      WHERE v.product_id = p.id AND so.is_visible = 1)) AS cnt
		FROM dbo.categories c
		LEFT JOIN dbo.category_translations tl ON tl.category_id = c.id AND tl.lang = @lang
		LEFT JOIN dbo.category_translations tr ON tr.category_id = c.id AND tr.lang = @def
		WHERE c.store_id = @store
		ORDER BY c.sort, COALESCE(tl.name, tr.name);`
	rows, err := r.db.QueryContext(ctx, query,
		sql.Named("store", storeID), sql.Named("lang", lang), sql.Named("def", defLang))
	if err != nil {
		return nil, fmt.Errorf("repository: категории магазина %d: %w", storeID, database.MapError(err))
	}
	defer rows.Close()

	var out []model.Category
	for rows.Next() {
		var c model.Category
		c.StoreID = storeID
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Name, &c.Slug, &c.Sort, &c.ProductCount); err != nil {
			return nil, fmt.Errorf("repository: чтение категории: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ProductQuery — параметры выборки товаров витрины.
type ProductQuery struct {
	StoreID    int64
	Lang       string
	DefLang    string
	CategoryID int64  // 0 = все категории
	Search     string // пусто = без поиска
	Limit      int
	Offset     int
}

// ListProducts возвращает страницу товаров и общее число подходящих.
func (r *CatalogRepository) ListProducts(ctx context.Context, q ProductQuery) ([]model.Product, int, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT p.id, ISNULL(p.category_id, 0), ISNULL(p.brand, ''),
		       COALESCE(tl.title, tr.title, ''), COALESCE(tl.slug, tr.slug, ''),
		       o.variant_id, o.price_minor, ISNULL(o.old_price_minor, 0), o.currency,
		       ISNULL(img.url, ''),
		       ISNULL((SELECT SUM(qty) FROM dbo.variant_stocks vs WHERE vs.variant_id = o.variant_id), 0) AS qty,
		       COUNT(*) OVER() AS total
		FROM dbo.products p
		CROSS APPLY (
			SELECT TOP 1 so.variant_id, so.price_minor, so.old_price_minor, so.currency
			FROM dbo.store_offers so JOIN dbo.variants v ON v.id = so.variant_id
			WHERE v.product_id = p.id AND so.is_visible = 1
			ORDER BY so.price_minor ASC
		) o
		LEFT JOIN dbo.product_translations tl ON tl.product_id = p.id AND tl.lang = @lang
		LEFT JOIN dbo.product_translations tr ON tr.product_id = p.id AND tr.lang = @def
		OUTER APPLY (SELECT TOP 1 url FROM dbo.product_images pi WHERE pi.product_id = p.id ORDER BY sort) img
		WHERE p.store_id = @store AND p.status = 'active'
		  AND (@cat = 0 OR p.category_id = @cat)
		  AND (@q = '' OR COALESCE(tl.title, tr.title) LIKE @like)
		ORDER BY p.id DESC
		OFFSET @off ROWS FETCH NEXT @lim ROWS ONLY;`

	rows, err := r.db.QueryContext(ctx, query,
		sql.Named("store", q.StoreID), sql.Named("lang", q.Lang), sql.Named("def", q.DefLang),
		sql.Named("cat", q.CategoryID), sql.Named("q", q.Search), sql.Named("like", "%"+escapeLike(q.Search)+"%"),
		sql.Named("off", q.Offset), sql.Named("lim", q.Limit))
	if err != nil {
		return nil, 0, fmt.Errorf("repository: товары магазина %d: %w", q.StoreID, database.MapError(err))
	}
	defer rows.Close()

	var (
		out   []model.Product
		total int
	)
	for rows.Next() {
		var (
			p   model.Product
			img string
			qty int
		)
		p.StoreID = q.StoreID
		if err := rows.Scan(&p.ID, &p.CategoryID, &p.Brand, &p.Title, &p.Slug,
			&p.VariantID, &p.PriceMinor, &p.OldPriceMinor, &p.Currency, &img, &qty, &total); err != nil {
			return nil, 0, fmt.Errorf("repository: чтение товара: %w", err)
		}
		p.Qty = qty
		p.Available = qty > 0
		if img != "" {
			p.Images = []string{img}
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// GetProduct возвращает товар витрины по id (в пределах магазина) со всеми
// изображениями. Не найден/архивный — database.ErrNotFound.
func (r *CatalogRepository) GetProduct(ctx context.Context, storeID, id int64, lang, defLang string) (*model.Product, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		SELECT p.id, ISNULL(p.category_id, 0), ISNULL(p.brand, ''),
		       COALESCE(tl.title, tr.title, ''), COALESCE(tl.description, tr.description, ''),
		       COALESCE(tl.slug, tr.slug, ''), COALESCE(tl.seo_title, tr.seo_title, ''),
		       COALESCE(tl.seo_description, tr.seo_description, ''),
		       o.variant_id, o.price_minor, ISNULL(o.old_price_minor, 0), o.currency,
		       ISNULL((SELECT SUM(qty) FROM dbo.variant_stocks vs WHERE vs.variant_id = o.variant_id), 0) AS qty
		FROM dbo.products p
		CROSS APPLY (
			SELECT TOP 1 so.variant_id, so.price_minor, so.old_price_minor, so.currency
			FROM dbo.store_offers so JOIN dbo.variants v ON v.id = so.variant_id
			WHERE v.product_id = p.id AND so.is_visible = 1
			ORDER BY so.price_minor ASC
		) o
		LEFT JOIN dbo.product_translations tl ON tl.product_id = p.id AND tl.lang = @lang
		LEFT JOIN dbo.product_translations tr ON tr.product_id = p.id AND tr.lang = @def
		WHERE p.store_id = @store AND p.id = @id AND p.status <> 'archived';`

	var (
		p   model.Product
		qty int
	)
	p.StoreID = storeID
	err := r.db.QueryRowContext(ctx, query,
		sql.Named("store", storeID), sql.Named("id", id), sql.Named("lang", lang), sql.Named("def", defLang)).
		Scan(&p.ID, &p.CategoryID, &p.Brand, &p.Title, &p.Description, &p.Slug, &p.SEOTitle, &p.SEODesc,
			&p.VariantID, &p.PriceMinor, &p.OldPriceMinor, &p.Currency, &qty)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: товар %d: %w", id, database.MapError(err))
	}
	p.Qty = qty
	p.Available = qty > 0

	imgs, err := r.productImages(ctx, id)
	if err != nil {
		return nil, err
	}
	p.Images = imgs
	return &p, nil
}

func (r *CatalogRepository) productImages(ctx context.Context, productID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT url FROM dbo.product_images WHERE product_id = @pid ORDER BY sort;`, sql.Named("pid", productID))
	if err != nil {
		return nil, fmt.Errorf("repository: изображения товара %d: %w", productID, database.MapError(err))
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// escapeLike экранирует спецсимволы LIKE, чтобы ввод поиска не менял шаблон.
func escapeLike(s string) string {
	r := strings.NewReplacer("[", "[[]", "%", "[%]", "_", "[_]")
	return r.Replace(strings.TrimSpace(s))
}
