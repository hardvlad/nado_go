package service

import (
	"context"
	"testing"

	"nado_go/internal/httpx"
	"nado_go/internal/model"
)

type memFeedbackStore struct{ saved []model.Feedback }

func (m *memFeedbackStore) Create(_ context.Context, f *model.Feedback) (int64, error) {
	m.saved = append(m.saved, *f)
	return int64(len(m.saved)), nil
}

func TestFeedbackSubmit(t *testing.T) {
	store := &memFeedbackStore{}
	svc := NewFeedbackService(store)
	ctx := context.Background()

	id, err := svc.Submit(ctx, FeedbackInput{
		Name: "Ерлан", Contact: "8 777 123 45 67", Topic: model.FeedbackQuestion,
		Message: "Как подключить Kaspi к магазину?", Consent: true, Lang: "ru",
	})
	if err != nil || id != 1 {
		t.Fatalf("Submit: id=%d err=%v", id, err)
	}
	if got := store.saved[0].Contact; got != "+77771234567" {
		t.Errorf("телефон не нормализован: %q", got)
	}

	if _, err := svc.Submit(ctx, FeedbackInput{
		Name: "Ерлан", Contact: "Ivan@Mail.KZ", Topic: model.FeedbackOther,
		Message: "Предлагаю добавить Halyk Market.", Consent: true,
	}); err != nil {
		t.Fatalf("email-контакт: %v", err)
	}
	if got := store.saved[1].Contact; got != "ivan@mail.kz" {
		t.Errorf("email не нормализован: %q", got)
	}
}

func TestFeedbackValidation(t *testing.T) {
	svc := NewFeedbackService(&memFeedbackStore{})

	_, err := svc.Submit(context.Background(), FeedbackInput{
		Name: "", Contact: "не контакт", Topic: "spam", Message: "коротко", Consent: false,
	})
	fields, ok := httpx.FieldErrors(err)
	if !ok {
		t.Fatalf("ожидалась ошибка полей, получено %v", err)
	}
	want := map[string]string{"name": "required", "contact": "contact", "topic": "oneof", "message": "min", "consent": "consent"}
	for field, tag := range want {
		if fields[field].Tag != tag {
			t.Errorf("поле %s: тег %q, ожидался %q", field, fields[field].Tag, tag)
		}
	}
}
