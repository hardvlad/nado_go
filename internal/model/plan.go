package model

import "nado_go/internal/money"

// Plan — тариф подписки продавца (D-06). Названия и описания не хранятся в
// модели: они переводятся по ключам plan.<code>.name и plan.<code>.tagline.
type Plan struct {
	Code     string
	Price    money.Money // за месяц
	Popular  bool        // выделяется на странице тарифов
	Features []PlanFeature
}

// PlanFeature — строка в карточке тарифа: ключ перевода и входит ли она в тариф.
type PlanFeature struct {
	Key      string
	Included bool
}

// Коды тарифов. Они же хранятся в accounts.plan_code.
const (
	PlanStart    = "start"
	PlanBusiness = "business"
	PlanPro      = "pro"
)
