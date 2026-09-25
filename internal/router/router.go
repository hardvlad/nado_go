// Package router собирает дерево маршрутов и цепочку middleware.
package router

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"nado_go/internal/config"
	"nado_go/internal/handler/api"
	"nado_go/internal/handler/jobsapi"
	"nado_go/internal/handler/web"
	"nado_go/internal/handler/webhook"
	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
)

// Deps — всё, что нужно роутеру. Явные зависимости вместо глобальных
// переменных: состав маршрутов виден по сигнатуре.
type Deps struct {
	Config          *config.Config
	Logger          *slog.Logger
	Static          fs.FS
	I18n            *i18n.Bundle
	Pages           *web.PageHandler
	Users           *api.UserHandler
	Health          *api.HealthHandler
	JobsAPI         *jobsapi.Handler
	GreenAPIWebhook *webhook.GreenAPI
}

// New возвращает корневой http.Handler приложения.
func New(d Deps) http.Handler {
	r := chi.NewRouter()

	// Порядок middleware важен: RequestID должен идти первым (его значение
	// попадает в логи), RequestLogger — раньше Recoverer, чтобы паника
	// логировалась с request_id и попала в лог завершения запроса.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(httpx.RequestLogger(d.Logger))
	r.Use(httpx.Recoverer)
	r.Use(middleware.CleanPath)
	r.Use(middleware.StripSlashes)
	r.Use(middleware.Compress(5))
	// Timeout отменяет контекст запроса по дедлайну: SQL-запросы, читающие
	// ctx, прерываются сами и не держат соединение из пула.
	r.Use(middleware.Timeout(d.Config.HTTP.RequestTimeout))

	// Пробы вне цепочки авторизации и без строгих заголовков — они должны
	// отвечать даже когда приложение деградировало.
	r.Get("/healthz", d.Health.Live)
	r.Get("/readyz", d.Health.Ready)

	mountStatic(r, d.Static)
	mountAPI(r, d)

	// API раздачи заданий удалённым воркерам: отдельный контур, без CORS и CSRF,
	// аутентификация по токену воркера внутри самого обработчика.
	if d.JobsAPI != nil {
		r.Mount("/jobs-api/v1", d.JobsAPI.Routes())
	}

	// Вебхуки провайдеров: без CSRF, аутентификация по токену в самом обработчике.
	if d.GreenAPIWebhook != nil {
		r.Mount("/webhooks/greenapi", d.GreenAPIWebhook.Routes())
	}

	// NotFound/MethodNotAllowed задаются до Mount: chi передаёт их
	// вложенным роутерам, у которых нет собственных обработчиков.
	r.NotFound(d.Pages.NotFound)
	r.MethodNotAllowed(d.Pages.MethodNotAllowed)

	// HTML-страницы: строгие security-заголовки только здесь — для JSON API
	// CSP смысла не имеет.
	r.Group(func(r chi.Router) {
		r.Use(httpx.SecurityHeaders(d.Config.IsProduction()))

		// Защита форм от межсайтовой подделки запросов (CSRF): POST с чужого
		// сайта — и с поддоменов магазинов *.nado.kz — отклоняется по
		// заголовкам Sec-Fetch-Site / Origin. Вместе с SameSite=Lax у cookie
		// сессии этого достаточно, отдельные токены в формах не нужны.
		cop := http.NewCrossOriginProtection()
		cop.SetDenyHandler(http.HandlerFunc(d.Pages.Forbidden))
		r.Use(cop.Handler)

		r.Use(d.Pages.Session)

		// Одни и те же страницы на каждом языке: /, /kk, /en.
		for _, lang := range i18n.Supported() {
			prefix := lang.Prefix()
			if prefix == "" {
				prefix = "/"
			}
			r.With(d.I18n.Middleware(lang)).Mount(prefix, d.Pages.Routes())
		}
	})

	return r
}

// mountAPI монтирует версионированное JSON API.
func mountAPI(r chi.Router, d Deps) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   d.Config.HTTP.AllowedOrigins,
			AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-Id"},
			ExposedHeaders:   []string{"Link", "X-Request-Id"},
			AllowCredentials: false,
			MaxAge:           300,
		}))
		r.Use(middleware.AllowContentType("application/json"))

		// Ошибки маршрутизации внутри API должны быть JSON, а не HTML-страницей.
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			httpx.Fail(w, r, httpx.ErrNotFound("Ресурс не найден"))
		})
		r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			httpx.Fail(w, r, httpx.NewError(http.StatusMethodNotAllowed, "method_not_allowed",
				"Метод не поддерживается для этого ресурса"))
		})

		// Учебный CRUD пользователей из скелета не защищён авторизацией, а в
		// таблице users теперь живые продавцы. Поэтому в проде он выключен;
		// локально остаётся как пример ресурса API.
		if !d.Config.IsProduction() {
			r.Mount("/users", d.Users.Routes())
		}
	})
}

// mountStatic отдаёт CSS/JS/картинки из встроенной файловой системы.
func mountStatic(r chi.Router, static fs.FS) {
	if static == nil {
		return
	}

	fileServer := http.StripPrefix("/static/", http.FileServerFS(static))

	r.Handle("/static/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Статика неизменна между сборками — отдаём с длинным кешем.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fileServer.ServeHTTP(w, req)
	}))
}
