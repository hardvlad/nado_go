package greenapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// testMgmt направляет Management на локальный сервер (http вместо https).
func testMgmt(t *testing.T, srv *httptest.Server) (*Management, string) {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	m := NewManagement()
	m.scheme = "http"
	return m, u.Host
}

func TestCreateInstance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/partner/createInstance/ptoken") {
			t.Errorf("неожиданный путь: %s", r.URL.Path)
		}
		w.Write([]byte(`{"idInstance":1103000000,"apiTokenInstance":"abc123"}`))
	}))
	defer srv.Close()

	m, host := testMgmt(t, srv)
	got, err := m.CreateInstance(context.Background(), CreateInstanceRequest{
		PartnerDomain: host, PartnerToken: "ptoken",
		WebhookURL: "https://nado.kz/webhooks/greenapi/w", WebhookToken: "w",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.InstanceID != "1103000000" || got.Token != "abc123" || got.APIDomain != host {
		t.Fatalf("создание инстанса: %+v", got)
	}
}

func TestState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/waInstance1103/getStateInstance/tok") {
			t.Errorf("неожиданный путь: %s", r.URL.Path)
		}
		w.Write([]byte(`{"stateInstance":"authorized"}`))
	}))
	defer srv.Close()

	m, host := testMgmt(t, srv)
	state, err := m.State(context.Background(), host, "1103", "tok")
	if err != nil || state != "authorized" {
		t.Fatalf("состояние: %q, %v", state, err)
	}
}

func TestQRAuthorizedReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"type":"alreadyLogged","message":"instance account already authorized"}`))
	}))
	defer srv.Close()

	m, host := testMgmt(t, srv)
	qr, err := m.QR(context.Background(), host, "1103", "tok")
	if err != nil || qr != "" {
		t.Fatalf("для авторизованного инстанса QR должен быть пуст: %q, %v", qr, err)
	}
}
