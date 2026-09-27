package service

import (
	"testing"
	"time"
)

func TestMilestoneFor(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		end      time.Time
		wantDays int
		wantOK   bool
	}{
		{"через 3 дня", now.Add(3 * 24 * time.Hour), 3, true},
		{"через ~2.5 дня → 3", now.Add(60 * time.Hour), 3, true},
		{"через 2 дня — нет вехи", now.Add(2 * 24 * time.Hour), 0, false},
		{"через 1 день", now.Add(24 * time.Hour), 1, true},
		{"через 12 часов → 1", now.Add(12 * time.Hour), 1, true},
		{"в день окончания", now, 0, true},
		{"уже истёк", now.Add(-12 * time.Hour), 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			days, ok := milestoneFor(c.end, now)
			if ok != c.wantOK || (ok && days != c.wantDays) {
				t.Errorf("milestoneFor = (%d, %v), ожидалось (%d, %v)", days, ok, c.wantDays, c.wantOK)
			}
		})
	}
}
