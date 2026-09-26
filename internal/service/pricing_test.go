package service

import (
	"testing"

	"nado_go/internal/model"
)

func TestApplyPriceRuleNil(t *testing.T) {
	if got := ApplyPriceRule(250000, nil); got != 250000 {
		t.Errorf("без правила цена не меняется: %d", got)
	}
}

func TestApplyPriceRulePercent(t *testing.T) {
	// -10% от 2500.00 = 2250.00, округление до 10 тг вниз.
	r := &model.PriceRule{AdjustKind: "percent", AdjustValue: -10, RoundTo: 10, RoundMode: "down"}
	if got := ApplyPriceRule(250000, r); got != 225000 {
		t.Errorf("−10%% с округлением = %d, ожидалось 225000", got)
	}
}

func TestApplyPriceRuleRoundingModes(t *testing.T) {
	// База 2437.00 (243700 тиын), +0%, округление до 10 тг.
	base := int64(243700)
	cases := []struct {
		mode string
		want int64
	}{
		{"down", 243000},    // 2430.00
		{"up", 244000},      // 2440.00
		{"nearest", 244000}, // 2437 → ближайшее 2440
	}
	for _, c := range cases {
		r := &model.PriceRule{AdjustKind: "percent", AdjustValue: 0, RoundTo: 10, RoundMode: c.mode}
		if got := ApplyPriceRule(base, r); got != c.want {
			t.Errorf("режим %s = %d, ожидалось %d", c.mode, got, c.want)
		}
	}
}

func TestApplyPriceRuleAmount(t *testing.T) {
	// +500 тенге к 2000.00 = 2500.00.
	r := &model.PriceRule{AdjustKind: "amount", AdjustValue: 500, RoundTo: 1, RoundMode: "nearest"}
	if got := ApplyPriceRule(200000, r); got != 250000 {
		t.Errorf("+500 = %d, ожидалось 250000", got)
	}
}

func TestApplyPriceRuleNeverNegative(t *testing.T) {
	r := &model.PriceRule{AdjustKind: "amount", AdjustValue: -9999, RoundTo: 1, RoundMode: "nearest"}
	if got := ApplyPriceRule(200000, r); got < 0 {
		t.Errorf("цена не должна быть отрицательной: %d", got)
	}
}

func TestSelectPriceRulePriority(t *testing.T) {
	rules := []model.PriceRule{
		{Scope: model.PriceScopeStore, AdjustValue: -5, Enabled: true},
		{Scope: model.PriceScopeCategory, ScopeID: 7, AdjustValue: -10, Enabled: true},
		{Scope: model.PriceScopeProduct, ScopeID: 42, AdjustValue: -15, Enabled: true},
	}
	// product побеждает.
	if r := SelectPriceRule(rules, 42, 7); r == nil || r.Scope != model.PriceScopeProduct {
		t.Errorf("ожидалось правило товара, получено %+v", r)
	}
	// нет правила товара → category.
	if r := SelectPriceRule(rules, 99, 7); r == nil || r.Scope != model.PriceScopeCategory {
		t.Errorf("ожидалось правило категории, получено %+v", r)
	}
	// ни товара, ни категории → store.
	if r := SelectPriceRule(rules, 99, 99); r == nil || r.Scope != model.PriceScopeStore {
		t.Errorf("ожидалось правило магазина, получено %+v", r)
	}
	// выключенные игнорируются.
	off := []model.PriceRule{{Scope: model.PriceScopeStore, Enabled: false}}
	if r := SelectPriceRule(off, 1, 1); r != nil {
		t.Errorf("выключенное правило не должно применяться: %+v", r)
	}
}
