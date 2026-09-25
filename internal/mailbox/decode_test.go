package mailbox

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecodeMessageBase64(t *testing.T) {
	// Одиночная часть text/plain в UTF-8, base64 — как раскодировал бы образец.
	payload := "Ваш код подтверждения: 482913"
	raw := "Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString([]byte(payload)) + "\r\n"

	text, err := decodeMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, payload) {
		t.Fatalf("текст не раскодирован: %q", text)
	}
	if got := Parse(text); got.Code != "482913" {
		t.Errorf("код из письма = %q", got.Code)
	}
}

func TestDecodeMessageQuotedPrintable(t *testing.T) {
	// quoted-printable: латиница проходит как есть, достаточно для строк доступа.
	raw := "Content-Type: text/html; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
		"=D0=9B=D0=BE=D0=B3=D0=B8=D0=BD: nado_1@kaspi.nado.kz<br>" +
		"=D0=9F=D0=B0=D1=80=D0=BE=D0=BB=D1=8C: pw777\r\n"

	text, err := decodeMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	got := Parse(text)
	if got.Login != "nado_1@kaspi.nado.kz" || got.Password != "pw777" {
		t.Fatalf("учётные данные из quoted-printable: %+v (текст %q)", got, text)
	}
}

func TestDecodeMessageMultipart(t *testing.T) {
	// multipart/alternative: код лежит в text/plain части.
	raw := "Content-Type: multipart/alternative; boundary=\"b1\"\r\n\r\n" +
		"--b1\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"Ваш код подтверждения: 314159\r\n" +
		"--b1\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n\r\n" +
		"<p>Ваш код подтверждения: 314159</p>\r\n" +
		"--b1--\r\n"

	text, err := decodeMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := Parse(text); got.Code != "314159" {
		t.Errorf("код из multipart = %q (текст %q)", got.Code, text)
	}
}
