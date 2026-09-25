package jobsapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nado_go/internal/database"
	"nado_go/internal/integration/marketplace/kaspi"
	"nado_go/internal/jobs"
	"nado_go/internal/model"
)

// Заглушки очереди и учёта воркеров без БД.

type fakeJobs struct {
	next   *jobs.Job
	owned  map[int64]*jobs.Job // задачи, «занятые» воркером
	leased string              // кто последним лизил
	done   map[int64][]byte
	failed map[int64]string
}

func (f *fakeJobs) Lease(_ context.Context, worker string, _ jobs.Execution, _ []string, _ time.Duration) (*jobs.Job, error) {
	f.leased = worker
	j := f.next
	f.next = nil
	return j, nil
}
func (f *fakeJobs) LoadOwned(_ context.Context, id int64, worker string) (*jobs.Job, error) {
	j, ok := f.owned[id]
	if !ok || "runner:"+jobRunnerCode != worker {
		return nil, database.ErrNotFound
	}
	return j, nil
}
func (f *fakeJobs) Complete(_ context.Context, id int64, result []byte) error {
	if f.done == nil {
		f.done = map[int64][]byte{}
	}
	f.done[id] = result
	return nil
}
func (f *fakeJobs) Fail(_ context.Context, j *jobs.Job, msg string, _ bool, _ time.Duration) error {
	if f.failed == nil {
		f.failed = map[int64]string{}
	}
	f.failed[j.ID] = msg
	return nil
}
func (f *fakeJobs) ExtendLease(context.Context, int64, string, time.Duration) error { return nil }

const jobRunnerCode = "runner-1"

type fakeRunners struct{ validToken string }

func (f fakeRunners) Authenticate(_ context.Context, token, _ string) (*jobs.RunnerInfo, error) {
	if token != f.validToken {
		return nil, database.ErrNotFound
	}
	return &jobs.RunnerInfo{ID: 1, Code: jobRunnerCode, Kinds: []string{jobs.KindKaspiSendOTP}}, nil
}

func newTestServer(t *testing.T, fj *fakeJobs) http.Handler {
	t.Helper()
	h := &Handler{jobs: fj, runners: fakeRunners{validToken: "good-token"}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return h.Routes()
}

func do(t *testing.T, srv http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestAuthRequired(t *testing.T) {
	srv := newTestServer(t, &fakeJobs{})
	if rec := do(t, srv, http.MethodPost, "/lease", "", `{}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("без токена ожидался 401, получен %d", rec.Code)
	}
	if rec := do(t, srv, http.MethodPost, "/lease", "wrong", `{}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("с чужим токеном ожидался 401, получен %d", rec.Code)
	}
}

func TestLeaseReturnsJobAndForbidsForeignKind(t *testing.T) {
	fj := &fakeJobs{next: &jobs.Job{ID: 5, Kind: jobs.KindKaspiSendOTP, Payload: json.RawMessage(`{"phone":"+7"}`), MaxAttempts: 3}}
	srv := newTestServer(t, fj)

	rec := do(t, srv, http.MethodPost, "/lease", "good-token", `{"kinds":["`+jobs.KindKaspiSendOTP+`"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("lease: статус %d, тело %s", rec.Code, rec.Body.String())
	}
	if fj.leased != "runner:"+jobRunnerCode {
		t.Errorf("locked_by = %q, ожидался runner:%s", fj.leased, jobRunnerCode)
	}
	var env struct {
		Data jobs.LeasedJob `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.ID != 5 || env.Data.Kind != jobs.KindKaspiSendOTP {
		t.Errorf("выдана не та задача: %+v", env.Data)
	}

	// Запрошен тип, не разрешённый воркеру, — 403.
	rec = do(t, srv, http.MethodPost, "/lease", "good-token", `{"kinds":["kaspi.sync_catalog"]}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("чужой тип: ожидался 403, получен %d", rec.Code)
	}
}

func TestLeaseNoJobs(t *testing.T) {
	srv := newTestServer(t, &fakeJobs{next: nil})
	rec := do(t, srv, http.MethodPost, "/lease", "good-token", `{"kinds":["`+jobs.KindKaspiSendOTP+`"]}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("нет задач: ожидался 204, получен %d", rec.Code)
	}
}

func TestCompleteOnlyOwnedJob(t *testing.T) {
	fj := &fakeJobs{owned: map[int64]*jobs.Job{7: {ID: 7, Kind: jobs.KindKaspiSendOTP}}}
	srv := newTestServer(t, fj)

	// Своя задача — успех.
	if rec := do(t, srv, http.MethodPost, "/jobs/7/complete", "good-token", `{"data":{"ok":true}}`); rec.Code != http.StatusNoContent {
		t.Fatalf("complete своей задачи: статус %d, тело %s", rec.Code, rec.Body.String())
	}
	if string(fj.done[7]) != `{"ok":true}` {
		t.Errorf("результат не сохранён: %q", fj.done[7])
	}

	// Чужая/неизвестная задача — 404, не трогаем.
	if rec := do(t, srv, http.MethodPost, "/jobs/999/complete", "good-token", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("чужая задача: ожидался 404, получен %d", rec.Code)
	}
}

func TestFailOwnedJob(t *testing.T) {
	fj := &fakeJobs{owned: map[int64]*jobs.Job{8: {ID: 8, Kind: jobs.KindKaspiSendOTP, Attempts: 1, MaxAttempts: 3}}}
	srv := newTestServer(t, fj)

	rec := do(t, srv, http.MethodPost, "/jobs/8/fail", "good-token", `{"error":"нет связи","permanent":false}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("fail: статус %d, тело %s", rec.Code, rec.Body.String())
	}
	if fj.failed[8] != "нет связи" {
		t.Errorf("ошибка не записана: %q", fj.failed[8])
	}
}

// Заглушки приёма каталога и кода MFA.

type fakeCatalog struct {
	lastConn int64
	lastReq  kaspi.IngestRequest
}

func (f *fakeCatalog) Ingest(_ context.Context, connectionID int64, req kaspi.IngestRequest) (kaspi.IngestResponse, error) {
	f.lastConn = connectionID
	f.lastReq = req
	return kaspi.IngestResponse{Created: len(req.Products)}, nil
}

type fakeMFA struct{ code string }

func (f fakeMFA) CodeForEmail(_ context.Context, email string) (string, bool, error) {
	if f.code == "" {
		return "", false, nil
	}
	return f.code, true, nil
}

func newTestServerFull(t *testing.T, fc *fakeCatalog, fm mfaCodeSource) http.Handler {
	t.Helper()
	h := &Handler{
		jobs: &fakeJobs{}, runners: fakeRunners{validToken: "good-token"},
		catalog: fc, mfa: fm, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return h.Routes()
}

func TestIngestCatalog(t *testing.T) {
	fc := &fakeCatalog{}
	srv := newTestServerFull(t, fc, fakeMFA{})

	body := `{"products":[{"sku":"a1","price_minor":100,"currency":"KZT"},{"sku":"a2","price_minor":200,"currency":"KZT"}],"final":true}`
	rec := do(t, srv, http.MethodPost, "/kaspi/catalog/42/products", "good-token", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("ingest: статус %d, тело %s", rec.Code, rec.Body.String())
	}
	if fc.lastConn != 42 || len(fc.lastReq.Products) != 2 || !fc.lastReq.Final {
		t.Errorf("приём каталога получил неверные данные: conn=%d, %+v", fc.lastConn, fc.lastReq)
	}

	// Без токена — отказ.
	if rec := do(t, srv, http.MethodPost, "/kaspi/catalog/42/products", "", body); rec.Code != http.StatusUnauthorized {
		t.Errorf("без токена ожидался 401, получен %d", rec.Code)
	}
}

// fakeOnboarding фиксирует вызовы шагов онбординга.
type fakeOnboarding struct {
	session   json.RawMessage
	merchants []model.KaspiMerchant
	merchant  string
	token     string
	failMsg   string
}

func (f *fakeOnboarding) SaveSession(_ context.Context, _ int64, s json.RawMessage) error {
	f.session = s
	return nil
}
func (f *fakeOnboarding) SaveMerchants(_ context.Context, _ int64, s json.RawMessage, m []model.KaspiMerchant) error {
	f.session, f.merchants = s, m
	return nil
}
func (f *fakeOnboarding) CompleteEmployee(_ context.Context, _ int64, merchantID, token string) error {
	f.merchant, f.token = merchantID, token
	return nil
}
func (f *fakeOnboarding) Fail(_ context.Context, _ int64, msg string) error {
	f.failMsg = msg
	return nil
}

func TestOnboardingEndpoints(t *testing.T) {
	fo := &fakeOnboarding{}
	h := &Handler{
		jobs: &fakeJobs{}, runners: fakeRunners{validToken: "good-token"},
		onboarding: fo, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	srv := h.Routes()

	// Сессия входа владельца.
	if rec := do(t, srv, http.MethodPost, "/kaspi/onboarding/7/session", "good-token", `{"phone":"+7","amp_cookie":"a"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("session: статус %d, тело %s", rec.Code, rec.Body.String())
	}
	if string(fo.session) == "" {
		t.Error("сессия не сохранена")
	}

	// Список кабинетов для выбора.
	body := `{"session":{"amp_cookie":"a","mc_sid":"s"},"merchants":[{"uid":"M1","name":"A"},{"uid":"M2","name":"B"}]}`
	if rec := do(t, srv, http.MethodPost, "/kaspi/onboarding/7/merchants", "good-token", body); rec.Code != http.StatusNoContent {
		t.Fatalf("merchants: статус %d, тело %s", rec.Code, rec.Body.String())
	}
	if len(fo.merchants) != 2 {
		t.Errorf("кабинеты не сохранены: %+v", fo.merchants)
	}

	// Созданный сотрудник и токен.
	if rec := do(t, srv, http.MethodPost, "/kaspi/onboarding/7/employee", "good-token", `{"merchant_id":"M2","api_token":"tok"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("employee: статус %d, тело %s", rec.Code, rec.Body.String())
	}
	if fo.merchant != "M2" || fo.token != "tok" {
		t.Errorf("сотрудник сохранён неверно: %q %q", fo.merchant, fo.token)
	}

	// Ошибка шага.
	if rec := do(t, srv, http.MethodPost, "/kaspi/onboarding/7/fail", "good-token", `{"error":"код неверный"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("fail: статус %d", rec.Code)
	}
	if fo.failMsg != "код неверный" {
		t.Errorf("ошибка не записана: %q", fo.failMsg)
	}

	// Без токена — отказ.
	if rec := do(t, srv, http.MethodPost, "/kaspi/onboarding/7/session", "", `{}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("без токена ожидался 401, получен %d", rec.Code)
	}
}

func TestMFACode(t *testing.T) {
	srv := newTestServerFull(t, &fakeCatalog{}, fakeMFA{code: "482913"})
	rec := do(t, srv, http.MethodGet, "/kaspi/otp-code?email=nado_1@kaspi.nado.kz", "good-token", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "482913") {
		t.Fatalf("код MFA: статус %d, тело %s", rec.Code, rec.Body.String())
	}

	// Нет кода — 204.
	srv2 := newTestServerFull(t, &fakeCatalog{}, fakeMFA{})
	if rec := do(t, srv2, http.MethodGet, "/kaspi/otp-code?email=x@y.z", "good-token", ""); rec.Code != http.StatusNoContent {
		t.Errorf("нет кода: ожидался 204, получен %d", rec.Code)
	}
}
