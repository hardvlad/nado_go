// Package secrets — шифрование секретов провайдеров и HMAC одноразовых кодов.
//
// Секреты (токены инстансов WhatsApp, учётные данные маркетплейсов) хранятся в
// БД только зашифрованными AES-256-GCM. Формат шифртекста:
// версия ключа (1 байт) | nonce (12 байт) | шифртекст+тег. Версия позволяет
// позже ротировать ключ: старые записи расшифровываются старым ключом.
//
// Одноразовые коды (OTP) хранятся не шифром, а HMAC-SHA256 с серверным ключом:
// простой SHA от 6 цифр перебирается мгновенно, HMAC без ключа — нет.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
)

// currentVersion — версия ключа, которой шифруются новые записи.
const currentVersion byte = 1

// ErrDecrypt — шифртекст повреждён, подделан или зашифрован другим ключом.
var ErrDecrypt = errors.New("secrets: не удалось расшифровать")

// Box шифрует и расшифровывает секреты.
type Box struct {
	aead    cipher.AEAD
	hmacKey []byte
}

// New создаёт Box из 32-байтного ключа.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: ключ должен быть 32 байта, получено %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: AES: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: GCM: %w", err)
	}
	// Ключ HMAC выводится из основного, а не совпадает с ним: один ключ не
	// должен использоваться в двух разных алгоритмах.
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("nado/otp-hmac/v1"))
	return &Box{aead: aead, hmacKey: mac.Sum(nil)}, nil
}

// Encrypt шифрует открытый текст. Каждый вызов — со своим случайным nonce.
func (b *Box) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: nonce: %w", err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plain)+b.aead.Overhead())
	out = append(out, currentVersion)
	out = append(out, nonce...)
	// Версия ключа входит в associated data: подменить её, не сломав тег, нельзя.
	return b.aead.Seal(out, nonce, plain, []byte{currentVersion}), nil
}

// Decrypt расшифровывает результат Encrypt.
func (b *Box) Decrypt(sealed []byte) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(sealed) < 1+ns+b.aead.Overhead() {
		return nil, ErrDecrypt
	}
	version := sealed[0]
	if version != currentVersion {
		return nil, fmt.Errorf("%w: неизвестная версия ключа %d", ErrDecrypt, version)
	}
	nonce, ct := sealed[1:1+ns], sealed[1+ns:]
	plain, err := b.aead.Open(nil, nonce, ct, []byte{version})
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}

// EncryptString / DecryptString — удобные обёртки для строковых секретов.
func (b *Box) EncryptString(s string) ([]byte, error) { return b.Encrypt([]byte(s)) }

func (b *Box) DecryptString(sealed []byte) (string, error) {
	p, err := b.Decrypt(sealed)
	return string(p), err
}

// HMAC — HMAC-SHA256 значения с серверным ключом (для хранения OTP).
func (b *Box) HMAC(value string) []byte {
	mac := hmac.New(sha256.New, b.hmacKey)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

// EqualHMAC сравнивает значение с сохранённым HMAC за постоянное время.
func (b *Box) EqualHMAC(value string, stored []byte) bool {
	return hmac.Equal(b.HMAC(value), stored)
}

// Secret — строка-секрет, которая не попадает в логи в открытом виде.
type Secret string

// LogValue скрывает значение в slog.
func (Secret) LogValue() slog.Value { return slog.StringValue("***") }

// String — тоже без раскрытия, на случай fmt.Print.
func (Secret) String() string { return "***" }

// Reveal возвращает настоящее значение — вызывать только там, где оно реально нужно.
func (s Secret) Reveal() string { return string(s) }
