package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/model"
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

// Регистрация продавца — по номеру телефона с кодом в WhatsApp (D-17), без
// пароля и без выбора тарифа. Обязательно: название компании или имя и телефон;
// email — по желанию. Пробный период даётся при добавлении магазина.
// Шаг 1 (GET/POST /register): форма и отправка кода. Шаг 2 (POST /register/verify):
// ввод кода → создание аккаунта.

// RegisterForm — GET /register.
func (h *PageHandler) RegisterForm(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	if tenant.FromContext(r.Context()) != nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/account"))
		return nil
	}
	return h.renderRegister(w, r, http.StatusOK, newForm())
}

// RegisterSubmit — POST /register: проверка полей и отправка кода на телефон.
func (h *PageHandler) RegisterSubmit(w http.ResponseWriter, r *http.Request) error {
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)

	form := newForm()
	for _, f := range []string{"name", "email", "phone"} {
		form.Values[f] = r.PostFormValue(f)
	}
	form.Checked["consent"] = r.PostFormValue("consent") == "on"

	if r.PostFormValue("website") != "" {
		// Бот заполнил ловушку — ничего не объясняем.
		return h.renderRegister(w, r, http.StatusOK, form)
	}
	if !h.registerByIP.Allow("register:" + httpx.ClientIP(r)) {
		form.Alert = l.T("error.429.text")
		return h.renderRegister(w, r, http.StatusTooManyRequests, form)
	}

	// Обязательные поля: имя/компания, телефон, согласие.
	if len([]rune(strings.TrimSpace(form.Values["name"]))) < 2 {
		form.Errors["name"] = l.T("validation.required")
	}
	if !form.Checked["consent"] {
		form.Errors["consent"] = l.T("validation.consent")
	}
	if len(form.Errors) > 0 {
		return h.renderRegister(w, r, http.StatusUnprocessableEntity, form)
	}

	// Телефон нового продавца не должен быть уже зарегистрирован.
	registered, err := h.Auth.PhoneRegistered(r.Context(), form.Values["phone"])
	if errors.Is(err, service.ErrOTPInvalidPhone) {
		form.Errors["phone"] = l.T("validation.phone")
		return h.renderRegister(w, r, http.StatusUnprocessableEntity, form)
	}
	if err != nil {
		return err
	}
	if registered {
		form.Alert = l.T("register.phone_taken")
		return h.renderRegister(w, r, http.StatusUnprocessableEntity, form)
	}

	phone, err := h.OTP.Request(r.Context(), form.Values["phone"], model.OTPPurposeRegister, string(l.Lang()), httpx.ClientIP(r))
	if err != nil {
		if msg, ok := otpErrorMessage(l, err); ok {
			form.Alert = msg
			return h.renderRegister(w, r, http.StatusUnprocessableEntity, form)
		}
		return err
	}

	// Переходим к вводу кода; данные регистрации несём в скрытых полях.
	verify := newForm()
	verify.Values["phone"] = phone
	verify.Values["name"] = form.Values["name"]
	verify.Values["email"] = form.Values["email"]
	return h.renderRegisterVerify(w, r, http.StatusOK, verify)
}

// RegisterVerify — POST /register/verify: проверка кода и создание аккаунта.
func (h *PageHandler) RegisterVerify(w http.ResponseWriter, r *http.Request) error {
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)

	form := newForm()
	for _, f := range []string{"phone", "name", "email"} {
		form.Values[f] = r.PostFormValue(f)
	}

	if err := h.OTP.Verify(r.Context(), form.Values["phone"], model.OTPPurposeRegister, r.PostFormValue("code")); err != nil {
		if msg, ok := otpErrorMessage(l, err); ok {
			form.Errors["code"] = msg
			return h.renderRegisterVerify(w, r, http.StatusUnprocessableEntity, form)
		}
		return err
	}

	token, err := h.Auth.RegisterByPhone(r.Context(), form.Values["name"], form.Values["email"], form.Values["phone"], string(l.Lang()), clientMeta(r))
	if fields, ok := httpx.FieldErrors(err); ok {
		form.Errors = localizeFields(l, fields)
		return h.renderRegisterVerify(w, r, http.StatusUnprocessableEntity, form)
	}
	if errors.Is(err, service.ErrPhoneTaken) {
		form.Alert = l.T("register.phone_taken")
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
	data := h.page(r, "register.title").With("Form", form)
	return h.Render.Render(w, status, "register", data)
}

func (h *PageHandler) renderRegisterVerify(w http.ResponseWriter, r *http.Request, status int, form FormView) error {
	data := h.page(r, "register.title").With("Form", form)
	return h.Render.Render(w, status, "register_verify", data)
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

	// Магазины продавца (D-23): у каждого своё подключение к маркетплейсу.
	var cards []StoreCard
	if h.Connections != nil {
		stores, err := h.Connections.Stores(r.Context(), p.AccountID)
		if err != nil {
			return err
		}
		for _, s := range stores {
			idStr := strconv.FormatInt(s.ID, 10)
			cards = append(cards, StoreCard{
				Store:         s,
				URL:           h.storeURL(s.Slug),
				EditURL:       i18n.Localize(l.Lang(), "/stores/"+idStr+"/edit"),
				CatalogURL:    i18n.Localize(l.Lang(), "/stores/"+idStr+"/catalog"),
				CategoriesURL: i18n.Localize(l.Lang(), "/stores/"+idStr+"/categories"),
				PaymentsURL:   i18n.Localize(l.Lang(), "/stores/"+idStr+"/payments"),
			})
		}
	}

	data := h.page(r, "account.title").
		With("Plan", plan).
		With("Welcome", l.T("account.welcome", "Name", p.UserName)).
		With("Stores", cards).
		With("Connected", r.URL.Query().Get("connected") == "1").
		With("Saved", r.URL.Query().Get("saved") == "1")
	return h.Render.Render(w, http.StatusOK, "account", data)
}

// StoreCard — магазин с готовыми ссылками для кабинета.
type StoreCard struct {
	model.Store
	URL           string // адрес витрины (кликабельная ссылка)
	EditURL       string // адрес формы редактирования
	CatalogURL    string // список товаров магазина
	CategoriesURL string // управление категориями
	PaymentsURL   string // настройки приёма оплат
}

// storeURL строит адрес витрины: в проде поддомен, локально — префикс /shop/{slug}.
func (h *PageHandler) storeURL(slug string) string {
	if h.ShopSuffix != "" {
		return "https://" + slug + "." + h.ShopSuffix
	}
	return h.PublicURL + "/shop/" + slug
}
