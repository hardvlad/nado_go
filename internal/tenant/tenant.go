// Package tenant — кто выполняет запрос: пользователь и его аккаунт (D-11).
//
// Middleware сессии кладёт Principal в контекст, handler достаёт его через
// FromContext и явно передаёт accountID в сервисы. Брать аккаунт «из
// контекста где-нибудь в репозитории» нельзя: пропущенный фильтр по
// account_id должен быть виден в сигнатуре.
package tenant

import (
	"context"

	"nado_go/internal/model"
)

type ctxKey struct{}

// WithPrincipal кладёт вошедшего пользователя в контекст.
func WithPrincipal(ctx context.Context, p *model.Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext возвращает вошедшего пользователя или nil для анонимного запроса.
func FromContext(ctx context.Context) *model.Principal {
	p, _ := ctx.Value(ctxKey{}).(*model.Principal)
	return p
}
