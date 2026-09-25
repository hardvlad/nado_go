package repository

import (
	"context"
	"database/sql"
	"fmt"

	"nado_go/internal/database"
	"nado_go/internal/model"
)

// FeedbackRepository — обращения с формы обратной связи.
type FeedbackRepository struct {
	db *database.DB
}

func NewFeedbackRepository(db *database.DB) *FeedbackRepository {
	return &FeedbackRepository{db: db}
}

// Create сохраняет обращение и возвращает его id.
func (r *FeedbackRepository) Create(ctx context.Context, f *model.Feedback) (int64, error) {
	ctx, cancel := r.db.Context(ctx)
	defer cancel()

	const query = `
		INSERT INTO dbo.feedback_messages (name, contact, topic, message, lang, ip, user_agent)
		OUTPUT INSERTED.id
		VALUES (@name, @contact, @topic, @message, @lang, @ip, @user_agent);`

	var id int64
	err := r.db.QueryRowContext(ctx, query,
		sql.Named("name", f.Name),
		sql.Named("contact", f.Contact),
		sql.Named("topic", f.Topic),
		sql.Named("message", f.Message),
		sql.Named("lang", f.Lang),
		sql.Named("ip", nullString(f.IP)),
		sql.Named("user_agent", nullString(truncate(f.UserAgent, 400))),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("repository: сохранение обращения: %w", database.MapError(err))
	}
	return id, nil
}
