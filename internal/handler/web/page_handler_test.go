package web

import "testing"

func TestSafeNext(t *testing.T) {
	for _, next := range []string{"/account", "/kk/account", "/account?tab=1"} {
		if got, ok := safeNext(next); !ok || got != next {
			t.Errorf("safeNext(%q) должен приниматься", next)
		}
	}
	for _, next := range []string{"", "account", "//evil.kz", "https://evil.kz/", `/\evil.kz`, "javascript:alert(1)"} {
		if _, ok := safeNext(next); ok {
			t.Errorf("safeNext(%q) должен отклоняться: это переход на чужой сайт", next)
		}
	}
}
