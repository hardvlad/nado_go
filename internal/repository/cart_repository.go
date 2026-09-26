package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// CartRepository — корзины витрины и их позиции. Цены при чтении берутся из
// store_offers, а не из корзины: клиенту не доверяем.
type CartRepository struct {
	db *database.DB
}

func NewCartRepository(db *database.DB) *CartRepository {
	return &CartRepository{db: db}
}

// EnsureCart находит корзину по токену или создаёт новую. Возвращает её id.
func (r *CartRepository) EnsureCart(ctx context.Context, storeID int64, token string) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		MERGE dbo.carts WITH (HOLDLOCK) AS t
		USING (SELECT @token AS token) AS s
		ON t.token = s.token
		WHEN MATCHED THEN UPDATE SET updated_at = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN INSERT (store_id, token) VALUES (@store, @token)
		OUTPUT INSERTED.id;`
	var id int64
	err := r.db.QueryRowContext(ctx, query, sql.Named("store", storeID), sql.Named("token", token)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if err = r.db.QueryRowContext(ctx, `SELECT id FROM dbo.carts WHERE token = @token;`, sql.Named("token", token)).Scan(&id); err != nil {
			return 0, fmt.Errorf("repository: корзина: %w", database.MapError(err))
		}
		return id, nil
	}
	if err != nil {
		return 0, fmt.Errorf("repository: корзина: %w", database.MapError(err))
	}
	return id, nil
}

// AddItem добавляет количество к позиции (или создаёт её).
func (r *CartRepository) AddItem(ctx context.Context, cartID, variantID int64, qty int) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		MERGE dbo.cart_items WITH (HOLDLOCK) AS t
		USING (SELECT @cart AS cart_id, @variant AS variant_id) AS s
		ON t.cart_id = s.cart_id AND t.variant_id = s.variant_id
		WHEN MATCHED THEN UPDATE SET qty = t.qty + @qty
		WHEN NOT MATCHED THEN INSERT (cart_id, variant_id, qty) VALUES (@cart, @variant, @qty);`
	if _, err := r.db.ExecContext(ctx, query,
		sql.Named("cart", cartID), sql.Named("variant", variantID), sql.Named("qty", qty)); err != nil {
		return fmt.Errorf("repository: добавление в корзину: %w", database.MapError(err))
	}
	return nil
}

// SetQty устанавливает количество позиции; qty<=0 удаляет её.
func (r *CartRepository) SetQty(ctx context.Context, cartID, variantID int64, qty int) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	if qty <= 0 {
		return r.RemoveItem(ctx, cartID, variantID)
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE dbo.cart_items SET qty = @qty WHERE cart_id = @cart AND variant_id = @variant;`,
		sql.Named("qty", qty), sql.Named("cart", cartID), sql.Named("variant", variantID)); err != nil {
		return fmt.Errorf("repository: изменение позиции: %w", database.MapError(err))
	}
	return nil
}

// RemoveItem убирает позицию из корзины.
func (r *CartRepository) RemoveItem(ctx context.Context, cartID, variantID int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM dbo.cart_items WHERE cart_id = @cart AND variant_id = @variant;`,
		sql.Named("cart", cartID), sql.Named("variant", variantID)); err != nil {
		return fmt.Errorf("repository: удаление позиции: %w", database.MapError(err))
	}
	return nil
}

// CountByToken возвращает суммарное число единиц в корзине (для бейджа). Нет
// корзины — 0.
func (r *CartRepository) CountByToken(ctx context.Context, storeID int64, token string) (int, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		SELECT ISNULL(SUM(ci.qty), 0)
		FROM dbo.carts c JOIN dbo.cart_items ci ON ci.cart_id = c.id
		WHERE c.token = @token AND c.store_id = @store;`
	var n int
	if err := r.db.QueryRowContext(ctx, query, sql.Named("token", token), sql.Named("store", storeID)).Scan(&n); err != nil {
		return 0, fmt.Errorf("repository: число позиций корзины: %w", database.MapError(err))
	}
	return n, nil
}

// Lines возвращает позиции корзины с актуальной ценой, наличием и данными показа.
func (r *CartRepository) Lines(ctx context.Context, storeID, cartID int64, lang, defLang string) ([]model.CartLine, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		SELECT ci.variant_id, v.product_id, COALESCE(tl.title, tr.title, ''), COALESCE(tl.slug, tr.slug, ''),
		       ISNULL(img.url, ''), v.sku, o.price_minor, o.currency, o.is_visible, ci.qty,
		       ISNULL((SELECT SUM(qty) FROM dbo.variant_stocks vs WHERE vs.variant_id = ci.variant_id), 0) AS avail
		FROM dbo.cart_items ci
		JOIN dbo.variants v ON v.id = ci.variant_id
		JOIN dbo.products p ON p.id = v.product_id
		LEFT JOIN dbo.store_offers o ON o.store_id = @store AND o.variant_id = ci.variant_id
		LEFT JOIN dbo.product_translations tl ON tl.product_id = p.id AND tl.lang = @lang
		LEFT JOIN dbo.product_translations tr ON tr.product_id = p.id AND tr.lang = @def
		OUTER APPLY (SELECT TOP 1 url FROM dbo.product_images pi WHERE pi.product_id = p.id ORDER BY sort) img
		WHERE ci.cart_id = @cart
		ORDER BY ci.added_at;`
	rows, err := r.db.QueryContext(ctx, query,
		sql.Named("store", storeID), sql.Named("cart", cartID), sql.Named("lang", lang), sql.Named("def", defLang))
	if err != nil {
		return nil, fmt.Errorf("repository: позиции корзины: %w", database.MapError(err))
	}
	defer rows.Close()

	var out []model.CartLine
	for rows.Next() {
		var (
			l        model.CartLine
			price    sql.NullInt64
			currency sql.NullString
			visible  sql.NullBool
			avail    int
		)
		if err := rows.Scan(&l.VariantID, &l.ProductID, &l.Title, &l.Slug, &l.Image, &l.SKU,
			&price, &currency, &visible, &l.Qty, &avail); err != nil {
			return nil, fmt.Errorf("repository: чтение позиции корзины: %w", err)
		}
		l.PriceMinor = price.Int64
		l.Currency = currency.String
		l.Available = avail
		l.InStock = visible.Valid && visible.Bool && avail >= l.Qty
		l.LineMinor = l.PriceMinor * int64(l.Qty)
		out = append(out, l)
	}
	return out, rows.Err()
}

// Clear удаляет все позиции корзины (после оформления заказа).
func (r *CartRepository) Clear(ctx context.Context, cartID int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	if _, err := r.db.ExecContext(ctx, `DELETE FROM dbo.cart_items WHERE cart_id = @cart;`, sql.Named("cart", cartID)); err != nil {
		return fmt.Errorf("repository: очистка корзины: %w", database.MapError(err))
	}
	return nil
}

// AttachCustomer привязывает корзину к вошедшему покупателю.
func (r *CartRepository) AttachCustomer(ctx context.Context, cartID, customerID int64) error {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	if _, err := r.db.ExecContext(ctx,
		`UPDATE dbo.carts SET customer_id = @cid WHERE id = @cart;`,
		sql.Named("cid", customerID), sql.Named("cart", cartID)); err != nil {
		return fmt.Errorf("repository: привязка корзины: %w", database.MapError(err))
	}
	return nil
}
