package model

import "time"

// Account — арендатор платформы: продавец, ИП или компания (D-11).
type Account struct {
	ID        int64
	Name      string
	Country   string // KZ, позже RU
	PlanCode  string
	Status    string
	CreatedAt time.Time
}

// Статусы аккаунта.
const (
	AccountStatusActive    = "active"
	AccountStatusSuspended = "suspended"
	AccountStatusClosed    = "closed"
)

// Роли участника аккаунта.
const (
	RoleOwner   = "owner"
	RoleAdmin   = "admin"
	RoleManager = "manager"
	RoleViewer  = "viewer"
)

// NewUser — данные для создания пользователя кабинета при регистрации.
// Хеш пароля уже посчитан сервисом: пароль в открытом виде ниже сервиса не уходит.
type NewUser struct {
	Email        string
	Name         string
	Phone        string // E.164 или пусто
	PasswordHash string
	Locale       string
}

// UserCredentials — то, что нужно для проверки входа.
type UserCredentials struct {
	UserID       int64
	Name         string
	Email        string
	Status       string
	PasswordHash string // пусто — вход по паролю невозможен
	AccountID    int64  // основной аккаунт пользователя; 0 — не состоит ни в одном
}

// Session — серверная сессия кабинета. В БД хранится только хеш токена:
// утечка таблицы не даёт войти под чужой сессией.
type Session struct {
	IDHash    []byte
	UserID    int64
	AccountID int64
	ExpiresAt time.Time
	IP        string
	UserAgent string
}

// Principal — вошедший пользователь в контексте конкретного аккаунта.
type Principal struct {
	UserID      int64
	UserName    string
	Email       string
	AccountID   int64
	AccountName string
	PlanCode    string
	Role        string
}

// Feedback — обращение с формы обратной связи.
type Feedback struct {
	ID        int64
	Name      string
	Contact   string
	Topic     string
	Message   string
	Lang      string
	IP        string
	UserAgent string
	CreatedAt time.Time
}

// Темы обращений.
const (
	FeedbackQuestion    = "question"
	FeedbackSuggestion  = "suggestion"
	FeedbackPartnership = "partnership"
	FeedbackOther       = "other"
)

// FeedbackTopics — темы в порядке показа в форме.
func FeedbackTopics() []string {
	return []string{FeedbackQuestion, FeedbackSuggestion, FeedbackPartnership, FeedbackOther}
}
