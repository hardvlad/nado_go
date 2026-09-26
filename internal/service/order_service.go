package service

import (
	"context"
	"encoding/json"
	"errors"

	"nado_go/internal/database"
	"nado_go/internal/httpx"
	"nado_go/internal/model"
	"nado_go/internal/repository"
)

// Ошибки оформления заказа для витрины.
var (
	ErrCheckoutEmpty       = errors.New("order: корзина пуста")
	ErrCheckoutUnavailable = errors.New("order: часть товаров недоступна")
	ErrOrderNotFound       = errors.New("order: заказ не найден")
)

// OrderService — оформление заказов витрины и доступ к ним.
type OrderService struct {
	orders *repository.OrderRepository
	carts  *repository.CartRepository
}

func NewOrderService(orders *repository.OrderRepository, carts *repository.CartRepository) *OrderService {
	return &OrderService{orders: orders, carts: carts}
}

// CheckoutInput — данные оформления с витрины.
type CheckoutInput struct {
	StoreID, AccountID int64
	CustomerID         int64
	CartToken          string
	Name, Phone, Email string
	Comment, Address   string
	Lang, DefLang      string
}

// Checkout оформляет заказ из корзины: создаёт заказ со снимком позиций и цен
// (в транзакции с блокировкой офферов) и очищает корзину. Оплата подключается
// отдельно — заказ создаётся в статусе awaiting_payment.
func (s *OrderService) Checkout(ctx context.Context, in CheckoutInput) (*model.Order, error) {
	cartID, err := s.carts.EnsureCart(ctx, in.StoreID, in.CartToken)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	lines, err := s.carts.Lines(ctx, in.StoreID, cartID, in.Lang, in.DefLang)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	if len(lines) == 0 {
		return nil, ErrCheckoutEmpty
	}

	items := make([]repository.OrderItemInput, 0, len(lines))
	for _, l := range lines {
		items = append(items, repository.OrderItemInput{VariantID: l.VariantID, Qty: l.Qty})
	}

	var addrJSON string
	if in.Address != "" {
		b, _ := json.Marshal(map[string]string{"text": in.Address})
		addrJSON = string(b)
	}

	order, err := s.orders.Create(ctx, repository.OrderInput{
		StoreID: in.StoreID, AccountID: in.AccountID, CustomerID: in.CustomerID,
		Name: in.Name, Phone: in.Phone, Email: in.Email, Comment: in.Comment, Address: addrJSON,
		Lang: in.Lang, DefLang: in.DefLang, Items: items,
	})
	switch {
	case errors.Is(err, repository.ErrCartEmpty):
		return nil, ErrCheckoutEmpty
	case errors.Is(err, repository.ErrItemUnavailable):
		return nil, ErrCheckoutUnavailable
	case err != nil:
		return nil, httpx.ErrInternal(err)
	}

	// Корзина оформлена — очищаем, чтобы обновление страницы не создало дубль.
	if err := s.carts.Clear(ctx, cartID); err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return order, nil
}

// Order возвращает заказ по номеру и токену (страница заказа защищена токеном).
func (s *OrderService) Order(ctx context.Context, storeID, number int64, token string) (*model.Order, error) {
	o, err := s.orders.GetByNumber(ctx, storeID, number, token)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return o, nil
}

// CustomerOrders возвращает заказы покупателя для кабинета.
func (s *OrderService) CustomerOrders(ctx context.Context, storeID, customerID int64) ([]model.Order, error) {
	orders, err := s.orders.ListByCustomer(ctx, storeID, customerID)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return orders, nil
}
