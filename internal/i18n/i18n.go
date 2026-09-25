// Package i18n — переводы интерфейса (ru, kk, en) и язык в URL.
//
// Язык определяется префиксом пути: без префикса — русский (язык по
// умолчанию), /kk/... — казахский, /en/... — английский. Язык не угадывается
// по Accept-Language: иначе поисковик проиндексирует не ту версию страницы.
//
// Тексты лежат в locales/<язык>.json. Русский — источник истины: новый ключ
// сначала добавляется в ru.json, тест следит, чтобы наборы ключей совпадали.
package i18n

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// Lang — код языка интерфейса.
type Lang string

const (
	RU Lang = "ru"
	KK Lang = "kk"
	EN Lang = "en"

	// Default — язык страниц без префикса в URL.
	Default = RU
)

// Supported — языки в порядке показа в переключателе.
func Supported() []Lang { return []Lang{RU, KK, EN} }

// Parse проверяет, что строка — поддерживаемый язык.
func Parse(s string) (Lang, bool) {
	for _, l := range Supported() {
		if string(l) == s {
			return l, true
		}
	}
	return "", false
}

// Prefix — префикс пути для языка: "" для языка по умолчанию, "/kk" и т.д.
func (l Lang) Prefix() string {
	if l == Default || l == "" {
		return ""
	}
	return "/" + string(l)
}

// Name — самоназвание языка для переключателя.
func (l Lang) Name() string {
	switch l {
	case KK:
		return "Қазақша"
	case EN:
		return "English"
	default:
		return "Русский"
	}
}

// Short — короткая подпись для кнопки переключателя.
func (l Lang) Short() string {
	switch l {
	case KK:
		return "ҚАЗ"
	case EN:
		return "EN"
	default:
		return "РУС"
	}
}

// SplitPath отделяет языковой префикс: "/kk/contact" → (kk, "/contact"),
// "/en" → (en, "/"), "/contact" → (ru, "/contact").
func SplitPath(p string) (Lang, string) {
	trimmed := strings.TrimPrefix(p, "/")
	first, rest, _ := strings.Cut(trimmed, "/")
	if l, ok := Parse(first); ok && l != Default {
		return l, "/" + rest
	}
	return Default, p
}

// Localize строит путь для языка из пути без префикса: ("/contact", kk) → "/kk/contact".
func Localize(l Lang, p string) string {
	if p == "" {
		p = "/"
	}
	if prefix := l.Prefix(); prefix != "" {
		if p == "/" {
			return prefix
		}
		return prefix + p
	}
	return p
}

//go:embed locales/*.json
var localesFS embed.FS

// Bundle — загруженные переводы всех языков.
type Bundle struct {
	localizers map[Lang]*Localizer
}

// Option настраивает Bundle.
type Option func(*options)

type options struct {
	onMissing func(lang Lang, id string)
}

// WithMissingHook вызывается, когда ключ не найден ни в языке, ни в русском.
// В тестах через него ловятся опечатки в ключах.
func WithMissingHook(fn func(lang Lang, id string)) Option {
	return func(o *options) { o.onMissing = fn }
}

// NewBundle загружает встроенные переводы.
func NewBundle(opts ...Option) (*Bundle, error) {
	return newBundle(localesFS, opts...)
}

func newBundle(fsys fs.FS, opts ...Option) (*Bundle, error) {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}

	b := goi18n.NewBundle(language.Russian)
	b.RegisterUnmarshalFunc("json", json.Unmarshal)

	for _, l := range Supported() {
		file := path.Join("locales", string(l)+".json")
		if _, err := b.LoadMessageFileFS(fsys, file); err != nil {
			return nil, fmt.Errorf("i18n: загрузка %s: %w", file, err)
		}
	}

	bundle := &Bundle{localizers: make(map[Lang]*Localizer, len(Supported()))}
	for _, l := range Supported() {
		bundle.localizers[l] = &Localizer{
			lang: l,
			// Недостающий перевод берётся из русского, а не показывается ключом.
			loc:       goi18n.NewLocalizer(b, string(l), string(Default)),
			onMissing: o.onMissing,
		}
	}
	return bundle, nil
}

// Localizer возвращает переводчик для языка (для неизвестного — русский).
func (b *Bundle) Localizer(l Lang) *Localizer {
	if loc, ok := b.localizers[l]; ok {
		return loc
	}
	return b.localizers[Default]
}

// Localizer переводит ключи на один язык. Безопасен для конкурентного
// использования: после создания не меняется.
type Localizer struct {
	lang      Lang
	loc       *goi18n.Localizer
	onMissing func(lang Lang, id string)
}

// Lang — язык переводчика.
func (l *Localizer) Lang() Lang {
	if l == nil {
		return Default
	}
	return l.lang
}

// T переводит ключ. args — пары «имя, значение» для подстановки в текст
// ({{.Name}} в переводе); значение "Count" выбирает форму множественного числа.
//
// В шаблоне: {{ .L.T "home.title" }}, {{ .L.T "account.welcome" "Name" .User }}.
func (l *Localizer) T(id string, args ...any) string {
	if l == nil {
		return id
	}

	var data map[string]any
	if len(args) > 0 {
		data = make(map[string]any, len(args)/2)
		for i := 0; i+1 < len(args); i += 2 {
			if key, ok := args[i].(string); ok {
				data[key] = args[i+1]
			}
		}
	}

	cfg := &goi18n.LocalizeConfig{MessageID: id, TemplateData: data}
	if count, ok := data["Count"]; ok {
		cfg.PluralCount = count
	}

	s, err := l.loc.Localize(cfg)
	if err != nil {
		var notFound *goi18n.MessageNotFoundErr
		if errors.As(err, &notFound) && l.onMissing != nil {
			l.onMissing(l.lang, id)
		}
		if s == "" {
			// Ключ на странице лучше пустоты: ошибка видна и не ломает вёрстку.
			return id
		}
	}
	return s
}
