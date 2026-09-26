package shop

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"nado_go/internal/model"
)

const perPage = 24

// category — GET /c/{slug}: товары категории. slug оканчивается на -{id}.
func (h *Handler) category(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	lang := langOf(r)
	id, ok := idFromSlug(chiURLParam(r, "slug"))
	if !ok {
		h.renderNotFound(w, r)
		return
	}
	page := pageParam(r)

	products, total, err := h.Catalog.Products(r.Context(), store.ID, lang, store.DefaultLang, id, "", page, perPage)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// Название категории берём из списка категорий (их немного).
	title := ""
	if cats, err := h.Catalog.Categories(r.Context(), store.ID, lang, store.DefaultLang); err == nil {
		for _, c := range cats {
			if c.ID == id {
				title = c.Name
				break
			}
		}
	}
	if title == "" {
		title = h.tr(r, "shop.category_title")
	}

	data := h.baseData(r, "")
	data["Title"] = title
	data["Heading"] = title
	data["Products"] = products
	base := langBaseOf(r) + "/c/" + chiURLParam(r, "slug") + "?page="
	addPagination(data, page, total, base)
	h.render(w, r, http.StatusOK, "catalog", data)
}

// search — GET /search?q=.
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	lang := langOf(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page := pageParam(r)

	var (
		products []model.Product
		total    int
		err      error
	)
	if query != "" {
		products, total, err = h.Catalog.Products(r.Context(), store.ID, lang, store.DefaultLang, 0, query, page, perPage)
		if err != nil {
			h.fail(w, r, err)
			return
		}
	}

	data := h.baseData(r, "shop.search_results")
	data["Heading"] = h.tr(r, "shop.search_results")
	data["Query"] = query
	data["Products"] = products
	data["Total"] = total
	base := langBaseOf(r) + "/search?q=" + url.QueryEscape(query) + "&page="
	addPagination(data, page, total, base)
	h.render(w, r, http.StatusOK, "catalog", data)
}

// product — GET /p/{slug}: карточка товара. slug оканчивается на -{id}.
func (h *Handler) product(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	lang := langOf(r)
	id, ok := idFromSlug(chiURLParam(r, "slug"))
	if !ok {
		h.renderNotFound(w, r)
		return
	}
	p, err := h.Catalog.Product(r.Context(), store.ID, id, lang, store.DefaultLang)
	if err != nil {
		h.renderNotFound(w, r)
		return
	}

	data := h.baseData(r, "")
	data["Title"] = p.Title
	data["Description"] = p.SEODesc
	data["Product"] = p
	h.render(w, r, http.StatusOK, "product", data)
}

// tr — перевод по ключу для текущего языка.
func (h *Handler) tr(r *http.Request, key string) string {
	return baseLocalizer(r).T(key)
}

// idFromSlug извлекает id из хвоста slug "...-123".
func idFromSlug(slug string) (int64, bool) {
	i := strings.LastIndex(slug, "-")
	if i < 0 || i == len(slug)-1 {
		// возможно, slug — это просто число
		if id, err := strconv.ParseInt(slug, 10, 64); err == nil && id > 0 {
			return id, true
		}
		return 0, false
	}
	id, err := strconv.ParseInt(slug[i+1:], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// pageParam читает номер страницы (>=1) из ?page.
func pageParam(r *http.Request) int {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	return p
}

// addPagination кладёт в данные номера страниц для пагинатора.
func addPagination(data map[string]any, page, total int, base string) {
	pages := (total + perPage - 1) / perPage
	data["Page"] = page
	data["TotalPages"] = pages
	data["PageBase"] = base
	nums := make([]int, 0, pages)
	for i := 1; i <= pages; i++ {
		nums = append(nums, i)
	}
	data["PageNums"] = nums
}
