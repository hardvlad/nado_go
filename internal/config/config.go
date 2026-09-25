// Package config загружает конфигурацию приложения из переменных окружения.
// Значения читаются один раз при старте; отсутствующие берутся из defaults.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App      App
	HTTP     HTTP
	Database Database
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
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) IsProduction() bool { return strings.EqualFold(c.App.Env, "prod") }

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
