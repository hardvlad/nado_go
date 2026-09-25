// Команда nado-admin — административные операции над платформой nado.
//
// Пока единственная группа команд — управление пулом инстансов WhatsApp
// (GreenAPI), из которых отправляются коды подтверждения при регистрации:
//
//	nado-admin greenapi create [-name "Основной"]   создать инстанс через партнёрский API
//	nado-admin greenapi qr -id <N>                   показать QR для авторизации (сохранит PNG)
//	nado-admin greenapi state -id <N>                проверить и обновить состояние
//	nado-admin greenapi list                         список инстансов пула
//
// Читает ту же конфигурацию, что и сервер (.env / переменные окружения):
// доступ к БД, SECRETS_KEY, GREEN_API_PARTNER_*, GREEN_API_WEBHOOK_TOKEN,
// APP_PUBLIC_URL.
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"

	"nado_go/internal/config"
	"nado_go/internal/database"
	"nado_go/internal/integration/messaging/greenapi"
	"nado_go/internal/repository"
	"nado_go/internal/secrets"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "nado-admin: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 || args[0] != "greenapi" {
		return fmt.Errorf("использование: nado-admin greenapi <create|qr|state|list> [флаги]")
	}
	action, rest := args[1], args[2:]

	env, err := newEnv()
	if err != nil {
		return err
	}
	defer env.close()

	switch action {
	case "create":
		return env.create(rest)
	case "qr":
		return env.qr(rest)
	case "state":
		return env.state(rest)
	case "list":
		return env.list(rest)
	default:
		return fmt.Errorf("неизвестная команда %q; доступны: create, qr, state, list", action)
	}
}

// env — общие зависимости команд.
type env struct {
	cfg  *config.Config
	db   *database.DB
	repo *repository.WhatsAppRepository
	mgmt *greenapi.Management
	ctx  context.Context
}

func newEnv() (*env, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx := context.Background()

	db, err := database.New(ctx, cfg.Database, log)
	if err != nil {
		return nil, err
	}
	box, err := secrets.New(cfg.Secrets.Key)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &env{
		cfg:  cfg,
		db:   db,
		repo: repository.NewWhatsAppRepository(db, box),
		mgmt: greenapi.NewManagement(),
		ctx:  ctx,
	}, nil
}

func (e *env) close() { _ = e.db.Close() }

func (e *env) create(args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	name := fs.String("name", "", "название инстанса для списка")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if e.cfg.Messaging.GreenAPIPartnerDomain == "" || e.cfg.Messaging.GreenAPIPartnerToken == "" {
		return fmt.Errorf("не заданы GREEN_API_PARTNER_DOMAIN и GREEN_API_PARTNER_TOKEN")
	}
	if e.cfg.Messaging.GreenAPIWebhookToken == "" {
		return fmt.Errorf("не задан GREEN_API_WEBHOOK_TOKEN (нужен для вебхука состояния)")
	}
	if e.cfg.App.PublicURL == "" {
		return fmt.Errorf("не задан APP_PUBLIC_URL (нужен для адреса вебхука)")
	}

	created, err := e.mgmt.CreateInstance(e.ctx, greenapi.CreateInstanceRequest{
		PartnerDomain: e.cfg.Messaging.GreenAPIPartnerDomain,
		PartnerToken:  e.cfg.Messaging.GreenAPIPartnerToken,
		WebhookURL:    e.cfg.App.PublicURL + "/webhooks/greenapi/" + e.cfg.Messaging.GreenAPIWebhookToken,
		WebhookToken:  e.cfg.Messaging.GreenAPIWebhookToken,
	})
	if err != nil {
		return err
	}

	id, err := e.repo.AddInstance(e.ctx, "greenapi", *name, created.APIDomain, created.InstanceID, created.Token)
	if err != nil {
		return err
	}

	fmt.Printf("Инстанс создан.\n  id в пуле:   %d\n  idInstance:  %s\n\n", id, created.InstanceID)
	fmt.Printf("Дальше авторизуйте номер: nado-admin greenapi qr -id %d\n", id)
	fmt.Println("Откройте сохранённый QR в WhatsApp → Связанные устройства → Привязать устройство.")
	return nil
}

func (e *env) qr(args []string) error {
	id, err := requireID(args)
	if err != nil {
		return err
	}
	inst, err := e.repo.GetInstance(e.ctx, id)
	if err != nil {
		return err
	}

	state, err := e.mgmt.State(e.ctx, inst.APIDomain, inst.InstanceID, inst.Token)
	if err != nil {
		return err
	}
	if state == "authorized" {
		_ = e.repo.SetStateByID(e.ctx, id, state, "")
		fmt.Println("Инстанс уже авторизован — QR не нужен.")
		return nil
	}

	pngB64, err := e.mgmt.QR(e.ctx, inst.APIDomain, inst.InstanceID, inst.Token)
	if err != nil {
		return err
	}
	if pngB64 == "" {
		fmt.Printf("QR недоступен (состояние: %s). Попробуйте позже.\n", state)
		return nil
	}
	png, err := base64.StdEncoding.DecodeString(pngB64)
	if err != nil {
		return fmt.Errorf("не удалось декодировать QR: %w", err)
	}
	file := fmt.Sprintf("greenapi-qr-%s.png", inst.InstanceID)
	if err := os.WriteFile(file, png, 0o600); err != nil {
		return fmt.Errorf("сохранение QR: %w", err)
	}
	fmt.Printf("QR сохранён в %s\n", file)
	fmt.Println("Откройте его и отсканируйте в WhatsApp → Связанные устройства.")
	fmt.Printf("После привязки проверьте: nado-admin greenapi state -id %d\n", id)
	return nil
}

func (e *env) state(args []string) error {
	id, err := requireID(args)
	if err != nil {
		return err
	}
	inst, err := e.repo.GetInstance(e.ctx, id)
	if err != nil {
		return err
	}
	state, err := e.mgmt.State(e.ctx, inst.APIDomain, inst.InstanceID, inst.Token)
	if err != nil {
		return err
	}
	if err := e.repo.SetStateByID(e.ctx, id, state, ""); err != nil {
		return err
	}
	fmt.Printf("Состояние инстанса %d: %s\n", id, state)
	if state == "authorized" {
		fmt.Println("Инстанс готов отправлять коды.")
	}
	return nil
}

func (e *env) list(_ []string) error {
	items, err := e.repo.ListInstances(e.ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("Пул пуст. Создайте инстанс: nado-admin greenapi create")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tИМЯ\tidInstance\tТЕЛЕФОН\tСОСТОЯНИЕ\tОТКЛЮЧЁН")
	for _, i := range items {
		off := ""
		if i.Disabled {
			off = "да"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", i.ID, i.Name, i.InstanceID, i.Phone, i.State, off)
	}
	return tw.Flush()
}

func requireID(args []string) (int64, error) {
	fs := flag.NewFlagSet("id", flag.ContinueOnError)
	id := fs.Int64("id", 0, "id инстанса в пуле")
	if err := fs.Parse(args); err != nil {
		return 0, err
	}
	if *id <= 0 {
		return 0, fmt.Errorf("укажите -id инстанса (см. nado-admin greenapi list)")
	}
	return *id, nil
}
