package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// leaseStore — то, что Runner'у нужно от очереди. Интерфейс объявлен здесь,
// у потребителя: в тестах подставляется заглушка без БД.
type leaseStore interface {
	Lease(ctx context.Context, worker string, exec Execution, kinds []string, leaseFor time.Duration) (*Job, error)
	Complete(ctx context.Context, id int64, result []byte) error
	Fail(ctx context.Context, j *Job, errMsg string, permanent bool, retryAfter time.Duration) error
	ExtendLease(ctx context.Context, id int64, worker string, leaseFor time.Duration) error
}

// Runner — встроенный воркер сервера: берёт из очереди локальные задачи
// (Execution=Local) и выполняет их зарегистрированными обработчиками.
// Удалённые задачи он не трогает — их выдаёт HTTP-API раздачи заданий.
type Runner struct {
	repo    leaseStore
	log     *slog.Logger
	worker  string
	lease   time.Duration
	poll    time.Duration
	timeout time.Duration // предельное время одной задачи

	mu       sync.RWMutex
	handlers map[string]Handler
}

// RunnerOption настраивает Runner.
type RunnerOption func(*Runner)

func WithLease(d time.Duration) RunnerOption   { return func(r *Runner) { r.lease = d } }
func WithPoll(d time.Duration) RunnerOption    { return func(r *Runner) { r.poll = d } }
func WithTimeout(d time.Duration) RunnerOption { return func(r *Runner) { r.timeout = d } }

// NewRunner создаёт встроенный воркер. worker попадает в jobs.locked_by.
func NewRunner(repo *Repository, log *slog.Logger, opts ...RunnerOption) *Runner {
	return newRunner(repo, log, opts...)
}

func newRunner(repo leaseStore, log *slog.Logger, opts ...RunnerOption) *Runner {
	host, _ := os.Hostname()
	r := &Runner{
		repo:     repo,
		log:      log,
		worker:   fmt.Sprintf("server@%s", host),
		lease:    60 * time.Second,
		poll:     2 * time.Second,
		timeout:  5 * time.Minute,
		handlers: make(map[string]Handler),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register привязывает обработчик к типу задачи. Вызывается до Run.
func (r *Runner) Register(kind string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[kind] = h
}

func (r *Runner) kinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for k := range r.handlers {
		out = append(out, k)
	}
	return out
}

// Run обслуживает очередь до отмены ctx. После отмены новые задачи не берутся,
// текущая дорабатывает (её ctx не привязан к ctx воркера, чтобы не оборваться
// на середине).
func (r *Runner) Run(ctx context.Context) error {
	kinds := r.kinds()
	if len(kinds) == 0 {
		r.log.Warn("jobs: не зарегистрировано ни одного обработчика — встроенный воркер простаивает")
	}
	r.log.Info("jobs: встроенный воркер запущен", slog.String("worker", r.worker), slog.Any("kinds", kinds))

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			r.log.Info("jobs: встроенный воркер остановлен")
			return nil
		case <-timer.C:
		}

		job, err := r.repo.Lease(ctx, r.worker, Local, kinds, r.lease)
		if err != nil {
			r.log.Error("jobs: захват задачи", slog.Any("error", err))
			timer.Reset(r.poll)
			continue
		}
		if job == nil {
			timer.Reset(r.poll) // очередь пуста — ждём
			continue
		}

		r.process(ctx, job)
		timer.Reset(0) // сразу пробуем взять следующую
	}
}

// process выполняет одну задачу с продлением аренды и записью итога.
func (r *Runner) process(parent context.Context, job *Job) {
	r.mu.RLock()
	handler, ok := r.handlers[job.Kind]
	r.mu.RUnlock()

	log := r.log.With(slog.Int64("job_id", job.ID), slog.String("kind", job.Kind),
		slog.Int("attempt", job.Attempts))

	if !ok {
		// Обработчик не зарегистрирован — не наша задача, помечаем провал, чтобы
		// не крутилась вечно.
		log.Error("jobs: нет обработчика для типа задачи")
		_ = r.repo.Fail(context.WithoutCancel(parent), job, "нет обработчика для типа "+job.Kind, true, 0)
		return
	}

	// Задача не должна оборваться из-за остановки воркера: свой контекст с
	// таймаутом, отвязанный от parent, но реагирующий на общий дедлайн.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), r.timeout)
	defer cancel()

	// Heartbeat: продлеваем аренду, пока задача выполняется дольше половины срока.
	stop := make(chan struct{})
	go r.heartbeat(ctx, job.ID, stop)

	result, err := runHandler(ctx, handler, job)
	close(stop)

	finishCtx := context.WithoutCancel(parent)
	if err != nil {
		permanent := IsPermanent(err)
		log.Warn("jobs: задача завершилась ошибкой", slog.Bool("permanent", permanent), slog.Any("error", err))
		if ferr := r.repo.Fail(finishCtx, job, err.Error(), permanent, 0); ferr != nil {
			log.Error("jobs: не удалось записать неуспех", slog.Any("error", ferr))
		}
		return
	}
	if err := r.repo.Complete(finishCtx, job.ID, result); err != nil {
		log.Error("jobs: не удалось записать результат", slog.Any("error", err))
		return
	}
	log.Info("jobs: задача выполнена")
}

// runHandler изолирует панику обработчика: одна задача не должна ронять воркер.
func runHandler(ctx context.Context, h Handler, job *Job) (result json.RawMessage, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("паника в обработчике: %v", rec)
		}
	}()
	return h(ctx, *job)
}

func (r *Runner) heartbeat(ctx context.Context, id int64, stop <-chan struct{}) {
	tick := time.NewTicker(r.lease / 2)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-tick.C:
			if err := r.repo.ExtendLease(ctx, id, r.worker, r.lease); err != nil {
				r.log.Warn("jobs: продление аренды", slog.Int64("job_id", id), slog.Any("error", err))
			}
		}
	}
}
