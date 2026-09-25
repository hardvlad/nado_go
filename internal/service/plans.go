package service

import (
	"nado_go/internal/model"
	"nado_go/internal/money"
)

// PlanCatalog — список тарифов платформы.
//
// Пока тарифы описаны в коде: биллинга нет, а цены и состав меняются вместе
// с релизом. Когда появится биллинг (дорожная карта, этап 11), каталог
// переедет в таблицу plans, а интерфейс сервиса останется прежним.
type PlanCatalog struct {
	plans []model.Plan
}

// NewPlanCatalog возвращает тарифы в порядке показа — от младшего к старшему.
func NewPlanCatalog() *PlanCatalog {
	return &PlanCatalog{plans: []model.Plan{
		{
			Code:  model.PlanStart,
			Price: money.FromMajor(20000, money.KZT),
			Features: []model.PlanFeature{
				{Key: "pf.no_commission", Included: true},
				{Key: "pf.stores_1", Included: true},
				{Key: "pf.products_1000", Included: true},
				{Key: "pf.marketplaces_1", Included: true},
				{Key: "pf.sync_60", Included: true},
				{Key: "pf.subdomain", Included: true},
				{Key: "pf.custom_domain"},
				{Key: "pf.price_rules"},
				{Key: "pf.app"},
			},
		},
		{
			Code:    model.PlanBusiness,
			Price:   money.FromMajor(30000, money.KZT),
			Popular: true,
			Features: []model.PlanFeature{
				{Key: "pf.no_commission", Included: true},
				{Key: "pf.stores_1", Included: true},
				{Key: "pf.products_10000", Included: true},
				{Key: "pf.marketplaces_3", Included: true},
				{Key: "pf.sync_30", Included: true},
				{Key: "pf.custom_domain", Included: true},
				{Key: "pf.price_rules", Included: true},
				{Key: "pf.all_themes", Included: true},
				{Key: "pf.app"},
			},
		},
		{
			Code:  model.PlanPro,
			Price: money.FromMajor(50000, money.KZT),
			Features: []model.PlanFeature{
				{Key: "pf.no_commission", Included: true},
				{Key: "pf.stores_3", Included: true},
				{Key: "pf.products_unlimited", Included: true},
				{Key: "pf.marketplaces_all", Included: true},
				{Key: "pf.sync_15", Included: true},
				{Key: "pf.custom_domain", Included: true},
				{Key: "pf.price_rules", Included: true},
				{Key: "pf.app", Included: true},
				{Key: "pf.priority_support", Included: true},
			},
		},
	}}
}

// List возвращает тарифы. Срез копируется: вызывающий не должен иметь
// возможности поменять каталог.
func (c *PlanCatalog) List() []model.Plan {
	out := make([]model.Plan, len(c.plans))
	copy(out, c.plans)
	return out
}

// Get находит тариф по коду.
func (c *PlanCatalog) Get(code string) (model.Plan, bool) {
	for _, p := range c.plans {
		if p.Code == code {
			return p, true
		}
	}
	return model.Plan{}, false
}

// Default — тариф, выбранный в форме регистрации, если продавец пришёл не
// со страницы тарифов.
func (c *PlanCatalog) Default() string { return model.PlanBusiness }
