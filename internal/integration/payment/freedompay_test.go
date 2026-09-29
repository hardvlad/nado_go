package payment

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestFPSignStableAndOrderIndependent(t *testing.T) {
	a := map[string]string{"pg_merchant_id": "42", "pg_order_id": "abc", "pg_amount": "1990.00"}
	b := map[string]string{"pg_amount": "1990.00", "pg_order_id": "abc", "pg_merchant_id": "42"}
	if fpSign("init_payment.php", a, "secret") != fpSign("init_payment.php", b, "secret") {
		t.Fatal("подпись должна не зависеть от порядка ключей в map")
	}
	// pg_sig в подпись не входит.
	c := map[string]string{"pg_merchant_id": "42", "pg_order_id": "abc", "pg_amount": "1990.00", "pg_sig": "whatever"}
	if fpSign("init_payment.php", a, "secret") != fpSign("init_payment.php", c, "secret") {
		t.Fatal("pg_sig не должен влиять на подпись")
	}
	if fpSign("init_payment.php", a, "secret") == fpSign("init_payment.php", a, "other") {
		t.Fatal("разный секрет — разная подпись")
	}
}

func TestFreedomPayParseCallback(t *testing.T) {
	p := NewFreedomPay()
	creds := Credentials{MerchantID: "42", Secret: "s3cr3t"}

	form := url.Values{
		"pg_order_id":   {"pay-100"},
		"pg_payment_id": {"9999"},
		"pg_result":     {"1"},
		"pg_amount":     {"1990.00"},
		"pg_salt":       {"abc"},
	}
	// Подпись как её посчитал бы провайдер: имя скрипта = последний сегмент пути.
	params := map[string]string{}
	for k := range form {
		params[k] = form.Get(k)
	}
	form.Set("pg_sig", fpSign("tok123", params, creds.Secret))

	req := httptest.NewRequest("POST", "/webhooks/payment/freedompay/tok123", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	cb, err := p.ParseCallback(req, creds)
	if err != nil {
		t.Fatalf("валидный вебхук отклонён: %v", err)
	}
	if cb.Ref != "pay-100" || cb.Status != StatusSucceeded {
		t.Fatalf("неверно разобран вебхук: %+v", cb)
	}

	// Подделанная подпись отклоняется.
	form.Set("pg_sig", "deadbeef")
	req2 := httptest.NewRequest("POST", "/webhooks/payment/freedompay/tok123", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := p.ParseCallback(req2, creds); err == nil {
		t.Fatal("вебхук с неверной подписью должен быть отклонён")
	}
}

func TestMajorAmount(t *testing.T) {
	cases := map[int64]string{0: "0.00", 5: "0.05", 199000: "1990.00", 199050: "1990.50"}
	for in, want := range cases {
		if got := MajorAmount(in); got != want {
			t.Errorf("MajorAmount(%d)=%q, ждали %q", in, got, want)
		}
	}
}
