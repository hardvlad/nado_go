// Package api — HTTP-обработчики JSON API. Слой отвечает только за разбор
// запроса и сериализацию ответа; вся логика живёт в service.
package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"nado_go/internal/httpx"
	"nado_go/internal/model"
	"nado_go/internal/service"
)

const (
	defaultPerPage = 20
	maxPerPage     = 100
)

type UserHandler struct {
	users *service.UserService
}

func NewUserHandler(users *service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

// Routes описывает подмаршруты /users — router не знает о внутренней структуре.
func (h *UserHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", httpx.Wrap(h.List))
	r.Post("/", httpx.Wrap(h.Create))
	r.Get("/{id}", httpx.Wrap(h.Get))
	r.Put("/{id}", httpx.Wrap(h.Update))
	r.Delete("/{id}", httpx.Wrap(h.Delete))
	return r
}

// List — GET /api/v1/users?page=&per_page=&search=&status=
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePagination(r, defaultPerPage, maxPerPage)

	filter := model.UserFilter{
		Search: httpx.QueryString(r, "search", ""),
		Status: httpx.QueryString(r, "status", ""),
		Limit:  p.Limit(),
		Offset: p.Offset(),
	}

	users, total, err := h.users.List(r.Context(), filter)
	if err != nil {
		return err
	}

	httpx.Paginated(w, users, p.Page, p.PerPage, total)
	return nil
}

// Get — GET /api/v1/users/{id}
func (h *UserHandler) Get(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		return err
	}

	user, err := h.users.Get(r.Context(), id)
	if err != nil {
		return err
	}

	httpx.OK(w, http.StatusOK, user)
	return nil
}

// Create — POST /api/v1/users
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) error {
	var in service.CreateUserInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}

	user, err := h.users.Create(r.Context(), in)
	if err != nil {
		return err
	}

	httpx.Created(w, "/api/v1/users/"+strconv.FormatInt(user.ID, 10), user)
	return nil
}

// Update — PUT /api/v1/users/{id}
func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		return err
	}

	var in service.UpdateUserInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}

	user, err := h.users.Update(r.Context(), id, in)
	if err != nil {
		return err
	}

	httpx.OK(w, http.StatusOK, user)
	return nil
}

// Delete — DELETE /api/v1/users/{id}
func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		return err
	}

	if err := h.users.Delete(r.Context(), id); err != nil {
		return err
	}

	httpx.NoContent(w)
	return nil
}
