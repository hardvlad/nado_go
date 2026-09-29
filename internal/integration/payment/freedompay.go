package payment

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

// FreedomPay — эквайринг Freedom Pay (бывш. PayBox), крупнейший агрегатор КЗ:
// карты, Apple/Google Pay, рассрочка. Протокол: init_payment.php + серверный
// result-вебхук. Подпись pg_sig — MD5 по алгоритму PayBox (имя скрипта; значения
// параметров в алфавитном порядке ключей; секрет — всё через ';').
//
// Формат запроса и подписи взят из публичной документации Freedom Pay. Реальные
// вызовы требуют мерчант-аккаунта и проверки в песочнице (см. launch-kz.md).
type FreedomPay struct {
	initURL string
	http    *http.Client
}

// FreedomPayInitURL — боевой адрес инициализации платежа.
const FreedomPayInitURL = "https://api.freedompay.kz/init_payment.php"

func NewFreedomPay() *FreedomPay {
	return &FreedomPay{
		initURL: FreedomPayInitURL,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (FreedomPay) Code() string { return "freedompay" }
func (FreedomPay) Manual() bool { return false }

type fpInitResponse struct {
	XMLName     xml.Name `xml:"response"`
	Status      string   `xml:"pg_status"`
	PaymentID   string   `xml:"pg_payment_id"`
	RedirectURL string   `xml:"pg_redirect_url"`
	ErrorCode   string   `xml:"pg_error_code"`
	ErrorDesc   string   `xml:"pg_error_description"`
}

// Start инициализирует платёж и возвращает адрес платёжной страницы.
func (p *FreedomPay) Start(ctx context.Context, in StartInput) (*StartResult, error) {
	params := map[string]string{
		"pg_merchant_id":    in.Creds.MerchantID,
		"pg_order_id":       in.Ref,
		"pg_amount":         MajorAmount(in.AmountMinor),
		"pg_currency":       defaultCurrency(in.Currency),
		"pg_description":    truncate(in.Description, 255),
		"pg_salt":           randSalt(),
		"pg_result_url":     in.CallbackURL,
		"pg_success_url":    in.SuccessURL,
		"pg_failure_url":    in.FailURL,
		"pg_request_method": "POST",
	}
	if in.Email != "" {
		params["pg_user_contact_email"] = in.Email
	}
	if in.Phone != "" {
		params["pg_user_phone"] = strings.TrimPrefix(in.Phone, "+")
	}
	if in.Creds.Testing {
		params["pg_testing_mode"] = "1"
	}
	params["pg_sig"] = fpSign("init_payment.php", params, in.Creds.Secret)

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.initURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("payment: freedompay init: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("payment: freedompay init: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var out fpInitResponse
	if err := xml.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("payment: freedompay init: ответ не разобран: %w", err)
	}
	if !strings.EqualFold(out.Status, "ok") || out.RedirectURL == "" {
		return nil, fmt.Errorf("payment: freedompay init отклонён: %s %s", out.ErrorCode, out.ErrorDesc)
	}
	return &StartResult{RedirectURL: out.RedirectURL, ProviderRef: out.PaymentID, Status: StatusPending}, nil
}

// ParseCallback разбирает result-вебхук и проверяет подпись. Имя скрипта для
// подписи — последний сегмент пути нашего result_url (мы его и задаём в CallbackURL).
func (p *FreedomPay) ParseCallback(r *http.Request, creds Credentials) (*Callback, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("payment: freedompay callback: %w", err)
	}
	params := map[string]string{}
	for k := range r.PostForm {
		params[k] = r.PostForm.Get(k)
	}
	got := params["pg_sig"]
	scriptName := path.Base(r.URL.Path)
	want := fpSign(scriptName, params, creds.Secret)
	if got == "" || !strings.EqualFold(got, want) {
		return nil, fmt.Errorf("payment: freedompay callback: подпись не совпала")
	}

	status := StatusFailed
	if params["pg_result"] == "1" {
		status = StatusSucceeded
	}
	if params["pg_can_reject"] == "1" && status != StatusSucceeded {
		status = StatusPending // провайдер спрашивает разрешение — платёж ещё не финальный
	}
	raw, _ := io.ReadAll(io.LimitReader(strings.NewReader(r.PostForm.Encode()), 1<<20))
	return &Callback{
		Ref:         params["pg_order_id"],
		ProviderRef: params["pg_payment_id"],
		Status:      status,
		AmountMinor: 0,
		Raw:         raw,
	}, nil
}

// CallbackResponse подтверждает приём вебхука в формате, который ждёт Freedom Pay.
func (FreedomPay) CallbackResponse(ok bool) (string, []byte) {
	status := "rejected"
	if ok {
		status = "ok"
	}
	return "application/xml", []byte(`<?xml version="1.0" encoding="utf-8"?><response><pg_status>` + status + `</pg_status></response>`)
}

// fpSign считает pg_sig: md5(scriptName; значения_по_алфавиту_ключей; secret).
// pg_sig в подпись не входит.
func fpSign(scriptName string, params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "pg_sig" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+2)
	parts = append(parts, scriptName)
	for _, k := range keys {
		parts = append(parts, params[k])
	}
	parts = append(parts, secret)
	sum := md5.Sum([]byte(strings.Join(parts, ";")))
	return hex.EncodeToString(sum[:])
}

func defaultCurrency(c string) string {
	if c == "" {
		return "KZT"
	}
	return c
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func randSalt() string {
	sum := md5.Sum([]byte(time.Now().Format(time.RFC3339Nano)))
	return hex.EncodeToString(sum[:])[:16]
}
