package model

import "time"

// Цели одноразового кода.
const (
	OTPPurposeLogin    = "login"
	OTPPurposeRegister = "register"
)

// OTPCode — одноразовый код подтверждения телефона. В БД хранится только HMAC
// кода (CodeHash), не сам код.
type OTPCode struct {
	ID          int64
	Purpose     string
	Phone       string // E.164
	CodeHash    []byte
	Channel     string
	InstanceID  int64
	MessageID   string
	Attempts    int
	MaxAttempts int
	ExpiresAt   time.Time
	IP          string
}
