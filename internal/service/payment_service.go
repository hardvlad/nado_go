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

// PlatformPay — мерчант платформы для приёма оплаты подписки по одному провайдеру.
type PlatformPay struct {
	Provider     string
	MerchantID   string
	Secret       string
	Terminal     string
	Testing      bool
	WebhookToken string
}

// PaymentConfig — параметры слоя оплат, не зависящие от запроса. Platform —
// мерчанты платформы по провайдерам (продавец выбирает, чем платить подписку).
type PaymentConfig struct {
	SubscriptionDays int
	Platform         map[string]PlatformPay
}

// PaymentService оркестрирует приём оплат: заказов витрины (деньги покупателя →
// мерчант продавца) и подписки платформе (деньги продавца → мерчант nado).
// У магазина может быть несколько активных методов; покупатель выбирает способ.
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
	if cfg.Platform == nil {
		cfg.Platform = map[string]PlatformPay{}
	}
	return &PaymentService{payments: payments, orders: orders, box: box, reg: reg, plans: plans, cfg: cfg, log: log}
}

// Providers — все коды провайдеров реестра (для добавления методов в кабинете).
func (s *PaymentService) Providers() []string { return s.reg.Codes() }

// providerNeedsCreds — провайдеру нужны реквизиты мерчанта (не dev/kaspi).
func providerNeedsCreds(provider string) bool { return provider == "freedompay" || provider == "halyk" }

// providerNeedsTerminal — провайдеру нужен терминал (Halyk ePay).
func providerNeedsTerminal(provider string) bool { return provider == "halyk" }

// StartResult — что делать витрине после инициализации платежа.
type StartResult struct {
	RedirectURL string              // непусто — отправить покупателя на страницу провайдера
	Widget      *payment.WidgetPage // непусто — показать страницу виджета (Halyk)
	Local       bool                // показать локальную страницу оплаты (dev/kaspi)
	Manual      bool                // ручное подтверждение (kaspi)
	Provider    string
	RefToken    string
}

// --- Методы оплаты магазина ---

// PaymentMethodView — метод оплаты магазина для кабинета/витрины (без секрета).
type PaymentMethodView struct {
	Provider      string
	Configured    bool // строка метода существует
	IsEnabled     bool
	MerchantID    string
	Terminal      string
	Testing       bool
	HasSecret     bool
	NeedsCreds    bool
	NeedsTerminal bool
	WebhookToken  string
}

// StoreMethods возвращает все провайдеры реестра с текущими настройками метода
// магазина (для раздела управления методами в кабинете).
func (s *PaymentService) StoreMethods(ctx context.Context, accountID, storeID int64) ([]PaymentMethodView, error) {
	rows, err := s.payments.ListStorePaymentMethods(ctx, accountID, storeID)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	byProvider := make(map[string]repository.StorePaymentMethod, len(rows))
	for _, m := range rows {
		byProvider[m.Provider] = m
	}
	var out []PaymentMethodView
	for _, code := range s.reg.Codes() {
		v := PaymentMethodView{Provider: code, NeedsCreds: providerNeedsCreds(code), NeedsTerminal: providerNeedsTerminal(code)}
		if m, ok := byProvider[code]; ok {
			v.Configured = true
			v.IsEnabled = m.IsEnabled
			v.MerchantID = m.MerchantID
			v.Terminal = m.Terminal
			v.Testing = m.Testing
			v.HasSecret = m.HasSecret
			v.WebhookToken = m.WebhookToken
		}
		out = append(out, v)
	}
	return out, nil
}

// EnabledStoreMethods возвращает включённые методы оплаты магазина (для выбора
// покупателем). Провайдеры без нужных реквизитов исключаются.
func (s *PaymentService) EnabledStoreMethods(ctx context.Context, accountID, storeID int64) ([]PaymentMethodView, error) {
	all, err := s.StoreMethods(ctx, accountID, storeID)
	if err != nil {
		return nil, err
	}
	var out []PaymentMethodView
	for _, m := range all {
		if m.IsEnabled && (!m.NeedsCreds || m.HasSecret) {
			out = append(out, m)
		}
	}
	return out, nil
}

// MethodForEdit возвращает метод оплаты для формы (пустой, если ещё не подключён).
func (s *PaymentService) MethodForEdit(ctx context.Context, accountID, storeID int64, provider string) (*PaymentMethodView, error) {
	if _, ok := s.reg.Get(provider); !ok {
		return nil, ErrPaymentProvider
	}
	m, err := s.payments.GetStorePaymentMethod(ctx, accountID, storeID, provider)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	v := &PaymentMethodView{Provider: provider, NeedsCreds: providerNeedsCreds(provider), NeedsTerminal: providerNeedsTerminal(provider)}
	if m != nil {
		v.Configured = true
		v.IsEnabled = m.IsEnabled
		v.MerchantID = m.MerchantID
		v.Terminal = m.Terminal
		v.Testing = m.Testing
		v.HasSecret = m.HasSecret
		v.WebhookToken = m.WebhookToken
	}
	return v, nil
}

// SaveMethodInput — ввод формы метода оплаты.
type SaveMethodInput struct {
	Provider   string
	IsEnabled  bool
	MerchantID string
	Terminal   string
	Secret     string // пусто — не менять
	Testing    bool
}

// SaveMethod создаёт или обновляет метод оплаты магазина. Секрет шифруется; токен
// вебхука генерируется один раз.
func (s *PaymentService) SaveMethod(ctx context.Context, accountID, storeID int64, in SaveMethodInput) error {
	if _, ok := s.reg.Get(in.Provider); !ok {
		return ErrPaymentProvider
	}
	cur, err := s.payments.GetStorePaymentMethod(ctx, accountID, storeID, in.Provider)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	save := repository.SaveStorePaymentMethodInput{
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
	if cur == nil || cur.WebhookToken == "" {
		save.WebhookToken = randToken(24)
		save.UpdateToken = true
	}
	if err := s.payments.SaveStorePaymentMethod(ctx, save); err != nil {
		return httpx.ErrInternal(err)
	}
	return nil
}

// DeleteMethod удаляет метод оплаты магазина.
func (s *PaymentService) DeleteMethod(ctx context.Context, accountID, storeID int64, provider string) error {
	if err := s.payments.DeleteStorePaymentMethod(ctx, accountID, storeID, provider); err != nil {
		return httpx.ErrInternal(err)
	}
	return nil
}

// --- Оплата заказа витрины ---

// StartOrderPayment создаёт платёж за заказ выбранным провайдером и возвращает,
// куда отправить покупателя. successURL/failURL/origin строит витрина.
func (s *PaymentService) StartOrderPayment(ctx context.Context, accountID, storeID int64, order *model.Order, provider, origin, successURL, failURL string) (*StartResult, error) {
	m, err := s.payments.GetStorePaymentMethod(ctx, accountID, storeID, provider)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	if m == nil || !m.IsEnabled || (providerNeedsCreds(provider) && !m.HasSecret) {
		return nil, ErrPaymentsDisabled
	}
	prov, ok := s.reg.Get(provider)
	if !ok {
		return nil, ErrPaymentProvider
	}

	ref := "o-" + randToken(24)
	if _, err := s.payments.CreatePayment(ctx, repository.NewPayment{
		AccountID: accountID, StoreID: storeID, Purpose: "order", OrderID: order.ID,
		Provider: provider, RefToken: ref, AmountMinor: order.TotalMinor, Currency: order.Currency,
	}); err != nil {
		return nil, httpx.ErrInternal(err)
	}

	creds, err := s.credsFor(m)
	if err != nil {
		return nil, err
	}
	callback := origin + "/webhooks/payment/" + provider + "/" + m.WebhookToken
	res, err := prov.Start(ctx, payment.StartInput{
		Ref: ref, AmountMinor: order.TotalMinor, Currency: order.Currency,
		Description: fmt.Sprintf("Заказ №%d", order.Number),
		Email:       order.CustomerEmail, Phone: order.CustomerPhone,
		SuccessURL: successURL, FailURL: failURL, CallbackURL: callback, Creds: creds,
	})
	if err != nil {
		s.log.Warn("оплата заказа: провайдер отклонил инициализацию", slog.Int64("store_id", storeID), slog.String("provider", provider), slog.Any("error", err))
		return nil, httpx.ErrInternal(err)
	}
	if res.ProviderRef != "" {
		_ = s.payments.SetProviderRef(ctx, mustGetPaymentID(ctx, s.payments, ref), res.ProviderRef)
	}
	return &StartResult{
		RedirectURL: res.RedirectURL, Widget: res.Widget,
		Local:  res.RedirectURL == "" && res.Widget == nil,
		Manual: prov.Manual(), Provider: provider, RefToken: ref,
	}, nil
}

// ConfirmDevOrder подтверждает платёж dev-провайдера (кнопка на локальной странице).
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

// PlatformMethod — доступный способ оплаты подписки (для выбора продавцом).
type PlatformMethod struct {
	Provider string
	Manual   bool
}

// PlatformMethods возвращает доступные способы оплаты подписки в порядке кодов.
func (s *PaymentService) PlatformMethods() []PlatformMethod {
	var out []PlatformMethod
	for _, code := range s.reg.Codes() {
		pp, ok := s.cfg.Platform[code]
		if !ok || !platformAvailable(code, pp) {
			continue
		}
		prov, _ := s.reg.Get(code)
		out = append(out, PlatformMethod{Provider: code, Manual: prov != nil && prov.Manual()})
	}
	return out
}

// PlatformPayEnabled — доступен ли хотя бы один способ оплаты подписки.
func (s *PaymentService) PlatformPayEnabled() bool { return len(s.PlatformMethods()) > 0 }

// StartSubscriptionPayment создаёт платёж за подписку магазина выбранным провайдером.
func (s *PaymentService) StartSubscriptionPayment(ctx context.Context, accountID, storeID int64, planCode, provider, origin, successURL, failURL string) (*StartResult, error) {
	pp, ok := s.cfg.Platform[provider]
	if !ok || !platformAvailable(provider, pp) {
		return nil, ErrSubscriptionNoPay
	}
	plan, ok := s.plans.Get(planCode)
	if !ok {
		return nil, httpx.ErrBadRequest("Неизвестный тариф")
	}
	prov, ok := s.reg.Get(provider)
	if !ok {
		return nil, ErrPaymentProvider
	}

	ref := "s-" + randToken(24)
	if _, err := s.payments.CreatePayment(ctx, repository.NewPayment{
		AccountID: accountID, StoreID: storeID, Purpose: "subscription", Provider: provider,
		RefToken: ref, AmountMinor: plan.Price.Minor, Currency: string(plan.Price.Currency),
		PlanCode: planCode, PeriodDays: s.cfg.SubscriptionDays,
	}); err != nil {
		return nil, httpx.ErrInternal(err)
	}

	callback := origin + "/webhooks/payment/" + provider + "/" + pp.WebhookToken
	res, err := prov.Start(ctx, payment.StartInput{
		Ref: ref, AmountMinor: plan.Price.Minor, Currency: string(plan.Price.Currency),
		Description: "Подписка nado: тариф " + planCode,
		SuccessURL:  successURL, FailURL: failURL, CallbackURL: callback,
		Creds: payment.Credentials{MerchantID: pp.MerchantID, Secret: pp.Secret, Terminal: pp.Terminal, Testing: pp.Testing},
	})
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return &StartResult{RedirectURL: res.RedirectURL, Widget: res.Widget, Local: res.RedirectURL == "" && res.Widget == nil, Manual: prov.Manual(), Provider: provider, RefToken: ref}, nil
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

// Subscription возвращает состояние подписки магазина.
func (s *PaymentService) Subscription(ctx context.Context, accountID, storeID int64) (*repository.Subscription, error) {
	sub, err := s.payments.GetStoreSubscription(ctx, accountID, storeID)
	if errors.Is(err, database.ErrNotFound) {
		return nil, httpx.ErrNotFound("Магазин не найден")
	}
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return sub, nil
}

// --- Вебхуки ---

// HandleWebhook обрабатывает входящий вебхук провайдера. token различает получателя:
// токен подписки платформы этого провайдера → подписка, иначе — метод магазина.
func (s *PaymentService) HandleWebhook(ctx context.Context, provider, token string, r *http.Request) (contentType string, body []byte, err error) {
	prov, ok := s.reg.Get(provider)
	if !ok {
		return "", nil, ErrPaymentProvider
	}

	// Подписка платформы (мерчант платформы для этого провайдера).
	if pp, ok := s.cfg.Platform[provider]; ok && pp.WebhookToken != "" && token == pp.WebhookToken {
		creds := payment.Credentials{MerchantID: pp.MerchantID, Secret: pp.Secret, Terminal: pp.Terminal, Testing: pp.Testing}
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

	// Оплата заказа магазина (метод по токену).
	m, rerr := s.payments.ResolveStoreMethodByToken(ctx, provider, token)
	if errors.Is(rerr, database.ErrNotFound) {
		return "", nil, ErrPaymentNotFound
	}
	if rerr != nil {
		return "", nil, httpx.ErrInternal(rerr)
	}
	creds, cerr := s.credsFor(m)
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
	return s.payments.ActivateStoreSubscription(ctx, updated.AccountID, updated.StoreID, updated.PlanCode, until)
}

func (s *PaymentService) credsFor(m *repository.StorePaymentMethod) (payment.Credentials, error) {
	creds := payment.Credentials{MerchantID: m.MerchantID, Terminal: m.Terminal, Testing: m.Testing}
	if len(m.SecretCiphertext) > 0 {
		secret, err := s.box.DecryptString(m.SecretCiphertext)
		if err != nil {
			return creds, httpx.ErrInternal(fmt.Errorf("секрет мерчанта нечитаем: %w", err))
		}
		creds.Secret = secret
	}
	return creds, nil
}

// platformAvailable — можно ли принять подписку этим провайдером платформы. Kaspi
// (ручной) для подписки не используется — нет автоактивации.
func platformAvailable(provider string, pp PlatformPay) bool {
	switch provider {
	case "dev":
		return true
	case "freedompay":
		return pp.MerchantID != "" && pp.Secret != "" && pp.WebhookToken != ""
	case "halyk":
		return pp.MerchantID != "" && pp.Secret != "" && pp.Terminal != "" && pp.WebhookToken != ""
	default:
		return false
	}
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
