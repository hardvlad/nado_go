package model

import "time"

// CartLine — позиция корзины с актуальной ценой и данными для показа. Цена и
// наличие берутся из store_offers/variant_stocks на момент запроса, не из cookie.
type CartLine struct {
	VariantID  int64
	ProductID  int64
	Title      string
	Slug       string
	Image      string
	SKU        string
	PriceMinor int64
	Currency   string
	Qty        int
	Available  int // доступный остаток
	InStock    bool
	LineMinor  int64 // PriceMinor * Qty
}

// Cart — корзина с посчитанными позициями и итогом.
type Cart struct {
	ID         int64
	Token      string
	Lines      []CartLine
	TotalMinor int64
	Count      int
	Currency   string
}

// Order — заказ витрины (снимок на момент оформления).
type Order struct {
	ID            int64
	StoreID       int64
	Number        int64
	Token         string
	Status        string
	CustomerName  string
	CustomerPhone string
	CustomerEmail string
	Comment       string
	Address       string
	SubtotalMinor int64
	TotalMinor    int64
	Currency      string
	Lang          string
	CreatedAt     time.Time
	Items         []OrderItem
}

// OrderItem — позиция заказа (снимок).
type OrderItem struct {
	VariantID  int64
	Title      string
	SKU        string
	Qty        int
	PriceMinor int64
	LineMinor  int64
}

// Статусы заказа (подмножество из domain-model, используемое витриной).
const (
	OrderNew             = "new"
	OrderAwaitingPayment = "awaiting_payment"
	OrderPaid            = "paid"
	OrderCanceled        = "canceled"
)
