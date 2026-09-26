package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// Ошибки оформления заказа.
var (
	ErrCartEmpty       = errors.New("repository: корзина пуста")
	ErrItemUnavailable = errors.New("repository: товар недоступен")
)

// OrderRepository — заказы витрины и их позиции.
type OrderRepository struct {
	db *database.DB
}

func NewOrderRepository(db *database.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// OrderItemInput — позиция для оформления (цена берётся из оффера в транзакции).
type OrderItemInput struct {
	VariantID int64
	Qty       int
}

// OrderInput — данные оформления заказа.
type OrderInput struct {
	StoreID, AccountID int64
	CustomerID         int64
	Name, Phone, Email string
	Comment, Address   string
	Lang, DefLang      string
	Items              []OrderItemInput
}

// Create оформляет заказ одной транзакцией: блокирует офферы (UPDLOCK), сверяет
// цену и наличие, делает снимок позиций и сумм и выдаёт номер заказа. Клиентские
// цены не используются — берётся актуальная цена оффера.
func (r *OrderRepository) Create(ctx context.Context, in OrderInput) (*model.Order, error) {
	if len(in.Items) == 0 {
		return nil, ErrCartEmpty
	}
	token, err := randomHex(16)
	if err != nil {
		return nil, err
	}

	order := &model.Order{
		StoreID: in.StoreID, Token: token, Status: model.OrderAwaitingPayment,
		CustomerName: in.Name, CustomerPhone: in.Phone, CustomerEmail: in.Email,
		Comment: in.Comment, Address: in.Address, Lang: in.Lang,
	}

	err = r.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sql.Tx) error {
		var subtotal int64
		currency := ""
		items := make([]model.OrderItem, 0, len(in.Items))

		for _, it := range in.Items {
			if it.Qty <= 0 {
				continue
			}
			// Блокируем оффер и читаем актуальную цену/наличие/название.
			const q = `
				SELECT o.price_minor, o.currency, o.is_visible,
				       COALESCE(tl.title, tr.title, v.sku), v.sku,
				       ISNULL((SELECT SUM(qty) FROM dbo.variant_stocks vs WHERE vs.variant_id = v.id), 0)
				FROM dbo.variants v WITH (UPDLOCK, HOLDLOCK)
				LEFT JOIN dbo.store_offers o ON o.store_id = @store AND o.variant_id = v.id
				LEFT JOIN dbo.product_translations tl ON tl.product_id = v.product_id AND tl.lang = @lang
				LEFT JOIN dbo.product_translations tr ON tr.product_id = v.product_id AND tr.lang = @def
				WHERE v.id = @variant AND v.store_id = @store;`
			var (
				price   sql.NullInt64
				cur     sql.NullString
				visible sql.NullBool
				title   string
				sku     string
				avail   int
			)
			err := tx.QueryRowContext(ctx, q,
				sql.Named("store", in.StoreID), sql.Named("variant", it.VariantID),
				sql.Named("lang", in.Lang), sql.Named("def", in.DefLang)).
				Scan(&price, &cur, &visible, &title, &sku, &avail)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: вариант %d", ErrItemUnavailable, it.VariantID)
			}
			if err != nil {
				return database.MapError(err)
			}
			if !price.Valid || !(visible.Valid && visible.Bool) || avail <= 0 {
				return fmt.Errorf("%w: %s", ErrItemUnavailable, title)
			}
			line := price.Int64 * int64(it.Qty)
			subtotal += line
			currency = cur.String
			items = append(items, model.OrderItem{
				VariantID: it.VariantID, Title: title, SKU: sku, Qty: it.Qty,
				PriceMinor: price.Int64, LineMinor: line,
			})
		}
		if len(items) == 0 {
			return ErrCartEmpty
		}

		// Номер заказа в пределах магазина (последовательный). Блокировка диапазона
		// не даёт двум заказам получить один номер.
		var number int64
		if err := tx.QueryRowContext(ctx,
			`SELECT ISNULL(MAX(number), 1000) + 1 FROM dbo.orders WITH (UPDLOCK, HOLDLOCK) WHERE store_id = @store;`,
			sql.Named("store", in.StoreID)).Scan(&number); err != nil {
			return database.MapError(err)
		}

		const insertOrder = `
			INSERT INTO dbo.orders (store_id, account_id, number, token, status, customer_id,
			                        customer_name, customer_phone, customer_email, address_json, comment,
			                        subtotal_minor, total_minor, currency, lang)
			OUTPUT INSERTED.id, INSERTED.created_at
			VALUES (@store, @acc, @number, @token, @status, @cid,
			        @name, @phone, @email, @addr, @comment,
			        @subtotal, @total, @cur, @lang);`
		if err := tx.QueryRowContext(ctx, insertOrder,
			sql.Named("store", in.StoreID), sql.Named("acc", in.AccountID), sql.Named("number", number),
			sql.Named("token", token), sql.Named("status", order.Status), sql.Named("cid", nullInt64(in.CustomerID)),
			sql.Named("name", nullString(in.Name)), sql.Named("phone", nullString(in.Phone)),
			sql.Named("email", nullString(in.Email)), sql.Named("addr", nullString(in.Address)),
			sql.Named("comment", nullString(in.Comment)),
			sql.Named("subtotal", subtotal), sql.Named("total", subtotal), sql.Named("cur", currency),
			sql.Named("lang", in.Lang)).Scan(&order.ID, &order.CreatedAt); err != nil {
			return database.MapError(err)
		}

		for _, it := range items {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO dbo.order_items (order_id, variant_id, title_snapshot, sku_snapshot, qty, price_minor)
				 VALUES (@oid, @vid, @title, @sku, @qty, @price);`,
				sql.Named("oid", order.ID), sql.Named("vid", it.VariantID), sql.Named("title", it.Title),
				sql.Named("sku", nullString(it.SKU)), sql.Named("qty", it.Qty), sql.Named("price", it.PriceMinor)); err != nil {
				return database.MapError(err)
			}
		}

		order.Number = number
		order.SubtotalMinor = subtotal
		order.TotalMinor = subtotal
		order.Currency = currency
		order.Items = items
		return nil
	})
	if err != nil {
		return nil, err
	}
	return order, nil
}

// GetByNumber возвращает заказ по номеру и токену (страница заказа защищена
// токеном: номер угадывается). Нет совпадения — database.ErrNotFound.
func (r *OrderRepository) GetByNumber(ctx context.Context, storeID, number int64, token string) (*model.Order, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		SELECT id, store_id, number, token, status, ISNULL(customer_name, ''), ISNULL(customer_phone, ''),
		       ISNULL(customer_email, ''), ISNULL(comment, ''), subtotal_minor, total_minor, currency, lang, created_at
		FROM dbo.orders WHERE store_id = @store AND number = @number AND token = @token;`
	var o model.Order
	err := r.db.QueryRowContext(ctx, query,
		sql.Named("store", storeID), sql.Named("number", number), sql.Named("token", token)).
		Scan(&o.ID, &o.StoreID, &o.Number, &o.Token, &o.Status, &o.CustomerName, &o.CustomerPhone,
			&o.CustomerEmail, &o.Comment, &o.SubtotalMinor, &o.TotalMinor, &o.Currency, &o.Lang, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: заказ %d: %w", number, database.MapError(err))
	}
	items, err := r.orderItems(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	o.Items = items
	return &o, nil
}

// ListByCustomer возвращает заказы покупателя (для кабинета).
func (r *OrderRepository) ListByCustomer(ctx context.Context, storeID, customerID int64) ([]model.Order, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()
	const query = `
		SELECT number, token, status, total_minor, currency, created_at
		FROM dbo.orders WHERE store_id = @store AND customer_id = @cid ORDER BY id DESC;`
	rows, err := r.db.QueryContext(ctx, query, sql.Named("store", storeID), sql.Named("cid", customerID))
	if err != nil {
		return nil, fmt.Errorf("repository: заказы покупателя: %w", database.MapError(err))
	}
	defer rows.Close()
	var out []model.Order
	for rows.Next() {
		var o model.Order
		if err := rows.Scan(&o.Number, &o.Token, &o.Status, &o.TotalMinor, &o.Currency, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *OrderRepository) orderItems(ctx context.Context, orderID int64) ([]model.OrderItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT variant_id, title_snapshot, ISNULL(sku_snapshot, ''), qty, price_minor
		 FROM dbo.order_items WHERE order_id = @oid ORDER BY id;`, sql.Named("oid", orderID))
	if err != nil {
		return nil, fmt.Errorf("repository: позиции заказа %d: %w", orderID, database.MapError(err))
	}
	defer rows.Close()
	var out []model.OrderItem
	for rows.Next() {
		var it model.OrderItem
		if err := rows.Scan(&it.VariantID, &it.Title, &it.SKU, &it.Qty, &it.PriceMinor); err != nil {
			return nil, err
		}
		it.LineMinor = it.PriceMinor * int64(it.Qty)
		out = append(out, it)
	}
	return out, rows.Err()
}

// randomHex возвращает n случайных байт в hex.
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("repository: генерация токена: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
