package api

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"nado_go/internal/httpx"
)

// Pinger — зависимость, живость которой проверяется (БД, кеш, брокер).
type Pinger interface {
	Health(ctx context.Context) error
}

// HealthHandler отвечает на пробы оркестратора.
//
// Разделение проб принципиально: liveness говорит «процесс жив, перезапуск не
// нужен», readiness — «можно слать трафик». При остановке сервиса readiness
// начинает отдавать 503 заранее, чтобы балансировщик увёл трафик до закрытия.
type HealthHandler struct {
	deps    map[string]Pinger
	version string
	ready   atomic.Bool
	started time.Time
}

func NewHealthHandler(version string, deps map[string]Pinger) *HealthHandler {
	h := &HealthHandler{deps: deps, version: version, started: time.Now()}
	h.ready.Store(true)
	return h
}

// SetReady переключает готовность принимать трафик.
func (h *HealthHandler) SetReady(ready bool) { h.ready.Store(ready) }

// Live — GET /healthz: процесс запущен и обрабатывает запросы.
func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	httpx.OK(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": h.version,
		"uptime":  time.Since(h.started).Round(time.Second).String(),
	})
}

// Ready — GET /readyz: проверяет зависимости и флаг готовности.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	if !h.ready.Load() {
		httpx.JSON(w, http.StatusServiceUnavailable, httpx.Envelope{
			Data: map[string]any{"status": "shutting_down"},
		})
		return
	}

	checks := make(map[string]string, len(h.deps))
	status := http.StatusOK

	for name, dep := range h.deps {
		if err := dep.Health(r.Context()); err != nil {
			checks[name] = "fail: " + err.Error()
			status = http.StatusServiceUnavailable
			continue
		}
		checks[name] = "ok"
	}

	httpx.JSON(w, status, httpx.Envelope{Data: map[string]any{
		"status": map[bool]string{true: "ok", false: "degraded"}[status == http.StatusOK],
		"checks": checks,
	}})
}
