// Package config загружает конфигурацию приложения из переменных окружения.
// Значения читаются один раз при старте; отсутствующие берутся из defaults.
package config

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App       App
	HTTP      HTTP
	Database  Database
	Secrets   Secrets
	Messaging Messaging
	Mailbox   Mailbox
	Kaspi     Kaspi
	Platform  Platform
}

// Platform — хосты платформы для маршрутизации по Host (D-04, D-18).
type Platform struct {
	RootHost   string // лендинг: nado.kz
	AppHost    string // кабинет продавца: app.nado.kz
	ShopSuffix string // поддомены витрин: <slug>.<ShopSuffix>. Пусто — диспетчеризация по Host выключена (dev: витрина под /shop/{slug})
}

// HostDispatch — включена ли маршрутизация витрин по Host (задан ShopSuffix).
func (p Platform) HostDispatch() bool { return p.ShopSuffix != "" }

// Kaspi — параметры кабинетного онбординга Kaspi (D-28).
type Kaspi struct {
	// EmployeeEmailDomain — домен адресов служебных сотрудников (catch-all ящик,
	// см. Mailbox). Адреса вида s{onboardingID}-{rand}@<домен>. Пусто — кабинетный
	// онбординг недоступен.
	EmployeeEmailDomain string
}

// Mailbox — общий почтовый ящик служебных сотрудников Kaspi (IMAP). Из писем
// читаются коды подтверждения входа в кабинет и пароли новых сотрудников.
// Ящик — инфраструктура платформы (catch-all), к продавцам не привязан.
type Mailbox struct {
	Addr       string        // host:port IMAPS, напр. mail.nado.kz:993
	User       string        // логин ящика
	Password   string        // пароль ящика
	Folder     string        // папка, обычно INBOX
	ServerName string        // имя для проверки TLS, если отличается от хоста
	Poll       time.Duration // период опроса
	// CodeTTL — сколько код из письма считается пригодным при выдаче воркеру и
	// как долго он хранится до очистки.
	CodeTTL time.Duration
}

// Enabled — ящик сконфигурирован (заданы адрес и логин). Пусто — поллер не
// запускается, коды MFA недоступны (в dev это нормально).
func (m Mailbox) Enabled() bool { return m.Addr != "" && m.User != "" }

// Secrets — ключ шифрования секретов провайдеров (токены инстансов WhatsApp,
// учётные данные маркетплейсов) и HMAC одноразовых кодов.
type Secrets struct {
	// Key — 32 байта для AES-256-GCM (SECRETS_KEY, base64). В prod обязателен.
	Key []byte
}

// Messaging — отправка сообщений (OTP при регистрации).
type Messaging struct {
	OTPTTL time.Duration // срок жизни кода
	// Партнёрский доступ GreenAPI — для создания инстансов пула.
	GreenAPIPartnerDomain string
	GreenAPIPartnerToken  string
	// GreenAPIWebhookToken — секрет в URL вебхука, по нему сервер проверяет,
	// что уведомление о статусе инстанса пришло от GreenAPI.
	GreenAPIWebhookToken string
}

type App struct {
	Name     string // имя сервиса в логах
	Env      string // local | dev | prod
	LogLevel slog.Level
	Debug    bool // в debug-режиме шаблоны перечитываются с диска на каждый запрос
	// PublicURL — адрес сайта без слеша в конце (https://nado.kz) для
	// canonical и hreflang. Пусто — ссылки относительные (локальная разработка).
	PublicURL string
}

type HTTP struct {
	Addr              string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	// ShutdownTimeout — сколько ждём завершения активных запросов при остановке.
	ShutdownTimeout time.Duration
	// RequestTimeout — предельное время обработки одного запроса (middleware).
	RequestTimeout time.Duration
	AllowedOrigins []string
}

type Database struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	Instance string // именованный инстанс SQL Server, напр. SQLEXPRESS

	Encrypt                bool
	TrustServerCertificate bool

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	// ConnectTimeout — таймаут установления соединения и начального Ping.
	ConnectTimeout time.Duration
	// QueryTimeout — таймаут по умолчанию для запросов в репозиториях.
	QueryTimeout time.Duration
}

// Load читает .env (если файл есть) и собирает конфигурацию из окружения.
// Реальные переменные окружения имеют приоритет над .env.
func Load() (*Config, error) {
	_ = godotenv.Load() // отсутствие .env — не ошибка (прод обычно без него)

	cfg := &Config{
		App: App{
			Name:      env("APP_NAME", "nado"),
			Env:       env("APP_ENV", "local"),
			LogLevel:  logLevel(env("LOG_LEVEL", "info")),
			Debug:     envBool("APP_DEBUG", true),
			PublicURL: strings.TrimSuffix(env("APP_PUBLIC_URL", ""), "/"),
		},
		HTTP: HTTP{
			Addr:              env("HTTP_ADDR", ":8080"),
			ReadTimeout:       envDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			ReadHeaderTimeout: envDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			WriteTimeout:      envDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       envDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout:   envDuration("HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
			RequestTimeout:    envDuration("HTTP_REQUEST_TIMEOUT", 25*time.Second),
			AllowedOrigins:    envList("HTTP_ALLOWED_ORIGINS", []string{"*"}),
		},
		Database: Database{
			Host:                   env("DB_HOST", "localhost"),
			Port:                   envInt("DB_PORT", 1433),
			User:                   env("DB_USER", "sa"),
			Password:               env("DB_PASSWORD", ""),
			Name:                   env("DB_NAME", "nado"),
			Instance:               env("DB_INSTANCE", ""),
			Encrypt:                envBool("DB_ENCRYPT", true),
			TrustServerCertificate: envBool("DB_TRUST_SERVER_CERTIFICATE", false),
			MaxOpenConns:           envInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:           envInt("DB_MAX_IDLE_CONNS", 25),
			ConnMaxLifetime:        envDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
			ConnMaxIdleTime:        envDuration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute),
			ConnectTimeout:         envDuration("DB_CONNECT_TIMEOUT", 10*time.Second),
			QueryTimeout:           envDuration("DB_QUERY_TIMEOUT", 5*time.Second),
		},
		Messaging: Messaging{
			OTPTTL:                envDuration("OTP_TTL", 5*time.Minute),
			GreenAPIPartnerDomain: env("GREEN_API_PARTNER_DOMAIN", ""),
			GreenAPIPartnerToken:  env("GREEN_API_PARTNER_TOKEN", ""),
			GreenAPIWebhookToken:  env("GREEN_API_WEBHOOK_TOKEN", ""),
		},
		Mailbox: Mailbox{
			Addr:       env("MAILBOX_IMAP_ADDR", ""),
			User:       env("MAILBOX_IMAP_USER", ""),
			Password:   env("MAILBOX_IMAP_PASSWORD", ""),
			Folder:     env("MAILBOX_IMAP_FOLDER", "INBOX"),
			ServerName: env("MAILBOX_IMAP_SERVER_NAME", ""),
			Poll:       envDuration("MAILBOX_IMAP_POLL", 15*time.Second),
			CodeTTL:    envDuration("MAILBOX_CODE_TTL", 10*time.Minute),
		},
		Kaspi: Kaspi{
			EmployeeEmailDomain: env("KASPI_EMPLOYEE_EMAIL_DOMAIN", "kaspi.nado.kz"),
		},
		Platform: Platform{
			RootHost:   strings.ToLower(env("PLATFORM_ROOT_HOST", "")),
			AppHost:    strings.ToLower(env("PLATFORM_APP_HOST", "")),
			ShopSuffix: strings.ToLower(strings.TrimPrefix(env("PLATFORM_SHOP_SUFFIX", ""), ".")),
		},
	}

	key, err := secretsKey(env("SECRETS_KEY", ""), cfg.IsProduction())
	if err != nil {
		return nil, err
	}
	cfg.Secrets.Key = key

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) IsProduction() bool { return strings.EqualFold(c.App.Env, "prod") }

// devSecretsKey — ключ для локальной разработки, когда SECRETS_KEY не задан.
// Только вне prod: зашифрованное им в dev-базе не имеет ценности.
var devSecretsKey = []byte("nado-dev-secrets-key-32-bytes!!!")

// secretsKey разбирает SECRETS_KEY (base64, 32 байта). В prod ключ обязателен;
// локально при его отсутствии используется фиксированный dev-ключ.
func secretsKey(raw string, prod bool) ([]byte, error) {
	if raw == "" {
		if prod {
			return nil, fmt.Errorf("config: SECRETS_KEY обязателен в prod (base64, 32 байта)")
		}
		return devSecretsKey, nil
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("config: SECRETS_KEY — некорректный base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("config: SECRETS_KEY должен быть 32 байта, получено %d", len(key))
	}
	return key, nil
}

func (c *Config) validate() error {
	if c.Database.Name == "" {
		return fmt.Errorf("config: DB_NAME обязателен")
	}
	if c.Database.User == "" {
		return fmt.Errorf("config: DB_USER обязателен")
	}
	if c.IsProduction() {
		if c.Database.Password == "" {
			return fmt.Errorf("config: DB_PASSWORD обязателен в prod")
		}
		if c.Database.TrustServerCertificate {
			return fmt.Errorf("config: DB_TRUST_SERVER_CERTIFICATE=true недопустим в prod")
		}
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		return fmt.Errorf("config: DB_MAX_IDLE_CONNS (%d) больше DB_MAX_OPEN_CONNS (%d)",
			c.Database.MaxIdleConns, c.Database.MaxOpenConns)
	}
	// Ящик служебных сотрудников либо выключен целиком, либо задан полностью:
	// без пароля поллер молча не смог бы войти и коды MFA не приходили бы.
	if c.Mailbox.Addr != "" || c.Mailbox.User != "" || c.Mailbox.Password != "" {
		if c.Mailbox.Addr == "" || c.Mailbox.User == "" || c.Mailbox.Password == "" {
			return fmt.Errorf("config: для почтового ящика нужны MAILBOX_IMAP_ADDR, MAILBOX_IMAP_USER и MAILBOX_IMAP_PASSWORD")
		}
	}
	return nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}

func envList(key string, def []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

func logLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
