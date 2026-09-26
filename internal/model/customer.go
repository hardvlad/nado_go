package model

import "time"

// Customer — покупатель магазина (D-17). Существует в пределах магазина, вход
// только по телефону.
type Customer struct {
	ID        int64
	StoreID   int64
	AccountID int64
	PhoneE164 string
	Name      string
	Email     string
	Status    string
	ConsentAt time.Time
	CreatedAt time.Time
}

// OTPChallenge — одноразовый код входа покупателя (хранится HMAC кода).
type OTPChallenge struct {
	ID          int64
	StoreID     int64
	PhoneE164   string
	Channel     string
	CodeHash    []byte
	Attempts    int
	MaxAttempts int
	ExpiresAt   time.Time
}

// Статусы покупателя.
const (
	CustomerActive  = "active"
	CustomerDeleted = "deleted"
)
