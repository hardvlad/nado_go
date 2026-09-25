package httpx

import (
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	l := NewRateLimiter(2, time.Minute)
	l.now = func() time.Time { return now }

	if !l.Allow("ip") || !l.Allow("ip") {
		t.Fatal("первые два события должны проходить")
	}
	if l.Allow("ip") {
		t.Fatal("третье событие в окне должно отклоняться")
	}
	if !l.Allow("other") {
		t.Fatal("другой ключ не должен зависеть от первого")
	}

	now = now.Add(time.Minute)
	if !l.Allow("ip") {
		t.Fatal("после окна счётчик должен сброситься")
	}
}
