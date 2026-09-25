package i18n

import (
	"context"
	"net/http"
)

type ctxKey struct{}

// WithLocalizer кладёт переводчик в контекст запроса.
func WithLocalizer(ctx context.Context, l *Localizer) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext достаёт переводчик. Если его нет (запрос не прошёл через
// Middleware), возвращается nil: методы Localizer безопасны для nil и отдают
// ключ вместо текста.
func FromContext(ctx context.Context) *Localizer {
	l, _ := ctx.Value(ctxKey{}).(*Localizer)
	return l
}

// Middleware выставляет язык для всех маршрутов, смонтированных под его префиксом.
func (b *Bundle) Middleware(l Lang) func(http.Handler) http.Handler {
	loc := b.Localizer(l)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithLocalizer(r.Context(), loc)))
		})
	}
}
