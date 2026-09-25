package kaspi

import "encoding/json"

// DTO приёма каталога: удалённый воркер шлёт нормализованные товары кабинета на
// сервер (/jobs-api/v1/kaspi/catalog/{connectionID}/products). Одни и те же
// структуры используют воркер (отправка) и сервер (приём).

// ProductPayload — один товар кабинета в запросе приёма.
type ProductPayload struct {
	SKU        string          `json:"sku"`
	MasterSKU  string          `json:"master_sku,omitempty"`
	Title      string          `json:"title,omitempty"`
	Brand      string          `json:"brand,omitempty"`
	CategoryID string          `json:"category_id,omitempty"`
	Images     []string        `json:"images,omitempty"`
	Available  bool            `json:"available"`
	PriceMinor int64           `json:"price_minor"`
	Currency   string          `json:"currency"`
	Stocks     []StockPayload  `json:"stocks,omitempty"`
	Raw        json.RawMessage `json:"raw,omitempty"`
}

// StockPayload — остаток товара на точке продавца.
type StockPayload struct {
	StoreCode string `json:"store_code"`
	Qty       int    `json:"qty"`
	Specified bool   `json:"specified"`
	PreOrder  int    `json:"preorder,omitempty"`
}

// IngestRequest — страница товаров. Final=true на последней странице обхода:
// сервер помечает не встреченные товары как снятые.
type IngestRequest struct {
	Products []ProductPayload `json:"products"`
	Final    bool             `json:"final"`
	// SeenSKUs при Final=true — все sku, встреченные за обход, чтобы отметить
	// пропавшие. Передаётся только на последней странице.
	SeenSKUs []string `json:"seen_skus,omitempty"`
}

// IngestResponse — итог приёма страницы.
type IngestResponse struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Removed int `json:"removed,omitempty"`
}
