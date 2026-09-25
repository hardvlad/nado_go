package service

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"nado_go/internal/database"
	"nado_go/internal/httpx"
	"nado_go/internal/model"
)

// memAuthStore — хранилище аккаунтов и сессий в памяти.
type memAuthStore struct {
	users    map[string]*model.UserCredentials // по email
	phones   map[string]*model.UserCredentials // по подтверждённому телефону
	sessions map[string]*model.Session         // по хешу
	nextID   int64
}

func newMemAuthStore() *memAuthStore {
	return &memAuthStore{
		users:    map[string]*model.UserCredentials{},
		phones:   map[string]*model.UserCredentials{},
		sessions: map[string]*model.Session{},
	}
}

func (m *memAuthStore) CreateWithOwner(_ context.Context, acc *model.Account, u *model.NewUser) (*model.Account, *model.User, error) {
	if u.Email != "" {
		if _, ok := m.users[u.Email]; ok {
			return nil, nil, database.ErrConflict
		}
	}
	if u.PhoneVerified {
		if _, ok := m.phones[u.Phone]; ok {
			return nil, nil, database.ErrConflict
		}
	}
	m.nextID++
	accID, userID := m.nextID*10, m.nextID
	creds := &model.UserCredentials{
		UserID: userID, Name: u.Name, Email: u.Email, Status: model.UserStatusActive,
		PasswordHash: u.PasswordHash, AccountID: accID,
	}
	if u.Email != "" {
		m.users[u.Email] = creds
	}
	if u.PhoneVerified {
		m.phones[u.Phone] = creds
	}
	created := *acc
	created.ID = accID
	return &created, &model.User{ID: userID, Email: u.Email, Name: u.Name}, nil
}

func (m *memAuthStore) GetCredentialsByEmail(_ context.Context, email string) (*model.UserCredentials, error) {
	if c, ok := m.users[email]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, database.ErrNotFound
}

func (m *memAuthStore) GetCredentialsByPhone(_ context.Context, phone string) (*model.UserCredentials, error) {
	if c, ok := m.phones[phone]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, database.ErrNotFound
}

func (m *memAuthStore) TouchLogin(context.Context, int64) error { return nil }

func (m *memAuthStore) Create(_ context.Context, s *model.Session) error {
	m.sessions[string(s.IDHash)] = s
	return nil
}

func (m *memAuthStore) GetPrincipal(_ context.Context, hash []byte) (*model.Principal, error) {
	s, ok := m.sessions[string(hash)]
	if !ok {
		return nil, database.ErrNotFound
	}
	return &model.Principal{UserID: s.UserID, AccountID: s.AccountID}, nil
}

func (m *memAuthStore) Delete(_ context.Context, hash []byte) error {
	delete(m.sessions, string(hash))
	return nil
}

func newAuth(t *testing.T) (*AuthService, *memAuthStore) {
	t.Helper()
	store := newMemAuthStore()
	svc, err := NewAuthService(store, store, NewPlanCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return svc, store
}

func validRegister() RegisterInput {
	return RegisterInput{
		Company: "Магазин Айгуль", Name: "Айгуль", Email: " Aigul@Example.KZ ",
		Phone: "8 (701) 234-56-78", Password: "очень-секретно", Plan: "business", Consent: true,
	}
}

func TestRegisterLoginLogout(t *testing.T) {
	svc, store := newAuth(t)
	ctx := context.Background()

	token, err := svc.Register(ctx, validRegister())
	if err != nil {
		t.Fatalf("регистрация: %v", err)
	}
	creds := store.users["aigul@example.kz"]
	if creds == nil {
		t.Fatal("email должен сохраняться в нижнем регистре без пробелов")
	}
	if bytes.Contains([]byte(creds.PasswordHash), []byte("очень-секретно")) {
		t.Fatal("пароль сохранён в открытом виде")
	}

	p, err := svc.Authenticate(ctx, token)
	if err != nil || p.UserID != creds.UserID {
		t.Fatalf("сессия после регистрации: %+v, %v", p, err)
	}

	if _, err := svc.Login(ctx, LoginInput{Email: "AIGUL@example.kz", Password: "неверный"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("неверный пароль: ожидалась ErrInvalidCredentials, получено %v", err)
	}
	if _, err := svc.Login(ctx, LoginInput{Email: "nobody@example.kz", Password: "что-угодно"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("неизвестный email: ожидалась ErrInvalidCredentials, получено %v", err)
	}

	token2, err := svc.Login(ctx, LoginInput{Email: "aigul@example.kz", Password: "очень-секретно"})
	if err != nil {
		t.Fatalf("вход: %v", err)
	}
	if err := svc.Logout(ctx, token2); err != nil {
		t.Fatalf("выход: %v", err)
	}
	if _, err := svc.Authenticate(ctx, token2); !errors.Is(err, ErrNoSession) {
		t.Fatalf("после выхода сессия должна быть недействительна, получено %v", err)
	}
	if _, err := svc.Authenticate(ctx, "подделка"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("чужой токен: %v", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	svc, _ := newAuth(t)
	ctx := context.Background()

	in := validRegister()
	in.Email = "не-email"
	in.Password = "123"
	in.Phone = "12345"
	in.Plan = "gold"
	in.Consent = false

	_, err := svc.Register(ctx, in)
	fields, ok := httpx.FieldErrors(err)
	if !ok {
		t.Fatalf("ожидалась ошибка полей, получено %v", err)
	}
	want := map[string]string{"email": "email", "password": "min", "phone": "phone", "plan": "oneof", "consent": "consent"}
	for field, tag := range want {
		if fields[field].Tag != tag {
			t.Errorf("поле %s: тег %q, ожидался %q", field, fields[field].Tag, tag)
		}
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	svc, _ := newAuth(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, validRegister()); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Register(ctx, validRegister())
	fields, ok := httpx.FieldErrors(err)
	if !ok || fields["email"].Tag != "email_taken" {
		t.Fatalf("ожидалась ошибка email_taken, получено %v", err)
	}
}

func TestPhoneRegisterAndLogin(t *testing.T) {
	svc, _ := newAuth(t)
	ctx := context.Background()

	// Регистрация по телефону (телефон уже подтверждён кодом выше по потоку).
	token, err := svc.RegisterByPhone(ctx, "Ержан", "8 701 555 44 33", "kk", ClientMeta{})
	if err != nil {
		t.Fatalf("RegisterByPhone: %v", err)
	}
	if p, err := svc.Authenticate(ctx, token); err != nil || p.UserID == 0 {
		t.Fatalf("сессия после регистрации по телефону: %+v, %v", p, err)
	}

	// Повторная регистрация того же номера — ErrPhoneTaken.
	if _, err := svc.RegisterByPhone(ctx, "Ержан", "+7 701 555 44 33", "kk", ClientMeta{}); !errors.Is(err, ErrPhoneTaken) {
		t.Fatalf("повтор номера: ожидалась ErrPhoneTaken, получено %v", err)
	}

	// Вход по тому же номеру в другом формате — успех.
	if _, err := svc.LoginByPhone(ctx, "87015554433", ClientMeta{}); err != nil {
		t.Fatalf("LoginByPhone: %v", err)
	}
	// Незарегистрированный номер.
	if _, err := svc.LoginByPhone(ctx, "+7 700 000 00 00", ClientMeta{}); !errors.Is(err, ErrPhoneNotRegistered) {
		t.Fatalf("незарегистрированный номер: ожидалась ErrPhoneNotRegistered, получено %v", err)
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"+7 700 123 45 67":  "+77001234567",
		"8 (701) 234-56-78": "+77012345678",
		"7001234567":        "+77001234567",
		"77001234567":       "+77001234567",
	}
	for in, want := range cases {
		if got, ok := NormalizePhone(in); !ok || got != want {
			t.Errorf("NormalizePhone(%q) = %q, %v; ожидалось %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"12345", "+1 202 555 0100", "700-12a-45-67", ""} {
		if _, ok := NormalizePhone(bad); ok {
			t.Errorf("NormalizePhone(%q) должен отклоняться", bad)
		}
	}
}
