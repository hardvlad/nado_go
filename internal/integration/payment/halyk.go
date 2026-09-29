package payment

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Halyk — эквайринг Halyk Bank (ePay). Клиентский виджет: сервер получает
// OAuth-токен (client_credentials), затем страница подключает payment-api.js и
// вызывает halyk.pay(config). Результат приходит на postLink (наш вебхук).
//
// Схема и поля взяты из референса продавца (docs/project/kaspi_samples/EPay.php).
// Три реквизита мерчанта: ClientID (=MerchantID), ClientSecret (=Secret),
// TerminalID (=Terminal). Боевой приём требует терминала ePay и проверки в
// песочнице (см. launch-kz.md).
type Halyk struct {
	http *http.Client
	// Адреса ePay для боевого и тестового режима (выбор по creds.Testing).
	oauthProd, oauthTest string
	libProd, libTest     string
}

func NewHalyk() *Halyk {
	return &Halyk{
		http:      &http.Client{Timeout: 20 * time.Second},
		oauthProd: "https://epay-oauth.homebank.kz/oauth2/token",
		oauthTest: "https://testoauth.homebank.kz/epay2/oauth2/token",
		libProd:   "https://epay.homebank.kz/payform/payment-api.js",
		libTest:   "https://test-epay.homebank.kz/payform/payment-api.js",
	}
}

func (Halyk) Code() string { return "halyk" }
func (Halyk) Manual() bool { return false }

// HalykScriptHosts — домены ePay для ослабления CSP на странице виджета.
var HalykScriptHosts = []string{"https://epay.homebank.kz", "https://test-epay.homebank.kz"}

// Start получает OAuth-токен и собирает конфиг виджета.
func (p *Halyk) Start(ctx context.Context, in StartInput) (*StartResult, error) {
	oauthURL, libURL := p.oauthProd, p.libProd
	if in.Creds.Testing {
		oauthURL, libURL = p.oauthTest, p.libTest
	}
	amount := strconv.FormatInt(in.AmountMinor/100, 10) // ePay: сумма в тенге (целое)
	secretHash := randHex(20)

	// ePay зовёт postLink при успехе и failurePostLink при отказе и сохраняет наши
	// query-параметры. Кладём в них наш invoiceId (для поиска платежа, т.к. тело
	// GET-callback пустое) и признак result — по нему определяем исход.
	successCb := withParams(in.CallbackURL, "invoiceId", in.Ref, "result", "success")
	failureCb := withParams(in.CallbackURL, "invoiceId", in.Ref, "result", "fail")

	form := url.Values{
		"grant_type":      {"client_credentials"},
		"scope":           {"payment usermanagement"},
		"client_id":       {in.Creds.MerchantID},
		"client_secret":   {in.Creds.Secret},
		"invoiceID":       {in.Ref},
		"amount":          {amount},
		"currency":        {defaultCurrency(in.Currency)},
		"terminal":        {in.Creds.Terminal},
		"postLink":        {successCb},
		"failurePostLink": {failureCb},
		"secret_hash":     {secretHash},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("payment: halyk oauth: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("payment: halyk oauth: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var token map[string]any
	if jsonErr := json.Unmarshal(body, &token); jsonErr != nil {
		return nil, fmt.Errorf("payment: halyk oauth (%d): ответ не разобран: %s", resp.StatusCode, snippet(body))
	}
	if _, ok := token["access_token"]; !ok || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("payment: halyk oauth отклонён (%d): %s", resp.StatusCode, snippet(body))
	}

	// Конфиг для halyk.pay(...) — как в референсе EPay.php.
	cfg := map[string]any{
		"invoiceId":       in.Ref,
		"backLink":        in.SuccessURL,
		"failureBackLink": in.FailURL,
		"postLink":        successCb,
		"failurePostLink": failureCb,
		"language":        "RU",
		"description":     in.Description,
		"accountId":       "",
		"terminal":        in.Creds.Terminal,
		"amount":          in.AmountMinor / 100,
		"currency":        defaultCurrency(in.Currency),
		"auth":            token,
		"phone":           strings.TrimPrefix(in.Phone, "+"),
		"email":           in.Email,
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("payment: halyk config: %w", err)
	}
	return &StartResult{
		Widget: &WidgetPage{LibURL: libURL, ConfigJSON: string(cfgJSON)},
		Status: StatusPending,
	}, nil
}

// ParseCallback разбирает результат ePay. ePay шлёт его на postLink как GET с
// параметрами в query, либо как POST (JSON или форма) — поддерживаем всё. token в
// пути уже привязал платёж к мерчанту; успех определяется по коду/статусу.
//
// ВНИМАНИЕ: для прода стоит дополнительно перепроверять статус транзакции через
// API ePay (check-status) — здесь проверка по телу/параметрам callback.
func (p *Halyk) ParseCallback(r *http.Request, _ Credentials) (*Callback, error) {
	m := map[string]any{}
	// Параметры из query (GET-callback ePay).
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	// Тело (POST): JSON или form-urlencoded.
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if len(body) > 0 {
		var jm map[string]any
		if json.Unmarshal(body, &jm) == nil {
			for k, v := range jm {
				m[k] = v
			}
		} else if form, err := url.ParseQuery(string(body)); err == nil {
			for k, v := range form {
				if len(v) > 0 {
					m[k] = v[0]
				}
			}
		}
	}

	ref := firstString(m, "invoiceId", "invoiceID", "orderId", "order_id", "orderid")
	if ref == "" {
		return nil, fmt.Errorf("payment: halyk callback: нет invoiceId (%s)", snippet(body))
	}
	// Исход: сперва по нашему признаку result (ePay зовёт postLink=success /
	// failurePostLink=fail), затем — по коду/статусу из тела, если он есть.
	status := StatusFailed
	switch firstString(m, "result") {
	case "success":
		status = StatusSucceeded
	case "fail":
		status = StatusFailed
	default:
		if halykSuccess(m) {
			status = StatusSucceeded
		}
	}
	return &Callback{
		Ref:         ref,
		ProviderRef: firstString(m, "id", "reference", "transactionId"),
		Status:      status,
		Raw:         body,
	}, nil
}

func (Halyk) CallbackResponse(ok bool) (string, []byte) {
	status := "error"
	if ok {
		status = "ok"
	}
	return "application/json", []byte(`{"status":"` + status + `"}`)
}

// halykSuccess определяет успех по разным вариантам поля результата ePay.
func halykSuccess(m map[string]any) bool {
	switch v := m["code"].(type) {
	case float64:
		if v == 0 {
			return true
		}
	case string:
		if isSuccessWord(v) {
			return true
		}
	}
	if s, ok := m["status"].(string); ok && isSuccessWord(s) {
		return true
	}
	if b, ok := m["success"].(bool); ok && b {
		return true
	}
	return false
}

func isSuccessWord(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0", "ok", "success", "auth", "charge", "paid", "captured", "approved":
		return true
	}
	return false
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// withParams добавляет query-параметры к URL (ePay сохраняет их при вызове
// postLink/failurePostLink и возвращает нам обратно).
func withParams(base string, kv ...string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// snippet — короткий фрагмент тела ответа для диагностики ошибки (без переводов
// строк). Тело ошибки OAuth ePay содержит error/error_description, не секреты.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

func randHex(n int) string {
	sum := md5.Sum([]byte(time.Now().Format(time.RFC3339Nano) + strconv.Itoa(n)))
	return hex.EncodeToString(sum[:])[:n]
}
