package shop

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"

	"nado_go/internal/i18n"
	"nado_go/internal/service"
)

const cartCookie = "nado_cart"

// cartToken возвращает токен корзины из cookie (или "").
func cartToken(r *http.Request) string {
	if c, err := r.Cookie(cartCookie); err == nil {
		return c.Value
	}
	return ""
}

// ensureCartToken возвращает токен корзины, создавая и ставя cookie при отсутствии.
func (h *Handler) ensureCartToken(w http.ResponseWriter, r *http.Request) string {
	if t := cartToken(r); t != "" {
		return t
	}
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	token := hex.EncodeToString(buf)
	http.SetCookie(w, &http.Cookie{
		Name: cartCookie, Value: token, Path: cookiePath(r),
		HttpOnly: true, Secure: h.Prod, SameSite: http.SameSiteLaxMode, MaxAge: 60 * 60 * 24 * 30,
	})
	return token
}

// cartAdd — POST /cart/add: добавить товар в корзину.
func (h *Handler) cartAdd(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, err)
		return
	}
	variantID, _ := strconv.ParseInt(r.PostFormValue("variant_id"), 10, 64)
	qty, _ := strconv.Atoi(r.PostFormValue("qty"))
	if qty <= 0 {
		qty = 1
	}
	if variantID <= 0 {
		h.redirect(w, r, "/cart")
		return
	}
	token := h.ensureCartToken(w, r)
	if err := h.Cart.Add(r.Context(), store.ID, token, variantID, qty); err != nil {
		h.fail(w, r, err)
		return
	}
	h.redirect(w, r, "/cart")
}

// cartUpdate — POST /cart/update: изменить количество позиции.
func (h *Handler) cartUpdate(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, err)
		return
	}
	variantID, _ := strconv.Atoi(r.PostFormValue("variant_id"))
	qty, _ := strconv.Atoi(r.PostFormValue("qty"))
	if err := h.Cart.SetQty(r.Context(), store.ID, cartToken(r), variantID, qty); err != nil {
		h.fail(w, r, err)
		return
	}
	h.redirect(w, r, "/cart")
}

// cartRemove — POST /cart/remove: убрать позицию.
func (h *Handler) cartRemove(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, err)
		return
	}
	variantID, _ := strconv.ParseInt(r.PostFormValue("variant_id"), 10, 64)
	if err := h.Cart.Remove(r.Context(), store.ID, cartToken(r), variantID); err != nil {
		h.fail(w, r, err)
		return
	}
	h.redirect(w, r, "/cart")
}

// cartPage — GET /cart.
func (h *Handler) cartPage(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	cart, err := h.Cart.View(r.Context(), store.ID, cartToken(r), langOf(r), store.DefaultLang)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := h.baseData(r, "shop.cart_title")
	data["Cart"] = cart
	h.render(w, r, http.StatusOK, "cart", data)
}

// checkoutPage — GET /checkout: требует входа.
func (h *Handler) checkoutPage(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	customer := customerModel(r)
	if customer == nil {
		data := h.baseData(r, "shop.login_title")
		data["Step"] = "phone"
		data["Next"] = "/checkout"
		h.render(w, r, http.StatusOK, "login", data)
		return
	}
	cart, err := h.Cart.View(r.Context(), store.ID, cartToken(r), langOf(r), store.DefaultLang)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if len(cart.Lines) == 0 {
		h.redirect(w, r, "/cart")
		return
	}
	data := h.baseData(r, "shop.checkout_title")
	data["Cart"] = cart
	data["Customer"] = customer
	h.render(w, r, http.StatusOK, "checkout", data)
}

// placeOrder — POST /checkout: создать заказ.
func (h *Handler) placeOrder(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	customer := customerModel(r)
	if customer == nil {
		h.redirect(w, r, "/login")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, err)
		return
	}

	order, err := h.Orders.Checkout(r.Context(), service.CheckoutInput{
		StoreID: store.ID, AccountID: store.AccountID, CustomerID: customer.ID,
		CartToken: cartToken(r),
		Name:      firstNonEmpty(r.PostFormValue("name"), customer.Name),
		Phone:     customer.PhoneE164,
		Email:     r.PostFormValue("email"),
		Comment:   r.PostFormValue("comment"),
		Address:   r.PostFormValue("address"),
		Lang:      langOf(r), DefLang: store.DefaultLang,
	})
	switch {
	case errors.Is(err, service.ErrCheckoutEmpty):
		h.redirect(w, r, "/cart")
	case errors.Is(err, service.ErrCheckoutUnavailable):
		cart, _ := h.Cart.View(r.Context(), store.ID, cartToken(r), langOf(r), store.DefaultLang)
		data := h.baseData(r, "shop.checkout_title")
		data["Cart"] = cart
		data["Customer"] = customer
		data["Error"] = i18n.FromContext(r.Context()).T("shop.err_unavailable")
		h.render(w, r, http.StatusConflict, "checkout", data)
	case err != nil:
		h.fail(w, r, err)
	default:
		h.redirect(w, r, "/order/"+strconv.FormatInt(order.Number, 10)+"?t="+order.Token)
	}
}

// orderPage — GET /order/{number}?t=token.
func (h *Handler) orderPage(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	number, err := strconv.ParseInt(chiURLParam(r, "number"), 10, 64)
	if err != nil {
		h.renderNotFound(w, r)
		return
	}
	order, err := h.Orders.Order(r.Context(), store.ID, number, r.URL.Query().Get("t"))
	if errors.Is(err, service.ErrOrderNotFound) {
		h.renderNotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := h.baseData(r, "shop.order_created")
	data["Order"] = order
	h.render(w, r, http.StatusOK, "order", data)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
