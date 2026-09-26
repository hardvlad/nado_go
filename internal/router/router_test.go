package router_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"nado_go/internal/config"
	"nado_go/internal/database"
	"nado_go/internal/handler/api"
	"nado_go/internal/handler/shop"
	"nado_go/internal/handler/web"
	"nado_go/internal/i18n"
	"nado_go/internal/model"
	"nado_go/internal/router"
	"nado_go/internal/service"
	"nado_go/internal/view"
	webassets "nado_go/web"
)

// Хранилища подменяются заглушками в памяти: маршруты и обработчики
// тестируются без живого SQL Server. Ради этого сервисы зависят от
// интерфейсов, а не от конкретных репозиториев.

type fakeUsers struct {
	users []model.User
}

func (f *fakeUsers) GetByID(_ context.Context, id int64) (*model.User, error) {
	for i := range f.users {
		if f.users[i].ID == id {
			return &f.users[i], nil
		}
	}
	return nil, database.ErrNotFound
}

func (f *fakeUsers) GetByEmail(_ context.Context, email string) (*model.User, error) {
	for i := range f.users {
		if f.users[i].Email == email {
			return &f.users[i], nil
		}
	}
	return nil, database.ErrNotFound
}

func (f *fakeUsers) List(_ context.Context, _ model.UserFilter) ([]model.User, int64, error) {
	return f.users, int64(len(f.users)), nil
}

func (f *fakeUsers) Create(_ context.Context, u *model.User) (*model.User, error) {
	u.ID = int64(len(f.users) + 1)
	u.CreatedAt, u.UpdatedAt = time.Now(), time.Now()
	f.users = append(f.users, *u)
	return u, nil
}

func (f *fakeUsers) Update(_ context.Context, u *model.User) (*model.User, error) { return u, nil }
func (f *fakeUsers) Delete(_ context.Context, _ int64) error                      { return nil }

// fakeAuth — аккаунты и сессии.
type fakeAuth struct {
	mu       sync.Mutex
	creds    map[string]*model.UserCredentials
	accounts map[int64]*model.Account
	sessions map[string]*model.Session
	nextID   int64
}

func (f *fakeAuth) CreateWithOwner(_ context.Context, acc *model.Account, u *model.NewUser) (*model.Account, *model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.creds[u.Email]; ok {
		return nil, nil, database.ErrConflict
	}
	f.nextID++
	a := *acc
	a.ID = f.nextID
	f.accounts[a.ID] = &a
	f.creds[u.Email] = &model.UserCredentials{
		UserID: f.nextID, Name: u.Name, Email: u.Email, Status: model.UserStatusActive,
		PasswordHash: u.PasswordHash, AccountID: a.ID,
	}
	return &a, &model.User{ID: f.nextID, Email: u.Email, Name: u.Name}, nil
}

func (f *fakeAuth) GetCredentialsByEmail(_ context.Context, email string) (*model.UserCredentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.creds[email]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeAuth) GetCredentialsByPhone(context.Context, string) (*model.UserCredentials, error) {
	// Вход по телефону в этих маршрутных тестах не проверяется (нужен OTP);
	// покрыт юнит-тестами в service. Здесь всегда «не найдено».
	return nil, database.ErrNotFound
}

func (f *fakeAuth) TouchLogin(context.Context, int64) error { return nil }

func (f *fakeAuth) Create(_ context.Context, s *model.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[string(s.IDHash)] = s
	return nil
}

func (f *fakeAuth) GetPrincipal(_ context.Context, hash []byte) (*model.Principal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[string(hash)]
	if !ok {
		return nil, database.ErrNotFound
	}
	a := f.accounts[s.AccountID]
	for _, c := range f.creds {
		if c.UserID == s.UserID {
			return &model.Principal{
				UserID: c.UserID, UserName: c.Name, Email: c.Email,
				AccountID: a.ID, AccountName: a.Name, PlanCode: a.PlanCode, Role: model.RoleOwner,
			}, nil
		}
	}
	return nil, database.ErrNotFound
}

func (f *fakeAuth) Delete(_ context.Context, hash []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, string(hash))
	return nil
}

type fakeFeedback struct {
	mu    sync.Mutex
	saved []model.Feedback
}

func (f *fakeFeedback) Create(_ context.Context, fb *model.Feedback) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, *fb)
	return int64(len(f.saved)), nil
}

type fakePinger struct{ err error }

func (f fakePinger) Health(context.Context) error { return f.err }

type testEnv struct {
	srv      http.Handler
	feedback *fakeFeedback
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	templates, err := webassets.Templates(false)
	if err != nil {
		t.Fatalf("шаблоны: %v", err)
	}
	renderer, err := view.New(templates, view.WithGlobals(map[string]any{
		"AppName": "nado", "Env": "test", "Version": "test",
	}))
	if err != nil {
		t.Fatalf("рендерер: %v", err)
	}
	static, err := webassets.Static(false)
	if err != nil {
		t.Fatalf("статика: %v", err)
	}

	// Любой ключ перевода, которого нет ни в одном языке, — ошибка теста:
	// иначе на странице окажется «home.hero.title» вместо текста.
	bundle, err := i18n.NewBundle(i18n.WithMissingHook(func(lang i18n.Lang, id string) {
		t.Errorf("нет перевода %q (%s)", id, lang)
	}))
	if err != nil {
		t.Fatalf("переводы: %v", err)
	}

	users := service.NewUserService(&fakeUsers{users: []model.User{{
		ID: 1, Email: "user@example.com", Name: "Тестовый",
		Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}})

	authStore := &fakeAuth{
		creds: map[string]*model.UserCredentials{}, accounts: map[int64]*model.Account{},
		sessions: map[string]*model.Session{},
	}
	plans := service.NewPlanCatalog()
	auth, err := service.NewAuthService(authStore, authStore, plans)
	if err != nil {
		t.Fatal(err)
	}
	feedbackStore := &fakeFeedback{}

	cfg := &config.Config{}
	cfg.App.Env = "test"
	cfg.HTTP.RequestTimeout = 5 * time.Second
	cfg.HTTP.AllowedOrigins = []string{"*"}

	srv := router.New(router.Deps{
		Config: cfg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Static: static,
		I18n:   bundle,
		Pages: web.NewPageHandler(web.Deps{
			Render: renderer, I18n: bundle, Plans: plans, Auth: auth,
			Feedback: service.NewFeedbackService(feedbackStore),
		}),
		Users:  api.NewUserHandler(users),
		Health: api.NewHealthHandler("test", map[string]api.Pinger{"database": fakePinger{}}),
		// Витрина подключена, чтобы покрыть монтирование /shop/* (регрессия на
		// порядок middleware chi: New не должен паниковать с Shop != nil).
		Shop: &shop.Handler{},
	})
	return &testEnv{srv: srv, feedback: feedbackStore}
}

func (e *testEnv) do(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

// postForm имитирует отправку формы браузером с той же страницы.
func postForm(target string, form url.Values, cookies ...*http.Cookie) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return req
}

func TestRoutes(t *testing.T) {
	env := newTestEnv(t)

	cases := []struct {
		name        string
		target      string
		wantStatus  int
		wantContent string
	}{
		{"лендинг", "/", http.StatusOK, "text/html"},
		{"лендинг kk", "/kk", http.StatusOK, "text/html"},
		{"лендинг en со слешем", "/en/", http.StatusOK, "text/html"}, // хвостовой слеш срезает StripSlashes
		{"обратная связь", "/contact", http.StatusOK, "text/html"},
		{"регистрация", "/en/register", http.StatusOK, "text/html"},
		{"вход", "/kk/login", http.StatusOK, "text/html"},
		{"кабинет без входа", "/account", http.StatusSeeOther, ""},
		{"несуществующая страница", "/nope", http.StatusNotFound, "text/html"},
		{"стили", "/static/css/app.css", http.StatusOK, "text/css"},
		{"токены", "/static/css/tokens.css", http.StatusOK, "text/css"},
		{"скрипт темы", "/static/js/theme.js", http.StatusOK, "javascript"},
		{"liveness", "/healthz", http.StatusOK, "application/json"},
		{"readiness", "/readyz", http.StatusOK, "application/json"},
		{"api список", "/api/v1/users", http.StatusOK, "application/json"},
		{"api несуществующий id", "/api/v1/users/999", http.StatusNotFound, "application/json"},
		{"api неизвестный ресурс", "/api/v1/nope", http.StatusNotFound, "application/json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(t, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("статус = %d, ожидался %d (тело: %.300s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); tc.wantContent != "" && !strings.Contains(ct, tc.wantContent) {
				t.Errorf("Content-Type = %q, ожидался %q", ct, tc.wantContent)
			}
		})
	}
}

// Каждая страница на каждом языке: правильный lang, переключатель языков,
// hreflang, светлая/тёмная тема, никаких непереведённых ключей (хук в bundle).
func TestPagesInAllLanguages(t *testing.T) {
	env := newTestEnv(t)

	marker := map[i18n.Lang]string{i18n.RU: "Тарифы", i18n.KK: "Тарифтер", i18n.EN: "Pricing"}
	for _, lang := range i18n.Supported() {
		for _, page := range []string{"/", "/contact", "/register", "/login", "/nope"} {
			target := i18n.Localize(lang, page)
			t.Run(target, func(t *testing.T) {
				rec := env.do(t, httptest.NewRequest(http.MethodGet, target, nil))
				body := rec.Body.String()

				if !strings.Contains(body, `<html lang="`+string(lang)+`"`) {
					t.Errorf("нет <html lang=%q>", lang)
				}
				if !strings.Contains(body, marker[lang]) {
					t.Errorf("нет текста %q на языке страницы", marker[lang])
				}
				for _, other := range i18n.Supported() {
					if !strings.Contains(body, `hreflang="`+string(other)+`"`) {
						t.Errorf("нет ссылки hreflang=%s", other)
					}
				}
				if strings.Contains(body, "data-theme=") {
					t.Error("без cookie режим «как в системе» — data-theme не ставится")
				}
				if !strings.Contains(body, `data-theme-toggle`) {
					t.Error("нет переключателя темы")
				}
			})
		}
	}
}

func TestThemeCookie(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "nado_theme", Value: "dark"})
	body := env.do(t, req).Body.String()

	if !strings.Contains(body, `<html lang="ru" data-theme="dark">`) {
		t.Error("тёмная тема из cookie должна рендериться сервером без мигания")
	}
	// Цвет строки состояния в обоих мета-тегах — тёмный.
	if strings.Count(body, `content="`+webassets.ThemeColorDark+`"`) != 2 {
		t.Error("при явной тёмной теме оба theme-color должны быть тёмными")
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "nado_theme", Value: "<script>"})
	if body := env.do(t, req).Body.String(); strings.Contains(body, "data-theme=") {
		t.Error("неизвестное значение cookie должно игнорироваться")
	}
}

func TestLandingShowsPlans(t *testing.T) {
	env := newTestEnv(t)
	body := env.do(t, httptest.NewRequest(http.MethodGet, "/", nil)).Body.String()

	for _, price := range []string{"20 000 ₸", "30 000 ₸", "50 000 ₸"} {
		if !strings.Contains(body, price) {
			t.Errorf("на лендинге нет цены %q", price)
		}
	}
	if !strings.Contains(body, "/register?plan=business") {
		t.Error("кнопка тарифа должна вести на регистрацию с выбранным тарифом")
	}
}

func TestContactForm(t *testing.T) {
	env := newTestEnv(t)

	valid := url.Values{
		"name": {"Айгерім"}, "contact": {"+7 701 234 56 78"}, "topic": {"suggestion"},
		"message": {"Добавьте, пожалуйста, Halyk Market."}, "consent": {"on"},
	}
	rec := env.do(t, postForm("/kk/contact", valid))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/kk/contact?sent=1" {
		t.Fatalf("успешная отправка: статус %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(env.feedback.saved) != 1 || env.feedback.saved[0].Lang != "kk" {
		t.Fatalf("обращение не сохранено: %+v", env.feedback.saved)
	}

	// Ошибки показываются на языке страницы, введённое сохраняется.
	invalid := url.Values{"name": {"А"}, "contact": {"никак"}, "topic": {"question"}, "message": {"коротко"}}
	rec = env.do(t, postForm("/kk/contact", invalid))
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("невалидная форма: статус %d", rec.Code)
	}
	for _, want := range []string{"Email немесе телефон нөмірін көрсетіңіз", `value="никак"`, `aria-invalid="true"`} {
		if !strings.Contains(body, want) {
			t.Errorf("нет %q в ответе", want)
		}
	}

	// Бот, заполнивший ловушку, получает «успех», но ничего не сохраняется.
	bot := url.Values{"website": {"http://spam"}}
	for k, v := range valid {
		bot[k] = v
	}
	if rec := env.do(t, postForm("/contact", bot)); rec.Code != http.StatusSeeOther {
		t.Fatalf("ловушка: статус %d", rec.Code)
	}
	if len(env.feedback.saved) != 1 {
		t.Error("сообщение бота не должно сохраняться")
	}

	if body := env.do(t, httptest.NewRequest(http.MethodGet, "/contact?sent=1", nil)).Body.String(); !strings.Contains(body, "Спасибо!") {
		t.Error("после отправки должно показываться подтверждение")
	}
}

func TestRegisterLoginLogout(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{
		"company": {"Магазин Даны"}, "name": {"Дана"}, "email": {"dana@example.kz"},
		"phone": {""}, "password": {"надёжный-пароль"}, "plan": {"pro"}, "consent": {"on"},
	}
	rec := env.do(t, postForm("/en/register", form))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/en/account" {
		t.Fatalf("регистрация: статус %d, Location %q, тело %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	session := sessionCookie(t, rec)
	if !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
		t.Error("cookie сессии должна быть HttpOnly и SameSite=Lax")
	}

	req := httptest.NewRequest(http.MethodGet, "/en/account", nil)
	req.AddCookie(session)
	rec = env.do(t, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Welcome, Дана!") {
		t.Fatalf("кабинет: статус %d", rec.Code)
	}

	// Повторная регистрация с тем же email — понятная ошибка у поля.
	if body := env.do(t, postForm("/register", form)).Body.String(); !strings.Contains(body, "Этот email уже зарегистрирован") {
		t.Error("нет ошибки о занятом email")
	}

	// Выход удаляет сессию.
	rec = env.do(t, postForm("/logout", nil, session))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("выход: статус %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(session)
	if rec := env.do(t, req); rec.Code != http.StatusSeeOther {
		t.Fatalf("после выхода кабинет должен требовать вход, статус %d", rec.Code)
	}

	// Неверный пароль.
	rec = env.do(t, postForm("/login", url.Values{"email": {"dana@example.kz"}, "password": {"не-тот"}}))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Неверный email или пароль") {
		t.Fatalf("неверный пароль: статус %d", rec.Code)
	}

	// Верный пароль и возврат на локальный адрес; внешний адрес игнорируется.
	rec = env.do(t, postForm("/login", url.Values{
		"email": {"DANA@example.kz"}, "password": {"надёжный-пароль"}, "next": {"https://evil.example/"},
	}))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account" {
		t.Fatalf("вход: статус %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestCrossSitePostRejected(t *testing.T) {
	env := newTestEnv(t)

	req := postForm("/contact", url.Values{"name": {"x"}})
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := env.do(t, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST с чужого сайта: статус %d, ожидался 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Запрос отклонён") {
		t.Error("отказ должен показываться страницей на языке сайта")
	}
	if len(env.feedback.saved) != 0 {
		t.Error("межсайтовый запрос не должен доходить до обработчика")
	}
}

func TestLoginRateLimit(t *testing.T) {
	env := newTestEnv(t)

	var last int
	for range 12 {
		last = env.do(t, postForm("/login", url.Values{"email": {"victim@example.kz"}, "password": {"guess"}})).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("после серии неудачных входов ожидался 429, получен %d", last)
	}
}

func TestCreateUserValidationAPI(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{"email":"не-email","name":"я"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := env.do(t, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("статус = %d, ожидался 422 (тело: %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if resp.Error.Code != "validation_failed" {
		t.Errorf("код ошибки = %q", resp.Error.Code)
	}
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "nado_session" && c.Value != "" {
			return c
		}
	}
	t.Fatal("после регистрации не выставлена cookie сессии")
	return nil
}
