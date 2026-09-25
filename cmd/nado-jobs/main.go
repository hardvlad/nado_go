// Команда nado-jobs — удалённый воркер фоновых задач.
//
// Забирает задания с сервера nado по HTTP и выполняет их. Каждый экземпляр
// запускается с указанием типов задач, которые он обслуживает, поэтому под
// каждую джобу можно поднять отдельный процесс (или несколько на разных
// серверах):
//
//	nado-jobs -server https://nado.kz -token $NADO_JOBS_TOKEN -kind kaspi.send_otp
//	nado-jobs -kind kaspi.sync_catalog,kaspi.import_orders -concurrency 2
//
// Токен воркера заводится в таблице job_runners (постоянный, в БД — только хеш).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"nado_go/internal/jobsclient"
	"nado_go/internal/worker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "nado-jobs: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		server      = flag.String("server", env("NADO_JOBS_SERVER", ""), "адрес сервера nado, напр. https://nado.kz")
		token       = flag.String("token", env("NADO_JOBS_TOKEN", ""), "постоянный токен воркера (или env NADO_JOBS_TOKEN)")
		kindsCSV    = flag.String("kind", env("NADO_JOBS_KINDS", ""), "типы задач через запятую")
		concurrency = flag.Int("concurrency", envInt("NADO_JOBS_CONCURRENCY", 1), "сколько задач обрабатывать одновременно")
		pollSec     = flag.Int("poll", envInt("NADO_JOBS_POLL", 2), "пауза опроса, сек, когда задач нет")
		debug       = flag.Bool("debug", false, "подробный лог")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	kinds := splitCSV(*kindsCSV)
	if len(kinds) == 0 {
		return fmt.Errorf("укажите типы задач: -kind kaspi.send_otp[,...]. Доступны: %s",
			strings.Join(worker.AvailableKinds(), ", "))
	}

	client, err := jobsclient.New(jobsclient.Config{
		ServerURL:   *server,
		Token:       *token,
		Kinds:       kinds,
		Poll:        time.Duration(*pollSec) * time.Second,
		Concurrency: *concurrency,
	}, log)
	if err != nil {
		return err
	}

	// Регистрируем обработчики только для запрошенных типов. Неизвестный тип —
	// ошибка запуска, а не молчаливый простой.
	if err := worker.Register(client, kinds); err != nil {
		return err
	}

	// SIGINT/SIGTERM: перестаём брать новые задачи, текущие дорабатывают.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return client.Run(ctx)
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
