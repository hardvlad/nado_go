package httpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// Envelope — единый формат JSON-ответа API.
type Envelope struct {
	Data  any            `json:"data,omitempty"`
	Meta  any            `json:"meta,omitempty"`
	Error *ErrorPayload  `json:"error,omitempty"`
	Extra map[string]any `json:"-"`
}

// ErrorPayload — тело ошибки API.
type ErrorPayload struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
}

// JSON пишет успешный ответ. Сериализация идёт в буфер: при ошибке маршалинга
// клиент не получит обрезанный JSON с уже отправленным статусом 200.
func JSON(w http.ResponseWriter, status int, payload any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)

	if err := enc.Encode(payload); err != nil {
		http.Error(w, `{"error":{"code":"encoding_failed","message":"Не удалось сформировать ответ"}}`,
			http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// OK — успешный ответ с данными.
func OK(w http.ResponseWriter, status int, data any) {
	JSON(w, status, Envelope{Data: data})
}

// NoContent — успешный ответ без тела.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// Fail пишет ошибку клиенту и логирует причину.
// Клиент видит только Message/Code; стек и текст внутренней ошибки — в логе.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	appErr := AsError(err)
	requestID := middleware.GetReqID(r.Context())

	log := Logger(r.Context())
	attrs := []any{
		slog.Int("status", appErr.Status),
		slog.String("code", appErr.Code),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	}
	if cause := appErr.Unwrap(); cause != nil {
		attrs = append(attrs, slog.String("cause", cause.Error()))
	}

	if appErr.Status >= http.StatusInternalServerError {
		log.Error(appErr.Message, attrs...)
	} else {
		log.Warn(appErr.Message, attrs...)
	}

	JSON(w, appErr.Status, Envelope{Error: &ErrorPayload{
		Code:      appErr.Code,
		Message:   appErr.Message,
		Fields:    appErr.Fields,
		RequestID: requestID,
	}})
}

// Meta — метаданные постраничной выдачи.
type Meta struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

// Paginated — успешный ответ со списком и метаданными пагинации.
func Paginated(w http.ResponseWriter, data any, page, perPage int, total int64) {
	totalPages := 0
	if perPage > 0 {
		totalPages = int((total + int64(perPage) - 1) / int64(perPage))
	}
	JSON(w, http.StatusOK, Envelope{
		Data: data,
		Meta: Meta{Page: page, PerPage: perPage, Total: total, TotalPages: totalPages},
	})
}

// Created ставит заголовок Location и отдаёт созданный ресурс.
func Created(w http.ResponseWriter, location string, data any) {
	if location != "" {
		w.Header().Set("Location", location)
	}
	JSON(w, http.StatusCreated, Envelope{Data: data})
}

// String пишет простой текстовый ответ (health-check и подобное).
func String(w http.ResponseWriter, status int, format string, args ...any) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, format, args...)
}
