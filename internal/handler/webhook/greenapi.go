// Package webhook — входящие уведомления внешних сервисов.
package webhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// InstanceStateStore — то, что вебхуку нужно от пула инстансов.
type InstanceStateStore interface {
	SetState(ctx context.Context, provider, instanceID, state, phone string) error
}

// GreenAPI принимает уведомления GreenAPI. Нужен только для поддержания
// актуального состояния инстансов пула (authorized/notAuthorized/blocked).
type GreenAPI struct {
	store InstanceStateStore
	token string // секрет в пути URL: /webhooks/greenapi/{token}
	log   *slog.Logger
}

func NewGreenAPI(store InstanceStateStore, token string, log *slog.Logger) *GreenAPI {
	return &GreenAPI{store: store, token: token, log: log}
}

// Routes монтируется под /webhooks/greenapi.
func (h *GreenAPI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/{token}", h.receive)
	return r
}

func (h *GreenAPI) receive(w http.ResponseWriter, r *http.Request) {
	// Токен в пути подтверждает, что уведомление от нашего инстанса GreenAPI.
	if h.token == "" || subtle.ConstantTimeCompare([]byte(chi.URLParam(r, "token")), []byte(h.token)) != 1 {
		http.NotFound(w, r)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var payload struct {
		TypeWebhook   string `json:"typeWebhook"`
		StateInstance string `json:"stateInstance"`
		InstanceData  struct {
			IDInstance json.Number `json:"idInstance"`
			WID        string      `json:"wid"`
		} `json:"instanceData"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		// Некорректное тело подтверждаем 200: GreenAPI не должен слать повторы.
		w.WriteHeader(http.StatusOK)
		return
	}

	if payload.TypeWebhook == "stateInstanceChanged" && payload.InstanceData.IDInstance.String() != "" {
		phone := ""
		if payload.StateInstance == "authorized" {
			phone = digitsBefore(payload.InstanceData.WID, '@')
		}
		if err := h.store.SetState(r.Context(), "greenapi", payload.InstanceData.IDInstance.String(), payload.StateInstance, phone); err != nil {
			h.log.Error("greenapi webhook: обновление состояния", slog.Any("error", err))
		}
	}
	w.WriteHeader(http.StatusOK)
}

// digitsBefore возвращает цифры строки до разделителя (wid вида "7700...@c.us").
func digitsBefore(s string, sep byte) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			break
		}
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
