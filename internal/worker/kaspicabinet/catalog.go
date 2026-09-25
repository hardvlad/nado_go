package kaspicabinet

import (
	"encoding/json"
	"fmt"
	"iter"
	"strings"
)

// CabinetProduct — нормализованный товар из кабинета (offer-view/list).
// Товар Kaspi — один SKU (один вариант), поэтому структура плоская.
type CabinetProduct struct {
	SKU        string // код товара продавца
	MasterSKU  string // код карточки-мастера Kaspi (общая для продавцов)
	Title      string
	Brand      string
	CategoryID string
	Images     []string
	Available  bool
	PriceMinor int64        // тиыны
	Stocks     []StoreStock // остатки по точкам продавца
	Raw        json.RawMessage
}

// StoreStock — остаток на точке продавца.
type StoreStock struct {
	StoreCode string // код точки (без префикса merchant)
	Qty       int    // 0, если остаток не ведётся, но товар доступен
	Specified bool   // ведётся ли точный остаток
	PreOrder  int    // дней предзаказа
}

const offerPageSize = 25 // как в образце

// Products обходит каталог кабинета: активные и снятые с продажи товары,
// постранично. Останавливается на ошибке (в т.ч. устаревшей сессии — 401).
func (c *Client) Products(sess *Session) iter.Seq2[CabinetProduct, error] {
	return func(yield func(CabinetProduct, error) bool) {
		for _, active := range []bool{true, false} {
			for page := 0; ; page++ {
				total, items, err := c.offerList(sess, active, page, offerPageSize)
				if err != nil {
					yield(CabinetProduct{}, err)
					return
				}
				for _, it := range items {
					if !yield(it.normalize(), nil) {
						return
					}
				}
				// Дошли до конца выборки: пустая страница или собрали весь total.
				if len(items) == 0 || (page+1)*offerPageSize >= total {
					break
				}
			}
		}
	}
}

// offerList запрашивает одну страницу товаров. active=true — в продаже,
// false — снятые.
func (c *Client) offerList(sess *Session, active bool, page, size int) (total int, items []offerItem, err error) {
	a := "false"
	if active {
		a = "true"
	}
	path := fmt.Sprintf("/bff/offer-view/list?m=%s&p=%d&l=%d&a=%s", sess.MerchantID, page, size, a)

	// Заголовки кабинета с cookie сессии (счётчик amp — как в образце для этого запроса).
	hdr := append([]string{
		hUA, "Accept: */*", hAccLang, hAccEnc, c.refKaspi(), hCTJSON, c.originKaspi(), hKeepAlive,
		cookie(sess.AmpCookie+".1s.0.1s", sess.MCSession, sess.MCSid),
	}, sec4...)

	resp, err := c.do("GET", c.hosts.mc+path, hdr, nil)
	if err != nil {
		return 0, nil, err
	}
	if resp.status == 401 {
		return 0, nil, ErrSessionExpired
	}
	if resp.status != 200 {
		return 0, nil, fmt.Errorf("kaspicabinet: offer-view/list → %d", resp.status)
	}

	var parsed offerListResponse
	if err := json.Unmarshal(resp.body, &parsed); err != nil {
		// Кабинет вернул не JSON (изменился формат или пришла HTML-страница).
		return 0, nil, fmt.Errorf("%w: offer-view/list", ErrFormatChanged)
	}
	return parsed.Total, parsed.Data, nil
}

// ErrSessionExpired — сессия кабинета устарела, нужен повторный вход.
var ErrSessionExpired = fmt.Errorf("kaspicabinet: сессия устарела")

// ErrFormatChanged — кабинет отдал неожиданный формат (изменился API/пришёл HTML).
var ErrFormatChanged = fmt.Errorf("kaspicabinet: неожиданный формат ответа кабинета")

// Структуры ответа offer-view/list (поля — по KaspiImport.php).
type offerListResponse struct {
	Total int         `json:"total"`
	Data  []offerItem `json:"data"`
}

type offerItem struct {
	SKU            string          `json:"sku"`
	MasterSKU      string          `json:"masterSku"`
	MasterTitle    string          `json:"masterTitle"`
	Model          string          `json:"model"`
	Title          string          `json:"title"`
	Brand          string          `json:"brand"`
	MasterCategory string          `json:"masterCategory"`
	Images         []string        `json:"images"`
	Available      bool            `json:"available"`
	Price          float64         `json:"price"`
	MinPrice       float64         `json:"minPrice"`
	Availabilities []availability  `json:"availabilities"`
	Stocks         []stockEntry    `json:"stocks"`
	raw            json.RawMessage `json:"-"`
}

type availability struct {
	StoreID        string `json:"storeId"` // "<merchant>_<code>"
	Available      string `json:"available"`
	PreOrder       int    `json:"preOrder"`
	StockSpecified bool   `json:"stockSpecified"`
	StockCount     int    `json:"stockCount"`
}

type stockEntry struct {
	StockLevel map[string]struct {
		Value int `json:"value"`
	} `json:"stockLevel"`
}

func (i *offerItem) UnmarshalJSON(b []byte) error {
	type alias offerItem
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*i = offerItem(a)
	i.raw = append(json.RawMessage(nil), b...)
	return nil
}

func (i offerItem) normalize() CabinetProduct {
	title := firstNonEmpty(i.Model, i.Title, i.MasterTitle)
	price := i.Price
	if price == 0 && i.MinPrice > 0 { // как в образце
		price = i.MinPrice
	}

	// Фактические остатки по точкам: storeId → value.
	stockByCode := map[string]int{}
	for _, s := range i.Stocks {
		for storeID, lvl := range s.StockLevel {
			stockByCode[storeCodeOf(storeID)] = lvl.Value
		}
	}

	var stocks []StoreStock
	for _, av := range i.Availabilities {
		if !strings.EqualFold(av.Available, "yes") {
			continue
		}
		code := storeCodeOf(av.StoreID)
		st := StoreStock{StoreCode: code, Specified: av.StockSpecified, PreOrder: av.PreOrder}
		if av.StockSpecified {
			if v, ok := stockByCode[code]; ok {
				st.Qty = v
			} else {
				st.Qty = av.StockCount
			}
		}
		stocks = append(stocks, st)
	}

	return CabinetProduct{
		SKU:        i.SKU,
		MasterSKU:  i.MasterSKU,
		Title:      title,
		Brand:      i.Brand,
		CategoryID: i.MasterCategory,
		Images:     i.Images,
		Available:  i.Available,
		PriceMinor: int64(price*100 + 0.5),
		Stocks:     stocks,
		Raw:        i.raw,
	}
}

// storeCodeOf отрезает префикс merchant из storeId "<merchant>_<code>".
func storeCodeOf(storeID string) string {
	if _, code, ok := strings.Cut(storeID, "_"); ok {
		return code
	}
	return storeID
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
