package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/service"
	"nado_go/internal/tenant"
)

// sessionCookieName — cookie с токеном сессии кабинета. Префикс __Host-
// запрещает ставить cookie с поддоменов: витрина продавца на *.nado.kz не
// сможет подменить сессию на nado.kz.
func (h *PageHandler) sessionCookieName() string {
	if h.SecureCookies {
		return "__Host-nado_session"
	}
	return "nado_session"
}

func (h *PageHandler) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.sessionCookieName(),
		Value:    token,
		Path:     "/",
		MaxAge:   int(service.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *PageHandler) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.sessionCookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// Session — middleware: по cookie находит вошедшего пользователя и кладёт
// его в контекст. Сбой БД не ломает публичные страницы: запрос продолжается
// как анонимный, а ошибка уходит в лог.
func (h *PageHandler) Session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(h.sessionCookieName())
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}

		p, err := h.Auth.Authenticate(r.Context(), c.Value)
		switch {
		case err == nil:
			r = r.WithContext(tenant.WithPrincipal(r.Context(), p))
		case errors.Is(err, service.ErrNoSession):
			h.clearSession(w)
		default:
			httpx.Logger(r.Context()).Error("проверка сессии", slog.Any("error", err))
		}
		next.ServeHTTP(w, r)
	})
}

// RegisterForm — GET /register.
func (h *PageHandler) RegisterForm(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	if tenant.FromContext(r.Context()) != nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/account"))
		return nil
	}
	form := newForm()
	plan := r.URL.Query().Get("plan")
	if _, ok := h.Plans.Get(plan); !ok {
		plan = h.Plans.Default()
	}
	form.Values["plan"] = plan
	return h.renderRegister(w, r, http.StatusOK, form)
}

// RegisterSubmit — POST /register.
func (h *PageHandler) RegisterSubmit(w http.ResponseWriter, r *http.Request) error {
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)

	form := newForm()
	for _, f := range []string{"company", "name", "email", "phone", "plan"} {
		form.Values[f] = r.PostFormValue(f)
	}
	form.Checked["consent"] = r.PostFormValue("consent") == "on"

	if r.PostFormValue("website") != "" {
		// Бот заполнил ловушку — аккаунт не создаём и ничего не объясняем.
		return h.renderRegister(w, r, http.StatusOK, form)
	}
	if !h.registerByIP.Allow("register:" + httpx.ClientIP(r)) {
		form.Alert = l.T("error.429.text")
		return h.renderRegister(w, r, http.StatusTooManyRequests, form)
	}

	token, err := h.Auth.Register(r.Context(), service.RegisterInput{
		Company:  form.Values["company"],
		Name:     form.Values["name"],
		Email:    form.Values["email"],
		Phone:    form.Values["phone"],
		Password: r.PostFormValue("password"),
		Plan:     form.Values["plan"],
		Consent:  form.Checked["consent"],
		Locale:   string(l.Lang()),
		Client:   clientMeta(r),
	})
	if fields, ok := httpx.FieldErrors(err); ok {
		form.Errors = localizeFields(l, fields)
		form.Alert = l.T("form.errors")
		return h.renderRegister(w, r, http.StatusUnprocessableEntity, form)
	}
	if err != nil {
		return err
	}

	h.setSession(w, token)
	redirect(w, r, i18n.Localize(l.Lang(), "/account"))
	return nil
}

func (h *PageHandler) renderRegister(w http.ResponseWriter, r *http.Request, status int, form FormView) error {
	l := h.localizer(r)
	plans := h.planViews(l)
	var selected PlanView
	for _, p := range plans {
		if p.Code == form.Values["plan"] {
			selected = p
		}
	}
	data := h.page(r, "register.title").
		With("Form", form).
		With("Plans", plans).
		With("SelectedPlan", selected)
	return h.Render.Render(w, status, "register", data)
}

// LoginForm — GET /login.
func (h *PageHandler) LoginForm(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	if tenant.FromContext(r.Context()) != nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/account"))
		return nil
	}
	form := newForm()
	if next, ok := safeNext(r.URL.Query().Get("next")); ok {
		form.Values["next"] = next
	}
	return h.renderLogin(w, r, http.StatusOK, form)
}

// LoginSubmit — POST /login.
func (h *PageHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) error {
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)

	form := newForm()
	form.Values["email"] = r.PostFormValue("email")
	if next, ok := safeNext(r.PostFormValue("next")); ok {
		form.Values["next"] = next
	}

	// Два лимита: по IP — от перебора многих аккаунтов с одного адреса,
	// по email — от перебора пароля одного аккаунта с многих адресов.
	email := strings.ToLower(strings.TrimSpace(form.Values["email"]))
	if !h.loginByIP.Allow("login:"+httpx.ClientIP(r)) || !h.loginByEmail.Allow("login:"+email) {
		form.Alert = l.T("error.429.text")
		return h.renderLogin(w, r, http.StatusTooManyRequests, form)
	}

	token, err := h.Auth.Login(r.Context(), service.LoginInput{
		Email:    form.Values["email"],
		Password: r.PostFormValue("password"),
		Client:   clientMeta(r),
	})
	if fields, ok := httpx.FieldErrors(err); ok {
		form.Errors = localizeFields(l, fields)
		form.Alert = l.T("form.errors")
		return h.renderLogin(w, r, http.StatusUnprocessableEntity, form)
	}
	if errors.Is(err, service.ErrInvalidCredentials) {
		form.Alert = l.T("login.error")
		return h.renderLogin(w, r, http.StatusUnauthorized, form)
	}
	if err != nil {
		return err
	}

	h.setSession(w, token)
	to := i18n.Localize(l.Lang(), "/account")
	if next := form.Values["next"]; next != "" {
		to = next
	}
	redirect(w, r, to)
	return nil
}

func (h *PageHandler) renderLogin(w http.ResponseWriter, r *http.Request, status int, form FormView) error {
	data := h.page(r, "login.title").With("Form", form)
	return h.Render.Render(w, status, "login", data)
}

// Logout — POST /logout. Только POST: выход по GET-ссылке позволил бы любому
// сайту разлогинить пользователя картинкой <img src=".../logout">.
func (h *PageHandler) Logout(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	if c, err := r.Cookie(h.sessionCookieName()); err == nil {
		if err := h.Auth.Logout(r.Context(), c.Value); err != nil {
			return err
		}
	}
	h.clearSession(w)
	redirect(w, r, i18n.Localize(l.Lang(), "/"))
	return nil
}

// Account — GET /account: заготовка кабинета продавца.
func (h *PageHandler) Account(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	p := tenant.FromContext(r.Context())
	if p == nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/login")+"?next="+i18n.Localize(l.Lang(), "/account"))
		return nil
	}

	var plan PlanView
	if pl, ok := h.Plans.Get(p.PlanCode); ok {
		plan = h.planView(l, pl)
	}
	data := h.page(r, "account.title").
		With("Plan", plan).
		With("Welcome", l.T("account.welcome", "Name", p.UserName))
	return h.Render.Render(w, http.StatusOK, "account", data)
}
