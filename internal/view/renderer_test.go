package view_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nado_go/internal/view"
	webassets "nado_go/web"
)

func newRenderer(t *testing.T) *view.Renderer {
	t.Helper()

	fsys, err := webassets.Templates(false)
	if err != nil {
		t.Fatalf("шаблоны недоступны: %v", err)
	}

	// New вызывает Warmup: тест падает, если сломан любой шаблон проекта.
	r, err := view.New(fsys, view.WithGlobals(map[string]any{
		"AppName": "nado",
		"Env":     "test",
		"Version": "test",
	}))
	if err != nil {
		t.Fatalf("компиляция шаблонов: %v", err)
	}
	return r
}

func TestRenderHomePage(t *testing.T) {
	r := newRenderer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	data := view.NewData(req, "Главная").
		With("Heading", "Заголовок страницы").
		With("Features", []string{"Первый пункт"})

	if err := r.Render(rec, http.StatusOK, "home", data); err != nil {
		t.Fatalf("рендеринг: %v", err)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"Заголовок страницы", // переменная страницы
		"Первый пункт",       // элемент среза
		"nado",               // глобальная переменная из WithGlobals
		"site-header",        // partial header
		"site-footer",        // partial footer
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в выводе нет %q", want)
		}
	}

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("неожиданный Content-Type: %q", ct)
	}
}

func TestRenderEscapesUserInput(t *testing.T) {
	r := newRenderer(t)
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rec := httptest.NewRecorder()

	data := view.NewData(req, "Пользователи").
		With("Users", nil).
		With("Search", `<script>alert(1)</script>`).
		With("Page", 1).
		With("TotalPages", 1).
		With("Total", int64(0))

	if err := r.Render(rec, http.StatusOK, "users", data); err != nil {
		t.Fatalf("рендеринг: %v", err)
	}

	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Error("пользовательский ввод попал в разметку без экранирования")
	}
}

func TestRenderAlternativeLayout(t *testing.T) {
	r := newRenderer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	data := view.NewData(req, "Ошибка").
		With("Status", http.StatusNotFound).
		With("Heading", "Страница не найдена").
		With("Message", "Проверьте адрес")

	if err := r.RenderLayout(rec, http.StatusNotFound, "minimal", "error", data); err != nil {
		t.Fatalf("рендеринг: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "page--minimal") {
		t.Error("использован не тот layout")
	}
	if strings.Contains(body, "site-header") {
		t.Error("minimal-layout не должен содержать шапку")
	}
}

func TestRenderUnknownPage(t *testing.T) {
	r := newRenderer(t)
	rec := httptest.NewRecorder()

	if err := r.Render(rec, http.StatusOK, "no-such-page", view.Data{}); err == nil {
		t.Fatal("ожидалась ошибка для несуществующей страницы")
	}
}
