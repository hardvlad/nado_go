package password

import (
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	h, err := Hash("пароль-123")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("неожиданный формат: %s", h)
	}

	ok, err := Verify("пароль-123", h)
	if err != nil || !ok {
		t.Fatalf("верный пароль не прошёл: ok=%v err=%v", ok, err)
	}
	if ok, _ := Verify("пароль-124", h); ok {
		t.Fatal("неверный пароль прошёл проверку")
	}

	// Соль случайная: два хеша одного пароля различаются.
	h2, _ := Hash("пароль-123")
	if h == h2 {
		t.Error("хеши одинакового пароля совпали — соль не работает")
	}
	if NeedsRehash(h) {
		t.Error("свежий хеш не должен требовать пересчёта")
	}
}

func TestVerifyMalformed(t *testing.T) {
	for _, bad := range []string{
		"",
		"plain-text",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA",
		"$argon2id$v=19$m=99999999,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$aGFzaA",
	} {
		if _, err := Verify("x", bad); err == nil {
			t.Errorf("ожидалась ошибка для %q", bad)
		}
	}
}
