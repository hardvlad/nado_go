package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/service"
	"nado_go/internal/tenant"
)

// Подключение магазина к Kaspi по токену официального Shop API.

// StoreConnectForm — GET /stores/new.
func (h *PageHandler) StoreConnectForm(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)
	if tenant.FromContext(r.Context()) == nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/login")+"?next="+i18n.Localize(l.Lang(), "/stores/new"))
		return nil
	}
	return h.renderStoreConnect(w, r, http.StatusOK, newForm())
}

// StoreConnectSubmit — POST /stores/new.
func (h *PageHandler) StoreConnectSubmit(w http.ResponseWriter, r *http.Request) error {
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
	form.Values["name"] = r.PostFormValue("name")

	name := form.Values["name"]
	token := r.PostFormValue("token")

	_, err := h.Connections.ConnectKaspi(r.Context(), p.AccountID, name, token)
	switch {
	case errors.Is(err, service.ErrStoreNameRequired):
		form.Errors["name"] = l.T("validation.required")
		return h.renderStoreConnect(w, r, http.StatusUnprocessableEntity, form)
	case errors.Is(err, service.ErrKaspiTokenInvalid):
		form.Errors["token"] = l.T("stores.err_token")
		return h.renderStoreConnect(w, r, http.StatusUnprocessableEntity, form)
	case err != nil:
		return err
	}

	redirect(w, r, i18n.Localize(l.Lang(), "/account")+"?connected=1")
	return nil
}

func (h *PageHandler) renderStoreConnect(w http.ResponseWriter, r *http.Request, status int, form FormView) error {
	data := h.page(r, "stores.connect_title").With("Form", form).
		With("CabinetEnabled", h.Onboarding != nil)
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

// renderCabinet отображает страницу статуса онбординга.
func (h *PageHandler) renderCabinet(w http.ResponseWriter, r *http.Request, status int, accountID, id int64, alert string) error {
	o, err := h.Onboarding.Get(r.Context(), accountID, id)
	if errors.Is(err, service.ErrOnboardingNotFound) {
		return httpx.ErrNotFound("Онбординг не найден")
	}
	if err != nil {
		return err
	}
	data := h.page(r, "stores.cabinet_title").With("Onb", o).With("Alert", alert).With("Form", newForm())
	return h.Render.Render(w, status, "kaspi_onboarding", data)
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
