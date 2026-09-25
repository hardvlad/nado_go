// Package messaging — отправка сообщений покупателям и продавцам.
//
// Сейчас единственное применение — коды подтверждения (OTP) при входе и
// регистрации продавца. Провайдер скрыт за интерфейсом Sender, чтобы позже
// заменить GreenAPI на официальный WhatsApp Business Platform без изменений в
// вызывающем коде.
package messaging

import (
	"context"
	"errors"
)

// ErrNoInstance — в пуле нет активного инстанса, с которого можно отправить.
var ErrNoInstance = errors.New("messaging: нет доступного инстанса отправки")

// Message — сообщение к отправке.
type Message struct {
	Phone string // в формате E.164 (+7...)
	Text  string
}

// Sent — результат успешной отправки.
type Sent struct {
	MessageID  string // id сообщения у провайдера
	InstanceID int64  // с какого инстанса пула ушло (0 — неизвестно)
}

// Sender отправляет сообщение. Реализации: greenapi (сейчас), waba (позже).
type Sender interface {
	// Send отправляет сообщение. Возвращает ErrNoInstance, если отправить не с чего.
	Send(ctx context.Context, msg Message) (*Sent, error)
	// Provider — имя провайдера для логов.
	Provider() string
}
