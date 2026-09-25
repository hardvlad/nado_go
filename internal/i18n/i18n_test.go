package i18n

import (
	"encoding/json"
	"io/fs"
	"path"
	"slices"
	"testing"
)

// Наборы ключей во всех языках должны совпадать: недостающий ключ тихо
// покажет русский текст на казахской странице, лишний — мёртвый перевод.
func TestLocalesHaveSameKeys(t *testing.T) {
	keys := func(l Lang) []string {
		raw, err := fs.ReadFile(localesFS, path.Join("locales", string(l)+".json"))
		if err != nil {
			t.Fatalf("чтение %s: %v", l, err)
		}
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("разбор %s: %v", l, err)
		}
		out := make([]string, 0, len(m))
		for k, v := range m {
			if v == "" {
				t.Errorf("%s: пустой перевод %q", l, k)
			}
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}

	base := keys(Default)
	for _, l := range Supported() {
		if l == Default {
			continue
		}
		got := keys(l)
		for _, k := range base {
			if !slices.Contains(got, k) {
				t.Errorf("%s: нет ключа %q", l, k)
			}
		}
		for _, k := range got {
			if !slices.Contains(base, k) {
				t.Errorf("%s: лишний ключ %q (нет в %s)", l, k, Default)
			}
		}
	}
}

func TestTranslateWithArgsAndFallback(t *testing.T) {
	var missing []string
	b, err := NewBundle(WithMissingHook(func(_ Lang, id string) { missing = append(missing, id) }))
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}

	cases := []struct {
		lang Lang
		id   string
		args []any
		want string
	}{
		{RU, "account.welcome", []any{"Name", "Айгуль"}, "Добро пожаловать, Айгуль!"},
		{KK, "account.welcome", []any{"Name", "Айгуль"}, "Қош келдіңіз, Айгуль!"},
		{EN, "validation.min", []any{"Param", "8"}, "At least 8 characters"},
		{EN, "no.such.key", nil, "no.such.key"},
	}
	for _, tc := range cases {
		if got := b.Localizer(tc.lang).T(tc.id, tc.args...); got != tc.want {
			t.Errorf("%s %s = %q, ожидалось %q", tc.lang, tc.id, got, tc.want)
		}
	}
	if !slices.Equal(missing, []string{"no.such.key"}) {
		t.Errorf("хук недостающих ключей = %v", missing)
	}

	var nilLoc *Localizer
	if got := nilLoc.T("home.title"); got != "home.title" {
		t.Errorf("nil-переводчик должен возвращать ключ, получено %q", got)
	}
}

func TestSplitAndLocalizePath(t *testing.T) {
	cases := []struct {
		in       string
		wantLang Lang
		wantPath string
	}{
		{"/", RU, "/"},
		{"/contact", RU, "/contact"},
		{"/kk", KK, "/"},
		{"/kk/", KK, "/"},
		{"/en/register", EN, "/register"},
		{"/ru/contact", RU, "/ru/contact"}, // русский живёт без префикса
		{"/kkk", RU, "/kkk"},
	}
	for _, tc := range cases {
		l, p := SplitPath(tc.in)
		if l != tc.wantLang || p != tc.wantPath {
			t.Errorf("SplitPath(%q) = (%s, %q), ожидалось (%s, %q)", tc.in, l, p, tc.wantLang, tc.wantPath)
		}
	}

	if got := Localize(KK, "/"); got != "/kk" {
		t.Errorf("Localize(kk, /) = %q", got)
	}
	if got := Localize(EN, "/contact"); got != "/en/contact" {
		t.Errorf("Localize(en, /contact) = %q", got)
	}
	if got := Localize(RU, "/contact"); got != "/contact" {
		t.Errorf("Localize(ru, /contact) = %q", got)
	}
}
