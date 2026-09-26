package shop

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
)

// robots — GET /robots.txt: разрешает обход и указывает карту сайта.
func (h *Handler) robots(w http.ResponseWriter, r *http.Request) {
	base := publicBase(r)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nSitemap: %s/sitemap.xml\n", base)
}

// urlEntry — запись карты сайта.
type urlEntry struct {
	Loc string `xml:"loc"`
}

type urlSet struct {
	XMLName xml.Name   `xml:"urlset"`
	NS      string     `xml:"xmlns,attr"`
	URLs    []urlEntry `xml:"url"`
}

// sitemap — GET /sitemap.xml: главная, категории и товары магазина.
func (h *Handler) sitemap(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	lang := langOf(r)
	base := publicBase(r)

	set := urlSet{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	set.URLs = append(set.URLs, urlEntry{Loc: base + "/"})

	if cats, err := h.Catalog.Categories(r.Context(), store.ID, lang, store.DefaultLang); err == nil {
		for _, c := range cats {
			set.URLs = append(set.URLs, urlEntry{Loc: base + "/c/" + c.Slug + "-" + strconv.FormatInt(c.ID, 10)})
		}
	}
	// Первая страница товаров (для больших каталогов нужен индекс — отдельный шаг).
	if products, _, err := h.Catalog.Products(r.Context(), store.ID, lang, store.DefaultLang, 0, "", 1, 1000); err == nil {
		for _, p := range products {
			set.URLs = append(set.URLs, urlEntry{Loc: base + "/p/" + p.Slug + "-" + strconv.FormatInt(p.ID, 10)})
		}
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(set)
}

// publicBase — базовый URL витрины для абсолютных ссылок (основной домен в проде,
// иначе — по запросу с префиксом разработки).
func publicBase(r *http.Request) string {
	store := StoreFrom(r.Context())
	if store.PrimaryHost != "" {
		return "https://" + store.PrimaryHost
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + PrefixFrom(r.Context())
}
