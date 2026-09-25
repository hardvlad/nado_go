// Package web — HTML-страницы: лендинг nado, обратная связь, регистрация и
// вход продавца, заготовка кабинета.
//
// Страницы доступны на трёх языках: без префикса — русский, /kk — казахский,
// /en — английский (Routes монтируется под каждый префикс). У каждой страницы
// есть светлая и тёмная тема (D-21): режим хранится в cookie nado_theme,
// сервер сразу рендерит нужный data-theme, чтобы страница не мигала.
package web

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/service"
	"nado_go/internal/tenant"
	"nado_go/internal/view"
	webassets "nado_go/web"
)

// Deps — зависимости страниц.
type Deps struct {
	Render   *view.Renderer
	I18n     *i18n.Bundle
	Plans    *service.PlanCatalog
	Auth     *service.AuthService
	Feedback *service.FeedbackService
	// SecureCookies — cookie только по HTTPS и с префиксом __Host-.
	// В проде обязательно, локально по http — выключено.
	SecureCookies bool
	// PublicURL — адрес сайта для canonical и hreflang, например https://nado.kz.
	// Пусто — ссылки относительные.
	PublicURL string
}

type PageHandler struct {
	Deps

	loginByIP    *httpx.RateLimiter
	loginByEmail *httpx.RateLimiter
	registerByIP *httpx.RateLimiter
	feedbackByIP *httpx.RateLimiter
}

func NewPageHandler(d Deps) *PageHandler {
	return &PageHandler{
		Deps: d,
		// Лимиты подобраны так, чтобы человек с опечатками их не замечал,
		// а перебор паролей и спам упирались сразу.
		loginByIP:    httpx.NewRateLimiter(20, 10*time.Minute),
		loginByEmail: httpx.NewRateLimiter(10, 15*time.Minute),
		registerByIP: httpx.NewRateLimiter(10, time.Hour),
		feedbackByIP: httpx.NewRateLimiter(5, 10*time.Minute),
	}
}

// Routes — страницы одного языка. Язык выставляет middleware i18n при монтировании.
func (h *PageHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.wrap(h.Home))
	r.Get("/contact", h.wrap(h.ContactForm))
	r.Post("/contact", h.wrap(h.ContactSubmit))
	r.Get("/register", h.wrap(h.RegisterForm))
	r.Post("/register", h.wrap(h.RegisterSubmit))
	r.Get("/login", h.wrap(h.LoginForm))
	r.Post("/login", h.wrap(h.LoginSubmit))
	r.Post("/logout", h.wrap(h.Logout))
	r.Get("/account", h.wrap(h.Account))
	return r
}

// wrap превращает ошибку обработчика в HTML-страницу ошибки, а не в JSON:
// пользователь лендинга не должен увидеть {"error": ...}.
func (h *PageHandler) wrap(fn httpx.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			h.fail(w, r, err)
		}
	}
}

func (h *PageHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	appErr := httpx.AsError(err)
	log := httpx.Logger(r.Context())
	if appErr.Status >= http.StatusInternalServerError {
		log.Error("ошибка страницы", slog.Any("error", err))
	} else {
		log.Warn("запрос страницы отклонён", slog.Any("error", err))
	}

	code := "500"
	switch appErr.Status {
	case http.StatusNotFound:
		code = "404"
	case http.StatusForbidden:
		code = "403"
	case http.StatusTooManyRequests:
		code = "429"
	}
	h.renderError(w, r, appErr.Status, code)
}

// NotFound — 404 для страниц.
func (h *PageHandler) NotFound(w http.ResponseWriter, r *http.Request) {
	h.renderError(w, r, http.StatusNotFound, "404")
}

// MethodNotAllowed — 405 для страниц.
func (h *PageHandler) MethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	h.renderError(w, r, http.StatusMethodNotAllowed, "405")
}

// Forbidden — отказ защиты от межсайтовых запросов (CSRF).
func (h *PageHandler) Forbidden(w http.ResponseWriter, r *http.Request) {
	h.renderError(w, r, http.StatusForbidden, "403")
}

// renderError отдаёт страницу ошибки. Если сам шаблон ошибки сломан,
// откатываемся на текстовый ответ — пользователь не должен получить пустоту.
func (h *PageHandler) renderError(w http.ResponseWriter, r *http.Request, status int, code string) {
	r = h.ensureLocalizer(r)
	data := h.page(r, "error."+code+".title").
		With("Status", status).
		With("Message", i18n.FromContext(r.Context()).T("error."+code+".text"))

	if err := h.Render.Render(w, status, "error", data); err != nil {
		httpx.Logger(r.Context()).Error("не удалось отрендерить страницу ошибки", slog.Any("error", err))
		httpx.String(w, status, "%d", status)
	}
}

// ensureLocalizer определяет язык по пути, если запрос не прошёл через
// языковой middleware (404 вне маршрутов, отказ CSRF-защиты).
func (h *PageHandler) ensureLocalizer(r *http.Request) *http.Request {
	if i18n.FromContext(r.Context()) != nil {
		return r
	}
	lang, _ := i18n.SplitPath(r.URL.Path)
	return r.WithContext(i18n.WithLocalizer(r.Context(), h.I18n.Localizer(lang)))
}

// Links — адреса страниц на текущем языке.
type Links struct {
	Home, Contact, Register, Login, Logout, Account string
}

func linksFor(lang i18n.Lang) Links {
	return Links{
		Home:     i18n.Localize(lang, "/"),
		Contact:  i18n.Localize(lang, "/contact"),
		Register: i18n.Localize(lang, "/register"),
		Login:    i18n.Localize(lang, "/login"),
		Logout:   i18n.Localize(lang, "/logout"),
		Account:  i18n.Localize(lang, "/account"),
	}
}

// LangLink — пункт переключателя языка: та же страница на другом языке.
type LangLink struct {
	Lang    i18n.Lang
	Name    string
	Short   string
	URL     string
	Current bool
}

// Alternate — ссылка hreflang для поисковиков.
type Alternate struct {
	Lang string
	URL  string
}

// ThemeMeta — цвета строки состояния для двух мета-тегов theme-color.
type ThemeMeta struct {
	Mode       string // system | light | dark
	Light      string
	Dark       string
	LightMedia string // что показать при системной светлой теме
	DarkMedia  string // что показать при системной тёмной теме
}

func themeMeta(r *http.Request) ThemeMeta {
	mode := "system"
	if c, err := r.Cookie("nado_theme"); err == nil && (c.Value == "light" || c.Value == "dark") {
		mode = c.Value
	}
	m := ThemeMeta{
		Mode: mode, Light: webassets.ThemeColorLight, Dark: webassets.ThemeColorDark,
		LightMedia: webassets.ThemeColorLight, DarkMedia: webassets.ThemeColorDark,
	}
	switch mode {
	case "light":
		m.DarkMedia = m.Light
	case "dark":
		m.LightMedia = m.Dark
	}
	return m
}

// page собирает переменные, общие для всех страниц сайта.
func (h *PageHandler) page(r *http.Request, titleKey string) view.Data {
	l := h.localizer(r)
	lang := l.Lang()
	_, basePath := i18n.SplitPath(r.URL.Path)

	langLinks := make([]LangLink, 0, len(i18n.Supported()))
	alternates := make([]Alternate, 0, len(i18n.Supported()))
	for _, other := range i18n.Supported() {
		u := i18n.Localize(other, basePath)
		langLinks = append(langLinks, LangLink{
			Lang: other, Name: other.Name(), Short: other.Short(),
			URL: withQuery(u, r.URL.RawQuery), Current: other == lang,
		})
		alternates = append(alternates, Alternate{Lang: string(other), URL: h.PublicURL + u})
	}

	return view.NewData(r, l.T(titleKey)).
		With("L", l).
		With("Lang", string(lang)).
		With("Description", l.T("meta.description")).
		With("Theme", themeMeta(r)).
		With("Links", linksFor(lang)).
		With("LangLinks", langLinks).
		With("CurrentLang", langLinks[indexOf(lang)]).
		With("Alternates", alternates).
		With("XDefault", h.PublicURL+i18n.Localize(i18n.Default, basePath)).
		With("Canonical", h.PublicURL+i18n.Localize(lang, basePath)).
		With("Principal", tenant.FromContext(r.Context())).
		With("Year", time.Now().Year())
}

func indexOf(lang i18n.Lang) int {
	for i, l := range i18n.Supported() {
		if l == lang {
			return i
		}
	}
	return 0
}

func withQuery(u, rawQuery string) string {
	if rawQuery == "" {
		return u
	}
	return u + "?" + rawQuery
}

// FormView — значения и ошибки формы для повторного показа.
type FormView struct {
	Values  map[string]string
	Checked map[string]bool
	Errors  map[string]string // поле → текст ошибки на языке страницы
	Alert   string            // общая ошибка над формой
}

func newForm() FormView {
	return FormView{Values: map[string]string{}, Checked: map[string]bool{}, Errors: map[string]string{}}
}

// knownValidationTags — правила, для которых есть перевод validation.<тег>.
var knownValidationTags = map[string]bool{
	"required": true, "email": true, "min": true, "max": true, "oneof": true,
	"email_taken": true, "contact": true, "phone": true, "consent": true,
}

// localizeFields переводит нарушенные правила в сообщения на языке страницы.
func localizeFields(l *i18n.Localizer, fields map[string]httpx.FieldError) map[string]string {
	out := make(map[string]string, len(fields))
	for name, fe := range fields {
		key := "validation.default"
		if knownValidationTags[fe.Tag] {
			key = "validation." + fe.Tag
		}
		out[name] = l.T(key, "Param", fe.Param)
	}
	return out
}

// parseForm читает тело формы с ограничением размера.
func parseForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		return httpx.ErrBadRequest("Некорректная форма").WithCause(err)
	}
	return nil
}

// redirect — 303 See Other: после POST браузер переходит по GET и не
// отправит форму повторно при обновлении страницы (Post/Redirect/Get).
func redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// safeNext проверяет адрес возврата после входа: только локальный путь.
// Иначе ссылка вида /login?next=https://evil.kz превращает вход в фишинг.
func safeNext(next string) (string, bool) {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, `\`) {
		return "", false
	}
	u, err := url.Parse(next)
	if err != nil || u.Host != "" || u.Scheme != "" {
		return "", false
	}
	return next, true
}

func clientMeta(r *http.Request) service.ClientMeta {
	return service.ClientMeta{IP: httpx.ClientIP(r), UserAgent: r.UserAgent()}
}
