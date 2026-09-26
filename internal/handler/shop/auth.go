package shop

import (
	"errors"
	"net/http"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/model"
	"nado_go/internal/service"
)

const customerCookie = "nado_cust"

// customerModel достаёт вошедшего покупателя из контекста (или nil).
func customerModel(r *http.Request) *model.Customer {
	c, _ := CustomerFrom(r.Context()).(*model.Customer)
	return c
}

// loginForm — GET /login: форма входа по телефону. Вошедшего отправляем в кабинет.
func (h *Handler) loginForm(w http.ResponseWriter, r *http.Request) {
	if customerModel(r) != nil {
		h.redirect(w, r, "/account")
		return
	}
	data := h.baseData(r, "shop.login_title")
	data["Step"] = "phone"
	h.render(w, r, http.StatusOK, "login", data)
}

// requestCode — POST /login: отправка кода на телефон.
func (h *Handler) requestCode(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, err)
		return
	}
	rawPhone := r.PostFormValue("phone")
	phone, err := h.Customers.RequestCode(r.Context(), store.ID, rawPhone, langOf(r), httpx.ClientIP(r))

	data := h.baseData(r, "shop.login_title")
	data["Phone"] = phone
	data["Name"] = r.PostFormValue("name")
	switch {
	case errors.Is(err, service.ErrCustomerPhone):
		data["Step"] = "phone"
		data["Error"] = i18n.FromContext(r.Context()).T("shop.err_phone")
		h.render(w, r, http.StatusUnprocessableEntity, "login", data)
	case errors.Is(err, service.ErrCustomerRate), errors.Is(err, service.ErrCustomerCooldown), errors.Is(err, service.ErrCustomerSend):
		data["Step"] = "phone"
		data["Error"] = i18n.FromContext(r.Context()).T("shop.err_send")
		h.render(w, r, http.StatusTooManyRequests, "login", data)
	case err != nil:
		h.fail(w, r, err)
	default:
		data["Step"] = "code"
		h.render(w, r, http.StatusOK, "login", data)
	}
}

// verifyCode — POST /login/verify: проверка кода и открытие сессии.
func (h *Handler) verifyCode(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, err)
		return
	}
	phone := r.PostFormValue("phone")
	code := r.PostFormValue("code")
	name := r.PostFormValue("name")

	customer, token, err := h.Customers.Verify(r.Context(), store.ID, store.AccountID, phone, code, name, r.UserAgent())
	if err != nil {
		data := h.baseData(r, "shop.login_title")
		data["Step"] = "code"
		data["Phone"] = phone
		data["Name"] = name
		if errors.Is(err, service.ErrCustomerBadCode) || errors.Is(err, service.ErrCustomerNoCode) {
			data["Error"] = i18n.FromContext(r.Context()).T("shop.err_code")
			h.render(w, r, http.StatusUnprocessableEntity, "login", data)
			return
		}
		h.fail(w, r, err)
		return
	}

	h.setCustomerCookie(w, r, token)
	_ = customer
	// Возврат на страницу, с которой пришли на вход (next), иначе в кабинет.
	next := r.PostFormValue("next")
	if !validNext(next) {
		next = "/account"
	}
	h.redirect(w, r, next)
}

// logout — POST /logout.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(customerCookie); err == nil {
		_ = h.Customers.Logout(r.Context(), c.Value)
	}
	h.clearCustomerCookie(w, r)
	h.redirect(w, r, "/")
}

// account — GET /account: кабинет покупателя (профиль, заказы, выход, удаление).
func (h *Handler) account(w http.ResponseWriter, r *http.Request) {
	customer := customerModel(r)
	if customer == nil {
		data := h.baseData(r, "shop.login_title")
		data["Step"] = "phone"
		data["Next"] = "/account"
		h.render(w, r, http.StatusOK, "login", data)
		return
	}
	data := h.baseData(r, "shop.account_title")
	data["Customer"] = customer
	if h.Orders != nil {
		store := StoreFrom(r.Context())
		if orders, err := h.Orders.CustomerOrders(r.Context(), store.ID, customer.ID); err == nil {
			data["Orders"] = orders
		}
	}
	h.render(w, r, http.StatusOK, "account", data)
}

// deleteAccount — POST /account/delete: удаление аккаунта (App Store 5.1.1).
func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	customer := customerModel(r)
	if customer == nil {
		h.redirect(w, r, "/login")
		return
	}
	store := StoreFrom(r.Context())
	if err := h.Customers.DeleteAccount(r.Context(), store.ID, customer.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	if c, err := r.Cookie(customerCookie); err == nil {
		_ = h.Customers.Logout(r.Context(), c.Value)
	}
	h.clearCustomerCookie(w, r)
	h.redirect(w, r, "/")
}

// --- Вспомогательное ---

// redirect ведёт на путь витрины с учётом префикса и языка (LangBase).
func (h *Handler) redirect(w http.ResponseWriter, r *http.Request, path string) {
	base := langBaseOf(r)
	http.Redirect(w, r, base+path, http.StatusSeeOther)
}

// langBaseOf строит базу ссылок витрины (префикс + язык, кроме языка по умолчанию).
func langBaseOf(r *http.Request) string {
	store := StoreFrom(r.Context())
	prefix := PrefixFrom(r.Context())
	lang := langOf(r)
	if lang != store.DefaultLang {
		return prefix + "/" + lang
	}
	return prefix
}

func (h *Handler) setCustomerCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     customerCookie,
		Value:    token,
		Path:     cookiePath(r),
		HttpOnly: true,
		Secure:   h.Prod,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.Customers.SessionTTL().Seconds()),
	})
}

func (h *Handler) clearCustomerCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: customerCookie, Value: "", Path: cookiePath(r),
		HttpOnly: true, Secure: h.Prod, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// cookiePath ограничивает cookie префиксом магазина в dev (/shop/{slug}), а в
// проде — всем доменом витрины.
func cookiePath(r *http.Request) string {
	if p := PrefixFrom(r.Context()); p != "" {
		return p + "/"
	}
	return "/"
}

// validNext допускает только локальный путь без схемы и хоста.
func validNext(next string) bool {
	return next != "" && next[0] == '/' && (len(next) == 1 || next[1] != '/')
}
