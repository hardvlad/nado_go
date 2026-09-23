package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type ctxKey int

const loggerKey ctxKey = iota

// WithLogger кладёт логгер в контекст запроса, чтобы нижние слои писали логи
// с теми же атрибутами (request_id и т.п.), не получая логгер параметром.
func WithLogger(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, log)
}

// Logger достаёт логгер из контекста; при отсутствии — глобальный по умолчанию.
func Logger(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(loggerKey).(*slog.Logger); ok && log != nil {
		return log
	}
	return slog.Default()
}

// RequestLogger логирует каждый запрос и пробрасывает логгер с request_id дальше.
func RequestLogger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			log := base.With(
				slog.String("request_id", middleware.GetReqID(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			r = r.WithContext(WithLogger(r.Context(), log))

			defer func() {
				attrs := []any{
					slog.Int("status", ww.Status()),
					slog.Int("bytes", ww.BytesWritten()),
					slog.Duration("duration", time.Since(start)),
					slog.String("remote_addr", r.RemoteAddr),
				}
				switch {
				case ww.Status() >= http.StatusInternalServerError:
					log.Error("запрос завершён с ошибкой", attrs...)
				case ww.Status() >= http.StatusBadRequest:
					log.Warn("запрос отклонён", attrs...)
				default:
					log.Info("запрос обработан", attrs...)
				}
			}()

			next.ServeHTTP(ww, r)
		})
	}
}

// Recoverer перехватывает панику в обработчике: логирует стек и отдаёт 500,
// не роняя весь процесс. Паника http.ErrAbortHandler пробрасывается дальше —
// это штатный способ прервать соединение.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if err, ok := rec.(error); ok && err == http.ErrAbortHandler {
				panic(rec)
			}

			Logger(r.Context()).Error("паника в обработчике",
				slog.Any("panic", rec),
				slog.String("stack", string(debug.Stack())),
			)

			// Если что-то уже записано в ответ, менять статус поздно.
			if ww, ok := w.(middleware.WrapResponseWriter); ok && ww.Status() != 0 {
				return
			}
			JSON(w, http.StatusInternalServerError, Envelope{Error: &ErrorPayload{
				Code:      "internal_error",
				Message:   "Внутренняя ошибка сервера",
				RequestID: middleware.GetReqID(r.Context()),
			}})
		}()

		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders выставляет базовые защитные заголовки для HTML-страниц.
// CSP намеренно строгая: inline-скрипты запрещены.
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy",
				"default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Handler — обработчик, возвращающий ошибку. Избавляет от копипасты
// «залогировать и ответить» в каждом хендлере.
type Handler func(w http.ResponseWriter, r *http.Request) error

// Wrap приводит Handler к http.HandlerFunc, отправляя ошибку через Fail.
func Wrap(h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			Fail(w, r, err)
		}
	}
}
