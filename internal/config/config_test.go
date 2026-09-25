package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDevSecretsKeyIs32Bytes(t *testing.T) {
	if len(devSecretsKey) != 32 {
		t.Fatalf("dev-ключ должен быть 32 байта для AES-256, сейчас %d", len(devSecretsKey))
	}
}

func TestSecretsKey(t *testing.T) {
	valid := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))

	if key, err := secretsKey(valid, true); err != nil || len(key) != 32 {
		t.Fatalf("корректный ключ: len=%d err=%v", len(key), err)
	}
	if _, err := secretsKey("", true); err == nil {
		t.Error("в prod пустой SECRETS_KEY должен быть ошибкой")
	}
	if key, err := secretsKey("", false); err != nil || len(key) != 32 {
		t.Errorf("вне prod без ключа — dev-ключ 32 байта: len=%d err=%v", len(key), err)
	}
	if _, err := secretsKey("не-base64!!", false); err == nil {
		t.Error("некорректный base64 должен быть ошибкой")
	}
	short := base64.StdEncoding.EncodeToString([]byte("short"))
	if _, err := secretsKey(short, false); err == nil {
		t.Error("ключ не 32 байта должен быть ошибкой")
	}
}
