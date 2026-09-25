package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"nado_go/internal/database"
	"nado_go/internal/httpx"
	"nado_go/internal/model"
	"nado_go/internal/password"
)

// SessionTTL — срок жизни сессии кабинета.
const SessionTTL = 30 * 24 * time.Hour

// ErrInvalidCredentials — неверный email или пароль. Намеренно одна ошибка
// на оба случая: иначе форма входа подсказывает, какие email зарегистрированы.
var ErrInvalidCredentials = errors.New("service: неверный email или пароль")

// ErrNoSession — сессии нет, она истекла или пользователь заблокирован.
var ErrNoSession = errors.New("service: сессия недействительна")

// AccountStore — создание аккаунта продавца.
type AccountStore interface {
	CreateWithOwner(ctx context.Context, acc *model.Account, owner *model.NewUser) (*model.Account, *model.User, error)
}

// SessionStore — учётные данные и сессии.
type SessionStore interface {
	GetCredentialsByEmail(ctx context.Context, email string) (*model.UserCredentials, error)
	TouchLogin(ctx context.Context, userID int64) error
	Create(ctx context.Context, s *model.Session) error
	GetPrincipal(ctx context.Context, idHash []byte) (*model.Principal, error)
	Delete(ctx context.Context, idHash []byte) error
}

// ClientMeta — откуда пришёл запрос; сохраняется в сессии для аудита.
type ClientMeta struct {
	IP        string
	UserAgent string
}

// RegisterInput — форма регистрации продавца. Имена полей совпадают с
// name у input в шаблоне: по ним обработчик раскладывает ошибки.
type RegisterInput struct {
	Company  string     `validate:"required,min=2,max=200"`
	Name     string     `validate:"required,min=2,max=150"`
	Email    string     `validate:"required,email,max=255"`
	Phone    string     `validate:"max=32"`
	Password string     `validate:"required,min=8,max=128"`
	Plan     string     `validate:"required"`
	Consent  bool       `validate:"required"`
	Locale   string     `validate:"-"`
	Client   ClientMeta `validate:"-"`
}

// LoginInput — форма входа.
type LoginInput struct {
	Email    string     `validate:"required,email,max=255"`
	Password string     `validate:"required,max=128"`
	Client   ClientMeta `validate:"-"`
}

type AuthService struct {
	accounts AccountStore
	sessions SessionStore
	plans    *PlanCatalog
	now      func() time.Time
	// dummyHash проверяется, когда email не найден: время ответа не должно
	// выдавать, существует ли пользователь.
	dummyHash string
}

func NewAuthService(accounts AccountStore, sessions SessionStore, plans *PlanCatalog) (*AuthService, error) {
	dummy, err := password.Hash("nado-dummy-password")
	if err != nil {
		return nil, fmt.Errorf("service: подготовка проверки входа: %w", err)
	}
	return &AuthService{
		accounts:  accounts,
		sessions:  sessions,
		plans:     plans,
		now:       time.Now,
		dummyHash: dummy,
	}, nil
}

// Register создаёт аккаунт продавца с владельцем и сразу открывает сессию.
// Возвращает токен для cookie.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (string, error) {
	in.Company = strings.TrimSpace(in.Company)
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = strings.TrimSpace(in.Phone)

	fields, err := httpx.CheckFields(in)
	if err != nil {
		return "", err
	}
	if fields == nil {
		fields = make(map[string]httpx.FieldError)
	}
	if _, ok := fields["consent"]; ok {
		fields["consent"] = httpx.FieldError{Tag: "consent"}
	}
	if _, ok := s.plans.Get(in.Plan); !ok {
		fields["plan"] = httpx.FieldError{Tag: "oneof"}
	}
	phone := ""
	if in.Phone != "" {
		p, ok := NormalizePhone(in.Phone)
		if !ok {
			fields["phone"] = httpx.FieldError{Tag: "phone"}
		}
		phone = p
	}
	if len(fields) > 0 {
		return "", httpx.ErrFields(fields)
	}

	hash, err := password.Hash(in.Password)
	if err != nil {
		return "", httpx.ErrInternal(err)
	}

	acc, user, err := s.accounts.CreateWithOwner(ctx,
		&model.Account{
			Name:     in.Company,
			Country:  "KZ", // старт в Казахстане (D-13)
			PlanCode: in.Plan,
			Status:   model.AccountStatusActive,
		},
		&model.NewUser{
			Email:        in.Email,
			Name:         in.Name,
			Phone:        phone,
			PasswordHash: hash,
			Locale:       in.Locale,
		})
	if err != nil {
		if errors.Is(err, database.ErrConflict) {
			return "", httpx.ErrFields(map[string]httpx.FieldError{"email": {Tag: "email_taken"}})
		}
		return "", httpx.ErrInternal(err)
	}

	return s.openSession(ctx, user.ID, acc.ID, in.Client)
}

// Login проверяет пароль и открывает сессию.
func (s *AuthService) Login(ctx context.Context, in LoginInput) (string, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	fields, err := httpx.CheckFields(in)
	if err != nil {
		return "", err
	}
	if fields != nil {
		return "", httpx.ErrFields(fields)
	}

	creds, err := s.sessions.GetCredentialsByEmail(ctx, in.Email)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			_, _ = password.Verify(in.Password, s.dummyHash)
			return "", ErrInvalidCredentials
		}
		return "", httpx.ErrInternal(err)
	}

	hash := creds.PasswordHash
	if hash == "" {
		hash = s.dummyHash
	}
	ok, err := password.Verify(in.Password, hash)
	if err != nil {
		return "", httpx.ErrInternal(fmt.Errorf("проверка пароля пользователя %d: %w", creds.UserID, err))
	}
	// Все причины отказа неразличимы для клиента: пароль, блокировка,
	// отсутствие аккаунта.
	if !ok || creds.PasswordHash == "" || creds.Status != model.UserStatusActive || creds.AccountID == 0 {
		return "", ErrInvalidCredentials
	}

	token, err := s.openSession(ctx, creds.UserID, creds.AccountID, in.Client)
	if err != nil {
		return "", err
	}
	if err := s.sessions.TouchLogin(ctx, creds.UserID); err != nil {
		// Время входа — справочная информация, из-за неё вход не срываем.
		httpx.Logger(ctx).Warn("не удалось отметить вход", slog.Int64("user_id", creds.UserID), slog.Any("error", err))
	}
	return token, nil
}

// Authenticate возвращает пользователя по токену из cookie.
func (s *AuthService) Authenticate(ctx context.Context, token string) (*model.Principal, error) {
	hash, ok := hashToken(token)
	if !ok {
		return nil, ErrNoSession
	}
	p, err := s.sessions.GetPrincipal(ctx, hash)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ErrNoSession
		}
		return nil, httpx.ErrInternal(err)
	}
	return p, nil
}

// Logout закрывает сессию. Неизвестный токен — не ошибка.
func (s *AuthService) Logout(ctx context.Context, token string) error {
	hash, ok := hashToken(token)
	if !ok {
		return nil
	}
	if err := s.sessions.Delete(ctx, hash); err != nil {
		return httpx.ErrInternal(err)
	}
	return nil
}

func (s *AuthService) openSession(ctx context.Context, userID, accountID int64, client ClientMeta) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", httpx.ErrInternal(fmt.Errorf("токен сессии: %w", err))
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash, _ := hashToken(token)

	if err := s.sessions.Create(ctx, &model.Session{
		IDHash:    hash,
		UserID:    userID,
		AccountID: accountID,
		ExpiresAt: s.now().Add(SessionTTL),
		IP:        client.IP,
		UserAgent: client.UserAgent,
	}); err != nil {
		return "", httpx.ErrInternal(err)
	}
	return token, nil
}

// hashToken — SHA-256 токена. Токены не длиннее 64 символов: всё, что длиннее,
// заведомо не наше, и хешировать мегабайтную cookie незачем.
func hashToken(token string) ([]byte, bool) {
	if token == "" || len(token) > 64 {
		return nil, false
	}
	sum := sha256.Sum256([]byte(token))
	return sum[:], true
}

// NormalizePhone приводит казахстанский или российский номер (+7) к E.164:
// «8 (700) 123-45-67», «+7 700 1234567», «7001234567» → «+77001234567».
func NormalizePhone(s string) (string, bool) {
	digits := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits = append(digits, c)
		case c == '+' || c == ' ' || c == '-' || c == '(' || c == ')':
		default:
			return "", false
		}
	}
	switch {
	case len(digits) == 11 && (digits[0] == '7' || digits[0] == '8'):
		digits = digits[1:]
	case len(digits) == 10:
	default:
		return "", false
	}
	return "+7" + string(digits), true
}
