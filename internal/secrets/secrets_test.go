package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func newBox(t *testing.T) *Box {
	t.Helper()
	b, err := New(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEncryptDecrypt(t *testing.T) {
	b := newBox(t)
	sealed, err := b.EncryptString("токен-инстанса-123")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("токен")) {
		t.Fatal("шифртекст содержит открытый текст")
	}
	got, err := b.DecryptString(sealed)
	if err != nil || got != "токен-инстанса-123" {
		t.Fatalf("расшифровка: %q, %v", got, err)
	}

	// Каждый раз новый nonce — одинаковый текст даёт разный шифртекст.
	sealed2, _ := b.EncryptString("токен-инстанса-123")
	if bytes.Equal(sealed, sealed2) {
		t.Error("одинаковый шифртекст — nonce не случайный")
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	b := newBox(t)
	sealed, _ := b.EncryptString("секрет")

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := b.Decrypt(tampered); !errors.Is(err, ErrDecrypt) {
		t.Errorf("подделанный шифртекст: ожидалась ErrDecrypt, получено %v", err)
	}

	other, _ := New(bytes.Repeat([]byte("z"), 32))
	if _, err := other.Decrypt(sealed); !errors.Is(err, ErrDecrypt) {
		t.Errorf("чужой ключ: ожидалась ErrDecrypt, получено %v", err)
	}

	if _, err := b.Decrypt([]byte{1, 2, 3}); !errors.Is(err, ErrDecrypt) {
		t.Errorf("короткий шифртекст: ожидалась ErrDecrypt, получено %v", err)
	}
}

func TestHMAC(t *testing.T) {
	b := newBox(t)
	stored := b.HMAC("482913")
	if !b.EqualHMAC("482913", stored) {
		t.Error("верный код не совпал")
	}
	if b.EqualHMAC("482914", stored) {
		t.Error("неверный код совпал")
	}
	// HMAC зависит от ключа: другой ключ — другой результат.
	other, _ := New(bytes.Repeat([]byte("z"), 32))
	if other.EqualHMAC("482913", stored) {
		t.Error("HMAC не должен совпадать при другом ключе")
	}
}

func TestSecretNotLogged(t *testing.T) {
	s := Secret("super-token")
	if fmt.Sprint(s) != "***" {
		t.Errorf("fmt раскрыл секрет: %q", fmt.Sprint(s))
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("x", slog.Any("token", s))
	if strings.Contains(buf.String(), "super-token") {
		t.Errorf("slog раскрыл секрет: %s", buf.String())
	}
	if s.Reveal() != "super-token" {
		t.Error("Reveal должен возвращать значение")
	}
}
