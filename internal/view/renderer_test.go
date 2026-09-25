package view_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"nado_go/internal/view"
	webassets "nado_go/web"
)

// Движок проверяется на маленьком наборе шаблонов-фикстур: так тесты не
// ломаются от правок вёрстки сайта. Шаблоны проекта проверяет
// TestProjectTemplatesCompile, а их вывод — тесты роутера.
func fixtures() fstest.MapFS {
	return fstest.MapFS{
		"layouts/base.gohtml": {Data: []byte(
			`<html><body class="base">{{ template "header" . }}{{ block "content" . }}{{ end }}</body></html>`)},
		"layouts/minimal.gohtml": {Data: []byte(
			`<html><body class="minimal">{{ block "content" . }}{{ end }}</body></html>`)},
		"partials/header.gohtml": {Data: []byte(
			`{{ define "header" }}<header>{{ .AppName }}</header>{{ end }}`)},
		"pages/home.gohtml": {Data: []byte(
			`{{ define "content" }}<h1>{{ .Heading }}</h1>{{ range .Items }}<i>{{ . }}</i>{{ end }}<p>{{ .Search }}</p>{{ end }}`)},
	}
}

func newRenderer(t *testing.T) *view.Renderer {
	t.Helper()
	r, err := view.New(fixtures(), view.WithGlobals(map[string]any{"AppName": "nado"}))
	if err != nil {
		t.Fatalf("компиляция шаблонов: %v", err)
	}
	return r
}

func TestRenderPage(t *testing.T) {
	r := newRenderer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	data := view.NewData(req, "Главная").With("Heading", "Заголовок").With("Items", []string{"первый"})
	if err := r.Render(rec, http.StatusOK, "home", data); err != nil {
		t.Fatalf("рендеринг: %v", err)
	}

	body := rec.Body.String()
	for _, want := range []string{"<h1>Заголовок</h1>", "<i>первый</i>", "<header>nado</header>", `class="base"`} {
		if !strings.Contains(body, want) {
			t.Errorf("в выводе нет %q:\n%s", want, body)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("неожиданный Content-Type: %q", ct)
	}
}

func TestRenderEscapesUserInput(t *testing.T) {
	r := newRenderer(t)
	rec := httptest.NewRecorder()

	data := view.Data{"Search": `<script>alert(1)</script>`}
	if err := r.Render(rec, http.StatusOK, "home", data); err != nil {
		t.Fatalf("рендеринг: %v", err)
	}
	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Error("пользовательский ввод попал в разметку без экранирования")
	}
}

func TestRenderAlternativeLayout(t *testing.T) {
	r := newRenderer(t)
	rec := httptest.NewRecorder()

	if err := r.RenderLayout(rec, http.StatusNotFound, "minimal", "home", view.Data{"Heading": "404"}); err != nil {
		t.Fatalf("рендеринг: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="minimal"`) || strings.Contains(body, "<header>") {
		t.Errorf("использован не тот layout:\n%s", body)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("статус = %d", rec.Code)
	}
}

func TestRenderUnknownPage(t *testing.T) {
	r := newRenderer(t)
	if err := r.Render(httptest.NewRecorder(), http.StatusOK, "no-such-page", view.Data{}); err == nil {
		t.Fatal("ожидалась ошибка для несуществующей страницы")
	}
}

func TestWarmupFailsOnBrokenTemplate(t *testing.T) {
	fsys := fixtures()
	fsys["pages/broken.gohtml"] = &fstest.MapFile{Data: []byte(`{{ define "content" }}{{ if }}{{ end }}`)}
	if _, err := view.New(fsys); err == nil {
		t.Fatal("сломанный шаблон должен ронять старт, а не первый запрос")
	}
}

// Все страницы проекта компилируются со всеми layout-ами.
func TestProjectTemplatesCompile(t *testing.T) {
	fsys, err := webassets.Templates(false)
	if err != nil {
		t.Fatalf("шаблоны недоступны: %v", err)
	}
	if _, err := view.New(fsys); err != nil {
		t.Fatalf("компиляция шаблонов проекта: %v", err)
	}
}
