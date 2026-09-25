package kaspicabinet

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeKaspi имитирует последовательность кабинета: один сервер отвечает за все
// хосты (idmc/kaspi/mc). Проверяет, что клиент проходит все шаги входа.
func fakeKaspi(t *testing.T) *httptest.Server {
	t.Helper()
	var smHits atomic.Int32

	mux := http.NewServeMux()
	base := func(r *http.Request) string { return "http://" + r.Host }

	mux.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	})

	mux.HandleFunc("/api/p/login", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Set-Cookie", "MS_AUTH_SSO=ssovalue; Path=/; HttpOnly")
		if strings.Contains(string(body), "_m_c") {
			// Второй запрос — с кодом: возвращаем redirectUrl.
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"redirectUrl":"/redir"}`))
			return
		}
		// Первый запрос — без кода: просим код (нет redirectUrl).
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	})

	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", base(r)+"/after")
		w.WriteHeader(302)
	})
	mux.HandleFunc("/after", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/yml/ms/feat/p/ft/pre/e", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	mux.HandleFunc("/s/m", func(w http.ResponseWriter, r *http.Request) {
		n := smHits.Add(1)
		if n <= 2 {
			w.Header().Set("Set-Cookie", "mc-session=sess; Path=/")
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"merchants":[{"uid":"30297603","name":"Shop"}]}`))
	})

	mux.HandleFunc("/oauth2/authorization/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "mc-arr=arr; Path=/")
		w.Header().Set("Location", base(r)+"/oauthredir1")
		w.WriteHeader(302)
	})
	mux.HandleFunc("/oauthredir1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", base(r)+"/oauthredir2")
		w.WriteHeader(302)
	})
	mux.HandleFunc("/oauthredir2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "mc-sid=sid; Path=/")
		w.Header().Set("Location", base(r)+"/done")
		w.WriteHeader(302)
	})

	return httptest.NewServer(mux)
}

func TestLoginFullFlow(t *testing.T) {
	srv := fakeKaspi(t)
	defer srv.Close()

	var codeAsked bool
	c := New(
		withHosts(hosts{idmc: srv.URL, kaspi: srv.URL, mc: srv.URL}),
		WithCodeProvider(func(email string) (string, error) {
			codeAsked = true
			if email != "nado_1@kaspi.nado.kz" {
				t.Errorf("код запрошен для неверного адреса: %s", email)
			}
			return "123456", nil
		}),
	)

	sess, err := c.Login("nado_1@kaspi.nado.kz", "secret", "")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !codeAsked {
		t.Error("код подтверждения не запрашивался")
	}
	if !sess.Valid() {
		t.Fatalf("сессия неполная: %+v", sess)
	}
	if sess.MerchantID != "30297603" {
		t.Errorf("merchantID = %q", sess.MerchantID)
	}
	if !strings.HasPrefix(sess.AmpCookie, "amp_6e9c16=") {
		t.Errorf("amp cookie неверного формата: %q", sess.AmpCookie)
	}
}

func TestLoginNeedsCodeProvider(t *testing.T) {
	srv := fakeKaspi(t)
	defer srv.Close()

	c := New(withHosts(hosts{idmc: srv.URL, kaspi: srv.URL, mc: srv.URL}))
	if _, err := c.Login("nado_1@kaspi.nado.kz", "secret", ""); err != ErrNeedCode {
		t.Fatalf("без CodeProvider ожидалась ErrNeedCode, получено %v", err)
	}
}

func TestLoginSelectMerchant(t *testing.T) {
	// Сервер, где у сотрудника два кабинета.
	var smHits atomic.Int32
	mux := http.NewServeMux()
	base := func(r *http.Request) string { return "http://" + r.Host }
	mux.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/api/p/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Set-Cookie", "MS_AUTH_SSO=x")
		_, _ = w.Write([]byte(`{"redirectUrl":"/redir"}`))
	})
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", base(r)+"/after")
		w.WriteHeader(302)
	})
	mux.HandleFunc("/after", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/yml/ms/feat/p/ft/pre/e", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/s/m", func(w http.ResponseWriter, _ *http.Request) {
		if smHits.Add(1) <= 2 {
			w.Header().Set("Set-Cookie", "mc-session=s")
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"merchants":[{"uid":"1","name":"A"},{"uid":"2","name":"B"}]}`))
	})
	mux.HandleFunc("/oauth2/authorization/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "mc-arr=a")
		w.Header().Set("Location", base(r)+"/o1")
		w.WriteHeader(302)
	})
	mux.HandleFunc("/o1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", base(r)+"/o2")
		w.WriteHeader(302)
	})
	mux.HandleFunc("/o2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "mc-sid=s")
		w.Header().Set("Location", base(r)+"/done")
		w.WriteHeader(302)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(withHosts(hosts{idmc: srv.URL, kaspi: srv.URL, mc: srv.URL}))
	_, err := c.Login("nado_1@kaspi.nado.kz", "secret", "")
	var sel ErrSelectMerchant
	if !isSelect(err, &sel) || len(sel.Merchants) != 2 {
		t.Fatalf("ожидался выбор из 2 кабинетов, получено %v", err)
	}

	// С выбранным merchant вход проходит.
	smHits.Store(0)
	sess, err := c.Login("nado_1@kaspi.nado.kz", "secret", "2")
	if err != nil || sess.MerchantID != "2" {
		t.Fatalf("с выбранным merchant: %+v, %v", sess, err)
	}
}

func isSelect(err error, target *ErrSelectMerchant) bool {
	e, ok := err.(ErrSelectMerchant)
	if ok {
		*target = e
	}
	return ok
}
