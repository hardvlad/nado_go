// Package jobsclient — клиент удалённого воркера: забирает задания с сервера
// по HTTP (/jobs-api/v1), выполняет зарегистрированными обработчиками и
// отправляет результат. Используется бинарником cmd/nado-jobs.
package jobsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"nado_go/internal/jobs"
)

// Handler выполняет одну задачу на стороне воркера. Возвращаемые данные уходят
// в jobs.result. Ошибку можно обернуть в jobs.Permanent, чтобы не повторять.
type Handler func(ctx context.Context, job jobs.LeasedJob) (json.RawMessage, error)

// Config — настройки воркера.
type Config struct {
	ServerURL   string        // https://nado.kz
	Token       string        // постоянный токен воркера
	Kinds       []string      // типы задач, которые берёт этот воркер
	Poll        time.Duration // пауза, когда задач нет (по умолчанию 2с)
	Concurrency int           // сколько задач одновременно (по умолчанию 1)
	HTTPTimeout time.Duration // таймаут одного HTTP-запроса к серверу
	JobTimeout  time.Duration // предельное время одной задачи
}

// Client забирает и выполняет задачи.
type Client struct {
	cfg      Config
	http     *http.Client
	log      *slog.Logger
	base     string
	handlers map[string]Handler
}

func New(cfg Config, log *slog.Logger) (*Client, error) {
	if cfg.ServerURL == "" || cfg.Token == "" {
		return nil, errors.New("jobsclient: нужны ServerURL и Token")
	}
	if len(cfg.Kinds) == 0 {
		return nil, errors.New("jobsclient: не заданы типы задач (-kind)")
	}
	if cfg.Poll <= 0 {
		cfg.Poll = 2 * time.Second
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 30 * time.Second
	}
	if cfg.JobTimeout <= 0 {
		cfg.JobTimeout = 10 * time.Minute
	}
	return &Client{
		cfg:      cfg,
		http:     &http.Client{Timeout: cfg.HTTPTimeout},
		log:      log,
		base:     strings.TrimSuffix(cfg.ServerURL, "/") + "/jobs-api/v1",
		handlers: make(map[string]Handler),
	}, nil
}

// Register привязывает обработчик к типу задачи (до Run).
func (c *Client) Register(kind string, h Handler) { c.handlers[kind] = h }

// Kinds — типы, которые воркер и запрашивает, и умеет обрабатывать.
func (c *Client) Kinds() []string { return c.cfg.Kinds }

// Run запускает Concurrency потоков до отмены ctx.
func (c *Client) Run(ctx context.Context) error {
	c.log.Info("воркер запущен", slog.String("server", c.cfg.ServerURL),
		slog.Any("kinds", c.cfg.Kinds), slog.Int("concurrency", c.cfg.Concurrency))

	var wg sync.WaitGroup
	for i := 0; i < c.cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.loop(ctx)
		}()
	}
	wg.Wait()
	c.log.Info("воркер остановлен")
	return nil
}

func (c *Client) loop(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		job, err := c.lease(ctx)
		switch {
		case errors.Is(err, context.Canceled):
			return
		case err != nil:
			c.log.Error("не удалось получить задачу", slog.Any("error", err))
			timer.Reset(c.cfg.Poll)
		case job == nil:
			timer.Reset(c.cfg.Poll) // задач нет
		default:
			c.process(ctx, job)
			timer.Reset(0) // сразу за следующей
		}
	}
}

func (c *Client) process(parent context.Context, job *jobs.LeasedJob) {
	log := c.log.With(slog.Int64("job_id", job.ID), slog.String("kind", job.Kind))
	handler, ok := c.handlers[job.Kind]
	if !ok {
		log.Error("нет обработчика для типа задачи — возвращаю серверу как постоянную ошибку")
		c.report(parent, job, nil, jobs.Permanent(fmt.Errorf("воркер не умеет %s", job.Kind)))
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), c.cfg.JobTimeout)
	defer cancel()

	stop := make(chan struct{})
	go c.heartbeat(ctx, job.ID, stop)

	data, err := safeRun(ctx, handler, *job)
	close(stop)

	c.report(context.WithoutCancel(parent), job, data, err)
	if err != nil {
		log.Warn("задача завершилась ошибкой", slog.Bool("permanent", jobs.IsPermanent(err)), slog.Any("error", err))
	} else {
		log.Info("задача выполнена")
	}
}

func safeRun(ctx context.Context, h Handler, job jobs.LeasedJob) (data json.RawMessage, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("паника в обработчике: %v", rec)
		}
	}()
	return h(ctx, job)
}

// report отправляет серверу итог: complete при успехе, иначе fail.
func (c *Client) report(ctx context.Context, job *jobs.LeasedJob, data json.RawMessage, runErr error) {
	if runErr == nil {
		if err := c.postJSON(ctx, fmt.Sprintf("/jobs/%d/complete", job.ID), jobs.CompleteRequest{Data: data}, nil); err != nil {
			c.log.Error("не удалось отправить результат", slog.Int64("job_id", job.ID), slog.Any("error", err))
		}
		return
	}
	req := jobs.FailRequest{Error: runErr.Error(), Permanent: jobs.IsPermanent(runErr)}
	var rl rateLimited
	if errors.As(runErr, &rl) {
		req.RetryAfterSeconds = int(rl.after.Seconds())
	}
	if err := c.postJSON(ctx, fmt.Sprintf("/jobs/%d/fail", job.ID), req, nil); err != nil {
		c.log.Error("не удалось отправить ошибку", slog.Int64("job_id", job.ID), slog.Any("error", err))
	}
}

func (c *Client) heartbeat(ctx context.Context, id int64, stop <-chan struct{}) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-tick.C:
			if err := c.postJSON(ctx, fmt.Sprintf("/jobs/%d/heartbeat", id), nil, nil); err != nil {
				c.log.Warn("heartbeat не прошёл", slog.Int64("job_id", id), slog.Any("error", err))
			}
		}
	}
}

func (c *Client) lease(ctx context.Context) (*jobs.LeasedJob, error) {
	var job jobs.LeasedJob
	status, err := c.do(ctx, http.MethodPost, "/lease", jobs.LeaseRequest{Kinds: c.cfg.Kinds}, &job)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, nil // задач нет
	}
	return &job, nil
}

func (c *Client) postJSON(ctx context.Context, path string, body, out any) error {
	_, err := c.do(ctx, http.MethodPost, path, body, out)
	return err
}

// Call выполняет произвольный запрос к API сервера (тот же base и токен).
// Используют обработчики воркера: приём каталога, получение кода MFA.
func (c *Client) Call(ctx context.Context, method, path string, body, out any) (int, error) {
	return c.do(ctx, method, path, body, out)
}

// do выполняет запрос к API воркеров. out и body могут быть nil.
func (c *Client) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("jobsclient: сериализация тела: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("jobsclient: %s %s → %d: %s", method, path, resp.StatusCode, snippet(payload))
	}
	if out != nil {
		// Ответы API воркеров обёрнуты в {"data": ...}.
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(payload, &env); err != nil {
			return resp.StatusCode, fmt.Errorf("jobsclient: разбор ответа: %w", err)
		}
		if len(env.Data) > 0 {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return resp.StatusCode, fmt.Errorf("jobsclient: разбор data: %w", err)
			}
		}
	}
	return resp.StatusCode, nil
}

func snippet(b []byte) string {
	const max = 300
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// rateLimited — ошибка обработчика с просьбой перенести задачу.
type rateLimited struct {
	err   error
	after time.Duration
}

func (e rateLimited) Error() string { return e.err.Error() }
func (e rateLimited) Unwrap() error { return e.err }

// RetryAfter оборачивает ошибку так, что сервер перенесёт задачу без штрафа.
func RetryAfter(err error, after time.Duration) error { return rateLimited{err: err, after: after} }
