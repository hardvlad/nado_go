package mailbox

import "testing"

func TestParseCode(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"как в образце", "Здравствуйте!\nВаш код подтверждения: 482913\nНикому не сообщайте.", "482913"},
		{"строчный маркер", "ваш код подтверждения: 000123 — спасибо", "000123"},
		{"код перед html-тегом", "<p>Ваш код подтверждения: 550011</p>", "550011"},
		{"пробел после двоеточия не обязателен", "код подтверждения:987654", "987654"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Parse(c.body)
			if got.Code != c.want {
				t.Errorf("Code = %q, ожидалось %q", got.Code, c.want)
			}
			if got.Login != "" || got.Password != "" {
				t.Errorf("для письма с кодом логин/пароль должны быть пусты: %+v", got)
			}
		})
	}
}

func TestParseCredentials(t *testing.T) {
	// Письмо с доступом сотрудника (HTML, строки через <br>, как в образце).
	body := "Доступ создан.<br>Логин: nado_1@kaspi.nado.kz<br>Пароль: S3cr!tPass<br>Спасибо"
	got := Parse(body)
	if got.Code != "" {
		t.Errorf("в письме с доступом кода быть не должно: %q", got.Code)
	}
	if got.Login != "nado_1@kaspi.nado.kz" {
		t.Errorf("Login = %q", got.Login)
	}
	if got.Password != "S3cr!tPass" {
		t.Errorf("Password = %q", got.Password)
	}
}

func TestParseCredentialsNewlines(t *testing.T) {
	// Тот же вид письма, но части разделены переводами строк, а не <br>.
	body := "Логин: user@x.kz\nПароль: pw123\n"
	got := Parse(body)
	if got.Login != "user@x.kz" || got.Password != "pw123" {
		t.Errorf("разбор по переводам строк не сработал: %+v", got)
	}
}

func TestParseCodeTakesPrecedence(t *testing.T) {
	// Если в письме есть маркер кода, оно трактуется как код, а не как доступ.
	body := "Ваш код подтверждения: 111222"
	if got := Parse(body); got.Code != "111222" {
		t.Errorf("Code = %q", got.Code)
	}
}

func TestParseNothing(t *testing.T) {
	if got := Parse("Обычное письмо без кодов и доступов."); got.Code != "" || got.Login != "" || got.Password != "" {
		t.Errorf("ожидался пустой результат, получено %+v", got)
	}
}
