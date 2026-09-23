// Package database отвечает за подключение к Microsoft SQL Server,
// настройку пула соединений и корректное освобождение ресурсов.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"time"

	mssql "github.com/microsoft/go-mssqldb"

	"nado_go/internal/config"
)

// DB — тонкая обёртка над *sql.DB: хранит таймаут запросов по умолчанию,
// чтобы репозитории не тянули конфиг целиком.
type DB struct {
	*sql.DB
	queryTimeout time.Duration
	log          *slog.Logger
}

// New открывает пул соединений и проверяет доступность сервера.
// Пул ленивый: реальные соединения создаются по мере необходимости,
// поэтому Ping здесь обязателен — иначе ошибки конфигурации всплывут
// только на первом пользовательском запросе.
func New(ctx context.Context, cfg config.Database, log *slog.Logger) (*DB, error) {
	connector, err := mssql.NewConnector(dsn(cfg))
	if err != nil {
		return nil, fmt.Errorf("database: некорректная строка подключения: %w", err)
	}

	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		// Закрываем пул, иначе фоновые горутины database/sql останутся жить.
		_ = db.Close()
		return nil, fmt.Errorf("database: нет связи с SQL Server %s: %w", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), err)
	}

	log.Info("подключение к SQL Server установлено",
		slog.String("host", cfg.Host),
		slog.Int("port", cfg.Port),
		slog.String("database", cfg.Name),
		slog.Int("max_open_conns", cfg.MaxOpenConns),
	)

	return &DB{DB: db, queryTimeout: cfg.QueryTimeout, log: log}, nil
}

// QueryTimeout возвращает таймаут по умолчанию для одного запроса.
func (d *DB) QueryTimeout() time.Duration { return d.queryTimeout }

// Context оборачивает переданный контекст таймаутом запроса по умолчанию.
// Если у вызывающего уже есть более жёсткий дедлайн, он сохраняется.
func (d *DB) Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d.queryTimeout)
}

// Health — быстрая проверка живости пула для /healthz.
func (d *DB) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, d.queryTimeout)
	defer cancel()
	return d.PingContext(ctx)
}

// Close закрывает пул. Вызывается при graceful shutdown строго после того,
// как HTTP-сервер перестал принимать запросы.
func (d *DB) Close() error {
	d.log.Info("закрытие пула соединений с БД")
	return d.DB.Close()
}

// dsn собирает строку подключения sqlserver://. url.URL сам экранирует
// пароль и имя пользователя — ручная конкатенация ломается на спецсимволах.
func dsn(cfg config.Database) string {
	query := url.Values{}
	query.Set("database", cfg.Name)
	query.Set("app name", "nado")
	query.Set("encrypt", strconv.FormatBool(cfg.Encrypt))
	query.Set("TrustServerCertificate", strconv.FormatBool(cfg.TrustServerCertificate))
	query.Set("connection timeout", strconv.Itoa(int(cfg.ConnectTimeout.Seconds())))

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		RawQuery: query.Encode(),
	}
	if cfg.Instance != "" {
		// Именованный инстанс указывается путём: sqlserver://host:port/SQLEXPRESS
		u.Path = cfg.Instance
	}
	return u.String()
}
