// Package mailbox читает общий почтовый ящик служебных сотрудников Kaspi и
// извлекает из писем коды подтверждения входа и учётные данные новых
// сотрудников. Ящик — инфраструктура платформы (catch-all), к продавцам не
// привязан. Разбор писем вынесен в чистые функции (parse.go) и покрыт тестами;
// транспорт IMAP — в imap.go.
package mailbox

import (
	"regexp"
	"strings"
)

// Parsed — что удалось извлечь из письма.
type Parsed struct {
	// Code — код подтверждения входа, если письмо содержит его.
	Code string
	// Login/Password — учётные данные нового служебного сотрудника (письмо
	// с доступом), если письмо содержит их.
	Login    string
	Password string
}

// маркеры письма с кодом подтверждения (как в исходном проекте).
var codeMarkers = []string{
	"Ваш код подтверждения:",
	"код подтверждения:",
}

// codeDigits — 4–8 цифр подряд после маркера.
var codeDigits = regexp.MustCompile(`\d{4,8}`)

// Parse извлекает код или учётные данные из текста письма (уже раскодированного
// из base64/quoted-printable и без HTML при возможности).
func Parse(body string) Parsed {
	var p Parsed

	// Код подтверждения: ищем маркер и первые цифры после него.
	lower := strings.ToLower(body)
	for _, m := range codeMarkers {
		if idx := strings.Index(lower, strings.ToLower(m)); idx >= 0 {
			tail := body[idx+len(m):]
			if code := codeDigits.FindString(tail); code != "" {
				p.Code = code
				return p
			}
		}
	}

	// Письмо с доступом сотрудника: строки «Логин:» и «Пароль:».
	// Строки могут быть разделены <br> (HTML-письмо) или переводами строк.
	for _, line := range splitLines(body) {
		if v, ok := afterPrefix(line, "Логин:"); ok {
			p.Login = v
		}
		if v, ok := afterPrefix(line, "Пароль:"); ok {
			p.Password = v
		}
	}
	return p
}

// splitLines делит текст на строки по перводам строки и по <br> (HTML-письма).
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	return strings.Split(s, "\n")
}

// afterPrefix возвращает часть строки после префикса (без учёта регистра и
// ведущих пробелов) и признак совпадения.
func afterPrefix(line, prefix string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < len(prefix) {
		return "", false
	}
	if !strings.EqualFold(trimmed[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(trimmed[len(prefix):]), true
}
