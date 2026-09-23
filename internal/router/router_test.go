package router_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nado_go/internal/config"
	"nado_go/internal/database"
	"nado_go/internal/handler/api"
	"nado_go/internal/handler/web"
	"nado_go/internal/model"
	"nado_go/internal/router"
	"nado_go/internal/service"
	"nado_go/internal/view"
	webassets "nado_go/web"
)

// fakeStore подменяет репозиторий: маршруты и обработчики тестируются
// без живого SQL Server. Ради этого сервис зависит от интерфейса, а не от
// конкретного репозитория.
type fakeStore struct {
	users []model.User
}

func (f *fakeStore) GetByID(_ context.Context, id int64) (*model.User, error) {
	for i := range f.users {
		if f.users[i].ID == id {
			return &f.users[i], nil
		}
	}
	return nil, database.ErrNotFound
}

func (f *fakeStore) GetByEmail(_ context.Context, email string) (*model.User, error) {
	for i := range f.users {
		if f.users[i].Email == email {
			return &f.users[i], nil
		}
	}
	return nil, database.ErrNotFound
}

func (f *fakeStore) List(_ context.Context, _ model.UserFilter) ([]model.User, int64, error) {
	return f.users, int64(len(f.users)), nil
}

func (f *fakeStore) Create(_ context.Context, u *model.User) (*model.User, error) {
	u.ID = int64(len(f.users) + 1)
	u.CreatedAt, u.UpdatedAt = time.Now(), time.Now()
	f.users = append(f.users, *u)
	return u, nil
}

func (f *fakeStore) Update(_ context.Context, u *model.User) (*model.User, error) { return u, nil }

func (f *fakeStore) Delete(_ context.Context, _ int64) error { return nil }

type fakePinger struct{ err error }

func (f fakePinger) Health(context.Context) error { return f.err }

func newTestServer(t *testing.T) http.Handler {
	t.Helper()

	templates, err := webassets.Templates(false)
	if err != nil {
		t.Fatalf("шаблоны: %v", err)
	}
	renderer, err := view.New(templates, view.WithGlobals(map[string]any{
		"AppName": "nado", "Env": "test", "Version": "test",
	}))
	if err != nil {
		t.Fatalf("рендерер: %v", err)
	}
	static, err := webassets.Static(false)
	if err != nil {
		t.Fatalf("статика: %v", err)
	}

	store := &fakeStore{users: []model.User{{
		ID: 1, Email: "user@example.com", Name: "Тестовый",
		Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}}
	users := service.NewUserService(store)

	cfg := &config.Config{}
	cfg.App.Env = "test"
	cfg.HTTP.RequestTimeout = 5 * time.Second
	cfg.HTTP.AllowedOrigins = []string{"*"}

	return router.New(router.Deps{
		Config: cfg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Static: static,
		Pages:  web.NewPageHandler(renderer, users),
		Users:  api.NewUserHandler(users),
		Health: api.NewHealthHandler("test", map[string]api.Pinger{"database": fakePinger{}}),
	})
}

func TestRoutes(t *testing.T) {
	srv := newTestServer(t)

	cases := []struct {
		name        string
		method      string
		target      string
		body        string
		wantStatus  int
		wantContent string
	}{
		{"главная", http.MethodGet, "/", "", http.StatusOK, "text/html"},
		{"список пользователей", http.MethodGet, "/users", "", http.StatusOK, "text/html"},
		{"карточка пользователя", http.MethodGet, "/users/1", "", http.StatusOK, "text/html"},
		{"несуществующая страница", http.MethodGet, "/nope", "", http.StatusNotFound, "text/html"},
		{"статика", http.MethodGet, "/static/css/app.css", "", http.StatusOK, "text/css"},
		{"liveness", http.MethodGet, "/healthz", "", http.StatusOK, "application/json"},
		{"readiness", http.MethodGet, "/readyz", "", http.StatusOK, "application/json"},
		{"api список", http.MethodGet, "/api/v1/users", "", http.StatusOK, "application/json"},
		{"api карточка", http.MethodGet, "/api/v1/users/1", "", http.StatusOK, "application/json"},
		{"api несуществующий id", http.MethodGet, "/api/v1/users/999", "", http.StatusNotFound, "application/json"},
		{"api неизвестный ресурс", http.MethodGet, "/api/v1/nope", "", http.StatusNotFound, "application/json"},
		{"api невалидный id", http.MethodGet, "/api/v1/users/abc", "", http.StatusBadRequest, "application/json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("статус = %d, ожидался %d (тело: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, tc.wantContent) {
				t.Errorf("Content-Type = %q, ожидался %q", ct, tc.wantContent)
			}
		})
	}
}

func TestCreateUserValidation(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{"email":"не-email","name":"я"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("статус = %d, ожидался 422 (тело: %s)", rec.Code, rec.Body.String())
	}

	var resp struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if resp.Error.Code != "validation_failed" {
		t.Errorf("код ошибки = %q", resp.Error.Code)
	}
	if _, ok := resp.Error.Fields["email"]; !ok {
		t.Errorf("нет описания ошибки по полю email: %v", resp.Error.Fields)
	}
}

func TestCreateUserSuccess(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users",
		strings.NewReader(`{"email":"new@example.com","name":"Новый пользователь"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("статус = %d, ожидался 201 (тело: %s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc == "" {
		t.Error("отсутствует заголовок Location")
	}
}
