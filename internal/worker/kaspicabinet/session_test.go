package kaspicabinet

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Ответы кабинета ставят несколько cookie; нужный — последний (как в образце
// на PHP). Регрессия на баг, из-за которого сохранялась первая cookie → 401.
func TestDoUsesLastSetCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "decoy=1; Path=/")
		w.Header().Add("Set-Cookie", "mc-session=real; Path=/; HttpOnly")
		w.WriteHeader(401)
	}))
	defer srv.Close()

	c := New(withHosts(hosts{mc: srv.URL, kaspi: srv.URL, idmc: srv.URL}))
	resp, err := c.do("GET", srv.URL+"/s/m", []string{hUA}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.setCookie != "mc-session=real" {
		t.Errorf("взята cookie %q, ожидалась последняя mc-session=real", resp.setCookie)
	}
}

// Тело ответа должно распаковываться прозрачно (net/http сам добавляет gzip,
// раз мы не проставляем Accept-Encoding сами).
func TestDoDecompressesGzip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "" {
			// net/http сам ставит Accept-Encoding: gzip и распаковывает ответ.
			// Наш Accept-Encoding из headers сюда попасть не должен.
			if r.Header.Get("Accept-Encoding") == "gzip, deflate, br, zstd" {
				t.Errorf("клиент отправил свой Accept-Encoding: %q", r.Header.Get("Accept-Encoding"))
			}
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write([]byte(`{"merchants":[{"uid":"M1","name":"Shop"}]}`))
		_ = zw.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	c := New(withHosts(hosts{mc: srv.URL, kaspi: srv.URL, idmc: srv.URL}))
	resp, err := c.do("GET", srv.URL+"/s/m", []string{hUA, hAccEnc}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(resp.body, []byte(`"uid":"M1"`)) {
		t.Errorf("тело не распаковано: %q", resp.body)
	}
}
