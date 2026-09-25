package service

import "testing"

func TestFormatKaspiPhone(t *testing.T) {
	cases := []struct{ in, want string }{
		{"77001234567", "+7 (700) 123-45-67"},
		{"87001234567", "+7 (700) 123-45-67"},
		{"+7 700 123 45 67", "+7 (700) 123-45-67"},
		{"+7 (700) 123-45-67", "+7 (700) 123-45-67"},
		{"70012345", ""},    // мало цифр
		{"12345678901", ""}, // не +7/8
		{"", ""},            // пусто
	}
	for _, c := range cases {
		if got := formatKaspiPhone(c.in); got != c.want {
			t.Errorf("formatKaspiPhone(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}
