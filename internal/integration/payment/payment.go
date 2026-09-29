// Package payment — приём онлайн-оплат за интерфейсом Provider (как маркетплейсы
// за marketplace.Connector). Один слой обслуживает и оплату заказов витрины
// (деньги покупателя → мерчант-аккаунт продавца, D-01), и оплату подписки
// платформе (деньги продавца → мерчант-аккаунт nado). Провайдеры не знают о БД:
// учётные данные передаются параметром, суммы — в минорных единицах.
package payment

import (
	"context"
	"net/http"
	"sort"
	"strconv"
)

// Status — статус платежа в терминах слоя оплат.
type Status string

const (
	StatusPending   Status = "pending"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// Credentials — учётные данные мерчанта (расшифрованные). Secret в логи не попадает.
type Credentials struct {
	MerchantID string
	Secret     string
	Testing    bool
}

// StartInput — данные для инициализации платежа.
type StartInput struct {
	Ref         string // наш идентификатор платежа (уникальный), возвращается провайдером
	AmountMinor int64
	Currency    string
	Description string
	Email       string
	Phone       string
	SuccessURL  string // куда вернуть покупателя при успехе
	FailURL     string // куда вернуть при отказе
	CallbackURL string // серверный вебхук о результате
	Creds       Credentials
}

// StartResult — куда отправить покупателя.
type StartResult struct {
	// RedirectURL — адрес платёжной страницы провайдера. Пусто — платёж
	// подтверждается на нашей стороне (dev-провайдер, ручной Kaspi): сервис
	// показывает локальную страницу оплаты.
	RedirectURL string
	ProviderRef string // идентификатор платежа у провайдера
	Status      Status
}

// Callback — разобранный и проверенный вебхук провайдера.
type Callback struct {
	Ref         string // наш идентификатор платежа (Ref из StartInput)
	ProviderRef string
	Status      Status
	AmountMinor int64
	Raw         []byte
}

// Provider — адаптер платёжного провайдера.
type Provider interface {
	Code() string
	// Start инициирует платёж. Ошибка — платёж не создан у провайдера.
	Start(ctx context.Context, in StartInput) (*StartResult, error)
	// ParseCallback разбирает вебхук и проверяет подпись учётными данными мерчанта.
	ParseCallback(r *http.Request, creds Credentials) (*Callback, error)
	// CallbackResponse — тело ответа, которое ждёт провайдер на вебхук.
	CallbackResponse(ok bool) (contentType string, body []byte)
	// Manual — платёж подтверждается вне онлайн-потока (ручной Kaspi): сервис не
	// ждёт вебхук, а показывает инструкции и статус «ожидает подтверждения».
	Manual() bool
}

// Registry — реестр провайдеров по коду.
type Registry struct {
	m map[string]Provider
}

// NewRegistry собирает реестр из провайдеров.
func NewRegistry(providers ...Provider) *Registry {
	m := make(map[string]Provider, len(providers))
	for _, p := range providers {
		m[p.Code()] = p
	}
	return &Registry{m: m}
}

// Get возвращает провайдера по коду.
func (r *Registry) Get(code string) (Provider, bool) {
	p, ok := r.m[code]
	return p, ok
}

// Codes возвращает коды провайдеров в алфавитном порядке (для UI выбора).
func (r *Registry) Codes() []string {
	out := make([]string, 0, len(r.m))
	for c := range r.m {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// MajorAmount форматирует минорную сумму в строку основных единиц с двумя
// знаками ("199000" → "1990.00") — формат pg_amount у большинства KZ-провайдеров.
func MajorAmount(minor int64) string {
	neg := minor < 0
	if neg {
		minor = -minor
	}
	s := strconv.FormatInt(minor/100, 10) + "." + pad2(minor%100)
	if neg {
		return "-" + s
	}
	return s
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}
