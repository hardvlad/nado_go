// Package app собирает приложение из компонентов и управляет его жизненным
// циклом: запуск, обслуживание запросов, безопасное завершение.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"nado_go/internal/config"
	"nado_go/internal/database"
	"nado_go/internal/handler/api"
	"nado_go/internal/handler/jobsapi"
	"nado_go/internal/handler/shop"
	"nado_go/internal/handler/web"
	"nado_go/internal/handler/webhook"
	"nado_go/internal/i18n"
	"nado_go/internal/integration/marketplace/kaspi"
	"nado_go/internal/integration/messaging/greenapi"
	"nado_go/internal/integration/payment"
	"nado_go/internal/jobs"
	"nado_go/internal/mailbox"
	"nado_go/internal/repository"
	"nado_go/internal/router"
	"nado_go/internal/secrets"
	"nado_go/internal/service"
	"nado_go/internal/theme"
	"nado_go/internal/view"
	webassets "nado_go/web"
)

// drainDelay — пауза между переводом /readyz в 503 и началом остановки.
// За это время балансировщик успевает увести трафик на другие инстансы,
// иначе часть запросов попадёт на уже закрывающийся сервер.
const drainDelay = 2 * time.Second

// App — собранное приложение со всеми зависимостями.
type App struct {
	cfg     *config.Config
	log     *slog.Logger
	db      *database.DB
	server  *http.Server
	health  *api.HealthHandler
	runner  *jobs.Runner                  // встроенный воркер локальных задач
	mailbox *mailbox.Poller               // опрос почтового ящика служебных сотрудников (может быть nil)
	trial   *service.TrialReminderService // напоминания об оплате перед концом пробного периода
	conn    *service.ConnectionService    // для регулярного импорта каталога по расписанию

	// runnerDone закрывается, когда встроенный воркер завершил текущую задачу.
	// Ждём его перед закрытием БД, иначе задача оборвётся на запросе к базе.
	runnerDone chan struct{}
	// mailboxDone закрывается, когда поллер почты остановлен.
	mailboxDone chan struct{}
	// trialDone закрывается, когда планировщик напоминаний остановлен.
	trialDone chan struct{}
	// catalogDone закрывается, когда планировщик импорта каталога остановлен.
	catalogDone chan struct{}
	// ordersDone закрывается, когда планировщик импорта заказов остановлен.
	ordersDone chan struct{}
}

// New загружает конфигурацию и создаёт все компоненты.
// Если что-то не поднялось (нет БД, сломан шаблон) — возвращается ошибка,
// и приложение не стартует вовсе, вместо работы в полурабочем состоянии.
func New(ctx context.Context, version string) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	log := newLogger(cfg)
	log.Info("запуск приложения",
		slog.String("version", version),
		slog.String("env", cfg.App.Env),
		slog.Bool("debug", cfg.App.Debug),
	)

	db, err := database.New(ctx, cfg.Database, log)
	if err != nil {
		return nil, err
	}

	renderer, err := newRenderer(cfg, version)
	if err != nil {
		// Пул уже открыт — закрываем, чтобы не оставить висящие соединения.
		_ = db.Close()
		return nil, err
	}

	staticFS, err := webassets.Static(cfg.App.Debug)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("app: статические файлы: %w", err)
	}

	bundle, err := i18n.NewBundle()
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	// Сборка слоёв снизу вверх: репозиторий → сервис → обработчик.
	userRepo := repository.NewUserRepository(db)
	userService := service.NewUserService(userRepo)

	plans := service.NewPlanCatalog()
	sessionRepo := repository.NewSessionRepository(db)
	authService, err := service.NewAuthService(repository.NewAccountRepository(db), sessionRepo, plans)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	feedbackService := service.NewFeedbackService(repository.NewFeedbackRepository(db))

	// Шифрование секретов, пул WhatsApp и одноразовые коды для входа по телефону.
	box, err := secrets.New(cfg.Secrets.Key)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	whatsappRepo := repository.NewWhatsAppRepository(db, box)
	sender := greenapi.New(whatsappRepo, log)
	// Текст кода на языке пользователя.
	codeMessage := func(code, lang string) string {
		l, ok := i18n.Parse(lang)
		if !ok {
			l = i18n.Default
		}
		return bundle.Localizer(l).T("phone.otp_message", "Code", code)
	}
	otpService := service.NewOTPService(
		repository.NewOTPRepository(db), box, sender, codeMessage, cfg.Messaging.OTPTTL, !cfg.IsProduction(), log)

	// Очередь фоновых задач и раздача заданий удалённым воркерам (D-12, D-23).
	jobsRepo := jobs.NewRepository(db)
	runner := jobs.NewRunner(jobsRepo, log)
	catalogService := service.NewKaspiCatalogService(
		repository.NewStoreRepository(db), repository.NewMarketplaceProductRepository(db), jobsRepo, log)

	// Сборка продаваемого каталога витрины из зеркала (marketplace_products →
	// products/variants/store_offers), локальная задача.
	catalogBuild := service.NewCatalogBuildService(
		repository.NewMarketplaceProductRepository(db), repository.NewCatalogRepository(db),
		repository.NewStoreRepository(db), log)
	runner.Register(jobs.KindCatalogBuild, func(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
		var p struct {
			ConnectionID int64 `json:"connection_id"`
		}
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return nil, jobs.Permanent(fmt.Errorf("app: payload catalog.build_store: %w", err))
		}
		built, err := catalogBuild.Rebuild(ctx, p.ConnectionID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]int{"built": built})
	})
	// Источник кода MFA для воркера — почтовый ящик служебных сотрудников (IMAP).
	// Коды кладёт поллер (см. Run), выдаёт репозиторий с учётом срока годности.
	kaspiMFARepo := repository.NewKaspiMFARepository(db)
	mfaSource := mfaCodeLookup{repo: kaspiMFARepo, ttl: cfg.Mailbox.CodeTTL}

	// Подключение магазинов к Kaspi (официальный Shop API) и импорт заказов.
	connService := service.NewConnectionService(
		repository.NewStoreRepository(db), repository.NewMarketplaceOrderRepository(db),
		box, kaspi.NewOfficial(), jobsRepo, cfg.Platform.ShopSuffix, cfg.Trial.Days, log)

	// Кабинетный онбординг Kaspi: отправка SMS владельцу, создание сотрудника,
	// приём его пароля из почты, импорт каталога (D-28).
	onboardingService := service.NewKaspiOnboardingService(
		repository.NewKaspiOnboardingRepository(db), connService, jobsRepo, box,
		cfg.Kaspi.EmployeeEmailDomain, log)

	jobsAPI := jobsapi.New(jobsRepo, jobs.NewRunnerRepository(db), catalogService, mfaSource, onboardingService, log)

	// Поллер почты запускается только если ящик сконфигурирован (в dev его обычно
	// нет — тогда коды MFA и пароли сотрудников не приходят, онбординг не завершается).
	var mailboxPoller *mailbox.Poller
	if cfg.Mailbox.Enabled() {
		mailboxPoller = mailbox.NewPoller(mailbox.Config{
			Addr:       cfg.Mailbox.Addr,
			User:       cfg.Mailbox.User,
			Password:   cfg.Mailbox.Password,
			Mailbox:    cfg.Mailbox.Folder,
			ServerName: cfg.Mailbox.ServerName,
			Poll:       cfg.Mailbox.Poll,
		}, mailboxSink{codes: kaspiMFARepo, onboarding: onboardingService, log: log}, log)
	} else {
		log.Warn("почтовый ящик служебных сотрудников не настроен: коды MFA Kaspi поступать не будут")
	}

	runner.Register(jobs.KindKaspiImportOrders, func(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
		var p struct {
			ConnectionID int64 `json:"connection_id"`
		}
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return nil, jobs.Permanent(fmt.Errorf("app: payload kaspi.import_orders: %w", err))
		}
		created, updated, err := connService.ImportKaspiOrders(ctx, p.ConnectionID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]int{"created": created, "updated": updated})
	})

	health := api.NewHealthHandler(version, map[string]api.Pinger{"database": db})

	// Заказы витрины: один сервис и для публичной части (checkout), и для кабинета.
	cartRepo := repository.NewCartRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	orderService := service.NewOrderService(orderRepo, cartRepo)

	// Слой онлайн-оплат: заказы витрины (мерчант продавца, D-01) и подписка
	// платформе (мерчант nado). Провайдеры за интерфейсом payment.Provider.
	paymentRegistry := payment.NewRegistry(payment.NewDev(), payment.NewFreedomPay(), payment.NewHalyk(), payment.NewKaspi())
	paymentService := service.NewPaymentService(
		repository.NewPaymentRepository(db), orderRepo, box, paymentRegistry, plans,
		service.PaymentConfig{
			DefaultProvider:  cfg.Payments.DefaultProvider,
			SubscriptionDays: cfg.Payments.SubscriptionDays,
			Platform: service.PlatformPay{
				Provider: cfg.Payments.PlatformProvider, MerchantID: cfg.Payments.PlatformMerchantID,
				Secret: cfg.Payments.PlatformSecret, Terminal: cfg.Payments.PlatformTerminal,
				Testing: cfg.Payments.PlatformTesting, WebhookToken: cfg.Payments.PlatformWebhookToken,
			},
		}, log)

	// Редактирование каталога из кабинета (категории и товары, D-14/D-23).
	catalogEdit := service.NewCatalogEditService(
		repository.NewCatalogRepository(db), repository.NewStoreRepository(db), jobsRepo, log)

	pages := web.NewPageHandler(web.Deps{
		Render:        renderer,
		I18n:          bundle,
		Plans:         plans,
		Auth:          authService,
		OTP:           otpService,
		Connections:   connService,
		Onboarding:    onboardingService,
		StoreOrders:   orderService,
		Catalog:       catalogEdit,
		Payments:      paymentService,
		Feedback:      feedbackService,
		SecureCookies: cfg.IsProduction(),
		PublicURL:     cfg.App.PublicURL,
		TrialDays:     cfg.Trial.Days,
		ShopSuffix:    cfg.Platform.ShopSuffix,
	})

	// Витрина магазина (публичная часть): темы, каталог, выбор магазина по slug/Host.
	themesFS, err := webassets.Themes(cfg.App.Debug)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("app: темы витрины: %w", err)
	}
	themeRenderer, err := theme.New(themesFS,
		theme.WithDebug(cfg.App.Debug), theme.WithImageCDN(cfg.Kaspi.ImagesCDNURL))
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	// Вход покупателей по телефону (D-17): код в WhatsApp через тот же пул GreenAPI.
	customerAuth := service.NewCustomerAuthService(
		repository.NewCustomerRepository(db), box, sender, codeMessage, !cfg.IsProduction(), log)

	shopHandler := shop.New(shop.Deps{
		Stores:      repository.NewStoreRepository(db),
		Catalog:     service.NewStorefrontService(repository.NewCatalogRepository(db)),
		Customers:   customerAuth,
		Cart:        service.NewCartService(cartRepo),
		Orders:      orderService,
		Payments:    paymentService,
		Render:      themeRenderer,
		ThemeStatic: themesFS,
		I18n:        bundle,
		NotFound:    pages.NotFound,
		Prod:        cfg.IsProduction(),
		Log:         log,
	})

	handler := router.New(router.Deps{
		Config:          cfg,
		Logger:          log,
		Static:          staticFS,
		I18n:            bundle,
		Pages:           pages,
		Shop:            shopHandler,
		Users:           api.NewUserHandler(userService),
		Health:          health,
		JobsAPI:         jobsAPI,
		GreenAPIWebhook: webhook.NewGreenAPI(whatsappRepo, cfg.Messaging.GreenAPIWebhookToken, log),
		PaymentWebhook:  webhook.NewPayment(paymentService, log),
	})

	server := &http.Server{
		Addr:    cfg.HTTP.Addr,
		Handler: handler,
		// Таймауты обязательны: без них медленный клиент занимает соединение
		// бесконечно (slowloris) и исчерпывает лимиты сервера.
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	// BaseContext намеренно не задаём: контекст запросов не должен быть
	// привязан к сигнальному ctx, иначе при SIGTERM активные запросы
	// оборвутся мгновенно вместо корректного дренирования.

	// Напоминания об оплате перед окончанием пробного периода (WhatsApp).
	trialReminders := service.NewTrialReminderService(
		repository.NewStoreRepository(db), sender, bundle, cfg.App.PublicURL, log)

	return &App{cfg: cfg, log: log, db: db, server: server, health: health,
		runner: runner, mailbox: mailboxPoller, trial: trialReminders, conn: connService}, nil
}

// Run обслуживает запросы до отмены ctx, затем выполняет безопасное завершение.
//
// Порядок остановки: readiness → пауза дренирования → Shutdown (дожидается
// активных запросов) → закрытие БД. Обратный порядок оборвал бы запросы,
// которые ещё ходят в базу.
func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	// Встроенный воркер локальных задач. По отмене ctx он перестаёт брать
	// новые задачи; текущую дорабатывает, поэтому БД закрываем только после него.
	a.runnerDone = make(chan struct{})
	go func() {
		defer close(a.runnerDone)
		_ = a.runner.Run(ctx)
	}()

	// Поллер почты (если ящик настроен): по отмене ctx он завершится сам.
	if a.mailbox != nil {
		a.mailboxDone = make(chan struct{})
		go func() {
			defer close(a.mailboxDone)
			a.mailbox.Run(ctx)
		}()
	}

	// Планировщик напоминаний об оплате: периодически сканирует магазины у порога
	// окончания пробного периода. По отмене ctx завершается.
	if a.trial != nil {
		a.trialDone = make(chan struct{})
		go func() {
			defer close(a.trialDone)
			a.runTrialReminders(ctx)
		}()
	}

	// Планировщик регулярного импорта каталога Kaspi (если задан интервал).
	if a.conn != nil && a.cfg.Catalog.SyncInterval > 0 {
		a.catalogDone = make(chan struct{})
		go func() {
			defer close(a.catalogDone)
			a.runCatalogSyncs(ctx)
		}()
	}

	// Планировщик регулярного импорта заказов Kaspi (если задан интервал).
	if a.conn != nil && a.cfg.Catalog.OrdersSyncInterval > 0 {
		a.ordersDone = make(chan struct{})
		go func() {
			defer close(a.ordersDone)
			a.runOrdersSyncs(ctx)
		}()
	}

	go func() {
		a.log.Info("HTTP-сервер слушает", slog.String("addr", a.cfg.HTTP.Addr))
		// ErrServerClosed — штатный результат Shutdown, не ошибка.
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("app: HTTP-сервер: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		// Сервер упал сам (например, порт занят) — ресурсы всё равно закрываем.
		a.closeResources()
		return err
	case <-ctx.Done():
		a.log.Info("получен сигнал остановки, начинаем безопасное завершение")
	}

	return a.shutdown(errCh)
}

func (a *App) shutdown(errCh <-chan error) error {
	a.health.SetReady(false)

	if a.cfg.IsProduction() {
		time.Sleep(drainDelay)
	}

	// Контекст остановки отвязан от отменённого ctx приложения: иначе
	// Shutdown завершился бы мгновенно, оборвав активные запросы.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.HTTP.ShutdownTimeout)
	defer cancel()

	var errs []error
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		// Дедлайн вышел — принудительно рвём оставшиеся соединения,
		// чтобы процесс не завис навсегда.
		a.log.Error("не все запросы завершились в отведённое время",
			slog.Duration("timeout", a.cfg.HTTP.ShutdownTimeout),
			slog.Any("error", err))
		errs = append(errs, err)
		_ = a.server.Close()
	}

	if err := <-errCh; err != nil {
		errs = append(errs, err)
	}

	a.closeResources()
	a.log.Info("приложение остановлено")

	return errors.Join(errs...)
}

// mfaCodeLookup выдаёт воркеру код подтверждения входа в кабинет по адресу
// служебного сотрудника, учитывая срок годности кода. Реализует mfaCodeSource
// из handler/jobsapi.
type mfaCodeLookup struct {
	repo *repository.KaspiMFARepository
	ttl  time.Duration
}

func (l mfaCodeLookup) CodeForEmail(ctx context.Context, email string) (string, bool, error) {
	return l.repo.Latest(ctx, email, l.ttl)
}

// mailboxSink принимает то, что поллер вычитал из писем. Коды подтверждения
// входа сохраняются в kaspi_mfa_codes; пароль нового сотрудника передаётся в
// онбординг (там он шифруется и ставится импорт каталога).
type mailboxSink struct {
	codes      *repository.KaspiMFARepository
	onboarding *service.KaspiOnboardingService
	log        *slog.Logger
}

func (s mailboxSink) SaveCode(ctx context.Context, email, code string) error {
	return s.codes.Insert(ctx, email, code)
}

func (s mailboxSink) SaveCredentials(ctx context.Context, login, password string) error {
	// Пароль в лог не пишем — только в зашифрованное хранилище через сервис.
	return s.onboarding.SetEmployeePassword(ctx, login, password)
}

// closeResources закрывает внешние ресурсы. Вызывается один раз, после того
// как HTTP-сервер перестал обрабатывать запросы.
func (a *App) closeResources() {
	// Дожидаемся встроенного воркера: его текущая задача может ходить в БД.
	if a.runnerDone != nil {
		select {
		case <-a.runnerDone:
		case <-time.After(a.cfg.HTTP.ShutdownTimeout):
			a.log.Warn("встроенный воркер не завершился в отведённое время")
		}
	}
	// И поллера почты: он тоже обращается к БД (сохранение кодов).
	if a.mailboxDone != nil {
		select {
		case <-a.mailboxDone:
		case <-time.After(a.cfg.HTTP.ShutdownTimeout):
			a.log.Warn("поллер почты не завершился в отведённое время")
		}
	}
	// И планировщика напоминаний (ходит в БД).
	if a.trialDone != nil {
		select {
		case <-a.trialDone:
		case <-time.After(a.cfg.HTTP.ShutdownTimeout):
			a.log.Warn("планировщик напоминаний не завершился в отведённое время")
		}
	}
	// И планировщика импорта каталога (ходит в БД).
	if a.catalogDone != nil {
		select {
		case <-a.catalogDone:
		case <-time.After(a.cfg.HTTP.ShutdownTimeout):
			a.log.Warn("планировщик импорта каталога не завершился в отведённое время")
		}
	}
	// И планировщика импорта заказов.
	if a.ordersDone != nil {
		select {
		case <-a.ordersDone:
		case <-time.After(a.cfg.HTTP.ShutdownTimeout):
			a.log.Warn("планировщик импорта заказов не завершился в отведённое время")
		}
	}
	if err := a.db.Close(); err != nil {
		a.log.Error("ошибка закрытия пула БД", slog.Any("error", err))
	}
}

// runCatalogSyncs периодически ставит импорт каталога для всех активных
// подключений Kaspi. Первый прогон — сразу при старте; далее с интервалом из
// конфигурации (KASPI_CATALOG_SYNC_INTERVAL).
func (a *App) runCatalogSyncs(ctx context.Context) {
	t := time.NewTicker(a.cfg.Catalog.SyncInterval)
	defer t.Stop()
	for {
		n, err := a.conn.EnqueueCatalogSyncs(ctx)
		if err != nil && ctx.Err() == nil {
			a.log.Warn("импорт каталога: постановка по расписанию", slog.Any("error", err))
		} else if n > 0 {
			a.log.Info("импорт каталога поставлен по расписанию", slog.Int("connections", n))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// runOrdersSyncs периодически ставит импорт заказов для всех активных
// подключений Kaspi. Первый прогон — сразу при старте; далее по интервалу
// (KASPI_ORDERS_SYNC_INTERVAL).
func (a *App) runOrdersSyncs(ctx context.Context) {
	t := time.NewTicker(a.cfg.Catalog.OrdersSyncInterval)
	defer t.Stop()
	for {
		n, err := a.conn.EnqueueOrdersSyncs(ctx)
		if err != nil && ctx.Err() == nil {
			a.log.Warn("импорт заказов: постановка по расписанию", slog.Any("error", err))
		} else if n > 0 {
			a.log.Info("импорт заказов поставлен по расписанию", slog.Int("connections", n))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// runTrialReminders периодически сканирует магазины на напоминания об оплате.
// Первый прогон — сразу при старте; далее с интервалом из конфигурации.
func (a *App) runTrialReminders(ctx context.Context) {
	interval := a.cfg.Trial.ReminderInterval
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := a.trial.Scan(ctx); err != nil && ctx.Err() == nil {
			a.log.Warn("напоминания об оплате: цикл сканирования", slog.Any("error", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// newRenderer создаёт движок шаблонов поверх embed.FS или диска.
func newRenderer(cfg *config.Config, version string) (*view.Renderer, error) {
	templatesFS, err := webassets.Templates(cfg.App.Debug)
	if err != nil {
		return nil, fmt.Errorf("app: шаблоны: %w", err)
	}

	// img дописывает CDN-префикс к относительным ссылкам изображений Kaspi при
	// показе (в базе они хранятся как есть) — как в рендере витрины.
	imageCDN := strings.TrimRight(cfg.Kaspi.ImagesCDNURL, "/")
	return view.New(templatesFS,
		view.WithDebug(cfg.App.Debug),
		view.WithFuncs(template.FuncMap{
			"img": func(path string) string {
				if path == "" || imageCDN == "" ||
					strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "//") {
					return path
				}
				if strings.HasPrefix(path, "/") {
					return imageCDN + path
				}
				return imageCDN + "/" + path
			},
		}),
		// Переменные, доступные на каждой странице без явной передачи.
		view.WithGlobals(map[string]any{
			"AppName": cfg.App.Name,
			"Env":     cfg.App.Env,
			"Version": version,
		}),
	)
}

// newLogger: в проде — JSON для сборщика логов, локально — человекочитаемый текст.
func newLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.App.LogLevel}

	var handler slog.Handler
	if cfg.IsProduction() {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	log := slog.New(handler).With(slog.String("service", cfg.App.Name))
	slog.SetDefault(log)
	return log
}
