package webhook

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// PaymentProcessor — то, что вебхуку нужно от слоя оплат.
type PaymentProcessor interface {
	HandleWebhook(ctx context.Context, provider, token string, r *http.Request) (contentType string, body []byte, err error)
}

// Payment принимает вебхуки платёжных провайдеров. Аутентификация — по токену в
// пути и подписи провайдера (проверяется внутри слоя оплат).
type Payment struct {
	proc PaymentProcessor
	log  *slog.Logger
}

func NewPayment(proc PaymentProcessor, log *slog.Logger) *Payment {
	return &Payment{proc: proc, log: log}
}

// Routes монтируется под /webhooks/payment.
func (h *Payment) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/{provider}/{token}", h.receive)
	return r
}

func (h *Payment) receive(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	token := chi.URLParam(r, "token")

	ct, body, err := h.proc.HandleWebhook(r.Context(), provider, token, r)
	if err != nil {
		// Если провайдер ждёт тело ответа даже при отказе — отдаём его (200),
		// иначе провайдер будет слать повторы. Нет тела — 400.
		h.log.Warn("вебхук оплаты отклонён", slog.String("provider", provider), slog.Any("error", err))
		if len(body) > 0 {
			writeBody(w, ct, body)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	writeBody(w, ct, body)
}

func writeBody(w http.ResponseWriter, ct string, body []byte) {
	if ct == "" {
		ct = "text/plain"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
