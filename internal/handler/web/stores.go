package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/model"
	"nado_go/internal/service"
	"nado_go/internal/tenant"
)

// Подключение магазина к Kaspi только через кабинет (вход по SMS-коду владельца,
// создание служебного сотрудника, сбор реквизитов и подтверждение продавцом).
// Подключение по токену API убрано.

// StoreConnectForm — GET /stores/new: форма начала подключения (имя + телефон).
func (h *PageHandler) StoreConnectForm(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	if tenant.FromContext(r.Context()) == nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/login")+"?next="+i18n.Localize(l.Lang(), "/stores/new"))
		return nil
	}
	return h.renderStoreConnect(w, r, http.StatusOK, newForm())
}

func (h *PageHandler) renderStoreConnect(w http.ResponseWriter, r *http.Request, status int, form FormView) error {
	data := h.page(r, "stores.connect_title").With("Form", form)
	return h.Render.Render(w, status, "store_connect", data)
}

// Подключение через кабинет Kaspi: вход владельца по SMS-коду и создание
// служебного сотрудника (D-28). Продавец вводит телефон владельца, затем код.

// KaspiCabinetStart — POST /stores/kaspi: заводит онбординг и запускает отправку
// SMS-кода владельцу.
func (h *PageHandler) KaspiCabinetStart(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	p := tenant.FromContext(r.Context())
	if p == nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/login"))
		return nil
	}
	if err := parseForm(w, r); err != nil {
		return err
	}

	form := newForm()
	name := r.PostFormValue("cab_name")
	phone := r.PostFormValue("phone")
	form.Values["cab_name"] = name
	form.Values["phone"] = phone

	id, err := h.Onboarding.Start(r.Context(), p.AccountID, name, phone)
	switch {
	case errors.Is(err, service.ErrStoreNameRequired):
		form.Errors["cab_name"] = l.T("validation.required")
		return h.renderStoreConnect(w, r, http.StatusUnprocessableEntity, form)
	case errors.Is(err, service.ErrPhoneRequired):
		form.Errors["phone"] = l.T("stores.err_phone")
		return h.renderStoreConnect(w, r, http.StatusUnprocessableEntity, form)
	case errors.Is(err, service.ErrMailboxNotConfigured):
		form.Alert = l.T("stores.err_cabinet_unavailable")
		return h.renderStoreConnect(w, r, http.StatusUnprocessableEntity, form)
	case err != nil:
		return err
	}

	redirect(w, r, cabinetPath(l.Lang(), id))
	return nil
}

// KaspiCabinetStatus — GET /stores/kaspi/{id}: текущий шаг онбординга.
func (h *PageHandler) KaspiCabinetStatus(w http.ResponseWriter, r *http.Request) error {
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
	return h.renderCabinet(w, r, http.StatusOK, p.AccountID, id, "")
}

// KaspiCabinetCode — POST /stores/kaspi/{id}/code: продавец ввёл SMS-код.
func (h *PageHandler) KaspiCabinetCode(w http.ResponseWriter, r *http.Request) error {
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
	if err := parseForm(w, r); err != nil {
		return err
	}

	serr := h.Onboarding.SubmitCode(r.Context(), p.AccountID, id, r.PostFormValue("code"))
	switch {
	case errors.Is(serr, service.ErrCodeRequired):
		return h.renderCabinet(w, r, http.StatusUnprocessableEntity, p.AccountID, id, l.T("stores.err_code"))
	case errors.Is(serr, service.ErrOnboardingState):
		return h.renderCabinet(w, r, http.StatusConflict, p.AccountID, id, l.T("stores.err_state"))
	case errors.Is(serr, service.ErrOnboardingNotFound):
		return httpx.ErrNotFound("Онбординг не найден")
	case serr != nil:
		return serr
	}
	redirect(w, r, cabinetPath(l.Lang(), id))
	return nil
}

// KaspiCabinetMerchant — POST /stores/kaspi/{id}/merchant: продавец выбрал кабинет.
func (h *PageHandler) KaspiCabinetMerchant(w http.ResponseWriter, r *http.Request) error {
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
	if err := parseForm(w, r); err != nil {
		return err
	}

	serr := h.Onboarding.ChooseMerchant(r.Context(), p.AccountID, id, r.PostFormValue("merchant"))
	switch {
	case errors.Is(serr, service.ErrMerchantChoice):
		return h.renderCabinet(w, r, http.StatusUnprocessableEntity, p.AccountID, id, l.T("stores.err_merchant"))
	case errors.Is(serr, service.ErrOnboardingState):
		return h.renderCabinet(w, r, http.StatusConflict, p.AccountID, id, l.T("stores.err_state"))
	case errors.Is(serr, service.ErrOnboardingNotFound):
		return httpx.ErrNotFound("Онбординг не найден")
	case serr != nil:
		return serr
	}
	redirect(w, r, cabinetPath(l.Lang(), id))
	return nil
}

// KaspiCabinetSave — POST /stores/kaspi/{id}/save: продавец подтвердил собранные
// данные (имя, e-mail, пароль, токен) → создаём магазин с проверкой и ставим импорт.
func (h *PageHandler) KaspiCabinetSave(w http.ResponseWriter, r *http.Request) error {
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
	if err := parseForm(w, r); err != nil {
		return err
	}

	serr := h.Onboarding.SaveStore(r.Context(), p.AccountID, id,
		r.PostFormValue("name"), r.PostFormValue("email"), r.PostFormValue("password"), r.PostFormValue("token"))
	switch {
	case errors.Is(serr, service.ErrStoreNameRequired):
		return h.renderCabinet(w, r, http.StatusUnprocessableEntity, p.AccountID, id, l.T("validation.required"))
	case errors.Is(serr, service.ErrKaspiTokenInvalid):
		return h.renderCabinet(w, r, http.StatusUnprocessableEntity, p.AccountID, id, l.T("stores.err_token"))
	case errors.Is(serr, service.ErrOnboardingState):
		return h.renderCabinet(w, r, http.StatusConflict, p.AccountID, id, l.T("stores.err_state"))
	case errors.Is(serr, service.ErrOnboardingNotFound):
		return httpx.ErrNotFound("Онбординг не найден")
	case serr != nil:
		return serr
	}
	redirect(w, r, i18n.Localize(l.Lang(), "/account")+"?connected=1")
	return nil
}

// renderCabinet отображает страницу статуса онбординга. Для статуса 'ready'
// подгружает собранные данные в форму подтверждения.
func (h *PageHandler) renderCabinet(w http.ResponseWriter, r *http.Request, status int, accountID, id int64, alert string) error {
	o, err := h.Onboarding.Get(r.Context(), accountID, id)
	if errors.Is(err, service.ErrOnboardingNotFound) {
		return httpx.ErrNotFound("Онбординг не найден")
	}
	if err != nil {
		return err
	}
	data := h.page(r, "stores.cabinet_title").With("Onb", o).With("Alert", alert).With("Form", newForm())
	if o.Status == model.OnboardingReady {
		if rv, err := h.Onboarding.Review(r.Context(), accountID, id); err == nil {
			data = data.With("Review", rv)
		}
	}
	return h.Render.Render(w, status, "kaspi_onboarding", data)
}

// StoreEditForm — GET /stores/{id}/edit: форма редактирования магазина.
func (h *PageHandler) StoreEditForm(w http.ResponseWriter, r *http.Request) error {
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
	ev, err := h.Connections.StoreForEdit(r.Context(), p.AccountID, id)
	if errors.Is(err, service.ErrStoreNotFound) {
		return httpx.ErrNotFound("Магазин не найден")
	}
	if err != nil {
		return err
	}
	form := newForm()
	form.Values["name"] = ev.Name
	form.Values["token"] = ev.Token
	form.Values["email"] = ev.CabinetLogin
	return h.renderStoreEdit(w, r, http.StatusOK, id, form)
}

// StoreEditSubmit — POST /stores/{id}/edit: сохранение с проверкой токена.
func (h *PageHandler) StoreEditSubmit(w http.ResponseWriter, r *http.Request) error {
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
	if err := parseForm(w, r); err != nil {
		return err
	}

	form := newForm()
	form.Values["name"] = r.PostFormValue("name")
	form.Values["token"] = r.PostFormValue("token")
	form.Values["email"] = r.PostFormValue("email")

	serr := h.Connections.UpdateKaspiStore(r.Context(), p.AccountID, id,
		r.PostFormValue("name"), r.PostFormValue("token"), r.PostFormValue("email"), r.PostFormValue("password"))
	switch {
	case errors.Is(serr, service.ErrStoreNameRequired):
		form.Errors["name"] = l.T("validation.required")
		return h.renderStoreEdit(w, r, http.StatusUnprocessableEntity, id, form)
	case errors.Is(serr, service.ErrKaspiTokenInvalid):
		form.Errors["token"] = l.T("stores.err_token")
		return h.renderStoreEdit(w, r, http.StatusUnprocessableEntity, id, form)
	case errors.Is(serr, service.ErrStoreNotFound):
		return httpx.ErrNotFound("Магазин не найден")
	case serr != nil:
		return serr
	}
	redirect(w, r, i18n.Localize(l.Lang(), "/account")+"?saved=1")
	return nil
}

func (h *PageHandler) renderStoreEdit(w http.ResponseWriter, r *http.Request, status int, id int64, form FormView) error {
	action := i18n.Localize(h.localizer(r).Lang(), fmt.Sprintf("/stores/%d/edit", id))
	data := h.page(r, "stores.edit_title").With("Form", form).With("Action", action)
	return h.Render.Render(w, status, "store_edit", data)
}

const ordersPerPage = 30

// OrdersList — GET /orders: список заказов маркетплейса продавца с поиском и
// фильтром по магазину.
func (h *PageHandler) OrdersList(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	p := tenant.FromContext(r.Context())
	if p == nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/login")+"?next="+i18n.Localize(l.Lang(), "/orders"))
		return nil
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	storeID, _ := strconv.ParseInt(r.URL.Query().Get("store"), 10, 64)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	orders, total, err := h.Connections.Orders(r.Context(), p.AccountID, storeID, query, page, ordersPerPage)
	if err != nil {
		return err
	}
	stores, err := h.Connections.Stores(r.Context(), p.AccountID)
	if err != nil {
		return err
	}

	totalPages := (total + ordersPerPage - 1) / ordersPerPage
	nums := make([]int, 0, totalPages)
	for i := 1; i <= totalPages; i++ {
		nums = append(nums, i)
	}
	base := i18n.Localize(l.Lang(), "/orders") + "?q=" + url.QueryEscape(query)
	if storeID > 0 {
		base += "&store=" + strconv.FormatInt(storeID, 10)
	}
	base += "&page="

	data := h.page(r, "orders.title").
		With("Orders", orders).
		With("Stores", stores).
		With("Query", query).
		With("StoreID", storeID).
		With("Total", total).
		With("Page", page).
		With("TotalPages", totalPages).
		With("PageNums", nums).
		With("PageBase", base)
	return h.Render.Render(w, http.StatusOK, "orders", data)
}

// cabinetID читает id онбординга из пути.
func cabinetID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, httpx.ErrBadRequest("Некорректный идентификатор")
	}
	return id, nil
}

// cabinetPath строит адрес страницы статуса онбординга на нужном языке.
func cabinetPath(lang i18n.Lang, id int64) string {
	return fmt.Sprintf("%s/%d", i18n.Localize(lang, "/stores/kaspi"), id)
}
