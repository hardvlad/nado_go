// Package web — обработчики, отдающие HTML-страницы.
package web

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"nado_go/internal/httpx"
	"nado_go/internal/model"
	"nado_go/internal/service"
	"nado_go/internal/view"
)

const (
	defaultPerPage = 20
	maxPerPage     = 50
)

type PageHandler struct {
	render *view.Renderer
	users  *service.UserService
}

func NewPageHandler(render *view.Renderer, users *service.UserService) *PageHandler {
	return &PageHandler{render: render, users: users}
}

// Routes — маршруты HTML-страниц.
func (h *PageHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", httpx.Wrap(h.Home))
	r.Get("/users", httpx.Wrap(h.Users))
	r.Get("/users/{id}", httpx.Wrap(h.UserDetails))
	return r
}

// Home — GET /
func (h *PageHandler) Home(w http.ResponseWriter, r *http.Request) error {
	data := view.NewData(r, "Главная").
		With("Heading", "Каркас приложения готов").
		With("Features", []string{
			"Graceful shutdown с дренированием запросов",
			"Пул соединений к Microsoft SQL Server",
			"Параметризованные запросы и транзакции",
			"Роутинг API и страниц на chi",
			"Шаблоны с layout-ами и partial-ами",
		})

	return h.render.Render(w, http.StatusOK, "home", data)
}

// Users — GET /users: список с постраничной навигацией.
func (h *PageHandler) Users(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePagination(r, defaultPerPage, maxPerPage)
	search := httpx.QueryString(r, "search", "")

	users, total, err := h.users.List(r.Context(), model.UserFilter{
		Search: search,
		Status: httpx.QueryString(r, "status", ""),
		Limit:  p.Limit(),
		Offset: p.Offset(),
	})
	if err != nil {
		return err
	}

	// Число страниц считаем здесь: в шаблоне нет арифметики над int64.
	totalPages := 0
	if p.PerPage > 0 {
		totalPages = int((total + int64(p.PerPage) - 1) / int64(p.PerPage))
	}

	data := view.NewData(r, "Пользователи").
		With("Users", users).
		With("Total", total).
		With("Page", p.Page).
		With("PerPage", p.PerPage).
		With("TotalPages", totalPages).
		With("Search", search)

	return h.render.Render(w, http.StatusOK, "users", data)
}

// UserDetails — GET /users/{id}
func (h *PageHandler) UserDetails(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		return err
	}

	user, err := h.users.Get(r.Context(), id)
	if err != nil {
		return err
	}

	data := view.NewData(r, user.Name).With("User", user)
	return h.render.Render(w, http.StatusOK, "user", data)
}

// NotFound — обработчик 404 для страниц.
func (h *PageHandler) NotFound(w http.ResponseWriter, r *http.Request) {
	h.renderError(w, r, http.StatusNotFound, "Страница не найдена",
		"Адрес "+r.URL.Path+" не существует.")
}

// MethodNotAllowed — обработчик 405 для страниц.
func (h *PageHandler) MethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	h.renderError(w, r, http.StatusMethodNotAllowed, "Метод не поддерживается",
		"Этот адрес не принимает запросы "+r.Method+".")
}

// renderError отдаёт страницу ошибки. Если сам шаблон ошибки сломан,
// откатываемся на текстовый ответ — пользователь не должен получить пустоту.
func (h *PageHandler) renderError(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	data := view.NewData(r, title).
		With("Status", status).
		With("Heading", title).
		With("Message", message)

	if err := h.render.Render(w, status, "error", data); err != nil {
		httpx.Logger(r.Context()).Error("не удалось отрендерить страницу ошибки", slog.Any("error", err))
		httpx.String(w, status, "%d %s", status, title)
	}
}
