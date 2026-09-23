// Команда nado — точка входа веб-приложения.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"nado_go/internal/app"
)

// version подставляется при сборке:
//
//	go build -ldflags "-X main.version=$(git describe --tags --always)" ./cmd/nado
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "фатальная ошибка: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// NotifyContext отменяет ctx по SIGINT/SIGTERM и — что важно — снимает
	// перехват после первого сигнала: повторный Ctrl+C убьёт процесс
	// немедленно, если штатное завершение зависнет.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, version)
	if err != nil {
		return err
	}

	return application.Run(ctx)
}
