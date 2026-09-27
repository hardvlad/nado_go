// Package kaspi — интеграция с Kaspi.kz.
//
// Этот файл — официальный Shop API (по токену из ЛК продавца, заголовок
// X-Auth-Token, формат JSON:API). Официальный API отдаёт заказы и принимает
// товары на запись, но НЕ отдаёт каталог/цены/остатки продавца — их импорт
// идёт через кабинет на удалённых воркерах (см. references/kaspi.md).
package kaspi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const defaultBaseURL = "https://kaspi.kz/shop/api/v2"

// ErrUnauthorized — токен недействителен (нужно переподключить магазин).
var ErrUnauthorized = errors.New("kaspi: токен недействителен")

// Official — клиент официального Shop API.
type Official struct {
	http    *http.Client
	baseURL string
}

// Option настраивает клиент.
type Option func(*Official)

// WithBaseURL переопределяет адрес API (для тестов и песочницы).
func WithBaseURL(u string) Option { return func(o *Official) { o.baseURL = u } }

func NewOfficial(opts ...Option) *Official {
	o := &Official{http: &http.Client{Timeout: 30 * time.Second}, baseURL: defaultBaseURL}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// AccountInfo — то, что удалось узнать о кабинете при проверке токена.
type AccountInfo struct {
	OrdersTotal int // всего заказов по данным meta (справочно)
}

// orderStates — состояния заказов Kaspi. Эндпоинт /orders требует фильтр по
// состоянию, поэтому импорт идёт по каждому состоянию (порт KaspiAPI.php).
var orderStates = []string{"NEW", "SIGN_REQUIRED", "PICKUP", "DELIVERY", "KASPI_DELIVERY", "ARCHIVE"}

// dayMillis — сутки в миллисекундах (creationDate у Kaspi — в мс).
const dayMillis = int64(86400000)

// Verify проверяет токен запросом заказов. Kaspi требует фильтр по состоянию и
// диапазон дат создания (в мс) — как KaspiAPI.validateToken; без них ответ 400,
// а не 200. Берём заведомо пустое будущее окно: важно лишь, что токен принят.
func (o *Official) Verify(ctx context.Context, token string) (*AccountInfo, error) {
	now := time.Now().UnixMilli()
	q := url.Values{}
	q.Set("page[number]", "0")
	q.Set("page[size]", "1")
	q.Set("filter[orders][state]", "NEW")
	q.Set("filter[orders][creationDate][$ge]", strconv.FormatInt(now, 10))
	q.Set("filter[orders][creationDate][$le]", strconv.FormatInt(now+dayMillis, 10))

	var resp ordersResponse
	if err := o.get(ctx, token, "/orders", q, &resp); err != nil {
		return nil, err
	}
	return &AccountInfo{OrdersTotal: resp.Meta.TotalCount}, nil
}

// Order — нормализованный заказ маркетплейса.
type Order struct {
	ExternalID    string
	Code          string
	State         string
	Status        string
	TotalMinor    int64
	Currency      string
	CustomerName  string
	CustomerPhone string
	OrderedAt     time.Time
	Raw           json.RawMessage
}

// Orders обходит заказы, созданные не раньше since, по всем состояниям. Kaspi
// требует фильтр по состоянию и диапазон дат создания (в мс), поэтому обход идёт
// по каждому состоянию с пагинацией (порт KaspiAPI.getOrdersByStatusAndCreateDate).
// Один заказ в момент запроса находится в одном состоянии, дублей между
// состояниями нет; повторную запись всё равно снимает Upsert по external_id.
func (o *Official) Orders(ctx context.Context, token string, since time.Time) iter.Seq2[Order, error] {
	return func(yield func(Order, error) bool) {
		const pageSize = 100
		now := time.Now()
		if since.IsZero() {
			since = now.Add(-14 * 24 * time.Hour)
		}
		geMs := strconv.FormatInt(since.UnixMilli(), 10)
		leMs := strconv.FormatInt(now.UnixMilli(), 10)

		for _, state := range orderStates {
			for page := 0; ; page++ {
				q := url.Values{}
				q.Set("page[number]", strconv.Itoa(page))
				q.Set("page[size]", strconv.Itoa(pageSize))
				q.Set("filter[orders][state]", state)
				q.Set("filter[orders][creationDate][$ge]", geMs)
				q.Set("filter[orders][creationDate][$le]", leMs)

				var resp ordersResponse
				if err := o.get(ctx, token, "/orders", q, &resp); err != nil {
					yield(Order{}, err)
					return
				}
				for _, d := range resp.Data {
					if !yield(d.normalize(), nil) {
						return
					}
				}
				// Последняя страница состояния: пришло меньше, чем размер страницы.
				if len(resp.Data) < pageSize {
					break
				}
			}
		}
	}
}

// get выполняет запрос к API с токеном и разбирает JSON:API ответ.
func (o *Official) get(ctx context.Context, token, path string, q url.Values, out any) error {
	u := o.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Auth-Token", token)
	req.Header.Set("Accept", "application/vnd.api+json")

	resp, err := o.http.Do(req)
	if err != nil {
		return fmt.Errorf("kaspi: запрос %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("kaspi: разбор ответа %s: %w", path, err)
		}
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	default:
		return fmt.Errorf("kaspi: %s → HTTP %d", path, resp.StatusCode)
	}
}

// Структуры JSON:API ответа заказов.
type ordersResponse struct {
	Data []orderData `json:"data"`
	Meta struct {
		TotalCount int `json:"totalCount"`
		PageCount  int `json:"pageCount"`
	} `json:"meta"`
}

type orderData struct {
	ID         string          `json:"id"`
	Attributes orderAttributes `json:"attributes"`
	raw        json.RawMessage
}

type orderAttributes struct {
	Code         string  `json:"code"`
	State        string  `json:"state"`
	Status       string  `json:"status"`
	TotalPrice   float64 `json:"totalPrice"`
	CreationDate int64   `json:"creationDate"` // мс
	Customer     struct {
		Name      string `json:"name"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		CellPhone string `json:"cellPhone"`
	} `json:"customer"`
}

// UnmarshalJSON сохраняет сырой JSON заказа для raw_json.
func (d *orderData) UnmarshalJSON(b []byte) error {
	type alias orderData
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*d = orderData(a)
	d.raw = append(json.RawMessage(nil), b...)
	return nil
}

func (d orderData) normalize() Order {
	at := d.Attributes
	name := at.Customer.Name
	if name == "" {
		name = joinName(at.Customer.FirstName, at.Customer.LastName)
	}
	var orderedAt time.Time
	if at.CreationDate > 0 {
		orderedAt = time.UnixMilli(at.CreationDate).UTC()
	}
	return Order{
		ExternalID:    d.ID,
		Code:          at.Code,
		State:         at.State,
		Status:        at.Status,
		TotalMinor:    int64(at.TotalPrice*100 + 0.5), // тенге → тиыны
		Currency:      "KZT",
		CustomerName:  name,
		CustomerPhone: at.Customer.CellPhone,
		OrderedAt:     orderedAt,
		Raw:           d.raw,
	}
}

func joinName(first, last string) string {
	switch {
	case first != "" && last != "":
		return first + " " + last
	case first != "":
		return first
	default:
		return last
	}
}
