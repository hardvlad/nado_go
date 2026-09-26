// Package theme рендерит страницы витрины из слоёв тем: базовая тема web/themes/base
// плюс переопределения темы магазина. Одноимённый шаблон из темы магазина
// перекрывает базовый (D-16).
package theme

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"nado_go/internal/money"
)

const (
	baseTheme     = "base"
	defaultLayout = "base"
	ext           = ".gohtml"
)

// Renderer компилирует и кеширует шаблоны тем. Базовая тема прогревается при
// старте (ошибка шаблона валит приложение сразу). В debug шаблоны перечитываются.
type Renderer struct {
	fsys  fs.FS // корень web/themes (подкаталог на тему)
	funcs template.FuncMap
	debug bool

	mu    sync.RWMutex
	cache map[string]*template.Template
}

type Option func(*Renderer)

func WithDebug(debug bool) Option { return func(r *Renderer) { r.debug = debug } }

// New создаёт рендер поверх ФС тем и прогревает базовую тему.
func New(fsys fs.FS, opts ...Option) (*Renderer, error) {
	r := &Renderer{fsys: fsys, funcs: builtinFuncs(), cache: map[string]*template.Template{}}
	for _, opt := range opts {
		opt(r)
	}
	if err := r.warmup(baseTheme); err != nil {
		return nil, err
	}
	return r, nil
}

// warmup компилирует все страницы темы со всеми её layout-ами.
func (r *Renderer) warmup(theme string) error {
	pages, err := fs.Glob(r.fsys, path.Join(baseTheme, "pages", "*"+ext))
	if err != nil {
		return fmt.Errorf("theme: обход страниц: %w", err)
	}
	if len(pages) == 0 {
		return fmt.Errorf("theme: не найдено ни одной страницы в %s/pages", baseTheme)
	}
	for _, p := range pages {
		name := strings.TrimSuffix(path.Base(p), ext)
		if _, err := r.compile(theme, defaultLayout, name); err != nil {
			return err
		}
	}
	return nil
}

// Render рендерит страницу темы в layout по умолчанию.
func (r *Renderer) Render(w http.ResponseWriter, status int, theme, page string, data map[string]any) error {
	tmpl, err := r.compile(theme, defaultLayout, page)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, defaultLayout+ext, data); err != nil {
		return fmt.Errorf("theme: рендеринг страницы %q: %w", page, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err = buf.WriteTo(w)
	return err
}

// RenderPartial рендерит именованный блок (для htmx-ответов).
func (r *Renderer) RenderPartial(w http.ResponseWriter, status int, theme, page, block string, data map[string]any) error {
	tmpl, err := r.compile(theme, defaultLayout, page)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, block, data); err != nil {
		return fmt.Errorf("theme: рендеринг блока %q: %w", block, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err = buf.WriteTo(w)
	return err
}

// compile собирает шаблоны для темы: базовый слой, затем переопределения темы.
func (r *Renderer) compile(theme, layout, page string) (*template.Template, error) {
	key := theme + ":" + layout + ":" + page
	if !r.debug {
		r.mu.RLock()
		t, ok := r.cache[key]
		r.mu.RUnlock()
		if ok {
			return t, nil
		}
	}

	tmpl := template.New(layout + ext).Funcs(r.funcs)

	// Слои: сначала базовая тема, затем тема магазина (перекрывает одноимённые).
	layers := []string{baseTheme}
	if theme != "" && theme != baseTheme {
		layers = append(layers, theme)
	}

	// Layout.
	for _, l := range layers {
		f := path.Join(l, "layouts", layout+ext)
		if exists(r.fsys, f) {
			if _, err := tmpl.ParseFS(r.fsys, f); err != nil {
				return nil, fmt.Errorf("theme: layout %q темы %q: %w", layout, l, err)
			}
		}
	}
	// Partials.
	for _, l := range layers {
		glob := path.Join(l, "partials", "*"+ext)
		if matches, _ := fs.Glob(r.fsys, glob); len(matches) > 0 {
			if _, err := tmpl.ParseFS(r.fsys, glob); err != nil {
				return nil, fmt.Errorf("theme: partials темы %q: %w", l, err)
			}
		}
	}
	// Страница.
	pageParsed := false
	for _, l := range layers {
		f := path.Join(l, "pages", page+ext)
		if exists(r.fsys, f) {
			if _, err := tmpl.ParseFS(r.fsys, f); err != nil {
				return nil, fmt.Errorf("theme: страница %q темы %q: %w", page, l, err)
			}
			pageParsed = true
		}
	}
	if !pageParsed {
		return nil, fmt.Errorf("theme: страница %q не найдена", page)
	}

	if !r.debug {
		r.mu.Lock()
		r.cache[key] = tmpl
		r.mu.Unlock()
	}
	return tmpl, nil
}

func exists(fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// builtinFuncs — функции шаблонов витрины.
func builtinFuncs() template.FuncMap {
	return template.FuncMap{
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict: нечётное число аргументов")
			}
			m := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				k, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: ключ не строка")
				}
				m[k] = values[i+1]
			}
			return m, nil
		},
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"money": func(minor int64, currency, lang string) string {
			return money.Money{Minor: minor, Currency: money.Currency(currency)}.Format(lang)
		},
		"default": func(def, value any) any {
			if value == nil || value == "" {
				return def
			}
			return value
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i + 1
			}
			return s
		},
		"now":  time.Now,
		"year": func() int { return time.Now().Year() },
	}
}
