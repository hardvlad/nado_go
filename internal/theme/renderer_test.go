package theme

import "testing"

func TestImgFunc(t *testing.T) {
	img := builtinFuncs("https://cdn.kaspi.kz/img")["img"].(func(string) string)

	cases := []struct{ in, want string }{
		{"/shop/p/100/i1.jpg", "https://cdn.kaspi.kz/img/shop/p/100/i1.jpg"}, // относительный с /
		{"a/b.jpg", "https://cdn.kaspi.kz/img/a/b.jpg"},                      // относительный без /
		{"https://other/x.jpg", "https://other/x.jpg"},                       // абсолютный — как есть
		{"//cdn/x.jpg", "//cdn/x.jpg"},                                       // протокол-относительный — как есть
		{"", ""},                                                             // пусто
	}
	for _, c := range cases {
		if got := img(c.in); got != c.want {
			t.Errorf("img(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}

	// Без префикса CDN ссылки не меняются.
	noCDN := builtinFuncs("")["img"].(func(string) string)
	if got := noCDN("a/b.jpg"); got != "a/b.jpg" {
		t.Errorf("без CDN: img = %q, ожидалось без изменений", got)
	}
}
