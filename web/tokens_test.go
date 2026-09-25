package web

import (
	"io/fs"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Проверки оформления из скилла nado-ui-design (D-21): светлая и тёмная тема
// обязательны, цвета только в токенах, контраст WCAG 2.2 AA в обоих режимах.

const tokensFile = "static/css/tokens.css"

var tokenRe = regexp.MustCompile(`(--[a-z0-9-]+)\s*:\s*([^;]+);`)

// block возвращает тело CSS-блока после селектора до парной закрывающей скобки.
func block(t *testing.T, css, selector string) map[string]string {
	t.Helper()
	i := strings.Index(css, selector)
	if i < 0 {
		t.Fatalf("в %s нет блока %q", tokensFile, selector)
	}
	start := strings.Index(css[i:], "{") + i + 1
	depth, end := 1, start
	for ; end < len(css) && depth > 0; end++ {
		switch css[end] {
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	out := map[string]string{}
	for _, m := range tokenRe.FindAllStringSubmatch(css[start:end], -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

func loadTokens(t *testing.T) (light, dark map[string]string) {
	t.Helper()
	raw, err := fs.ReadFile(staticFS, tokensFile)
	if err != nil {
		t.Fatalf("чтение токенов: %v", err)
	}
	css := string(raw)
	light = block(t, css, ":root {")
	dark = block(t, css, `:root[data-theme="dark"] {`)
	media := block(t, css, `:root:not([data-theme="light"]) {`)

	// Тёмная тема записана дважды (явный выбор и «как в системе») — блоки
	// обязаны совпадать, иначе режимы поведут себя по-разному.
	for k, v := range dark {
		if media[k] != v {
			t.Errorf("тёмные блоки расходятся по %s: %q и %q", k, v, media[k])
		}
	}
	for k := range media {
		if _, ok := dark[k]; !ok {
			t.Errorf("токен %s есть только в media-блоке тёмной темы", k)
		}
	}
	return light, dark
}

func TestTokensDefinedForBothThemes(t *testing.T) {
	light, dark := loadTokens(t)

	names := func(m map[string]string) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			if strings.HasPrefix(k, "--color-") || strings.HasPrefix(k, "--shadow-") {
				out = append(out, k)
			}
		}
		slices.Sort(out)
		return out
	}
	if l, d := names(light), names(dark); !slices.Equal(l, d) {
		t.Errorf("наборы токенов светлой и тёмной темы различаются:\nсветлая: %v\nтёмная:  %v", l, d)
	}

	if light["--color-bg"] != ThemeColorLight {
		t.Errorf("ThemeColorLight = %s, а --color-bg светлой темы = %s", ThemeColorLight, light["--color-bg"])
	}
	if dark["--color-bg"] != ThemeColorDark {
		t.Errorf("ThemeColorDark = %s, а --color-bg тёмной темы = %s", ThemeColorDark, dark["--color-bg"])
	}
}

func TestTokensContrast(t *testing.T) {
	light, dark := loadTokens(t)

	type pair struct {
		fg, bg string
		min    float64
	}
	pairs := []pair{}
	for _, bg := range []string{"bg", "surface", "surface-2"} {
		pairs = append(pairs, pair{"text", bg, 4.5}, pair{"text-muted", bg, 4.5})
	}
	pairs = append(pairs,
		pair{"on-primary", "primary", 4.5},
		pair{"on-primary", "primary-hover", 4.5},
		pair{"primary", "bg", 4.5},
		pair{"primary", "surface", 4.5},
		pair{"primary", "primary-soft", 4.5},
		pair{"primary", "on-primary", 4.5},
		pair{"primary-hover", "bg", 4.5},
		pair{"primary-hover", "surface", 4.5},
		pair{"primary-hover", "surface-2", 4.5},
		pair{"text", "primary-soft", 4.5},
		pair{"text-muted", "primary-soft", 4.5},
		pair{"accent", "bg", 4.5},
		pair{"accent", "surface", 4.5},
		pair{"accent", "accent-soft", 3},
		pair{"border-strong", "surface", 3},
		pair{"border-strong", "bg", 3},
		pair{"focus", "bg", 3},
		pair{"focus", "surface", 3},
	)
	for _, s := range []string{"success", "warning", "danger", "info"} {
		pairs = append(pairs, pair{s, s + "-bg", 4.5}, pair{s, "surface", 4.5})
	}

	for _, theme := range []struct {
		name   string
		tokens map[string]string
	}{{"светлая", light}, {"тёмная", dark}} {
		for _, p := range pairs {
			fg, bg := theme.tokens["--color-"+p.fg], theme.tokens["--color-"+p.bg]
			ratio, err := contrast(fg, bg)
			if err != nil {
				t.Errorf("%s: %s/%s: %v", theme.name, p.fg, p.bg, err)
				continue
			}
			if ratio < p.min {
				t.Errorf("%s тема: %s (%s) на %s (%s) — контраст %.2f, нужно не меньше %.1f",
					theme.name, p.fg, fg, p.bg, bg, ratio, p.min)
			}
		}
	}
}

// TestNoRawColors: цвета задаются только в tokens.css. Цвет в правиле
// компонента останется светлым пятном в тёмной теме.
func TestNoRawColors(t *testing.T) {
	colorRe := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\b(?:rgba?|hsla?|oklch)\(`)

	err := fs.WalkDir(staticFS, "static/css", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == tokensFile {
			return err
		}
		raw, err := fs.ReadFile(staticFS, p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if colorRe.MatchString(line) {
				t.Errorf("%s:%d: цвет вне токенов: %s", p, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// В шаблонах запрещены inline-стили (их и так режет CSP) и цвета в SVG.
	attrRe := regexp.MustCompile(`\sstyle=|(?:fill|stroke|stop-color)="#`)
	err = fs.WalkDir(templatesFS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".gohtml" {
			return err
		}
		raw, err := fs.ReadFile(templatesFS, p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if attrRe.MatchString(line) {
				t.Errorf("%s:%d: inline-стиль или цвет в шаблоне: %s", p, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// contrast — коэффициент контраста WCAG 2.x для двух цветов #rrggbb.
func contrast(a, b string) (float64, error) {
	la, err := luminance(a)
	if err != nil {
		return 0, err
	}
	lb, err := luminance(b)
	if err != nil {
		return 0, err
	}
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05), nil
}

func luminance(hex string) (float64, error) {
	if len(hex) != 7 || hex[0] != '#' {
		return 0, strconv.ErrSyntax
	}
	channel := func(s string) (float64, error) {
		v, err := strconv.ParseUint(s, 16, 8)
		if err != nil {
			return 0, err
		}
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92, nil
		}
		return math.Pow((c+0.055)/1.055, 2.4), nil
	}
	r, err := channel(hex[1:3])
	if err != nil {
		return 0, err
	}
	g, err := channel(hex[3:5])
	if err != nil {
		return 0, err
	}
	b, err := channel(hex[5:7])
	if err != nil {
		return 0, err
	}
	return 0.2126*r + 0.7152*g + 0.0722*b, nil
}
