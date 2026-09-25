package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"nado_go/internal/database"
	"nado_go/internal/integration/messaging"
	"nado_go/internal/model"
	"nado_go/internal/secrets"
)

type otpRow struct {
	code    *model.OTPCode
	created time.Time
	used    bool
}

type fakeOTPStore struct {
	rows   []*otpRow
	nextID int64
}

func (f *fakeOTPStore) Create(_ context.Context, c *model.OTPCode) (int64, error) {
	f.nextID++
	c.ID = f.nextID
	cp := *c
	f.rows = append(f.rows, &otpRow{code: &cp, created: time.Now()})
	return c.ID, nil
}
func (f *fakeOTPStore) Latest(_ context.Context, phone, purpose string) (*model.OTPCode, error) {
	for i := len(f.rows) - 1; i >= 0; i-- {
		r := f.rows[i]
		if r.code.Phone == phone && r.code.Purpose == purpose && !r.used {
			return r.code, nil
		}
	}
	return nil, database.ErrNotFound
}
func (f *fakeOTPStore) LastSentAt(_ context.Context, phone string) (time.Time, error) {
	for i := len(f.rows) - 1; i >= 0; i-- {
		if f.rows[i].code.Phone == phone {
			return f.rows[i].created, nil
		}
	}
	return time.Time{}, nil
}
func (f *fakeOTPStore) IncAttempt(_ context.Context, id int64) error {
	for _, r := range f.rows {
		if r.code.ID == id {
			r.code.Attempts++
		}
	}
	return nil
}
func (f *fakeOTPStore) MarkUsed(_ context.Context, id int64) (bool, error) {
	for _, r := range f.rows {
		if r.code.ID == id {
			if r.used {
				return false, nil
			}
			r.used = true
			return true, nil
		}
	}
	return false, nil
}

type fakeSender struct {
	lastText string
	fail     error
}

func (f *fakeSender) Provider() string { return "fake" }
func (f *fakeSender) Send(_ context.Context, m messaging.Message) (*messaging.Sent, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	f.lastText = m.Text
	return &messaging.Sent{MessageID: "m1", InstanceID: 1}, nil
}

func newOTP(t *testing.T, sender messaging.Sender) (*OTPService, *fakeOTPStore) {
	t.Helper()
	box, err := secrets.New(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeOTPStore{}
	// Текст = код: так тест узнаёт сгенерированный код.
	svc := NewOTPService(store, box, sender, func(code, _ string) string { return code },
		5*time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return svc, store
}

func TestOTPRequestAndVerify(t *testing.T) {
	sender := &fakeSender{}
	svc, _ := newOTP(t, sender)
	ctx := context.Background()

	if _, err := svc.Request(ctx, "+7 700 111 22 33", model.OTPPurposeLogin, "ru", "1.1.1.1"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	code := sender.lastText
	if len(code) != otpCodeDigits {
		t.Fatalf("код должен быть из %d цифр, получено %q", otpCodeDigits, code)
	}

	if err := svc.Verify(ctx, "+77001112233", model.OTPPurposeLogin, "000000"); !errors.Is(err, ErrOTPWrongCode) {
		if code != "000000" { // маловероятное совпадение
			t.Fatalf("неверный код: ожидалась ErrOTPWrongCode, получено %v", err)
		}
	}
	if err := svc.Verify(ctx, "+77001112233", model.OTPPurposeLogin, code); err != nil {
		t.Fatalf("верный код: %v", err)
	}
	// Повторное использование — уже нельзя.
	if err := svc.Verify(ctx, "+77001112233", model.OTPPurposeLogin, code); err == nil {
		t.Error("использованный код не должен приниматься повторно")
	}
}

func TestOTPCooldown(t *testing.T) {
	svc, _ := newOTP(t, &fakeSender{})
	ctx := context.Background()
	if _, err := svc.Request(ctx, "+77001112233", model.OTPPurposeLogin, "ru", "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Request(ctx, "+77001112233", model.OTPPurposeLogin, "ru", "1.1.1.1"); !errors.Is(err, ErrOTPCooldown) {
		t.Fatalf("повтор сразу: ожидалась ErrOTPCooldown, получено %v", err)
	}
}

func TestOTPInvalidPhone(t *testing.T) {
	svc, _ := newOTP(t, &fakeSender{})
	if _, err := svc.Request(context.Background(), "12345", model.OTPPurposeLogin, "ru", "1.1.1.1"); !errors.Is(err, ErrOTPInvalidPhone) {
		t.Fatalf("ожидалась ErrOTPInvalidPhone, получено %v", err)
	}
}

func TestOTPTooManyAttempts(t *testing.T) {
	sender := &fakeSender{}
	svc, _ := newOTP(t, sender)
	ctx := context.Background()
	if _, err := svc.Request(ctx, "+77001112233", model.OTPPurposeLogin, "ru", "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < otpMaxAttempts; i++ {
		_ = svc.Verify(ctx, "+77001112233", model.OTPPurposeLogin, "999999")
	}
	if err := svc.Verify(ctx, "+77001112233", model.OTPPurposeLogin, sender.lastText); !errors.Is(err, ErrOTPTooManyAttempts) {
		t.Fatalf("после лимита попыток ожидалась ErrOTPTooManyAttempts, получено %v", err)
	}
}

func TestOTPSendFailure(t *testing.T) {
	svc, store := newOTP(t, &fakeSender{fail: messaging.ErrNoInstance})
	if _, err := svc.Request(context.Background(), "+77001112233", model.OTPPurposeLogin, "ru", "1.1.1.1"); !errors.Is(err, ErrOTPSendFailed) {
		t.Fatalf("ожидалась ErrOTPSendFailed, получено %v", err)
	}
	if len(store.rows) != 0 {
		t.Error("при неудачной отправке код не должен сохраняться")
	}
}
