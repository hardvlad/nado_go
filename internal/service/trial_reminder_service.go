package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"nado_go/internal/i18n"
	"nado_go/internal/integration/messaging"
	"nado_go/internal/repository"
)

// TrialReminderService напоминает продавцу об оплате перед окончанием пробного
// периода магазина: за 3 дня, за 1 день и в день окончания шлёт в WhatsApp
// ссылку на оплату. Каждая веха отправляется один раз (dedup в trial_reminders).
type TrialReminderService struct {
	stores    *repository.StoreRepository
	sender    messaging.Sender
	bundle    *i18n.Bundle
	publicURL string
	log       *slog.Logger
	now       func() time.Time
}

func NewTrialReminderService(stores *repository.StoreRepository, sender messaging.Sender, bundle *i18n.Bundle, publicURL string, log *slog.Logger) *TrialReminderService {
	return &TrialReminderService{stores: stores, sender: sender, bundle: bundle, publicURL: publicURL, log: log, now: time.Now}
}

// Scan находит магазины у порога окончания пробного периода и шлёт напоминания.
func (s *TrialReminderService) Scan(ctx context.Context) error {
	now := s.now()
	// Окно кандидатов покрывает все вехи (3 дня до … день после конца).
	candidates, err := s.stores.DueTrials(ctx, now.Add(-2*24*time.Hour), now.Add(4*24*time.Hour))
	if err != nil {
		return err
	}
	for _, c := range candidates {
		days, ok := milestoneFor(c.TrialEndsAt, now)
		if !ok {
			continue
		}
		claimed, err := s.stores.ClaimTrialReminder(ctx, c.StoreID, days)
		if err != nil {
			s.log.Warn("напоминание: пометка вехи", slog.Int64("store_id", c.StoreID), slog.Any("error", err))
			continue
		}
		if !claimed {
			continue // уже отправляли эту веху
		}
		if err := s.send(ctx, c, days); err != nil {
			s.log.Warn("напоминание: отправка", slog.Int64("store_id", c.StoreID), slog.Any("error", err))
			// Отправка не удалась — снимаем пометку, чтобы повторить позже.
			_ = s.stores.ReleaseTrialReminder(ctx, c.StoreID, days)
			continue
		}
		s.log.Info("напоминание об оплате отправлено",
			slog.Int64("store_id", c.StoreID), slog.Int("days_before", days))
	}
	return nil
}

// send формирует и отправляет сообщение с ссылкой на оплату.
func (s *TrialReminderService) send(ctx context.Context, c repository.TrialCandidate, days int) error {
	lang, ok := i18n.Parse(c.OwnerLocale)
	if !ok {
		lang = i18n.Default
	}
	url := s.publicURL + i18n.Localize(lang, "/account")
	text := s.bundle.Localizer(lang).T(fmt.Sprintf("trial.reminder_%d", days), "Store", c.StoreName, "URL", url)
	_, err := s.sender.Send(ctx, messaging.Message{Phone: c.OwnerPhone, Text: text})
	return err
}

// milestoneFor возвращает веху напоминания (3/1/0) по числу оставшихся дней.
// Промежуточные значения (например, 2 дня) не дают напоминания.
func milestoneFor(end, now time.Time) (int, bool) {
	daysLeft := int(math.Ceil(end.Sub(now).Hours() / 24))
	switch {
	case daysLeft <= 0:
		return 0, true
	case daysLeft == 1:
		return 1, true
	case daysLeft == 3:
		return 3, true
	default:
		return 0, false
	}
}
