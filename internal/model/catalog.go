package model

import "time"

// Каталог витрины (уровень магазина, D-23). Контент по языкам — в переводах.

// Category — категория магазина.
type Category struct {
	ID           int64
	StoreID      int64
	ParentID     int64
	ExternalCode string
	Name         string // из перевода для языка страницы
	Slug         string
	Sort         int
	ProductCount int // для витрины (опционально)
}

// Product — товар витрины с полями для листинга/карточки на языке страницы.
type Product struct {
	ID          int64
	StoreID     int64
	CategoryID  int64
	Brand       string
	Status      string
	Title       string
	Description string // HTML, санитайзится перед выводом
	Slug        string
	SEOTitle    string
	SEODesc     string
	Images      []string
	// Первый вариант и его оффер — для листинга (у зеркала Kaspi один вариант).
	VariantID     int64
	SKU           string
	PriceMinor    int64
	OldPriceMinor int64
	Currency      string
	Available     bool
	Qty           int
	UpdatedAt     time.Time
}

// Статусы товара.
const (
	ProductDraft    = "draft"
	ProductActive   = "active"
	ProductArchived = "archived"
)

// PriceRule — правило цены магазина (D-14).
type PriceRule struct {
	ID          int64
	StoreID     int64
	Scope       string // store | category | product
	ScopeID     int64
	AdjustKind  string  // percent | amount
	AdjustValue float64 // -10 = -10%; для amount — в основных единицах валюты
	RoundTo     int64   // 1 | 10 | 100 (в основных единицах)
	RoundMode   string  // up | down | nearest
	Enabled     bool
}

// Области правил цены.
const (
	PriceScopeStore    = "store"
	PriceScopeCategory = "category"
	PriceScopeProduct  = "product"
)
