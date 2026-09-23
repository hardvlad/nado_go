// Package view — рендеринг HTML-страниц из файлов шаблонов.
//
// Схема сборки страницы:
//
//	layouts/base.gohtml    — каркас: <html>, <head>, общая разметка
//	partials/*.gohtml      — переиспользуемые блоки (header, footer, alert, ...)
//	pages/<name>.gohtml    — конкретная страница, определяет {{define "content"}}
//
// Каждая страница компилируется в отдельный набор шаблонов вместе со всеми
// layout-ами и partial-ами, поэтому из страницы доступны любые {{template ...}}
// из других файлов. В data передаются переменные для подстановки.
package view

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
)

const (
	// DefaultLayout — layout, используемый, если не указан другой.
	DefaultLayout = "base"

	layoutsGlob  = "layouts/*.gohtml"
	partialsGlob = "partials/*.gohtml"
	layoutsDir   = "layouts"
	pagesDir     = "pages"
	ext          = ".gohtml"
)

// Renderer компилирует и кеширует шаблоны.
//
// В production все страницы парсятся один раз при старте (Warmup): ошибка в
// шаблоне валит приложение сразу, а не на запросе пользователя. В debug-режиме
// шаблоны перечитываются с диска на каждый рендер — правки видны без пересборки.
type Renderer struct {
	fsys  fs.FS
	funcs template.FuncMap
	debug bool

	mu    sync.RWMutex
	cache map[string]*template.Template

	// globals — переменные, доступные на каждой странице (имя приложения и т.п.).
	globals map[string]any
}

// Option настраивает Renderer при создании.
type Option func(*Renderer)

// WithDebug включает перечитывание шаблонов с диска на каждый запрос.
func WithDebug(debug bool) Option { return func(r *Renderer) { r.debug = debug } }

// WithFuncs добавляет пользовательские функции в шаблоны.
func WithFuncs(funcs template.FuncMap) Option {
	return func(r *Renderer) {
		for name, fn := range funcs {
			r.funcs[name] = fn
		}
	}
}

// WithGlobals задаёт переменные, подставляемые в каждую страницу.
func WithGlobals(globals map[string]any) Option {
	return func(r *Renderer) {
		for k, v := range globals {
			r.globals[k] = v
		}
	}
}

// New создаёт Renderer поверх файловой системы с шаблонами.
// fsys — корень каталога templates (embed.FS в проде, os.DirFS в разработке).
func New(fsys fs.FS, opts ...Option) (*Renderer, error) {
	r := &Renderer{
		fsys:    fsys,
		funcs:   builtinFuncs(),
		cache:   make(map[string]*template.Template),
		globals: make(map[string]any),
	}
	for _, opt := range opts {
		opt(r)
	}

	if err := r.Warmup(); err != nil {
		return nil, err
	}
	return r, nil
}

// Warmup компилирует все сочетания layout+страница и проверяет их корректность.
func (r *Renderer) Warmup() error {
	pages, err := fs.Glob(r.fsys, path.Join(pagesDir, "*"+ext))
	if err != nil {
		return fmt.Errorf("view: обход каталога страниц: %w", err)
	}
	if len(pages) == 0 {
		return fmt.Errorf("view: не найдено ни одной страницы в %s/", pagesDir)
	}

	layouts, err := fs.Glob(r.fsys, layoutsGlob)
	if err != nil {
		return fmt.Errorf("view: обход каталога layouts: %w", err)
	}
	if len(layouts) == 0 {
		return fmt.Errorf("view: не найдено ни одного layout в %s/", layoutsDir)
	}

	for _, page := range pages {
		pageName := strings.TrimSuffix(path.Base(page), ext)
		for _, layout := range layouts {
			layoutName := strings.TrimSuffix(path.Base(layout), ext)
			if _, err := r.compile(layoutName, pageName); err != nil {
				return err
			}
		}
	}
	return nil
}

// Render рендерит страницу pages/<page>.gohtml в layout по умолчанию.
//
// Шаблон сначала выполняется в буфер: если рендеринг упадёт на середине,
// клиент не получит наполовину сформированную страницу со статусом 200.
func (r *Renderer) Render(w http.ResponseWriter, status int, page string, data Data) error {
	return r.RenderLayout(w, status, DefaultLayout, page, data)
}

// RenderLayout — то же, что Render, но с явным выбором layout-а
// (например "minimal" для страницы входа).
func (r *Renderer) RenderLayout(w http.ResponseWriter, status int, layout, page string, data Data) error {
	tmpl, err := r.compile(layout, page)
	if err != nil {
		return err
	}

	if data == nil {
		data = Data{}
	}
	for k, v := range r.globals {
		if _, exists := data[k]; !exists {
			data[k] = v
		}
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, layout+ext, data); err != nil {
		return fmt.Errorf("view: рендеринг страницы %q: %w", page, err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err = buf.WriteTo(w)
	return err
}

// RenderPartial рендерит один именованный блок — для htmx/AJAX-ответов,
// обновляющих фрагмент страницы без перезагрузки.
func (r *Renderer) RenderPartial(w http.ResponseWriter, status int, page, block string, data Data) error {
	tmpl, err := r.compile(DefaultLayout, page)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, block, data); err != nil {
		return fmt.Errorf("view: рендеринг блока %q: %w", block, err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err = buf.WriteTo(w)
	return err
}

// compile возвращает готовый набор шаблонов для пары layout+page,
// используя кеш (в debug-режиме кеш игнорируется).
func (r *Renderer) compile(layout, page string) (*template.Template, error) {
	key := layout + ":" + page

	if !r.debug {
		r.mu.RLock()
		tmpl, ok := r.cache[key]
		r.mu.RUnlock()
		if ok {
			return tmpl, nil
		}
	}

	layoutFile := path.Join(layoutsDir, layout+ext)
	pageFile := path.Join(pagesDir, page+ext)

	// Порядок важен: layout парсится первым и становится корневым шаблоном,
	// страница переопределяет объявленные в нём блоки ({{define "content"}}).
	tmpl, err := template.New(layout+ext).Funcs(r.funcs).ParseFS(r.fsys, layoutFile)
	if err != nil {
		return nil, fmt.Errorf("view: layout %q: %w", layout, err)
	}
	if _, err := tmpl.ParseFS(r.fsys, partialsGlob); err != nil {
		return nil, fmt.Errorf("view: partials: %w", err)
	}
	if _, err := tmpl.ParseFS(r.fsys, pageFile); err != nil {
		return nil, fmt.Errorf("view: страница %q: %w", page, err)
	}

	if !r.debug {
		r.mu.Lock()
		r.cache[key] = tmpl
		r.mu.Unlock()
	}
	return tmpl, nil
}

// builtinFuncs — функции, доступные во всех шаблонах.
func builtinFuncs() template.FuncMap {
	return template.FuncMap{
		// dict собирает map прямо в шаблоне — так в partial можно передать
		// несколько переменных: {{template "alert" dict "Kind" "error" "Text" .Err}}
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict: ожидается чётное число аргументов")
			}
			m := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: ключ %v не строка", values[i])
				}
				m[key] = values[i+1]
			}
			return m, nil
		},
		// safeHTML отключает экранирование. Применять только к доверенному
		// содержимому — иначе это прямой путь к XSS.
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"safeAttr": func(s string) template.HTMLAttr { return template.HTMLAttr(s) },
		"default": func(def, value any) any {
			if value == nil || value == "" {
				return def
			}
			return value
		},
		"formatDate": func(layout string, t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format(layout)
		},
		"now":   time.Now,
		"year":  func() int { return time.Now().Year() },
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"add":   func(a, b int) int { return a + b },
		"sub":   func(a, b int) int { return a - b },
	}
}
