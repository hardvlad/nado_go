package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"nado_go/internal/jobs"
	"nado_go/internal/jobsclient"
	"nado_go/internal/worker/kaspicabinet"
)

// Обработчики кабинетного онбординга Kaspi (D-28). Воркер выполняет вход в
// кабинет владельца и создание служебного сотрудника, а результат каждого шага
// отправляет на сервер (по образцу приёма каталога): сервер хранит состояние и
// показывает продавцу нужный экран.

// sendOTPPayload — задание kaspi.send_otp.
type sendOTPPayload struct {
	OnboardingID int64  `json:"onboarding_id"`
	Phone        string `json:"phone"`
}

// verifyOTPPayload — задание kaspi.verify_otp.
type verifyOTPPayload struct {
	OnboardingID     int64                     `json:"onboarding_id"`
	Session          kaspicabinet.OwnerSession `json:"session"`
	OTP              string                    `json:"otp"`
	Name             string                    `json:"name"`
	Email            string                    `json:"email"`
	SelectedMerchant string                    `json:"selected_merchant"`
}

// newKaspiSendOTPHandler выполняет вход владельца по телефону (Kaspi шлёт SMS) и
// отправляет полученную сессию на сервер.
func newKaspiSendOTPHandler(client *jobsclient.Client) jobsclient.Handler {
	return func(ctx context.Context, job jobs.LeasedJob) (json.RawMessage, error) {
		var p sendOTPPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return nil, jobs.Permanent(fmt.Errorf("worker: payload kaspi.send_otp: %w", err))
		}
		if p.OnboardingID == 0 || p.Phone == "" {
			return nil, jobs.Permanent(errors.New("worker: в задании нет onboarding_id/phone"))
		}

		cab := kaspicabinet.New()
		sess, err := cab.SendOwnerOTP(p.Phone)
		if err != nil {
			// Кабинет не пустил — повтор не поможет, сообщаем продавцу.
			reportOnboardingFail(ctx, client, p.OnboardingID, "не удалось отправить код: "+err.Error())
			return nil, jobs.Permanent(err)
		}

		path := fmt.Sprintf("/kaspi/onboarding/%d/session", p.OnboardingID)
		if _, err := client.Call(ctx, http.MethodPost, path, sess, nil); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"ok": true})
	}
}

// newKaspiVerifyOTPHandler проверяет SMS-код (или выбор кабинета) и создаёт
// служебного сотрудника, затем отправляет результат на сервер.
func newKaspiVerifyOTPHandler(client *jobsclient.Client) jobsclient.Handler {
	return func(ctx context.Context, job jobs.LeasedJob) (json.RawMessage, error) {
		var p verifyOTPPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return nil, jobs.Permanent(fmt.Errorf("worker: payload kaspi.verify_otp: %w", err))
		}
		if p.OnboardingID == 0 || p.Email == "" {
			return nil, jobs.Permanent(errors.New("worker: в задании нет onboarding_id/email"))
		}

		cab := kaspicabinet.New()
		sess := p.Session

		var (
			res *kaspicabinet.EmployeeResult
			err error
		)
		if p.SelectedMerchant != "" {
			res, err = cab.CreateEmployeeForMerchant(&sess, p.SelectedMerchant, p.Name, p.Email, nil)
		} else {
			res, err = cab.CreateEmployee(&sess, p.OTP, p.Name, p.Email, nil)
		}

		// У владельца несколько кабинетов — отдаём список для выбора продавцом.
		var sel kaspicabinet.ErrSelectMerchant
		if errors.As(err, &sel) {
			body := map[string]any{"session": sess, "merchants": sel.Merchants}
			path := fmt.Sprintf("/kaspi/onboarding/%d/merchants", p.OnboardingID)
			if _, perr := client.Call(ctx, http.MethodPost, path, body, nil); perr != nil {
				return nil, perr
			}
			return json.Marshal(map[string]any{"need_merchant": true})
		}
		if err != nil {
			reportOnboardingFail(ctx, client, p.OnboardingID, "не удалось создать сотрудника: "+err.Error())
			return nil, jobs.Permanent(err)
		}

		body := map[string]any{"merchant_id": res.MerchantID, "api_token": res.APIToken}
		path := fmt.Sprintf("/kaspi/onboarding/%d/employee", p.OnboardingID)
		if _, perr := client.Call(ctx, http.MethodPost, path, body, nil); perr != nil {
			return nil, perr
		}
		return json.Marshal(map[string]any{"merchant_id": res.MerchantID})
	}
}

// reportOnboardingFail сообщает серверу о неустранимой ошибке шага онбординга,
// чтобы продавец увидел причину. Ошибка отправки только логируется в вызывающем.
func reportOnboardingFail(ctx context.Context, client *jobsclient.Client, id int64, msg string) {
	path := fmt.Sprintf("/kaspi/onboarding/%d/fail", id)
	_, _ = client.Call(ctx, http.MethodPost, path, map[string]string{"error": msg}, nil)
}
