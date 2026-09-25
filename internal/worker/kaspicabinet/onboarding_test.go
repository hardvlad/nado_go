package kaspicabinet

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// onboardServer имитирует кабинет для онбординга владельца: отправка SMS-кода,
// его проверка и создание служебного сотрудника. merchants — сколько кабинетов
// вернуть на шаге списка; existingToken — токен из getTokenApi ("" → придётся
// выпускать через tokenGenerate).
func onboardServer(t *testing.T, merchants int, existingToken string) (*httptest.Server, *onboardHits) {
	t.Helper()
	h := &onboardHits{}
	mux := http.NewServeMux()
	base := func(r *http.Request) string { return "http://" + r.Host }

	mux.HandleFunc("/mc/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/yml/ms/feat/p/ft/pre/e", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	// Первый /s/m (при отправке кода) — 401 с cookie сессии; следующий (после
	// проверки кода) — 200 со списком кабинетов.
	mux.HandleFunc("/s/m", func(w http.ResponseWriter, _ *http.Request) {
		if h.sm.Add(1) == 1 {
			w.Header().Set("Set-Cookie", "mc-session=sess; Path=/")
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
		if merchants == 1 {
			_, _ = w.Write([]byte(`{"merchants":[{"uid":"M1","name":"Shop"}]}`))
		} else {
			_, _ = w.Write([]byte(`{"merchants":[{"uid":"M1","name":"Shop 1"},{"uid":"M2","name":"Shop 2"}]}`))
		}
	})

	mux.HandleFunc("/oauth2/authorization/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "mc-arr=arr; Path=/")
		w.Header().Set("Location", base(r)+"/sso?x=1") // это redirectURL
		w.WriteHeader(302)
	})
	// /sso: при отправке кода отдаёт cookie MS_AUTH_SSO (location не используется);
	// после проверки кода тот же адрес (с &continue) ведёт дальше в mc.
	mux.HandleFunc("/sso", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "MS_AUTH_SSO=sso; Path=/")
		w.Header().Set("Location", base(r)+"/mcredir")
		w.WriteHeader(302)
	})
	mux.HandleFunc("/mcredir", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "mc-sid=sid; Path=/")
		w.Header().Set("Location", base(r)+"/mcfinal")
		w.WriteHeader(302)
	})
	mux.HandleFunc("/mcfinal", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	mux.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/api/p/login", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(body), "_ph"):
			h.sentOTP.Store(true)
		case strings.Contains(string(body), "_c"):
			h.checkedOTP.Store(true)
			w.Header().Set("Set-Cookie", "MS_AUTH_SSO=sso2; Path=/")
		}
		w.WriteHeader(200)
	})

	mux.HandleFunc("/user-assignments/api/v1/mc/users/registered", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	})
	mux.HandleFunc("/mc/facade/graphql", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		if strings.Contains(r.URL.RawQuery, "getTokenApi") {
			if existingToken == "" {
				_, _ = w.Write([]byte(`{"data":{"merchant":{"integration":{"token":null}}}}`))
			} else {
				_, _ = w.Write([]byte(`{"data":{"merchant":{"integration":{"token":"` + existingToken + `"}}}}`))
			}
			return
		}
		// tokenGenerate
		h.generated.Store(true)
		_, _ = w.Write([]byte(`{"data":{"tokenGenerate":"gen-token"}}`))
	})
	mux.HandleFunc("/user-assignments/api/v1/mc/users/add-email", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.addEmailBody = string(body)
		w.WriteHeader(200)
	})

	return httptest.NewServer(mux), h
}

type onboardHits struct {
	sm           atomic.Int32
	sentOTP      atomic.Bool
	checkedOTP   atomic.Bool
	generated    atomic.Bool
	addEmailBody string
}

func newOnboardClient(url string) *Client {
	return New(withHosts(hosts{idmc: url, kaspi: url, mc: url}))
}

func TestSendOwnerOTP(t *testing.T) {
	srv, h := onboardServer(t, 1, "existing")
	defer srv.Close()
	c := newOnboardClient(srv.URL)

	sess, err := c.SendOwnerOTP("+7 (700) 123-45-67")
	if err != nil {
		t.Fatalf("SendOwnerOTP: %v", err)
	}
	if !h.sentOTP.Load() {
		t.Error("Kaspi не получил запрос на отправку SMS-кода")
	}
	if sess.MSAuthSSO == "" || sess.MCSession == "" || sess.MCArr == "" {
		t.Errorf("cookie сессии не заполнены: %+v", sess)
	}
	if !strings.HasSuffix(sess.RedirectURL, "&continue") {
		t.Errorf("redirectURL без &continue: %q", sess.RedirectURL)
	}
}

func TestCreateEmployeeSingleMerchant(t *testing.T) {
	srv, h := onboardServer(t, 1, "existing-token")
	defer srv.Close()
	c := newOnboardClient(srv.URL)

	sess, err := c.SendOwnerOTP("+7 (700) 123-45-67")
	if err != nil {
		t.Fatalf("SendOwnerOTP: %v", err)
	}
	res, err := c.CreateEmployee(sess, "111222", "nado", "nado_1@kaspi.nado.kz", nil)
	if err != nil {
		t.Fatalf("CreateEmployee: %v", err)
	}
	if !h.checkedOTP.Load() {
		t.Error("код не проверялся")
	}
	if res.MerchantID != "M1" {
		t.Errorf("merchantID = %q", res.MerchantID)
	}
	if res.APIToken != "existing-token" {
		t.Errorf("token = %q, ожидался существующий", res.APIToken)
	}
	if h.generated.Load() {
		t.Error("токен выпущен заново, хотя уже был")
	}
	// В add-email ушли наши роли по умолчанию и наш email.
	if !strings.Contains(h.addEmailBody, "nado_1@kaspi.nado.kz") || !strings.Contains(h.addEmailBody, "MANAGE_OFFERS") {
		t.Errorf("тело add-email неверно: %s", h.addEmailBody)
	}
}

func TestCreateEmployeeGeneratesToken(t *testing.T) {
	srv, h := onboardServer(t, 1, "") // токена ещё нет
	defer srv.Close()
	c := newOnboardClient(srv.URL)

	sess, err := c.SendOwnerOTP("+7 (700) 123-45-67")
	if err != nil {
		t.Fatalf("SendOwnerOTP: %v", err)
	}
	res, err := c.CreateEmployee(sess, "111222", "nado", "nado_1@kaspi.nado.kz", nil)
	if err != nil {
		t.Fatalf("CreateEmployee: %v", err)
	}
	if !h.generated.Load() {
		t.Error("токен не выпущен, хотя его не было")
	}
	if res.APIToken != "gen-token" {
		t.Errorf("token = %q, ожидался выпущенный", res.APIToken)
	}
}

func TestCreateEmployeeSelectMerchant(t *testing.T) {
	srv, _ := onboardServer(t, 2, "existing-token")
	defer srv.Close()
	c := newOnboardClient(srv.URL)

	sess, err := c.SendOwnerOTP("+7 (700) 123-45-67")
	if err != nil {
		t.Fatalf("SendOwnerOTP: %v", err)
	}
	_, err = c.CreateEmployee(sess, "111222", "nado", "nado_1@kaspi.nado.kz", nil)
	var sel ErrSelectMerchant
	if !errors.As(err, &sel) {
		t.Fatalf("ожидалась ErrSelectMerchant, получено %v", err)
	}
	if len(sel.Merchants) != 2 {
		t.Fatalf("ожидалось 2 кабинета, получено %d", len(sel.Merchants))
	}
	// Сессия должна быть дополнена для последующего выбора.
	if sess.MCSid == "" || len(sess.Merchants) != 2 {
		t.Fatalf("сессия не дополнена состоянием выбора: %+v", sess)
	}

	// Продавец выбрал второй кабинет.
	res, err := c.CreateEmployeeForMerchant(sess, "M2", "nado", "nado_1@kaspi.nado.kz", nil)
	if err != nil {
		t.Fatalf("CreateEmployeeForMerchant: %v", err)
	}
	if res.MerchantID != "M2" {
		t.Errorf("merchantID = %q", res.MerchantID)
	}
	if res.APIToken != "existing-token" {
		t.Errorf("token = %q", res.APIToken)
	}
}
