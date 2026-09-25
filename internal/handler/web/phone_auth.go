package web

import (
	"errors"
	"net/http"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/model"
	"nado_go/internal/service"
	"nado_go/internal/tenant"
)

// Вход и регистрация продавца по номеру телефона с кодом в WhatsApp.
// Шаг 1 (/phone): ввод номера → отправка кода. Шаг 2 (/phone/verify): ввод кода
// (и имени для нового номера) → сессия. Вход по email+паролю остаётся отдельно.

// PhoneForm — GET /phone: ввод номера.
func (h *PageHandler) PhoneForm(w http.ResponseWriter, r *http.Request) error {
	if tenant.FromContext(r.Context()) != nil {
		redirect(w, r, i18n.Localize(h.localizer(r).Lang(), "/account"))
		return nil
	}
	return h.renderPhone(w, r, http.StatusOK, newForm())
}

// PhoneRequest — POST /phone: отправка кода на номер.
func (h *PageHandler) PhoneRequest(w http.ResponseWriter, r *http.Request) error {
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)

	form := newForm()
	form.Values["phone"] = r.PostFormValue("phone")

	registered, err := h.Auth.PhoneRegistered(r.Context(), form.Values["phone"])
	if errors.Is(err, service.ErrOTPInvalidPhone) {
		form.Errors["phone"] = l.T("validation.phone")
		return h.renderPhone(w, r, http.StatusUnprocessableEntity, form)
	}
	if err != nil {
		return err
	}

	purpose := model.OTPPurposeLogin
	if !registered {
		purpose = model.OTPPurposeRegister
	}

	phone, err := h.OTP.Request(r.Context(), form.Values["phone"], purpose, string(l.Lang()), httpx.ClientIP(r))
	if err != nil {
		if msg, ok := otpErrorMessage(l, err); ok {
			form.Alert = msg
			return h.renderPhone(w, r, http.StatusUnprocessableEntity, form)
		}
		return err
	}

	// Переходим к вводу кода. Нормализованный номер и цель — в скрытых полях;
	// решение «вход или регистрация» на шаге проверки перепринимается заново.
	verify := newForm()
	verify.Values["phone"] = phone
	verify.Values["purpose"] = purpose
	return h.renderPhoneVerify(w, r, http.StatusOK, verify, !registered)
}

// PhoneVerify — POST /phone/verify: проверка кода и вход/регистрация.
func (h *PageHandler) PhoneVerify(w http.ResponseWriter, r *http.Request) error {
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)

	form := newForm()
	form.Values["phone"] = r.PostFormValue("phone")
	form.Values["purpose"] = r.PostFormValue("purpose")
	form.Values["name"] = r.PostFormValue("name")
	purpose := form.Values["purpose"]
	if purpose != model.OTPPurposeLogin && purpose != model.OTPPurposeRegister {
		purpose = model.OTPPurposeLogin
	}

	// Решение вход/регистрация берём из БД, а не из скрытого поля.
	registered, err := h.Auth.PhoneRegistered(r.Context(), form.Values["phone"])
	if err != nil && !errors.Is(err, service.ErrOTPInvalidPhone) {
		return err
	}
	needName := !registered

	if err := h.OTP.Verify(r.Context(), form.Values["phone"], purpose, r.PostFormValue("code")); err != nil {
		if msg, ok := otpErrorMessage(l, err); ok {
			form.Errors["code"] = msg
			return h.renderPhoneVerify(w, r, http.StatusUnprocessableEntity, form, needName)
		}
		return err
	}

	var token string
	if registered {
		token, err = h.Auth.LoginByPhone(r.Context(), form.Values["phone"], clientMeta(r))
	} else {
		token, err = h.Auth.RegisterByPhone(r.Context(), form.Values["name"], form.Values["phone"], string(l.Lang()), clientMeta(r))
	}
	if fields, ok := httpx.FieldErrors(err); ok {
		form.Errors = localizeFields(l, fields)
		return h.renderPhoneVerify(w, r, http.StatusUnprocessableEntity, form, needName)
	}
	if errors.Is(err, service.ErrPhoneNotRegistered) {
		// Номер исчез между шагами — начать заново.
		form.Alert = l.T("phone.restart")
		return h.renderPhone(w, r, http.StatusUnprocessableEntity, form)
	}
	if errors.Is(err, service.ErrPhoneTaken) {
		token, err = h.Auth.LoginByPhone(r.Context(), form.Values["phone"], clientMeta(r))
	}
	if err != nil {
		return err
	}

	h.setSession(w, token)
	redirect(w, r, i18n.Localize(l.Lang(), "/account"))
	return nil
}

func (h *PageHandler) renderPhone(w http.ResponseWriter, r *http.Request, status int, form FormView) error {
	data := h.page(r, "phone.title").With("Form", form)
	return h.Render.Render(w, status, "phone", data)
}

func (h *PageHandler) renderPhoneVerify(w http.ResponseWriter, r *http.Request, status int, form FormView, needName bool) error {
	data := h.page(r, "phone.title").With("Form", form).With("NeedName", needName)
	return h.Render.Render(w, status, "phone_verify", data)
}

// otpErrorMessage переводит ошибки OTP в текст на языке страницы.
// Второй результат false — ошибка не «пользовательская» (внутренняя).
func otpErrorMessage(l *i18n.Localizer, err error) (string, bool) {
	switch {
	case errors.Is(err, service.ErrOTPWrongCode), errors.Is(err, service.ErrOTPNotFound):
		return l.T("phone.err_wrong_code"), true
	case errors.Is(err, service.ErrOTPExpired):
		return l.T("phone.err_expired"), true
	case errors.Is(err, service.ErrOTPTooManyAttempts):
		return l.T("phone.err_too_many"), true
	case errors.Is(err, service.ErrOTPCooldown):
		return l.T("phone.err_cooldown"), true
	case errors.Is(err, service.ErrOTPRateLimited):
		return l.T("phone.err_rate_limited"), true
	case errors.Is(err, service.ErrOTPSendFailed):
		return l.T("phone.err_send"), true
	case errors.Is(err, service.ErrOTPInvalidPhone):
		return l.T("validation.phone"), true
	default:
		return "", false
	}
}
