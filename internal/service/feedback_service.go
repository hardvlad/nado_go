package service

import (
	"context"
	"log/slog"
	"strings"

	"nado_go/internal/httpx"
	"nado_go/internal/model"
)

// FeedbackStore — хранилище обращений.
type FeedbackStore interface {
	Create(ctx context.Context, f *model.Feedback) (int64, error)
}

// FeedbackInput — форма обратной связи.
type FeedbackInput struct {
	Name    string     `validate:"required,min=2,max=150"`
	Contact string     `validate:"required,max=255"`
	Topic   string     `validate:"required,oneof=question suggestion partnership other"`
	Message string     `validate:"required,min=10,max=4000"`
	Consent bool       `validate:"required"`
	Lang    string     `validate:"-"`
	Client  ClientMeta `validate:"-"`
}

type FeedbackService struct {
	store FeedbackStore
}

func NewFeedbackService(store FeedbackStore) *FeedbackService {
	return &FeedbackService{store: store}
}

// Submit проверяет и сохраняет обращение.
func (s *FeedbackService) Submit(ctx context.Context, in FeedbackInput) (int64, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Contact = strings.TrimSpace(in.Contact)
	in.Message = strings.TrimSpace(in.Message)

	fields, err := httpx.CheckFields(in)
	if err != nil {
		return 0, err
	}
	if fields == nil {
		fields = make(map[string]httpx.FieldError)
	}
	if _, ok := fields["consent"]; ok {
		fields["consent"] = httpx.FieldError{Tag: "consent"}
	}

	// Контакт — email или телефон: ответить нужно хоть куда-то.
	if _, bad := fields["contact"]; !bad {
		contact, ok := normalizeContact(in.Contact)
		if !ok {
			fields["contact"] = httpx.FieldError{Tag: "contact"}
		}
		in.Contact = contact
	}
	if len(fields) > 0 {
		return 0, httpx.ErrFields(fields)
	}

	id, err := s.store.Create(ctx, &model.Feedback{
		Name:      in.Name,
		Contact:   in.Contact,
		Topic:     in.Topic,
		Message:   in.Message,
		Lang:      in.Lang,
		IP:        in.Client.IP,
		UserAgent: in.Client.UserAgent,
	})
	if err != nil {
		return 0, httpx.ErrInternal(err)
	}

	// Уведомления команде пока нет — обращения видны в таблице и в логе.
	httpx.Logger(ctx).Info("новое обращение с формы обратной связи",
		slog.Int64("feedback_id", id), slog.String("topic", in.Topic))
	return id, nil
}

// normalizeContact принимает email или телефон и приводит к единому виду.
func normalizeContact(s string) (string, bool) {
	if strings.Contains(s, "@") {
		email := strings.ToLower(s)
		type emailOnly struct {
			V string `validate:"email"`
		}
		if fields, err := httpx.CheckFields(emailOnly{V: email}); err != nil || fields != nil {
			return "", false
		}
		return email, true
	}
	return NormalizePhone(s)
}
