// Package app собирает приложение из компонентов и управляет его жизненным
// циклом: запуск, обслуживание запросов, безопасное завершение.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"nado_go/internal/config"
	"nado_go/internal/database"
	"nado_go/internal/handler/api"
	"nado_go/internal/handler/web"
	"nado_go/internal/i18n"
	"nado_go/internal/repository"
	"nado_go/internal/router"
	"nado_go/internal/service"
	"nado_go/internal/view"
	webassets "nado_go/web"
)

// drainDelay — пауза между переводом /readyz в 503 и началом остановки.
// За это время балансировщик успевает увести трафик на другие инстансы,
// иначе часть запросов попадёт на уже закрывающийся сервер.
const drainDelay = 2 * time.Second

// App — собранное приложение со всеми зависимостями.
type App struct {
	cfg    *config.Config
	log    *slog.Logger
	db     *database.DB
	server *http.Server
	health *api.HealthHandler
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

	health := api.NewHealthHandler(version, map[string]api.Pinger{"database": db})

	handler := router.New(router.Deps{
		Config: cfg,
		Logger: log,
		Static: staticFS,
		I18n:   bundle,
		Pages: web.NewPageHandler(web.Deps{
			Render:        renderer,
			I18n:          bundle,
			Plans:         plans,
			Auth:          authService,
			Feedback:      feedbackService,
			SecureCookies: cfg.IsProduction(),
			PublicURL:     cfg.App.PublicURL,
		}),
		Users:  api.NewUserHandler(userService),
		Health: health,
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

	return &App{cfg: cfg, log: log, db: db, server: server, health: health}, nil
}

// Run обслуживает запросы до отмены ctx, затем выполняет безопасное завершение.
//
// Порядок остановки: readiness → пауза дренирования → Shutdown (дожидается
// активных запросов) → закрытие БД. Обратный порядок оборвал бы запросы,
// которые ещё ходят в базу.
func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

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

// closeResources закрывает внешние ресурсы. Вызывается один раз, после того
// как HTTP-сервер перестал обрабатывать запросы.
func (a *App) closeResources() {
	if err := a.db.Close(); err != nil {
		a.log.Error("ошибка закрытия пула БД", slog.Any("error", err))
	}
}

// newRenderer создаёт движок шаблонов поверх embed.FS или диска.
func newRenderer(cfg *config.Config, version string) (*view.Renderer, error) {
	templatesFS, err := webassets.Templates(cfg.App.Debug)
	if err != nil {
		return nil, fmt.Errorf("app: шаблоны: %w", err)
	}

	return view.New(templatesFS,
		view.WithDebug(cfg.App.Debug),
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
