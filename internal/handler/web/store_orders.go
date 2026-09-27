package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/service"
	"nado_go/internal/tenant"
)

const storeOrdersPerPage = 30

// orderStatuses — статусы заказа витрины для фильтра (и подписи статуса).
var orderStatuses = []string{
	"new", "awaiting_payment", "paid", "processing", "shipped", "delivered", "completed", "canceled", "refunded",
}

// statusOption — пункт фильтра статусов с локализованной подписью.
type statusOption struct {
	Value    string
	Label    string
	Selected bool
}

func statusOptions(l *i18n.Localizer, selected string) []statusOption {
	out := make([]statusOption, 0, len(orderStatuses))
	for _, s := range orderStatuses {
		out = append(out, statusOption{Value: s, Label: l.T("sorders.status_" + s), Selected: s == selected})
	}
	return out
}

// StoreOrdersList — GET /store-orders: заказы, оформленные на витрине магазина.
func (h *PageHandler) StoreOrdersList(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	p := tenant.FromContext(r.Context())
	if p == nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/login")+"?next="+i18n.Localize(l.Lang(), "/store-orders"))
		return nil
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	storeID, _ := strconv.ParseInt(r.URL.Query().Get("store"), 10, 64)
	status := r.URL.Query().Get("status")
	if !validOrderStatus(status) {
		status = ""
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	orders, total, err := h.StoreOrders.CabinetOrders(r.Context(), p.AccountID, storeID, query, status, page, storeOrdersPerPage)
	if err != nil {
		return err
	}
	stores, err := h.Connections.Stores(r.Context(), p.AccountID)
	if err != nil {
		return err
	}

	totalPages := (total + storeOrdersPerPage - 1) / storeOrdersPerPage
	nums := make([]int, 0, totalPages)
	for i := 1; i <= totalPages; i++ {
		nums = append(nums, i)
	}
	base := i18n.Localize(l.Lang(), "/store-orders") + "?q=" + url.QueryEscape(query)
	if storeID > 0 {
		base += "&store=" + strconv.FormatInt(storeID, 10)
	}
	if status != "" {
		base += "&status=" + url.QueryEscape(status)
	}
	base += "&page="

	data := h.page(r, "sorders.title").
		With("Orders", orders).
		With("Stores", stores).
		With("Statuses", statusOptions(l, status)).
		With("Query", query).
		With("StoreID", storeID).
		With("Status", status).
		With("Total", total).
		With("Page", page).
		With("TotalPages", totalPages).
		With("PageNums", nums).
		With("PageBase", base)
	return h.Render.Render(w, http.StatusOK, "store_orders", data)
}

// StoreOrderDetail — GET /store-orders/{id}: детали заказа витрины.
func (h *PageHandler) StoreOrderDetail(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	p := tenant.FromContext(r.Context())
	if p == nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/login"))
		return nil
	}
	id, err := cabinetID(r)
	if err != nil {
		return err
	}

	order, err := h.StoreOrders.CabinetOrder(r.Context(), p.AccountID, id)
	if errors.Is(err, service.ErrOrderNotFound) {
		return httpx.ErrNotFound("Заказ не найден")
	}
	if err != nil {
		return err
	}

	data := h.page(r, "sorders.detail_title").
		With("Order", order).
		With("StatusLabel", l.T("sorders.status_"+order.Status)).
		With("AddressText", addressText(order.Address))
	return h.Render.Render(w, http.StatusOK, "store_order", data)
}

func validOrderStatus(s string) bool {
	for _, x := range orderStatuses {
		if x == s {
			return true
		}
	}
	return false
}

// addressText достаёт текст адреса из JSON {"text": "..."} (как пишет checkout).
func addressText(raw string) string {
	if raw == "" {
		return ""
	}
	var a struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return raw
	}
	return a.Text
}
