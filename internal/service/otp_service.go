package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"nado_go/internal/database"
	"nado_go/internal/httpx"
	"nado_go/internal/integration/messaging"
	"nado_go/internal/model"
	"nado_go/internal/secrets"
)

// Ошибки OTP. Обработчик переводит их в сообщения на языке страницы.
var (
	ErrOTPInvalidPhone    = errors.New("otp: некорректный номер")
	ErrOTPCooldown        = errors.New("otp: код уже отправлен, подождите")
	ErrOTPRateLimited     = errors.New("otp: слишком много запросов кода")
	ErrOTPSendFailed      = errors.New("otp: не удалось отправить код")
	ErrOTPNotFound        = errors.New("otp: код не запрашивался или истёк")
	ErrOTPExpired         = errors.New("otp: срок действия кода истёк")
	ErrOTPTooManyAttempts = errors.New("otp: слишком много попыток ввода")
	ErrOTPWrongCode       = errors.New("otp: неверный код")
)

const (
	otpResendCooldown = 60 * time.Second
	otpMaxAttempts    = 5
	otpCodeDigits     = 6
)

// OTPStore — хранилище одноразовых кодов.
type OTPStore interface {
	Create(ctx context.Context, c *model.OTPCode) (int64, error)
	Latest(ctx context.Context, phone, purpose string) (*model.OTPCode, error)
	LastSentAt(ctx context.Context, phone string) (time.Time, error)
	IncAttempt(ctx context.Context, id int64) error
	MarkUsed(ctx context.Context, id int64) (bool, error)
}

// CodeMessage формирует текст сообщения с кодом на нужном языке.
type CodeMessage func(code, lang string) string

// OTPService выдаёт и проверяет коды подтверждения телефона.
type OTPService struct {
	store   OTPStore
	box     *secrets.Box
	sender  messaging.Sender
	message CodeMessage
	ttl     time.Duration
	log     *slog.Logger
	now     func() time.Time

	byPhone *httpx.RateLimiter
	byIP    *httpx.RateLimiter
}

func NewOTPService(store OTPStore, box *secrets.Box, sender messaging.Sender, msg CodeMessage, ttl time.Duration, log *slog.Logger) *OTPService {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &OTPService{
		store:   store,
		box:     box,
		sender:  sender,
		message: msg,
		ttl:     ttl,
		log:     log,
		now:     time.Now,
		byPhone: httpx.NewRateLimiter(5, time.Hour),  // до 5 кодов на номер в час
		byIP:    httpx.NewRateLimiter(30, time.Hour), // до 30 запросов с адреса в час
	}
}

// Request генерирует код, сохраняет его HMAC и отправляет на номер.
// Возвращает нормализованный номер (E.164) для показа пользователю.
func (s *OTPService) Request(ctx context.Context, rawPhone, purpose, lang, ip string) (string, error) {
	phone, ok := NormalizePhone(rawPhone)
	if !ok {
		return "", ErrOTPInvalidPhone
	}

	// Лимиты защищают от перебора и от накрутки счёта за сообщения.
	if !s.byIP.Allow("otp:"+ip) || !s.byPhone.Allow("otp:"+phone) {
		return phone, ErrOTPRateLimited
	}

	// Пауза между отправками на один номер.
	last, err := s.store.LastSentAt(ctx, phone)
	if err != nil {
		return phone, httpx.ErrInternal(err)
	}
	if !last.IsZero() && s.now().Sub(last) < otpResendCooldown {
		return phone, ErrOTPCooldown
	}

	code, err := randomCode(otpCodeDigits)
	if err != nil {
		return phone, httpx.ErrInternal(err)
	}

	sent, sendErr := s.sender.Send(ctx, messaging.Message{Phone: phone, Text: s.message(code, lang)})

	rec := &model.OTPCode{
		Purpose:     purpose,
		Phone:       phone,
		CodeHash:    s.box.HMAC(code),
		Channel:     "whatsapp",
		MaxAttempts: otpMaxAttempts,
		ExpiresAt:   s.now().Add(s.ttl),
		IP:          ip,
	}
	if sent != nil {
		rec.InstanceID = sent.InstanceID
		rec.MessageID = sent.MessageID
	}
	// Код сохраняем, только если он ушёл: иначе пользователь получит «отправлено»,
	// но кода у него не будет.
	if sendErr != nil {
		s.log.Error("otp: отправка кода", slog.String("purpose", purpose), slog.Any("error", sendErr))
		if errors.Is(sendErr, messaging.ErrNoInstance) {
			return phone, ErrOTPSendFailed
		}
		return phone, ErrOTPSendFailed
	}
	if _, err := s.store.Create(ctx, rec); err != nil {
		return phone, httpx.ErrInternal(err)
	}
	return phone, nil
}

// Verify проверяет код. Успех — номер подтверждён.
func (s *OTPService) Verify(ctx context.Context, rawPhone, purpose, code string) error {
	phone, ok := NormalizePhone(rawPhone)
	if !ok {
		return ErrOTPInvalidPhone
	}

	rec, err := s.store.Latest(ctx, phone, purpose)
	if errors.Is(err, database.ErrNotFound) {
		return ErrOTPNotFound
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}
	if s.now().After(rec.ExpiresAt) {
		return ErrOTPExpired
	}
	if rec.Attempts >= rec.MaxAttempts {
		return ErrOTPTooManyAttempts
	}
	if err := s.store.IncAttempt(ctx, rec.ID); err != nil {
		return httpx.ErrInternal(err)
	}
	if !s.box.EqualHMAC(code, rec.CodeHash) {
		return ErrOTPWrongCode
	}
	used, err := s.store.MarkUsed(ctx, rec.ID)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	if !used {
		// Код уже использован параллельным запросом.
		return ErrOTPWrongCode
	}
	return nil
}

// randomCode возвращает код из n десятичных цифр (с ведущими нулями).
func randomCode(n int) (string, error) {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	v, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("otp: генерация кода: %w", err)
	}
	return fmt.Sprintf("%0*d", n, v), nil
}
