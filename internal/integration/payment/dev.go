package payment

import (
	"context"
	"errors"
	"net/http"
)

// Dev — тестовый провайдер для локальной разработки: не ходит наружу. Оплата
// подтверждается на нашей стороне (кнопка на странице оплаты витрины → сервис
// помечает платёж успешным). Позволяет прогнать сквозной сценарий без песочницы.
type Dev struct{}

func NewDev() *Dev { return &Dev{} }

func (Dev) Code() string { return "dev" }
func (Dev) Manual() bool { return false }

// Start не создаёт внешний платёж: пустой RedirectURL — сигнал сервису показать
// локальную страницу оплаты с кнопкой подтверждения.
func (Dev) Start(_ context.Context, in StartInput) (*StartResult, error) {
	return &StartResult{RedirectURL: "", ProviderRef: in.Ref, Status: StatusPending}, nil
}

// ParseCallback у dev-провайдера не используется: подтверждение идёт через сервис.
func (Dev) ParseCallback(_ *http.Request, _ Credentials) (*Callback, error) {
	return nil, errors.New("payment: dev-провайдер не принимает вебхуки")
}

func (Dev) CallbackResponse(bool) (string, []byte) { return "text/plain", []byte("ok") }
