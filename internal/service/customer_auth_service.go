package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"nado_go/internal/database"
	"nado_go/internal/httpx"
	"nado_go/internal/integration/messaging"
	"nado_go/internal/model"
	"nado_go/internal/repository"
	"nado_go/internal/secrets"
)

// Ошибки входа покупателя (переводятся обработчиком витрины).
var (
	ErrCustomerPhone    = errors.New("customer: некорректный номер")
	ErrCustomerCooldown = errors.New("customer: код уже отправлен, подождите")
	ErrCustomerRate     = errors.New("customer: слишком много запросов кода")
	ErrCustomerSend     = errors.New("customer: не удалось отправить код")
	ErrCustomerNoCode   = errors.New("customer: код не запрашивался или истёк")
	ErrCustomerBadCode  = errors.New("customer: неверный или просроченный код")
)

const (
	customerCodeTTL     = 5 * time.Minute
	customerResendPause = 60 * time.Second
	customerSessionTTL  = 30 * 24 * time.Hour
	customerCodeDigits  = 6
	customerMaxAttempts = 5
)

// CustomerAuthService — вход и регистрация покупателя по телефону (D-17).
// Коды отправляет платформенный отправитель (сейчас WhatsApp через GreenAPI).
type CustomerAuthService struct {
	repo    *repository.CustomerRepository
	box     *secrets.Box
	sender  messaging.Sender
	message CodeMessage
	log     *slog.Logger
	now     func() time.Time

	byPhone *httpx.RateLimiter
	byIP    *httpx.RateLimiter
}

func NewCustomerAuthService(repo *repository.CustomerRepository, box *secrets.Box, sender messaging.Sender, msg CodeMessage, log *slog.Logger) *CustomerAuthService {
	return &CustomerAuthService{
		repo: repo, box: box, sender: sender, message: msg, log: log, now: time.Now,
		byPhone: httpx.NewRateLimiter(5, time.Hour),
		byIP:    httpx.NewRateLimiter(30, time.Hour),
	}
}

// RequestCode генерирует код, сохраняет его HMAC и отправляет покупателю.
// Возвращает нормализованный номер для показа.
func (s *CustomerAuthService) RequestCode(ctx context.Context, storeID int64, rawPhone, lang, ip string) (string, error) {
	phone, ok := NormalizePhone(rawPhone)
	if !ok {
		return "", ErrCustomerPhone
	}
	if !s.byIP.Allow("cust:"+ip) || !s.byPhone.Allow("cust:"+phone) {
		return phone, ErrCustomerRate
	}

	last, err := s.repo.LastChallengeAt(ctx, storeID, phone)
	if err != nil {
		return phone, httpx.ErrInternal(err)
	}
	if !last.IsZero() && s.now().Sub(last) < customerResendPause {
		return phone, ErrCustomerCooldown
	}

	code, err := randomCode(customerCodeDigits)
	if err != nil {
		return phone, httpx.ErrInternal(err)
	}

	if _, err := s.sender.Send(ctx, messaging.Message{Phone: phone, Text: s.message(code, lang)}); err != nil {
		s.log.Error("customer: отправка кода", slog.Any("error", err))
		return phone, ErrCustomerSend
	}

	// Сохраняем только после успешной отправки.
	if err := s.repo.CreateChallenge(ctx, &model.OTPChallenge{
		StoreID: storeID, PhoneE164: phone, Channel: "whatsapp",
		CodeHash: s.box.HMAC(code), MaxAttempts: customerMaxAttempts,
		ExpiresAt: s.now().Add(customerCodeTTL),
	}, ip); err != nil {
		return phone, httpx.ErrInternal(err)
	}
	return phone, nil
}

// Verify проверяет код, создаёт/находит покупателя и открывает сессию.
// Возвращает покупателя и сырой токен сессии (кладётся в cookie витрины).
func (s *CustomerAuthService) Verify(ctx context.Context, storeID, accountID int64, rawPhone, code, name, ua string) (*model.Customer, string, error) {
	phone, ok := NormalizePhone(rawPhone)
	if !ok {
		return nil, "", ErrCustomerPhone
	}

	rec, err := s.repo.LatestChallenge(ctx, storeID, phone)
	if errors.Is(err, database.ErrNotFound) {
		return nil, "", ErrCustomerNoCode
	}
	if err != nil {
		return nil, "", httpx.ErrInternal(err)
	}
	if s.now().After(rec.ExpiresAt) || rec.Attempts >= rec.MaxAttempts {
		return nil, "", ErrCustomerBadCode
	}
	if err := s.repo.IncChallengeAttempt(ctx, rec.ID); err != nil {
		return nil, "", httpx.ErrInternal(err)
	}
	if !s.box.EqualHMAC(code, rec.CodeHash) {
		return nil, "", ErrCustomerBadCode
	}
	used, err := s.repo.MarkChallengeVerified(ctx, rec.ID)
	if err != nil {
		return nil, "", httpx.ErrInternal(err)
	}
	if !used {
		return nil, "", ErrCustomerBadCode
	}

	customer, err := s.repo.UpsertCustomer(ctx, storeID, accountID, phone, name)
	if err != nil {
		return nil, "", httpx.ErrInternal(err)
	}

	token, hash, err := newSessionToken()
	if err != nil {
		return nil, "", httpx.ErrInternal(err)
	}
	if err := s.repo.CreateSession(ctx, hash, customer.ID, storeID, ua, s.now().Add(customerSessionTTL)); err != nil {
		return nil, "", httpx.ErrInternal(err)
	}
	return customer, token, nil
}

// LoadCustomer возвращает покупателя по токену сессии витрины (или nil).
func (s *CustomerAuthService) LoadCustomer(ctx context.Context, storeID int64, token string) *model.Customer {
	hash, ok := hashToken(token)
	if !ok {
		return nil
	}
	c, err := s.repo.CustomerBySession(ctx, hash, storeID)
	if err != nil {
		return nil
	}
	return c
}

// Logout удаляет сессию по токену.
func (s *CustomerAuthService) Logout(ctx context.Context, token string) error {
	hash, ok := hashToken(token)
	if !ok {
		return nil
	}
	return s.repo.DeleteSession(ctx, hash)
}

// DeleteAccount обезличивает покупателя и удаляет его сессии.
func (s *CustomerAuthService) DeleteAccount(ctx context.Context, storeID, customerID int64) error {
	return s.repo.DeleteCustomer(ctx, storeID, customerID)
}

// SessionTTL — срок жизни cookie сессии покупателя.
func (s *CustomerAuthService) SessionTTL() time.Duration { return customerSessionTTL }

// newSessionToken возвращает сырой токен и его SHA-256 (в БД хранится хеш).
func newSessionToken() (token string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", nil, err
	}
	token = hex.EncodeToString(buf)
	h, _ := hashToken(token) // 64 hex-символа всегда валидны
	return token, h, nil
}
