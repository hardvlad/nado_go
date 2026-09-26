package service

import (
	"math"

	"github.com/shopspring/decimal"

	"nado_go/internal/model"
)

// Расчёт цены витрины из цены маркетплейса по правилам магазина (D-14).
// Правила: приоритет product > category > store; наценка (процент или сумма),
// затем округление до заданного шага. Ручная цена задаётся на уровне оффера и
// правилами не трогается (см. store_offers.price_mode).

// SelectPriceRule выбирает применимое правило для товара по приоритету
// product > category > store. Возвращает nil, если подходящих правил нет.
func SelectPriceRule(rules []model.PriceRule, productID, categoryID int64) *model.PriceRule {
	var byProduct, byCategory, byStore *model.PriceRule
	for i := range rules {
		r := &rules[i]
		if !r.Enabled {
			continue
		}
		switch r.Scope {
		case model.PriceScopeProduct:
			if r.ScopeID == productID && byProduct == nil {
				byProduct = r
			}
		case model.PriceScopeCategory:
			if r.ScopeID == categoryID && byCategory == nil {
				byCategory = r
			}
		case model.PriceScopeStore:
			if byStore == nil {
				byStore = r
			}
		}
	}
	switch {
	case byProduct != nil:
		return byProduct
	case byCategory != nil:
		return byCategory
	default:
		return byStore
	}
}

// ApplyPriceRule вычисляет цену витрины (в минорных единицах) из цены источника.
// nil-правило или база ≤ 0 → база без изменений. Деньги считаются в decimal,
// без float, затем округляются к заданному шагу.
func ApplyPriceRule(baseMinor int64, r *model.PriceRule) int64 {
	if baseMinor <= 0 || r == nil {
		return baseMinor
	}

	base := decimal.NewFromInt(baseMinor)
	var adjusted decimal.Decimal
	switch r.AdjustKind {
	case "amount":
		// adjust_value в основных единицах → переводим в минорные (×100).
		adjusted = base.Add(decimal.NewFromFloat(r.AdjustValue).Mul(decimal.NewFromInt(100)))
	default: // percent
		factor := decimal.NewFromInt(100).Add(decimal.NewFromFloat(r.AdjustValue)).Div(decimal.NewFromInt(100))
		adjusted = base.Mul(factor)
	}
	if adjusted.IsNegative() {
		adjusted = decimal.Zero
	}

	minor := adjusted.Round(0).IntPart()
	return roundMinor(minor, r.RoundTo, r.RoundMode)
}

// roundMinor округляет минорную сумму к шагу roundTo (в основных единицах).
// roundTo=1 и шаг в минорных единицах = 100; roundTo=10 → 1000 и т.д.
func roundMinor(minor, roundTo int64, mode string) int64 {
	if roundTo <= 1 {
		return minor
	}
	step := roundTo * 100 // основные → минорные
	if step <= 0 {
		return minor
	}
	q := float64(minor) / float64(step)
	var mult float64
	switch mode {
	case "up":
		mult = math.Ceil(q)
	case "down":
		mult = math.Floor(q)
	default: // nearest
		mult = math.Round(q)
	}
	return int64(mult) * step
}
