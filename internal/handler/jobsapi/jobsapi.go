// Package jobsapi — HTTP-API раздачи заданий удалённым воркерам.
//
// Базовый путь /jobs-api/v1. Это отдельный контур от /api/v1: без CORS и CSRF,
// аутентификация только по постоянному токену воркера (Authorization: Bearer).
// Через это API удалённые воркеры берут задачи с Execution=Remote, продлевают
// аренду и отправляют результат.
package jobsapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"nado_go/internal/database"
	"nado_go/internal/httpx"
	"nado_go/internal/integration/marketplace/kaspi"
	"nado_go/internal/jobs"
	"nado_go/internal/model"
)

const (
	defaultLease = 90 * time.Second
	maxLease     = 10 * time.Minute
)

// jobStore и runnerAuth — то, что нужно API от очереди и учёта воркеров.
// Интерфейсы объявлены у потребителя: в тестах подставляются заглушки без БД.
type jobStore interface {
	Lease(ctx context.Context, worker string, exec jobs.Execution, kinds []string, leaseFor time.Duration) (*jobs.Job, error)
	LoadOwned(ctx context.Context, id int64, worker string) (*jobs.Job, error)
	Complete(ctx context.Context, id int64, result []byte) error
	Fail(ctx context.Context, j *jobs.Job, errMsg string, permanent bool, retryAfter time.Duration) error
	ExtendLease(ctx context.Context, id int64, worker string, leaseFor time.Duration) error
}

type runnerAuth interface {
	Authenticate(ctx context.Context, token, ip string) (*jobs.RunnerInfo, error)
}

// catalogIngestor принимает импортированный воркером каталог Kaspi.
type catalogIngestor interface {
	Ingest(ctx context.Context, connectionID int64, req kaspi.IngestRequest) (kaspi.IngestResponse, error)
}

// mfaCodeSource отдаёт код подтверждения входа для служебного сотрудника.
type mfaCodeSource interface {
	CodeForEmail(ctx context.Context, email string) (code string, found bool, err error)
}

// onboardingSink принимает результаты шагов кабинетного онбординга Kaspi от
// воркера: сессию входа владельца, список кабинетов и созданного сотрудника.
type onboardingSink interface {
	SaveSession(ctx context.Context, id int64, session json.RawMessage) error
	SaveMerchants(ctx context.Context, id int64, session json.RawMessage, merchants []model.KaspiMerchant) error
	CompleteEmployee(ctx context.Context, id int64, merchantID, apiToken string) error
	Fail(ctx context.Context, id int64, msg string) error
}

type Handler struct {
	jobs       jobStore
	runners    runnerAuth
	catalog    catalogIngestor
	mfa        mfaCodeSource
	onboarding onboardingSink
	log        *slog.Logger
}

func New(jobsRepo *jobs.Repository, runners *jobs.RunnerRepository, catalog catalogIngestor, mfa mfaCodeSource, onboarding onboardingSink, log *slog.Logger) *Handler {
	return &Handler{jobs: jobsRepo, runners: runners, catalog: catalog, mfa: mfa, onboarding: onboarding, log: log}
}

// Routes — маршруты API воркеров. Аутентификация — первым middleware.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(h.authenticate)
	r.Post("/lease", httpx.Wrap(h.lease))
	r.Post("/jobs/{id}/heartbeat", httpx.Wrap(h.heartbeat))
	r.Post("/jobs/{id}/complete", httpx.Wrap(h.complete))
	r.Post("/jobs/{id}/fail", httpx.Wrap(h.fail))

	// Приём каталога Kaspi от воркера и выдача кода подтверждения входа.
	if h.catalog != nil {
		r.Post("/kaspi/catalog/{connectionID}/products", httpx.Wrap(h.ingestCatalog))
	}
	if h.mfa != nil {
		r.Get("/kaspi/otp-code", httpx.Wrap(h.mfaCode))
	}

	// Шаги кабинетного онбординга Kaspi: воркер присылает сессию входа владельца,
	// список кабинетов, созданного сотрудника или ошибку.
	if h.onboarding != nil {
		r.Post("/kaspi/onboarding/{id}/session", httpx.Wrap(h.onboardSession))
		r.Post("/kaspi/onboarding/{id}/merchants", httpx.Wrap(h.onboardMerchants))
		r.Post("/kaspi/onboarding/{id}/employee", httpx.Wrap(h.onboardEmployee))
		r.Post("/kaspi/onboarding/{id}/fail", httpx.Wrap(h.onboardFail))
	}
	return r
}

// onboardSession принимает сессию входа владельца (после отправки SMS-кода).
func (h *Handler) onboardSession(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	body, err := readLimited(w, r, 64<<10)
	if err != nil {
		return err
	}
	if !json.Valid(body) {
		return httpx.ErrBadRequest("Некорректный JSON сессии")
	}
	if err := h.onboarding.SaveSession(r.Context(), id, body); err != nil {
		return httpx.ErrInternal(err)
	}
	httpx.NoContent(w)
	return nil
}

// onboardMerchants принимает обновлённую сессию и список кабинетов для выбора.
func (h *Handler) onboardMerchants(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	var req struct {
		Session   json.RawMessage       `json:"session"`
		Merchants []model.KaspiMerchant `json:"merchants"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if len(req.Session) == 0 || !json.Valid(req.Session) {
		return httpx.ErrBadRequest("Нет сессии")
	}
	if err := h.onboarding.SaveMerchants(r.Context(), id, req.Session, req.Merchants); err != nil {
		return httpx.ErrInternal(err)
	}
	httpx.NoContent(w)
	return nil
}

// onboardEmployee принимает созданного сотрудника и токен API кабинета.
func (h *Handler) onboardEmployee(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	var req struct {
		MerchantID string `json:"merchant_id"`
		APIToken   string `json:"api_token"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.onboarding.CompleteEmployee(r.Context(), id, req.MerchantID, req.APIToken); err != nil {
		return httpx.ErrInternal(err)
	}
	httpx.NoContent(w)
	return nil
}

// onboardFail помечает онбординг ошибкой (для показа продавцу).
func (h *Handler) onboardFail(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	var req struct {
		Error string `json:"error"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.onboarding.Fail(r.Context(), id, req.Error); err != nil {
		return httpx.ErrInternal(err)
	}
	httpx.NoContent(w)
	return nil
}

// readLimited читает тело запроса с ограничением размера.
func readLimited(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, httpx.ErrBadRequest("Не удалось прочитать тело").WithCause(err)
	}
	return body, nil
}

// ingestCatalog принимает страницу товаров, импортированных воркером из кабинета.
func (h *Handler) ingestCatalog(w http.ResponseWriter, r *http.Request) error {
	connID, err := httpx.ParseID(chi.URLParam(r, "connectionID"))
	if err != nil {
		return err
	}

	// Страница каталога крупнее обычного тела — поднимаем лимит.
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return httpx.ErrBadRequest("Не удалось прочитать тело").WithCause(err)
	}
	var req kaspi.IngestRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return httpx.ErrBadRequest("Некорректный JSON каталога").WithCause(err)
	}

	resp, err := h.catalog.Ingest(r.Context(), connID, req)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	httpx.OK(w, http.StatusOK, resp)
	return nil
}

// mfaCode отдаёт код подтверждения входа служебного сотрудника (из почты).
// Нет кода — 204: воркер подождёт и спросит снова.
func (h *Handler) mfaCode(w http.ResponseWriter, r *http.Request) error {
	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if email == "" {
		return httpx.ErrBadRequest("Нужен параметр email")
	}
	code, found, err := h.mfa.CodeForEmail(r.Context(), email)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	if !found {
		httpx.NoContent(w)
		return nil
	}
	httpx.OK(w, http.StatusOK, map[string]string{"code": code})
	return nil
}

type ctxKey int

const runnerKey ctxKey = iota

// authenticate проверяет токен воркера и кладёт его в контекст.
func (h *Handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			httpx.Fail(w, r, httpx.ErrUnauthorized("Требуется токен воркера"))
			return
		}
		runner, err := h.runners.Authenticate(r.Context(), token, httpx.ClientIP(r))
		if errors.Is(err, database.ErrNotFound) {
			httpx.Fail(w, r, httpx.ErrUnauthorized("Неизвестный или отключённый воркер"))
			return
		}
		if err != nil {
			httpx.Fail(w, r, httpx.ErrInternal(err))
			return
		}
		ctx := context.WithValue(r.Context(), runnerKey, runner)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func runnerFrom(ctx context.Context) *jobs.RunnerInfo {
	rn, _ := ctx.Value(runnerKey).(*jobs.RunnerInfo)
	return rn
}

// workerID — как воркер записывается в jobs.locked_by.
func workerID(rn *jobs.RunnerInfo) string { return "runner:" + rn.Code }

// lease выдаёт воркеру одну удалённую задачу разрешённого ему типа.
func (h *Handler) lease(w http.ResponseWriter, r *http.Request) error {
	rn := runnerFrom(r.Context())

	var req jobs.LeaseRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	// Пересечение запрошенных воркером типов и разрешённых ему в БД: воркер не
	// возьмёт задачу, на которую у него нет прав.
	kinds := make([]string, 0, len(req.Kinds))
	for _, k := range req.Kinds {
		if rn.Allows(k) {
			kinds = append(kinds, k)
		}
	}
	if len(req.Kinds) > 0 && len(kinds) == 0 {
		return httpx.ErrForbidden("Воркеру не разрешён ни один из запрошенных типов задач")
	}

	lease := defaultLease
	if req.LeaseSeconds > 0 {
		lease = min(time.Duration(req.LeaseSeconds)*time.Second, maxLease)
	}

	job, err := h.jobs.Lease(r.Context(), workerID(rn), jobs.Remote, kinds, lease)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	if job == nil {
		httpx.NoContent(w) // задач нет
		return nil
	}

	httpx.OK(w, http.StatusOK, jobs.LeasedJob{
		ID:          job.ID,
		Kind:        job.Kind,
		AccountID:   job.AccountID,
		Payload:     job.Payload,
		Attempts:    job.Attempts,
		MaxAttempts: job.MaxAttempts,
	})
	return nil
}

func (h *Handler) heartbeat(w http.ResponseWriter, r *http.Request) error {
	rn := runnerFrom(r.Context())
	id, err := httpx.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	if err := h.jobs.ExtendLease(r.Context(), id, workerID(rn), defaultLease); err != nil {
		return httpx.ErrInternal(err)
	}
	httpx.NoContent(w)
	return nil
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) error {
	rn := runnerFrom(r.Context())
	job, err := h.ownedJob(w, r, rn)
	if err != nil {
		return err
	}

	var req jobs.CompleteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.jobs.Complete(r.Context(), job.ID, req.Data); err != nil {
		return httpx.ErrInternal(err)
	}
	h.log.Info("jobs: удалённая задача выполнена",
		slog.Int64("job_id", job.ID), slog.String("kind", job.Kind), slog.String("runner", rn.Code))
	httpx.NoContent(w)
	return nil
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request) error {
	rn := runnerFrom(r.Context())
	job, err := h.ownedJob(w, r, rn)
	if err != nil {
		return err
	}

	var req jobs.FailRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	msg := req.Error
	if msg == "" {
		msg = "воркер вернул ошибку без описания"
	}
	if err := h.jobs.Fail(r.Context(), job, msg, req.Permanent, time.Duration(req.RetryAfterSeconds)*time.Second); err != nil {
		return httpx.ErrInternal(err)
	}
	h.log.Warn("jobs: удалённая задача завершилась ошибкой",
		slog.Int64("job_id", job.ID), slog.String("kind", job.Kind), slog.String("runner", rn.Code),
		slog.Bool("permanent", req.Permanent))
	httpx.NoContent(w)
	return nil
}

// ownedJob загружает задачу, только если она числится за этим воркером.
func (h *Handler) ownedJob(_ http.ResponseWriter, r *http.Request, rn *jobs.RunnerInfo) (*jobs.Job, error) {
	id, err := httpx.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		return nil, err
	}
	job, err := h.jobs.LoadOwned(r.Context(), id, workerID(rn))
	if errors.Is(err, database.ErrNotFound) {
		// Аренда истекла и задачу забрал другой воркер, либо это чужая задача.
		return nil, httpx.ErrNotFound("Задача не найдена или не принадлежит воркеру")
	}
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return job, nil
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}
