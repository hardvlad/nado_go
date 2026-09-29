package shop

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"nado_go/internal/integration/payment"
	"nado_go/internal/service"
)

// halykCSP — CSP страницы виджета Halyk: домены ePay в script-src/frame-src.
func halykCSP() string {
	hosts := strings.Join(payment.HalykScriptHosts, " ")
	return "default-src 'self'; img-src 'self' data: https:; style-src 'self'; " +
		"script-src 'self' " + hosts + "; frame-src " + hosts + "; connect-src 'self' " + hosts + "; base-uri 'self'"
}

// origin возвращает scheme://host текущего запроса (для абсолютных ссылок
// возврата и вебхука).
func (h *Handler) origin(r *http.Request) string {
	scheme := "http"
	if h.Prod || r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// payStart — GET /pay/{number}?t=token: инициирует оплату заказа.
func (h *Handler) payStart(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	number, err := strconv.ParseInt(chiURLParam(r, "number"), 10, 64)
	if err != nil {
		h.renderNotFound(w, r)
		return
	}
	token := r.URL.Query().Get("t")
	order, err := h.Orders.Order(r.Context(), store.ID, number, token)
	if errors.Is(err, service.ErrOrderNotFound) {
		h.renderNotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}

	prefix := PrefixFrom(r.Context())
	orderPath := prefix + "/order/" + strconv.FormatInt(number, 10) + "?t=" + token
	if order.Status == "paid" {
		h.redirect(w, r, "/order/"+strconv.FormatInt(number, 10)+"?t="+token)
		return
	}

	success := h.origin(r) + orderPath
	res, err := h.Payments.StartOrderPayment(r.Context(), store.AccountID, store.ID, order, h.origin(r), success, success)
	if errors.Is(err, service.ErrPaymentsDisabled) {
		// Оплата не настроена — возвращаем на страницу заказа (там подскажем связаться).
		h.redirect(w, r, "/order/"+strconv.FormatInt(number, 10)+"?t="+token)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if res.RedirectURL != "" {
		http.Redirect(w, r, res.RedirectURL, http.StatusSeeOther)
		return
	}

	// Страница оплаты: виджет провайдера (Halyk), dev-подтверждение или инструкции Kaspi.
	data := h.baseData(r, "shop.pay_title")
	data["Order"] = order
	data["RefToken"] = res.RefToken
	data["Manual"] = res.Manual
	data["Provider"] = res.Provider
	data["OrderToken"] = token
	if res.Widget != nil {
		w.Header().Set("Content-Security-Policy", halykCSP())
		data["Widget"] = res.Widget
	}
	h.render(w, r, http.StatusOK, "pay", data)
}

// payConfirmDev — POST /pay/{number}/confirm: подтверждение оплаты dev-провайдером.
func (h *Handler) payConfirmDev(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	number, err := strconv.ParseInt(chiURLParam(r, "number"), 10, 64)
	if err != nil {
		h.renderNotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, err)
		return
	}
	token := r.PostFormValue("t")
	if _, err := h.Orders.Order(r.Context(), store.ID, number, token); err != nil {
		h.renderNotFound(w, r)
		return
	}
	if err := h.Payments.ConfirmDevOrder(r.Context(), store.AccountID, store.ID, r.PostFormValue("ref")); err != nil {
		h.fail(w, r, err)
		return
	}
	h.redirect(w, r, "/order/"+strconv.FormatInt(number, 10)+"?t="+token)
}
