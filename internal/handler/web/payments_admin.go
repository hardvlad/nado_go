package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/integration/payment"
	"nado_go/internal/service"
	"nado_go/internal/tenant"
)

// halykCSP — CSP страницы виджета Halyk ePay: домены ePay в script-src/frame-src.
func halykCSP() string {
	hosts := strings.Join(payment.HalykScriptHosts, " ")
	return "default-src 'self'; img-src 'self' data: https:; style-src 'self'; " +
		"script-src 'self' " + hosts + "; frame-src " + hosts + "; connect-src 'self' " + hosts + "; base-uri 'self'"
}

// Кабинет: настройки приёма оплат магазином и оплата подписки платформе.

// publicOrigin возвращает базовый адрес (scheme://host) для абсолютных ссылок:
// PublicURL из конфигурации, иначе — из запроса.
func (h *PageHandler) publicOrigin(r *http.Request) string {
	if h.PublicURL != "" {
		return h.PublicURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// StorePaymentsForm — GET /stores/{id}/payments.
func (h *PageHandler) StorePaymentsForm(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	set, err := h.Payments.StoreSettings(r.Context(), p.AccountID, storeID)
	if err != nil {
		return err
	}
	return h.renderStorePayments(w, r, http.StatusOK, storeID, set, newForm(), r.URL.Query().Get("saved") == "1")
}

// StorePaymentsSubmit — POST /stores/{id}/payments.
func (h *PageHandler) StorePaymentsSubmit(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)

	in := service.SaveStoreSettingsInput{
		Provider:   r.PostFormValue("provider"),
		IsEnabled:  r.PostFormValue("enabled") != "",
		MerchantID: r.PostFormValue("merchant_id"),
		Terminal:   r.PostFormValue("terminal_id"),
		Secret:     r.PostFormValue("secret"),
		Testing:    r.PostFormValue("testing") != "",
	}
	err := h.Payments.SaveStoreSettings(r.Context(), p.AccountID, storeID, in)
	switch {
	case errors.Is(err, service.ErrPaymentProvider):
		set, _ := h.Payments.StoreSettings(r.Context(), p.AccountID, storeID)
		form := newForm()
		form.Alert = l.T("pay.err_provider")
		return h.renderStorePayments(w, r, http.StatusUnprocessableEntity, storeID, set, form, false)
	case err != nil:
		return err
	}
	redirect(w, r, i18n.Localize(l.Lang(), fmt.Sprintf("/stores/%d/payments", storeID))+"?saved=1")
	return nil
}

func (h *PageHandler) renderStorePayments(w http.ResponseWriter, r *http.Request, status int, storeID int64, set *service.StoreSettingsView, form FormView, saved bool) error {
	l := h.localizer(r)
	providers := make([]statusOption, 0)
	for _, code := range h.Payments.Providers() {
		providers = append(providers, statusOption{Value: code, Label: l.T("pay.provider_" + code), Selected: code == set.Provider})
	}
	webhookURL := ""
	if set.WebhookToken != "" && set.Provider != "dev" && set.Provider != "kaspi" {
		webhookURL = h.publicOrigin(r) + "/webhooks/payment/" + set.Provider + "/" + set.WebhookToken
	}

	data := h.page(r, "pay.title").
		With("StoreID", storeID).
		With("StoreName", h.storeName(r, tenant.FromContext(r.Context()).AccountID, storeID)).
		With("Settings", set).
		With("Providers", providers).
		With("WebhookURL", webhookURL).
		With("Saved", saved).
		With("Form", form).
		With("Action", i18n.Localize(l.Lang(), fmt.Sprintf("/stores/%d/payments", storeID))).
		With("AccountURL", i18n.Localize(l.Lang(), "/account"))
	return h.Render.Render(w, status, "store_payments", data)
}

// --- Подписка платформе ---

// Billing — GET /billing: статус подписки и оплата тарифа.
func (h *PageHandler) Billing(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	p := tenant.FromContext(r.Context())
	if p == nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/login")+"?next="+i18n.Localize(l.Lang(), "/billing"))
		return nil
	}
	sub, err := h.Payments.Subscription(r.Context(), p.AccountID)
	if err != nil {
		return err
	}

	active := sub.Status == "active" && sub.Until.After(time.Now())
	data := h.page(r, "billing.title").
		With("Status", sub.Status).
		With("Active", active).
		With("Until", sub.Until).
		With("CurrentPlan", sub.PlanCode).
		With("Plans", h.planViews(l)).
		With("PayEnabled", h.Payments.PlatformPayEnabled()).
		With("SubscribeAction", i18n.Localize(l.Lang(), "/billing/subscribe")).
		With("Paid", r.URL.Query().Get("paid") == "1")
	return h.Render.Render(w, http.StatusOK, "billing", data)
}

// BillingSubscribe — POST /billing/subscribe: оплатить тариф.
func (h *PageHandler) BillingSubscribe(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	p := tenant.FromContext(r.Context())
	if p == nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/login"))
		return nil
	}
	if err := parseForm(w, r); err != nil {
		return err
	}
	plan := r.PostFormValue("plan")
	origin := h.publicOrigin(r)
	success := origin + i18n.Localize(l.Lang(), "/billing") + "?paid=1"

	res, err := h.Payments.StartSubscriptionPayment(r.Context(), p.AccountID, plan, origin, success, success)
	if errors.Is(err, service.ErrSubscriptionNoPay) {
		return httpx.ErrBadRequest(l.T("billing.pay_unavailable"))
	}
	if err != nil {
		return err
	}
	if res.RedirectURL != "" {
		redirect(w, r, res.RedirectURL)
		return nil
	}
	data := h.page(r, "billing.title").
		With("RefToken", res.RefToken).
		With("ConfirmAction", i18n.Localize(l.Lang(), "/billing/confirm"))
	if res.Widget != nil {
		// Виджет Halyk: ослабляем CSP (домены ePay) и рендерим страницу виджета.
		w.Header().Set("Content-Security-Policy", halykCSP())
		data = data.With("Widget", res.Widget)
	}
	return h.Render.Render(w, http.StatusOK, "billing_pay", data)
}

// BillingConfirmDev — POST /billing/confirm: подтверждение оплаты dev-провайдером.
func (h *PageHandler) BillingConfirmDev(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	p := tenant.FromContext(r.Context())
	if p == nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/login"))
		return nil
	}
	if err := parseForm(w, r); err != nil {
		return err
	}
	if err := h.Payments.ConfirmDevSubscription(r.Context(), p.AccountID, r.PostFormValue("ref")); err != nil {
		return err
	}
	redirect(w, r, i18n.Localize(l.Lang(), "/billing")+"?paid=1")
	return nil
}

// storePaymentsPath — адрес настроек оплаты магазина (для ссылки в кабинете).
func storePaymentsPath(lang i18n.Lang, storeID int64) string {
	return i18n.Localize(lang, "/stores/"+strconv.FormatInt(storeID, 10)+"/payments")
}
