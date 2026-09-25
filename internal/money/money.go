// Package money — денежные суммы в минорных единицах (тиынах, копейках).
//
// Никаких float: 0.1 + 0.2 в двоичной арифметике не равно 0.3, а в деньгах
// такие ошибки превращаются в расхождения с банком и кассой.
package money

import (
	"strconv"
	"strings"
)

// Currency — код валюты ISO 4217.
type Currency string

const (
	KZT Currency = "KZT"
	RUB Currency = "RUB"
)

// Symbol — знак валюты для витрины.
func (c Currency) Symbol() string {
	switch c {
	case KZT:
		return "₸"
	case RUB:
		return "₽"
	default:
		return string(c)
	}
}

// minorDigits — число знаков после запятой. У тенге и рубля их два.
func (c Currency) minorDigits() int { return 2 }

// Money — сумма в минорных единицах валюты.
type Money struct {
	Minor    int64
	Currency Currency
}

// FromMajor создаёт сумму из целых основных единиц: FromMajor(20000, KZT) — 20 000 ₸.
func FromMajor(major int64, c Currency) Money {
	scale := int64(1)
	for range c.minorDigits() {
		scale *= 10
	}
	return Money{Minor: major * scale, Currency: c}
}

// Format возвращает сумму для показа на языке интерфейса:
// ru/kk — «20 000 ₸», en — «₸20,000». Дробная часть выводится, только если
// она не нулевая: «1 990,50 ₸».
//
// Разделитель разрядов — неразрывный пробел: сумма не должна переноситься
// на две строки посреди числа.
func (m Money) Format(lang string) string {
	neg := m.Minor < 0
	minor := m.Minor
	if neg {
		minor = -minor
	}

	scale := int64(1)
	for range m.Currency.minorDigits() {
		scale *= 10
	}
	major, frac := minor/scale, minor%scale

	thousands, decimal := " ", ","
	if lang == "en" {
		thousands, decimal = ",", "."
	}

	var b strings.Builder
	if neg {
		b.WriteString("−")
	}
	if lang == "en" {
		b.WriteString(m.Currency.Symbol())
	}
	b.WriteString(groupDigits(strconv.FormatInt(major, 10), thousands))
	if frac != 0 {
		b.WriteString(decimal)
		fs := strconv.FormatInt(frac, 10)
		for len(fs) < m.Currency.minorDigits() {
			fs = "0" + fs
		}
		b.WriteString(fs)
	}
	if lang != "en" {
		b.WriteString(" ")
		b.WriteString(m.Currency.Symbol())
	}
	return b.String()
}

// groupDigits расставляет разделитель разрядов: "1234567" → "1 234 567".
func groupDigits(s, sep string) string {
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	head := len(s) % 3
	if head > 0 {
		b.WriteString(s[:head])
	}
	for i := head; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteString(sep)
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
