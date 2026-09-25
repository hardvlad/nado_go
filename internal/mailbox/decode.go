package mailbox

import (
	"fmt"
	"io"
	"strings"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // регистрирует windows-1251 и др. кодировки писем
)

// maxBodyBytes ограничивает объём одной части письма, который мы читаем.
// Коды и учётные данные короткие; большие вложения нам не нужны и лишь заняли
// бы память.
const maxBodyBytes = 1 << 20 // 1 МБ

// decodeMessage читает письмо (MIME) и возвращает его текст: конкатенацию всех
// текстовых частей с раскодированным transfer-encoding (base64,
// quoted-printable) и приведённой к UTF-8 кодировкой. Именно по этому тексту
// потом работает Parse.
//
// Раскодирование вынесено в чистую функцию (вход — io.Reader), чтобы покрыть его
// тестами на реальных примерах писем без IMAP.
func decodeMessage(r io.Reader) (string, error) {
	m, err := message.Read(r)
	if err != nil {
		// message.Read возвращает UnknownEncodingError/UnknownCharsetError как
		// «мягкую» ошибку вместе с пригодным m: тело доступно как есть.
		if !message.IsUnknownCharset(err) {
			return "", fmt.Errorf("mailbox: разбор письма: %w", err)
		}
	}

	var b strings.Builder
	if err := collectText(m, &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

// collectText обходит части письма и собирает текст из text/*.
func collectText(e *message.Entity, b *strings.Builder) error {
	if mr := e.MultipartReader(); mr != nil {
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return fmt.Errorf("mailbox: часть письма: %w", err)
			}
			if err := collectText(part, b); err != nil {
				return err
			}
		}
	}

	mediaType, _, _ := e.Header.ContentType()
	if !strings.HasPrefix(mediaType, "text/") {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(e.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("mailbox: чтение части письма: %w", err)
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	b.Write(data)
	return nil
}
