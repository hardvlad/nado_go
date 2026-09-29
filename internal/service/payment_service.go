package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"nado_go/internal/database"
	"nado_go/internal/httpx"
	"nado_go/internal/integration/payment"
	"nado_go/internal/model"
	"nado_go/internal/repository"
	"nado_go/internal/secrets"
)

// Ошибки слоя оплат.
var (
	ErrPaymentsDisabled  = errors.New("service: приём оплат не настроен для магазина")
	ErrPaymentProvider   = errors.New("service: неизвестный платёжный провайдер")
	ErrPaymentNotFound   = errors.New("service: платёж не найден")
	ErrSubscriptionNoPay = errors.New("service: оплата подписки картой не настроена")
)

// PlatformPay — мерчант платформы для оплаты подписки продавцами.
type PlatformPay struct {
	Provider     string
	MerchantID   string
	Secret       string
	Terminal     string
	Testing      bool
	WebhookToken string
}

// PaymentConfig — параметры слоя оплат, не зависящие от запроса.
type PaymentConfig struct {
	DefaultProvider  string
	SubscriptionDays int
	Platform         PlatformPay
}

// PaymentService оркестрирует приём оплат: заказов витрины (деньги покупателя →
// мерчант продавца) и подписки платформе (деньги продавца → мерчант nado).
type PaymentService struct {
	payments *repository.PaymentRepository
	orders   *repository.OrderRepository
	box      *secrets.Box
	reg      *payment.Registry
	plans    *PlanCatalog
	cfg      PaymentConfig
	log      *slog.Logger
}

func NewPaymentService(payments *repository.PaymentRepository, orders *repository.OrderRepository, box *secrets.Box, reg *payment.Registry, plans *PlanCatalog, cfg PaymentConfig, log *slog.Logger) *PaymentService {
	if cfg.SubscriptionDays <= 0 {
		cfg.SubscriptionDays = 30
	}
	return &PaymentService{payments: payments, orders: orders, box: box, reg: reg, plans: plans, cfg: cfg, log: log}
}

// Providers — коды доступных провайдеров (для выбора в кабинете).
func (s *PaymentService) Providers() []string { return s.reg.Codes() }

// StartResult — что делать витрине после инициализации платежа.
type StartResult struct {
	RedirectURL string              // непусто — отправить покупателя на страницу провайдера
	Widget      *payment.WidgetPage // непусто — показать страницу виджета (Halyk)
	Local       bool                // показать локальную страницу оплаты (dev/kaspi)
	Manual      bool                // ручное подтверждение (kaspi)
	Provider    string
	RefToken    string
}

// --- Настройки оплаты магазина ---

// StoreSettingsView — настройки оплаты магазина для кабинета (без секрета).
type StoreSettingsView struct {
	Provider     string
	IsEnabled    bool
	MerchantID   string
	Terminal     string
	Testing      bool
	HasSecret    bool
	WebhookToken string
}

// StoreSettings возвращает настройки оплаты магазина (значения по умолчанию, если
// ещё не заданы).
func (s *PaymentService) StoreSettings(ctx context.Context, accountID, storeID int64) (*StoreSettingsView, error) {
	set, err := s.payments.GetStorePaymentSettings(ctx, accountID, storeID)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	if set == nil {
		return &StoreSettingsView{Provider: s.cfg.DefaultProvider}, nil
	}
	return &StoreSettingsView{
		Provider: set.Provider, IsEnabled: set.IsEnabled, MerchantID: set.MerchantID, Terminal: set.Terminal,
		Testing: set.Testing, HasSecret: set.HasSecret, WebhookToken: set.WebhookToken,
	}, nil
}

// SaveStoreSettingsInput — ввод формы настроек оплаты магазина.
type SaveStoreSettingsInput struct {
	Provider   string
	IsEnabled  bool
	MerchantID string
	Terminal   string
	Secret     string // пусто — не менять
	Testing    bool
}

// SaveStoreSettings сохраняет настройки оплаты магазина. Секрет шифруется; токен
// вебхука генерируется при первом сохранении онлайн-провайдера.
func (s *PaymentService) SaveStoreSettings(ctx context.Context, accountID, storeID int64, in SaveStoreSettingsInput) error {
	if _, ok := s.reg.Get(in.Provider); !ok {
		return ErrPaymentProvider
	}
	cur, err := s.payments.GetStorePaymentSettings(ctx, accountID, storeID)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	save := repository.SaveStorePaymentSettingsInput{
		StoreID: storeID, AccountID: accountID, Provider: in.Provider,
		IsEnabled: in.IsEnabled, MerchantID: strings.TrimSpace(in.MerchantID),
		Terminal: strings.TrimSpace(in.Terminal), Testing: in.Testing,
	}
	if in.Secret != "" {
		cipher, err := s.box.EncryptString(in.Secret)
		if err != nil {
			return httpx.ErrInternal(err)
		}
		save.SecretCiphertext = cipher
		save.UpdateSecret = true
	}
	// Токен вебхука нужен онлайн-провайдерам; генерируем один раз и сохраняем.
	if cur == nil || cur.WebhookToken == "" {
		save.WebhookToken = randToken(24)
		save.UpdateToken = true
	}
	if err := s.payments.SaveStorePaymentSettings(ctx, save); err != nil {
		return httpx.ErrInternal(err)
	}
	return nil
}

// --- Оплата заказа витрины ---

// StartOrderPayment создаёт платёж за заказ и возвращает, куда отправить покупателя.
// successURL/failURL/origin строит вызывающая витрина (знает хост и префикс).
func (s *PaymentService) StartOrderPayment(ctx context.Context, accountID, storeID int64, order *model.Order, origin, successURL, failURL string) (*StartResult, error) {
	set, err := s.payments.GetStorePaymentSettings(ctx, accountID, storeID)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	if set == nil || !set.IsEnabled {
		return nil, ErrPaymentsDisabled
	}
	prov, ok := s.reg.Get(set.Provider)
	if !ok {
		return nil, ErrPaymentProvider
	}

	ref := "o-" + randToken(24)
	if _, err := s.payments.CreatePayment(ctx, repository.NewPayment{
		AccountID: accountID, StoreID: storeID, Purpose: "order", OrderID: order.ID,
		Provider: set.Provider, RefToken: ref, AmountMinor: order.TotalMinor, Currency: order.Currency,
	}); err != nil {
		return nil, httpx.ErrInternal(err)
	}

	creds, err := s.credsFor(set)
	if err != nil {
		return nil, err
	}
	callback := origin + "/webhooks/payment/" + set.Provider + "/" + set.WebhookToken
	res, err := prov.Start(ctx, payment.StartInput{
		Ref: ref, AmountMinor: order.TotalMinor, Currency: order.Currency,
		Description: fmt.Sprintf("Заказ №%d", order.Number),
		Email:       order.CustomerEmail, Phone: order.CustomerPhone,
		SuccessURL: successURL, FailURL: failURL, CallbackURL: callback, Creds: creds,
	})
	if err != nil {
		s.log.Warn("оплата заказа: провайдер отклонил инициализацию", slog.Int64("store_id", storeID), slog.Any("error", err))
		return nil, httpx.ErrInternal(err)
	}
	if res.ProviderRef != "" {
		_ = s.payments.SetProviderRef(ctx, mustGetPaymentID(ctx, s.payments, ref), res.ProviderRef)
	}
	return &StartResult{
		RedirectURL: res.RedirectURL, Widget: res.Widget,
		Local:  res.RedirectURL == "" && res.Widget == nil,
		Manual: prov.Manual(), Provider: set.Provider, RefToken: ref,
	}, nil
}

// ConfirmDevOrder подтверждает платёж dev-провайдера (кнопка на локальной странице
// оплаты). Только для провайдера dev.
func (s *PaymentService) ConfirmDevOrder(ctx context.Context, accountID, storeID int64, ref string) error {
	p, err := s.payments.GetPaymentByRef(ctx, ref)
	if errors.Is(err, database.ErrNotFound) {
		return ErrPaymentNotFound
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}
	if p.AccountID != accountID || p.StoreID != storeID || p.Provider != "dev" || p.Purpose != "order" {
		return ErrPaymentNotFound
	}
	return s.settleOrder(ctx, ref, "")
}

// --- Оплата подписки платформе ---

// StartSubscriptionPayment создаёт платёж за подписку и возвращает, куда отправить
// продавца.
func (s *PaymentService) StartSubscriptionPayment(ctx context.Context, accountID int64, planCode, origin, successURL, failURL string) (*StartResult, error) {
	if !s.platformPayEnabled() {
		return nil, ErrSubscriptionNoPay
	}
	plan, ok := s.plans.Get(planCode)
	if !ok {
		return nil, httpx.ErrBadRequest("Неизвестный тариф")
	}
	prov, ok := s.reg.Get(s.cfg.Platform.Provider)
	if !ok {
		return nil, ErrPaymentProvider
	}

	ref := "s-" + randToken(24)
	if _, err := s.payments.CreatePayment(ctx, repository.NewPayment{
		AccountID: accountID, Purpose: "subscription", Provider: s.cfg.Platform.Provider,
		RefToken: ref, AmountMinor: plan.Price.Minor, Currency: string(plan.Price.Currency),
		PlanCode: planCode, PeriodDays: s.cfg.SubscriptionDays,
	}); err != nil {
		return nil, httpx.ErrInternal(err)
	}

	callback := origin + "/webhooks/payment/" + s.cfg.Platform.Provider + "/" + s.cfg.Platform.WebhookToken
	res, err := prov.Start(ctx, payment.StartInput{
		Ref: ref, AmountMinor: plan.Price.Minor, Currency: string(plan.Price.Currency),
		Description: "Подписка nado: тариф " + planCode,
		SuccessURL:  successURL, FailURL: failURL, CallbackURL: callback,
		Creds: payment.Credentials{MerchantID: s.cfg.Platform.MerchantID, Secret: s.cfg.Platform.Secret, Terminal: s.cfg.Platform.Terminal, Testing: s.cfg.Platform.Testing},
	})
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return &StartResult{RedirectURL: res.RedirectURL, Widget: res.Widget, Local: res.RedirectURL == "" && res.Widget == nil, Manual: prov.Manual(), Provider: s.cfg.Platform.Provider, RefToken: ref}, nil
}

// ConfirmDevSubscription подтверждает платёж подписки dev-провайдером.
func (s *PaymentService) ConfirmDevSubscription(ctx context.Context, accountID int64, ref string) error {
	p, err := s.payments.GetPaymentByRef(ctx, ref)
	if errors.Is(err, database.ErrNotFound) {
		return ErrPaymentNotFound
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}
	if p.AccountID != accountID || p.Provider != "dev" || p.Purpose != "subscription" {
		return ErrPaymentNotFound
	}
	return s.settleSubscription(ctx, p)
}

// Subscription возвращает состояние подписки аккаунта.
func (s *PaymentService) Subscription(ctx context.Context, accountID int64) (*repository.Subscription, error) {
	sub, err := s.payments.GetSubscription(ctx, accountID)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return sub, nil
}

// PlatformPayEnabled — можно ли оплатить подписку картой.
func (s *PaymentService) PlatformPayEnabled() bool { return s.platformPayEnabled() }

// --- Вебхуки ---

// HandleWebhook обрабатывает входящий вебхук провайдера. token различает получателя:
// токен подписки платформы → подписка, иначе — по настройкам магазина.
func (s *PaymentService) HandleWebhook(ctx context.Context, provider, token string, r *http.Request) (contentType string, body []byte, err error) {
	prov, ok := s.reg.Get(provider)
	if !ok {
		return "", nil, ErrPaymentProvider
	}

	// Подписка платформы.
	if s.cfg.Platform.WebhookToken != "" && provider == s.cfg.Platform.Provider && token == s.cfg.Platform.WebhookToken {
		creds := payment.Credentials{MerchantID: s.cfg.Platform.MerchantID, Secret: s.cfg.Platform.Secret, Terminal: s.cfg.Platform.Terminal, Testing: s.cfg.Platform.Testing}
		cb, perr := prov.ParseCallback(r, creds)
		if perr != nil {
			ct, b := prov.CallbackResponse(false)
			return ct, b, perr
		}
		if cb.Status == payment.StatusSucceeded {
			p, gerr := s.payments.GetPaymentByRef(ctx, cb.Ref)
			if gerr == nil && p.Purpose == "subscription" {
				if err := s.settleSubscription(ctx, p); err != nil {
					s.log.Error("вебхук подписки: активация не удалась", slog.Any("error", err))
				}
			}
		}
		ct, b := prov.CallbackResponse(true)
		return ct, b, nil
	}

	// Оплата заказа магазина.
	set, rerr := s.payments.ResolveStorePaymentByToken(ctx, provider, token)
	if errors.Is(rerr, database.ErrNotFound) {
		return "", nil, ErrPaymentNotFound
	}
	if rerr != nil {
		return "", nil, httpx.ErrInternal(rerr)
	}
	creds, cerr := s.credsFor(set)
	if cerr != nil {
		return "", nil, cerr
	}
	cb, perr := prov.ParseCallback(r, creds)
	if perr != nil {
		ct, b := prov.CallbackResponse(false)
		return ct, b, perr
	}
	switch cb.Status {
	case payment.StatusSucceeded:
		if err := s.settleOrder(ctx, cb.Ref, cb.ProviderRef); err != nil {
			s.log.Error("вебхук оплаты: проводка не удалась", slog.Any("error", err))
		}
	case payment.StatusFailed, payment.StatusCanceled:
		if _, err := s.payments.MarkPaymentStatus(ctx, cb.Ref, string(cb.Status), cb.ProviderRef); err != nil {
			s.log.Warn("вебхук оплаты: смена статуса не удалась", slog.Any("error", err))
		}
	}
	ct, b := prov.CallbackResponse(true)
	return ct, b, nil
}

// --- Внутреннее ---

// settleOrder помечает платёж успешным и заказ оплаченным (идемпотентно).
func (s *PaymentService) settleOrder(ctx context.Context, ref, providerRef string) error {
	p, err := s.payments.MarkPaymentStatus(ctx, ref, "succeeded", providerRef)
	if errors.Is(err, database.ErrNotFound) {
		return ErrPaymentNotFound
	}
	if err != nil {
		return err
	}
	if p.OrderID != 0 {
		return s.orders.MarkPaid(ctx, p.OrderID, p.ID)
	}
	return nil
}

// settleSubscription помечает платёж успешным и продлевает подписку.
func (s *PaymentService) settleSubscription(ctx context.Context, p *repository.PaymentRow) error {
	updated, err := s.payments.MarkPaymentStatus(ctx, p.RefToken, "succeeded", "")
	if err != nil {
		return err
	}
	days := updated.PeriodDays
	if days <= 0 {
		days = s.cfg.SubscriptionDays
	}
	until := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	return s.payments.ActivateSubscription(ctx, updated.AccountID, updated.PlanCode, until)
}

func (s *PaymentService) credsFor(set *repository.StorePaymentSettings) (payment.Credentials, error) {
	creds := payment.Credentials{MerchantID: set.MerchantID, Terminal: set.Terminal, Testing: set.Testing}
	if len(set.SecretCiphertext) > 0 {
		secret, err := s.box.DecryptString(set.SecretCiphertext)
		if err != nil {
			return creds, httpx.ErrInternal(fmt.Errorf("секрет мерчанта нечитаем: %w", err))
		}
		creds.Secret = secret
	}
	return creds, nil
}

func (s *PaymentService) platformPayEnabled() bool {
	p := s.cfg.Platform
	if p.Provider == "" {
		return false
	}
	if p.Provider == "dev" {
		return true
	}
	return p.MerchantID != "" && p.Secret != "" && p.WebhookToken != ""
}

// mustGetPaymentID возвращает id платежа по ref (для установки provider_ref сразу
// после создания). Ошибку глушим: provider_ref не критичен для проводки.
func mustGetPaymentID(ctx context.Context, repo *repository.PaymentRepository, ref string) int64 {
	p, err := repo.GetPaymentByRef(ctx, ref)
	if err != nil {
		return 0
	}
	return p.ID
}
