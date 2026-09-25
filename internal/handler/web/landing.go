package web

import (
	"fmt"
	"net/http"

	"nado_go/internal/i18n"
	"nado_go/internal/model"
)

// Card — карточка преимущества или шага: иконка и тексты на языке страницы.
type Card struct {
	Icon  string
	Title string
	Text  string
}

// PlanView — тариф, подготовленный к показу.
type PlanView struct {
	Code     string
	Name     string
	Tagline  string
	Price    string
	Popular  bool
	URL      string // регистрация с выбранным тарифом
	Features []FeatureView
}

type FeatureView struct {
	Text     string
	Included bool
}

// QA — вопрос и ответ FAQ.
type QA struct {
	Q, A string
}

// cards собирает карточки из ключей prefix.1.title, prefix.1.text, ...
func cards(l *i18n.Localizer, prefix string, icons []string) []Card {
	out := make([]Card, len(icons))
	for i, icon := range icons {
		n := i + 1
		out[i] = Card{
			Icon:  icon,
			Title: l.T(fmt.Sprintf("%s.%d.title", prefix, n)),
			Text:  l.T(fmt.Sprintf("%s.%d.text", prefix, n)),
		}
	}
	return out
}

func (h *PageHandler) planViews(l *i18n.Localizer) []PlanView {
	lang := l.Lang()
	plans := h.Plans.List()
	out := make([]PlanView, len(plans))
	for i, p := range plans {
		out[i] = h.planView(l, p)
		out[i].URL = i18n.Localize(lang, "/register") + "?plan=" + p.Code
	}
	return out
}

func (h *PageHandler) planView(l *i18n.Localizer, p model.Plan) PlanView {
	features := make([]FeatureView, len(p.Features))
	for i, f := range p.Features {
		features[i] = FeatureView{Text: l.T(f.Key), Included: f.Included}
	}
	return PlanView{
		Code:     p.Code,
		Name:     l.T("plan." + p.Code + ".name"),
		Tagline:  l.T("plan." + p.Code + ".tagline"),
		Price:    p.Price.Format(string(l.Lang())),
		Popular:  p.Popular,
		Features: features,
	}
}

// Home — GET /: лендинг платформы.
func (h *PageHandler) Home(w http.ResponseWriter, r *http.Request) error {
	l := h.localizer(r)

	faq := make([]QA, 5)
	for i := range faq {
		faq[i] = QA{Q: l.T(fmt.Sprintf("faq.%d.q", i+1)), A: l.T(fmt.Sprintf("faq.%d.a", i+1))}
	}

	data := h.page(r, "home.title").
		With("Marketplaces", []string{l.T("mp.kaspi"), l.T("mp.wildberries"), l.T("mp.ozon"), l.T("mp.yandex")}).
		With("Steps", cards(l, "home.how", []string{"plug", "sliders", "cart"})).
		With("SellerBenefits", cards(l, "home.sellers", []string{"percent", "download", "refresh", "tag", "wallet", "star"})).
		With("BuyerBenefits", cards(l, "home.buyers", []string{"tag", "truck", "message", "globe"})).
		With("Plans", h.planViews(l)).
		With("FAQ", faq)

	return h.Render.Render(w, http.StatusOK, "home", data)
}

func (h *PageHandler) localizer(r *http.Request) *i18n.Localizer {
	if l := i18n.FromContext(r.Context()); l != nil {
		return l
	}
	lang, _ := i18n.SplitPath(r.URL.Path)
	return h.I18n.Localizer(lang)
}
