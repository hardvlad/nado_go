package model

import "time"

// Статусы магазина.
const (
	StoreStatusActive   = "active"
	StoreStatusDisabled = "disabled"
	StoreStatusArchived = "archived"
)

// Store — магазин продавца: витрина + подключение к маркетплейсу (D-23).
// Для списка в кабинете часть полей приходит из связанного подключения.
type Store struct {
	ID           int64
	Name         string
	Slug         string
	Status       string
	BaseCurrency string

	Marketplace      string // код подключённого маркетплейса или ""
	ConnectionStatus string // active | invalid | paused | ""
	OrdersSyncAt     time.Time
}

// StorefrontStore — магазин, разрешённый для витрины по Host или slug.
// Содержит всё, что нужно рендеру витрины, без похода в БД на каждый запрос.
type StorefrontStore struct {
	ID            int64
	AccountID     int64
	Slug          string
	Name          string
	Status        string
	DefaultLang   string
	BaseCurrency  string
	ThemeCode     string
	ThemeSettings string // JSON схемы темы (может быть пусто)
	PrimaryHost   string // основной домен (для canonical и 301 с неосновных)
}

// Active — магазин обслуживает витрину (оплачен и не архивен).
func (s StorefrontStore) Active() bool { return s.Status == StoreStatusActive }

// MarketplaceOrder — заказ маркетплейса (зеркало Kaspi).
type MarketplaceOrder struct {
	ConnectionID  int64
	StoreID       int64
	AccountID     int64
	ExternalID    string
	Code          string
	State         string
	Status        string
	TotalMinor    int64
	Currency      string
	CustomerName  string
	CustomerPhone string
	OrderedAt     time.Time
	Raw           []byte
}

// MarketplaceProduct — товар маркетплейса (зеркало Kaspi) для сохранения.
type MarketplaceProduct struct {
	ConnectionID int64
	StoreID      int64
	AccountID    int64
	SKU          string
	MasterSKU    string
	Title        string
	Brand        string
	CategoryExt  string
	PriceMinor   int64
	Currency     string
	Available    bool
	ImagesJSON   string // JSON-массив URL
	ContentHash  []byte
	Raw          []byte
	Stocks       []MarketplaceProductStock
}

// MarketplaceProductStock — остаток товара на точке продавца.
type MarketplaceProductStock struct {
	StoreCode string
	Qty       int
	Specified bool
	PreOrder  int
}
