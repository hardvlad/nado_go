// Package password — хеширование паролей кабинета алгоритмом Argon2id.
//
// Формат хеша — PHC-строка: $argon2id$v=19$m=19456,t=2,p=1$<соль>$<хеш>.
// Параметры хранятся в самой строке, поэтому их можно усилить позже:
// старые хеши продолжат проверяться, а NeedsRehash подскажет, какие пересчитать
// при следующем входе.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Параметры по минимальной рекомендации OWASP для Argon2id (19 МиБ, 2 прохода).
// Память на один вход ограничена: форма входа защищена ограничителем частоты,
// иначе поток попыток съест память сервера.
const (
	memoryKiB   = 19 * 1024
	iterations  = 2
	parallelism = 1
	saltLen     = 16
	keyLen      = 32
)

// ErrMalformed — строка не является хешем в поддерживаемом формате.
var ErrMalformed = errors.New("password: некорректный формат хеша")

var b64 = base64.RawStdEncoding

// Hash возвращает PHC-строку для пароля со случайной солью.
func Hash(plain string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: соль: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, iterations, memoryKiB, parallelism, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryKiB, iterations, parallelism, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify проверяет пароль. Сравнение — за постоянное время, чтобы время
// ответа не подсказывало, сколько байт совпало.
func Verify(plain, encoded string) (bool, error) {
	p, salt, key, err := decode(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(plain), salt, p.t, p.m, p.p, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1, nil
}

// NeedsRehash сообщает, что хеш посчитан со слабее текущих параметрами.
func NeedsRehash(encoded string) bool {
	p, _, _, err := decode(encoded)
	if err != nil {
		return true
	}
	return p.m < memoryKiB || p.t < iterations
}

type params struct {
	m, t uint32
	p    uint8
}

func decode(encoded string) (params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=..,t=..,p=..", соль, хеш
	if len(parts) != 6 || parts[1] != "argon2id" {
		return params{}, nil, nil, ErrMalformed
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return params{}, nil, nil, ErrMalformed
	}

	var p params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.m, &p.t, &p.p); err != nil {
		return params{}, nil, nil, ErrMalformed
	}
	// Защита от хеша с заведомо огромными параметрами: проверка одного пароля
	// не должна требовать гигабайт памяти.
	if p.m == 0 || p.m > 256*1024 || p.t == 0 || p.t > 10 || p.p == 0 {
		return params{}, nil, nil, ErrMalformed
	}

	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return params{}, nil, nil, ErrMalformed
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) < 16 {
		return params{}, nil, nil, ErrMalformed
	}
	return p, salt, key, nil
}
