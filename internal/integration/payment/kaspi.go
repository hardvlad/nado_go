package payment

import (
	"context"
	"errors"
	"net/http"
)

// Kaspi — полуручной приём Kaspi Pay (D-01, дорожная карта этап 7). Kaspi не даёт
// массового онлайн-эквайринга для сторонних сайтов, поэтому на старте: покупателю
// показываются реквизиты/QR продавца, а факт оплаты продавец подтверждает в
// кабинете. Онлайн-вебхука нет — Manual()=true.
type Kaspi struct{}

func NewKaspi() *Kaspi { return &Kaspi{} }

func (Kaspi) Code() string { return "kaspi" }
func (Kaspi) Manual() bool { return true }

// Start не ходит наружу: сервис покажет инструкции по оплате Kaspi.
func (Kaspi) Start(_ context.Context, in StartInput) (*StartResult, error) {
	return &StartResult{RedirectURL: "", ProviderRef: in.Ref, Status: StatusPending}, nil
}

func (Kaspi) ParseCallback(_ *http.Request, _ Credentials) (*Callback, error) {
	return nil, errors.New("payment: Kaspi подтверждается вручную")
}

func (Kaspi) CallbackResponse(bool) (string, []byte) { return "text/plain", []byte("ok") }
