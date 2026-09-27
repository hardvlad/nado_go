package model

import (
	"testing"
	"time"
)

func TestStoreTrial(t *testing.T) {
	var zero Store
	if zero.TrialActive() || zero.TrialDaysLeft() != 0 {
		t.Error("без trial_ends_at пробный период неактивен и дней 0")
	}

	future := Store{TrialEndsAt: time.Now().Add(72 * time.Hour)}
	if !future.TrialActive() {
		t.Error("будущий конец пробного — активен")
	}
	if d := future.TrialDaysLeft(); d != 3 {
		t.Errorf("через 72ч осталось дней = %d, ожидалось 3", d)
	}

	// Меньше суток, но ещё не истёк — округляем вверх до 1 дня.
	almost := Store{TrialEndsAt: time.Now().Add(2 * time.Hour)}
	if d := almost.TrialDaysLeft(); d != 1 {
		t.Errorf("через 2ч осталось дней = %d, ожидалось 1", d)
	}

	past := Store{TrialEndsAt: time.Now().Add(-time.Hour)}
	if past.TrialActive() || past.TrialDaysLeft() != 0 {
		t.Error("истёкший пробный — неактивен и 0 дней")
	}
}
