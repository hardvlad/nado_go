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

	form := url.Values{
		"grant_type":      {"client_credentials"},
		"scope":           {"payment"},
		"client_id":       {in.Creds.MerchantID},
		"client_secret":   {in.Creds.Secret},
		"invoiceID":       {in.Ref},
		"amount":          {amount},
		"currency":        {defaultCurrency(in.Currency)},
		"terminal":        {in.Creds.Terminal},
		"postLink":        {in.CallbackURL},
		"failurePostLink": {in.CallbackURL},
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
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("payment: halyk oauth: ответ не разобран: %w", err)
	}
	if _, ok := token["access_token"]; !ok || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("payment: halyk oauth отклонён (%d)", resp.StatusCode)
	}

	// Конфиг для halyk.pay(...) — как в референсе EPay.php.
	cfg := map[string]any{
		"invoiceId":       in.Ref,
		"backLink":        in.SuccessURL,
		"failureBackLink": in.FailURL,
		"postLink":        in.CallbackURL,
		"failurePostLink": in.CallbackURL,
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

// ParseCallback разбирает postLink ePay (JSON тела). token в пути уже привязал
// платёж к мерчанту; успех определяется по коду/статусу.
//
// ВНИМАНИЕ: для прода стоит дополнительно перепроверять статус транзакции через
// API ePay (check-status) — здесь минимальная проверка по телу postLink.
func (p *Halyk) ParseCallback(r *http.Request, _ Credentials) (*Callback, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("payment: halyk callback: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("payment: halyk callback: тело не разобрано: %w", err)
	}
	ref := firstString(m, "invoiceId", "invoiceID", "orderId")
	if ref == "" {
		return nil, fmt.Errorf("payment: halyk callback: нет invoiceId")
	}
	status := StatusFailed
	if halykSuccess(m) {
		status = StatusSucceeded
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

func randHex(n int) string {
	sum := md5.Sum([]byte(time.Now().Format(time.RFC3339Nano) + strconv.Itoa(n)))
	return hex.EncodeToString(sum[:])[:n]
}
