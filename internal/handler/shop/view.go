package shop

import (
	"log/slog"
	"net/http"

	"nado_go/internal/i18n"
)

// baseData собирает переменные, общие для всех страниц витрины.
func (h *Handler) baseData(r *http.Request, titleKey string) map[string]any {
	ctx := r.Context()
	store := StoreFrom(ctx)
	l := i18n.FromContext(ctx)
	lang := string(l.Lang())
	prefix := PrefixFrom(ctx)

	// Ссылки строятся от LangBase: язык добавляется префиксом, кроме языка
	// магазина по умолчанию (для чистых канонических адресов).
	langBase := prefix
	if lang != store.DefaultLang {
		langBase = prefix + "/" + lang
	}

	title := ""
	if titleKey != "" {
		title = l.T(titleKey)
	}

	data := map[string]any{
		"Store":     store,
		"L":         l,
		"Lang":      lang,
		"Prefix":    prefix,
		"LangBase":  langBase,
		"Currency":  store.BaseCurrency,
		"Title":     title,
		"ThemeMode": themeMode(r),
		"Customer":  CustomerFrom(ctx),
		"CartCount": CartCountFrom(ctx),
		"Query":     "",
	}
	// Канонический адрес — на основном домене магазина (только когда он известен,
	// т.е. в проде с выбором по Host).
	if store.PrimaryHost != "" {
		langPart := ""
		if lang != store.DefaultLang {
			langPart = "/" + lang
		}
		data["Canonical"] = "https://" + store.PrimaryHost + langPart + r.URL.Path
	}
	return data
}

// render рендерит страницу темы магазина, логируя ошибку рендера.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, page string, data map[string]any) {
	store := StoreFrom(r.Context())
	if err := h.Render.Render(w, status, store.ThemeCode, page, data); err != nil {
		h.Log.Error("витрина: ошибка рендера", slog.String("page", page), slog.Any("error", err))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// renderNotFound отдаёт страницу 404 темы магазина.
func (h *Handler) renderNotFound(w http.ResponseWriter, r *http.Request) {
	data := h.baseData(r, "shop.notfound_title")
	h.render(w, r, http.StatusNotFound, "404", data)
}

// baseLocalizer — локализатор языка страницы из контекста.
func baseLocalizer(r *http.Request) *i18n.Localizer {
	return i18n.FromContext(r.Context())
}

// themeMode читает выбранный режим темы из cookie (system по умолчанию).
func themeMode(r *http.Request) string {
	if c, err := r.Cookie("nado_theme"); err == nil && (c.Value == "light" || c.Value == "dark") {
		return c.Value
	}
	return "system"
}
