package mailbox

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

// Config — параметры подключения к общему почтовому ящику служебных сотрудников.
type Config struct {
	Addr     string        // host:port IMAPS, напр. mail.nado.kz:993
	User     string        // логин ящика (catch-all служебных адресов)
	Password string        // пароль ящика
	Mailbox  string        // папка, обычно INBOX
	Poll     time.Duration // период опроса
	// ServerName для проверки TLS-сертификата, если отличается от хоста в Addr.
	ServerName string
}

// Sink принимает извлечённые из писем данные. Разделён на две операции, потому
// что письмо бывает двух видов: код подтверждения входа и учётные данные нового
// служебного сотрудника (онбординг).
type Sink interface {
	// SaveCode сохраняет код подтверждения для адреса-получателя (кому пришло).
	SaveCode(ctx context.Context, email, code string) error
	// SaveCredentials сохраняет логин и пароль нового служебного сотрудника.
	SaveCredentials(ctx context.Context, login, password string) error
}

// Poller опрашивает ящик по IMAPS и передаёт найденное в Sink.
type Poller struct {
	cfg  Config
	sink Sink
	log  *slog.Logger
	// dial вынесен в поле, чтобы тесты подменяли транспорт; в бою — dialTLS.
	dial func(ctx context.Context, cfg Config) (imapConn, error)
}

// imapConn — то, чем Poller пользуется от IMAP-соединения. Интерфейс позволяет
// тестировать логику опроса без настоящего сервера.
type imapConn interface {
	Select(name string, readOnly bool) (*imap.MailboxStatus, error)
	Search(criteria *imap.SearchCriteria) ([]uint32, error)
	Fetch(seqset *imap.SeqSet, items []imap.FetchItem, ch chan *imap.Message) error
	Store(seqset *imap.SeqSet, item imap.StoreItem, value interface{}, ch chan *imap.Message) error
	Logout() error
}

// NewPoller собирает поллер с боевым TLS-транспортом.
func NewPoller(cfg Config, sink Sink, log *slog.Logger) *Poller {
	if cfg.Mailbox == "" {
		cfg.Mailbox = "INBOX"
	}
	return &Poller{cfg: cfg, sink: sink, log: log, dial: dialTLS}
}

// Run опрашивает ящик, пока не отменят ctx. Ошибка одного цикла не останавливает
// поллер: почта — вспомогательный канал, временная недоступность IMAP не должна
// ронять процесс.
func (p *Poller) Run(ctx context.Context) {
	t := time.NewTicker(p.cfg.Poll)
	defer t.Stop()

	for {
		if err := p.pollOnce(ctx); err != nil && ctx.Err() == nil {
			p.log.Warn("почтовый ящик: цикл опроса завершился ошибкой", slog.Any("error", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// pollOnce подключается, читает непрочитанные письма и помечает обработанные
// прочитанными. Один цикл — одно соединение.
func (p *Poller) pollOnce(ctx context.Context) error {
	conn, err := p.dial(ctx, p.cfg)
	if err != nil {
		return fmt.Errorf("mailbox: подключение к IMAP: %w", err)
	}
	defer func() { _ = conn.Logout() }()

	if _, err := conn.Select(p.cfg.Mailbox, false); err != nil {
		return fmt.Errorf("mailbox: выбор папки %q: %w", p.cfg.Mailbox, err)
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	ids, err := conn.Search(criteria)
	if err != nil {
		return fmt.Errorf("mailbox: поиск непрочитанных: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(ids...)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchEnvelope, section.FetchItem()}

	messages := make(chan *imap.Message, 16)
	fetchErr := make(chan error, 1)
	go func() { fetchErr <- conn.Fetch(seqset, items, messages) }()

	var processed imap.SeqSet
	for msg := range messages {
		id, ok := p.handleMessage(ctx, msg, section)
		if ok {
			processed.AddNum(id)
		}
	}
	if err := <-fetchErr; err != nil {
		return fmt.Errorf("mailbox: выборка писем: %w", err)
	}

	// Помечаем прочитанными только те письма, что успели обработать, чтобы
	// сбой в середине не «потерял» ещё не разобранные коды.
	if !processed.Empty() {
		flags := []interface{}{imap.SeenFlag}
		if err := conn.Store(&processed, imap.FormatFlagsOp(imap.AddFlags, true), flags, nil); err != nil {
			return fmt.Errorf("mailbox: пометка писем прочитанными: %w", err)
		}
	}
	return nil
}

// handleMessage разбирает одно письмо и отдаёт извлечённое в Sink. Возвращает
// (seqNum, true), если письмо можно пометить прочитанным.
func (p *Poller) handleMessage(ctx context.Context, msg *imap.Message, section *imap.BodySectionName) (uint32, bool) {
	if msg == nil {
		return 0, false
	}
	body := msg.GetBody(section)
	if body == nil {
		p.log.Warn("почтовый ящик: письмо без тела", slog.Uint64("seq", uint64(msg.SeqNum)))
		return 0, false
	}
	text, err := decodeMessage(body)
	if err != nil {
		p.log.Warn("почтовый ящик: не удалось разобрать письмо",
			slog.Uint64("seq", uint64(msg.SeqNum)), slog.Any("error", err))
		return 0, false
	}

	parsed := Parse(text)
	switch {
	case parsed.Code != "":
		// Код адресуется получателю письма (служебному сотруднику).
		to := recipient(msg)
		if to == "" {
			p.log.Warn("почтовый ящик: код без адреса-получателя", slog.Uint64("seq", uint64(msg.SeqNum)))
			return 0, false
		}
		if err := p.sink.SaveCode(ctx, to, parsed.Code); err != nil {
			p.log.Error("почтовый ящик: сохранение кода", slog.Any("error", err))
			return 0, false
		}
		// Код в лог не пишем.
		p.log.Info("почтовый ящик: получен код подтверждения", slog.String("to", to))
		return msg.SeqNum, true

	case parsed.Login != "" && parsed.Password != "":
		if err := p.sink.SaveCredentials(ctx, parsed.Login, parsed.Password); err != nil {
			p.log.Error("почтовый ящик: сохранение учётных данных сотрудника", slog.Any("error", err))
			return 0, false
		}
		// Пароль в лог не пишем.
		p.log.Info("почтовый ящик: получены учётные данные сотрудника", slog.String("login", parsed.Login))
		return msg.SeqNum, true

	default:
		// Не наше письмо (реклама, служебное) — помечаем прочитанным, чтобы не
		// перебирать его каждый цикл.
		return msg.SeqNum, true
	}
}

// recipient берёт адрес первого получателя (To) — это адрес служебного
// сотрудника, которому пришёл код.
func recipient(msg *imap.Message) string {
	if msg.Envelope == nil || len(msg.Envelope.To) == 0 {
		return ""
	}
	a := msg.Envelope.To[0]
	if a.MailboxName == "" || a.HostName == "" {
		return ""
	}
	return strings.ToLower(a.MailboxName + "@" + a.HostName)
}

// dialTLS подключается к IMAP по TLS с проверкой сертификата. В отличие от
// образца на PHP (novalidate-cert) проверку НЕ отключаем.
func dialTLS(ctx context.Context, cfg Config) (imapConn, error) {
	serverName := cfg.ServerName
	if serverName == "" {
		if host, _, ok := strings.Cut(cfg.Addr, ":"); ok {
			serverName = host
		} else {
			serverName = cfg.Addr
		}
	}
	c, err := client.DialTLS(cfg.Addr, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12})
	if err != nil {
		return nil, err
	}
	if err := c.Login(cfg.User, cfg.Password); err != nil {
		_ = c.Logout()
		return nil, fmt.Errorf("вход в ящик: %w", err)
	}
	return c, nil
}
