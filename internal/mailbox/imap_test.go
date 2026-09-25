package mailbox

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/emersion/go-imap"
)

// собирает поллер с заглушкой-приёмником и тихим логом.
func testPoller(sink Sink) *Poller {
	return &Poller{sink: sink, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

type capturingSink struct {
	codeEmail, code string
	login, password string
}

func (s *capturingSink) SaveCode(_ context.Context, email, code string) error {
	s.codeEmail, s.code = email, code
	return nil
}
func (s *capturingSink) SaveCredentials(_ context.Context, login, password string) error {
	s.login, s.password = login, password
	return nil
}

// makeMessage строит письмо с телом raw и получателем to@host для GetBody(section).
func makeMessage(section *imap.BodySectionName, raw, mailbox, host string) *imap.Message {
	msg := imap.NewMessage(1, nil)
	msg.Body = map[*imap.BodySectionName]imap.Literal{section: bytes.NewBufferString(raw)}
	if mailbox != "" {
		msg.Envelope = &imap.Envelope{To: []*imap.Address{{MailboxName: mailbox, HostName: host}}}
	}
	return msg
}

func TestHandleMessageCode(t *testing.T) {
	sink := &capturingSink{}
	p := testPoller(sink)
	section := &imap.BodySectionName{}
	raw := "Content-Type: text/plain; charset=utf-8\r\n\r\nВаш код подтверждения: 482913\r\n"
	msg := makeMessage(section, raw, "nado_1", "kaspi.nado.kz")

	seq, ok := p.handleMessage(context.Background(), msg, section)
	if !ok || seq != 1 {
		t.Fatalf("ожидалось (1, true), получено (%d, %v)", seq, ok)
	}
	if sink.codeEmail != "nado_1@kaspi.nado.kz" || sink.code != "482913" {
		t.Errorf("код сохранён неверно: email=%q code=%q", sink.codeEmail, sink.code)
	}
}

func TestHandleMessageCredentials(t *testing.T) {
	sink := &capturingSink{}
	p := testPoller(sink)
	section := &imap.BodySectionName{}
	raw := "Content-Type: text/html; charset=utf-8\r\n\r\nЛогин: nado_2@kaspi.nado.kz<br>Пароль: pw999\r\n"
	msg := makeMessage(section, raw, "service", "nado.kz")

	seq, ok := p.handleMessage(context.Background(), msg, section)
	if !ok || seq != 1 {
		t.Fatalf("ожидалось (1, true), получено (%d, %v)", seq, ok)
	}
	if sink.login != "nado_2@kaspi.nado.kz" || sink.password != "pw999" {
		t.Errorf("учётные данные сохранены неверно: %+v", sink)
	}
	if sink.code != "" {
		t.Errorf("для письма с доступом код сохранять не нужно: %q", sink.code)
	}
}

func TestHandleMessageCodeWithoutRecipient(t *testing.T) {
	// Код есть, но получатель неизвестен — сохранить некуда, не помечаем прочитанным.
	sink := &capturingSink{}
	p := testPoller(sink)
	section := &imap.BodySectionName{}
	raw := "Content-Type: text/plain; charset=utf-8\r\n\r\nВаш код подтверждения: 121212\r\n"
	msg := makeMessage(section, raw, "", "")

	if _, ok := p.handleMessage(context.Background(), msg, section); ok {
		t.Error("без адреса-получателя письмо не должно помечаться обработанным")
	}
	if sink.code != "" {
		t.Errorf("код не должен был сохраниться: %q", sink.code)
	}
}

func TestHandleMessageIrrelevant(t *testing.T) {
	// Постороннее письмо: ничего не сохраняем, но помечаем прочитанным.
	sink := &capturingSink{}
	p := testPoller(sink)
	section := &imap.BodySectionName{}
	raw := "Content-Type: text/plain; charset=utf-8\r\n\r\nРекламная рассылка\r\n"
	msg := makeMessage(section, raw, "someone", "nado.kz")

	seq, ok := p.handleMessage(context.Background(), msg, section)
	if !ok || seq != 1 {
		t.Fatalf("постороннее письмо: ожидалось (1, true), получено (%d, %v)", seq, ok)
	}
	if sink.code != "" || sink.login != "" {
		t.Errorf("постороннее письмо ничего не должно сохранять: %+v", sink)
	}
}
