package model

import "time"

// WhatsAppInstance — строка пула инстансов для показа (без токена).
type WhatsAppInstance struct {
	ID         int64
	Name       string
	Provider   string
	InstanceID string
	Phone      string
	State      string
	Disabled   bool
	LastSentAt time.Time
}
