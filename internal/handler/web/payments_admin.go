package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/integration/payment"
	"nado_go/internal/service"
	"nado_go/internal/tenant"
)

// Кабинет: управление методами приёма оплат магазином (несколько провайдеров
// одновременно) и оплата подписки платформе с выбором провайдера.

// halykCSP — CSP страницы виджета Halyk ePay: домены ePay в script-src/frame-src.
func halykCSP() string {
	hosts := strings.Join(payment.HalykScriptHosts, " ")
	return "default-src 'self'; img-src 'self' data: https:; style-src 'self'; " +
		"script-src 'self' " + hosts + "; frame-src " + hosts + "; connect-src 'self' " + hosts + "; base-uri 'self'"
}

// publicOrigin возвращает базовый адрес (scheme://host) для абсолютных ссылок.
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

// providerLabels строит подписи провайдеров на языке страницы.
func (h *PageHandler) providerLabel(l *i18n.Localizer, code string) string {
	return l.T("pay.provider_" + code)
}

// StorePaymentsList — GET /stores/{id}/payments: список методов оплаты магазина.
func (h *PageHandler) StorePaymentsList(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	l := h.localizer(r)
	methods, err := h.Payments.StoreMethods(r.Context(), p.AccountID, storeID)
	if err != nil {
		return err
	}
	type row struct {
		Provider, Label, EditURL string
		Configured, IsEnabled    bool
	}
	base := i18n.Localize(l.Lang(), fmt.Sprintf("/stores/%d/payments", storeID))
	rows := make([]row, 0, len(methods))
	enabled := 0
	for _, m := range methods {
		if m.IsEnabled {
			enabled++
		}
		rows = append(rows, row{
			Provider: m.Provider, Label: h.providerLabel(l, m.Provider),
			EditURL: base + "/" + m.Provider, Configured: m.Configured, IsEnabled: m.IsEnabled,
		})
	}
	data := h.page(r, "pay.title").
		With("StoreID", storeID).
		With("StoreName", h.storeName(r, p.AccountID, storeID)).
		With("Methods", rows).
		With("EnabledCount", enabled).
		With("AccountURL", i18n.Localize(l.Lang(), "/account")).
		With("Saved", r.URL.Query().Get("saved") == "1").
		With("Deleted", r.URL.Query().Get("deleted") == "1")
	return h.Render.Render(w, http.StatusOK, "store_payments", data)
}

// StorePaymentMethodForm — GET /stores/{id}/payments/{provider}: форма метода.
func (h *PageHandler) StorePaymentMethodForm(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	provider := chi.URLParam(r, "provider")
	m, err := h.Payments.MethodForEdit(r.Context(), p.AccountID, storeID, provider)
	if errors.Is(err, service.ErrPaymentProvider) {
		return httpx.ErrNotFound("Провайдер не найден")
	}
	if err != nil {
		return err
	}
	return h.renderMethodForm(w, r, http.StatusOK, storeID, m, newForm())
}

// StorePaymentMethodSubmit — POST /stores/{id}/payments/{provider}.
func (h *PageHandler) StorePaymentMethodSubmit(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	provider := chi.URLParam(r, "provider")
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)
	in := service.SaveMethodInput{
		Provider:   provider,
		IsEnabled:  r.PostFormValue("enabled") != "",
		MerchantID: r.PostFormValue("merchant_id"),
		Terminal:   r.PostFormValue("terminal_id"),
		Secret:     r.PostFormValue("secret"),
		Testing:    r.PostFormValue("testing") != "",
	}
	err := h.Payments.SaveMethod(r.Context(), p.AccountID, storeID, in)
	switch {
	case errors.Is(err, service.ErrPaymentProvider):
		return httpx.ErrNotFound("Провайдер не найден")
	case err != nil:
		return err
	}
	redirect(w, r, i18n.Localize(l.Lang(), fmt.Sprintf("/stores/%d/payments", storeID))+"?saved=1")
	return nil
}

// StorePaymentMethodDelete — POST /stores/{id}/payments/{provider}/delete.
func (h *PageHandler) StorePaymentMethodDelete(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	provider := chi.URLParam(r, "provider")
	l := h.localizer(r)
	if err := h.Payments.DeleteMethod(r.Context(), p.AccountID, storeID, provider); err != nil {
		return err
	}
	redirect(w, r, i18n.Localize(l.Lang(), fmt.Sprintf("/stores/%d/payments", storeID))+"?deleted=1")
	return nil
}

func (h *PageHandler) renderMethodForm(w http.ResponseWriter, r *http.Request, status int, storeID int64, m *service.PaymentMethodView, form FormView) error {
	l := h.localizer(r)
	base := i18n.Localize(l.Lang(), fmt.Sprintf("/stores/%d/payments", storeID))
	webhookURL := ""
	if m.WebhookToken != "" && m.NeedsCreds {
		webhookURL = h.publicOrigin(r) + "/webhooks/payment/" + m.Provider + "/" + m.WebhookToken
	}
	data := h.page(r, "pay.method_title").
		With("StoreID", storeID).
		With("StoreName", h.storeName(r, tenant.FromContext(r.Context()).AccountID, storeID)).
		With("Method", m).
		With("ProviderLabel", h.providerLabel(l, m.Provider)).
		With("WebhookURL", webhookURL).
		With("Form", form).
		With("Action", base+"/"+m.Provider).
		With("DeleteAction", base+"/"+m.Provider+"/delete").
		With("ListURL", base)
	return h.Render.Render(w, status, "store_payment_edit", data)
}

// --- Подписка магазина ---

// billingBase — адрес страницы подписки магазина.
func billingBase(lang i18n.Lang, storeID int64) string {
	return i18n.Localize(lang, fmt.Sprintf("/stores/%d/billing", storeID))
}

// StoreBilling — GET /stores/{id}/billing: подписка магазина, тарифы и оплата.
func (h *PageHandler) StoreBilling(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	l := h.localizer(r)
	sub, err := h.Payments.Subscription(r.Context(), p.AccountID, storeID)
	if err != nil {
		return err
	}

	methods := h.Payments.PlatformMethods()
	type methodOpt struct{ Value, Label string }
	opts := make([]methodOpt, 0, len(methods))
	for _, m := range methods {
		opts = append(opts, methodOpt{Value: m.Provider, Label: h.providerLabel(l, m.Provider)})
	}

	active := sub.Status == "active" && sub.Until.After(time.Now())
	data := h.page(r, "billing.title").
		With("StoreID", storeID).
		With("StoreName", h.storeName(r, p.AccountID, storeID)).
		With("Status", sub.Status).
		With("Active", active).
		With("Until", sub.Until).
		With("CurrentPlan", sub.PlanCode).
		With("Plans", h.planViews(l)).
		With("PayEnabled", len(opts) > 0).
		With("Methods", opts).
		With("SubscribeAction", billingBase(l.Lang(), storeID)+"/subscribe").
		With("AccountURL", i18n.Localize(l.Lang(), "/account")).
		With("Paid", r.URL.Query().Get("paid") == "1")
	return h.Render.Render(w, http.StatusOK, "billing", data)
}

// StoreBillingSubscribe — POST /stores/{id}/billing/subscribe.
func (h *PageHandler) StoreBillingSubscribe(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	l := h.localizer(r)
	if err := parseForm(w, r); err != nil {
		return err
	}
	plan := r.PostFormValue("plan")
	provider := r.PostFormValue("provider")
	origin := h.publicOrigin(r)
	success := origin + billingBase(l.Lang(), storeID) + "?paid=1"

	res, err := h.Payments.StartSubscriptionPayment(r.Context(), p.AccountID, storeID, plan, provider, origin, success, success)
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
		With("BillingURL", billingBase(l.Lang(), storeID)).
		With("ConfirmAction", billingBase(l.Lang(), storeID)+"/confirm")
	if res.Widget != nil {
		w.Header().Set("Content-Security-Policy", halykCSP())
		data = data.With("Widget", res.Widget)
	}
	return h.Render.Render(w, http.StatusOK, "billing_pay", data)
}

// StoreBillingConfirmDev — POST /stores/{id}/billing/confirm: dev-подтверждение.
func (h *PageHandler) StoreBillingConfirmDev(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	l := h.localizer(r)
	if err := parseForm(w, r); err != nil {
		return err
	}
	if err := h.Payments.ConfirmDevSubscription(r.Context(), p.AccountID, r.PostFormValue("ref")); err != nil {
		return err
	}
	redirect(w, r, billingBase(l.Lang(), storeID)+"?paid=1")
	return nil
}
