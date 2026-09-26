package shop

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"nado_go/internal/i18n"
	"nado_go/internal/model"
	"nado_go/internal/service"
	"nado_go/internal/theme"
)

const storeCacheTTL = 60 * time.Second

// Catalog — чтение каталога витрины (реализуется service.StorefrontService).
type Catalog interface {
	Categories(ctx context.Context, storeID int64, lang, defLang string) ([]model.Category, error)
	Products(ctx context.Context, storeID int64, lang, defLang string, categoryID int64, search string, page, perPage int) ([]model.Product, int, error)
	Product(ctx context.Context, storeID, id int64, lang, defLang string) (*model.Product, error)
}

// CustomerAuth — вход покупателя по телефону (реализуется service.CustomerAuthService).
type CustomerAuth interface {
	RequestCode(ctx context.Context, storeID int64, rawPhone, lang, ip string) (string, error)
	Verify(ctx context.Context, storeID, accountID int64, rawPhone, code, name, ua string) (*model.Customer, string, error)
	LoadCustomer(ctx context.Context, storeID int64, token string) *model.Customer
	Logout(ctx context.Context, token string) error
	DeleteAccount(ctx context.Context, storeID, customerID int64) error
	SessionTTL() time.Duration
}

// CartOps — операции корзины (реализуется service.CartService).
type CartOps interface {
	Add(ctx context.Context, storeID int64, token string, variantID int64, qty int) error
	SetQty(ctx context.Context, storeID int64, token string, variantID, qty int) error
	Remove(ctx context.Context, storeID int64, token string, variantID int64) error
	View(ctx context.Context, storeID int64, token, lang, defLang string) (*model.Cart, error)
	Count(ctx context.Context, storeID int64, token string) int
}

// OrderOps — оформление и просмотр заказов (реализуется service.OrderService).
type OrderOps interface {
	Checkout(ctx context.Context, in service.CheckoutInput) (*model.Order, error)
	Order(ctx context.Context, storeID, number int64, token string) (*model.Order, error)
	CustomerOrders(ctx context.Context, storeID, customerID int64) ([]model.Order, error)
}

// Deps — зависимости витрины.
type Deps struct {
	Stores      Resolver
	Catalog     Catalog
	Customers   CustomerAuth
	Cart        CartOps
	Orders      OrderOps
	Render      *theme.Renderer
	ThemeStatic fs.FS // статика тем (web/themes), отдаётся под /static
	I18n        *i18n.Bundle
	NotFound    http.HandlerFunc // 404 платформы, когда магазин не найден/выключен
	Prod        bool             // прод: Secure-cookie и HSTS
	Log         *slog.Logger
}

// Handler обслуживает витрину магазина.
type Handler struct {
	Deps
	cache *storeCache
	pages http.Handler
}

func New(d Deps) *Handler {
	h := &Handler{Deps: d, cache: newStoreCache(d.Stores, storeCacheTTL)}
	h.pages = h.buildRoutes()
	return h
}

// buildRoutes собирает маршруты витрины (матчинг по очищенному пути).
func (h *Handler) buildRoutes() http.Handler {
	r := chi.NewRouter()

	// Статика тем: /static/... отдаётся из web/themes/base/static (и переопределений).
	// Без строгих заголовков и до CSRF — это обычные файлы.
	if h.ThemeStatic != nil {
		fileServer := http.StripPrefix("/static/", http.FileServerFS(themeStaticFS{h.ThemeStatic}))
		r.Handle("/static/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=3600")
			fileServer.ServeHTTP(w, req)
		}))
	}

	// Заголовки безопасности и защита форм. Витрина в проде обслуживается по Host
	// вне общей группы платформы, поэтому ставит их сама.
	r.Group(func(r chi.Router) {
		r.Use(h.securityHeaders)
		cop := http.NewCrossOriginProtection()
		cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			h.renderNotFound(w, req) // отказ CSRF — безопасная заглушка
		}))
		r.Use(cop.Handler)

		r.Get("/", h.home)
		r.Get("/robots.txt", h.robots)
		r.Get("/sitemap.xml", h.sitemap)
		r.Get("/c/{slug}", h.category)
		r.Get("/p/{slug}", h.product)
		r.Get("/search", h.search)

		r.Get("/cart", h.cartPage)
		r.Post("/cart/add", h.cartAdd)
		r.Post("/cart/update", h.cartUpdate)
		r.Post("/cart/remove", h.cartRemove)

		r.Get("/checkout", h.checkoutPage)
		r.Post("/checkout", h.placeOrder)
		r.Get("/order/{number}", h.orderPage)

		r.Get("/login", h.loginForm)
		r.Post("/login", h.requestCode)
		r.Post("/login/verify", h.verifyCode)
		r.Post("/logout", h.logout)
		r.Get("/account", h.account)
		r.Post("/account/delete", h.deleteAccount)
	})
	return r
}

// securityHeaders задаёт заголовки витрины. CSP разрешает внешние картинки
// (фото товаров с CDN маркетплейса), но скрипты и стили — только свои.
func (h *Handler) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		head := w.Header()
		head.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: https:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'")
		head.Set("X-Content-Type-Options", "nosniff")
		head.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if h.Prod {
			head.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// home — главная витрины: категории и последние товары.
func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	store := StoreFrom(r.Context())
	lang := langOf(r)

	cats, err := h.Catalog.Categories(r.Context(), store.ID, lang, store.DefaultLang)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	products, _, err := h.Catalog.Products(r.Context(), store.ID, lang, store.DefaultLang, 0, "", 1, 24)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	data := h.baseData(r, "shop.home_title")
	data["Categories"] = cats
	data["Products"] = products
	h.render(w, r, http.StatusOK, "home", data)
}

// fail логирует ошибку и отдаёт 500 (страницей темы, если возможно).
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.Log.Error("витрина: ошибка обработки", slog.Any("error", err))
	data := h.baseData(r, "shop.error_title")
	h.render(w, r, http.StatusInternalServerError, "404", data)
}

// chiURLParam — доступ к параметрам пути chi из других файлов пакета.
func chiURLParam(r *http.Request, key string) string { return chi.URLParam(r, key) }

// langOf возвращает язык страницы из контекста.
func langOf(r *http.Request) string {
	if l := i18n.FromContext(r.Context()); l != nil {
		return string(l.Lang())
	}
	return string(i18n.Default)
}

// DevHandler обслуживает витрину локально по префиксу /shop/{slug}/... .
func (h *Handler) DevHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/shop/")
		slug, remainder, _ := strings.Cut(rest, "/")
		if slug == "" {
			h.storeNotFound(w, r)
			return
		}
		store, err := h.cache.bySlugKey(r.Context(), slug)
		if err != nil || store == nil || !store.Active() {
			h.storeNotFound(w, r)
			return
		}
		h.serve(w, r, store, "/"+remainder, "/shop/"+slug)
	}
}

// HostHandler обслуживает витрину в проде: магазин выбирается по домену запроса.
func (h *Handler) HostHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store, err := h.cache.byHostKey(r.Context(), normalizeHost(r.Host))
		if err != nil || store == nil || !store.Active() {
			h.storeNotFound(w, r)
			return
		}
		h.serve(w, r, store, r.URL.Path, "")
	})
}

// serve кладёт магазин/язык/префикс в контекст и передаёт запрос маршрутам.
func (h *Handler) serve(w http.ResponseWriter, r *http.Request, store *model.StorefrontStore, path, prefix string) {
	lang, cleanPath := splitLang(path, store.DefaultLang)

	ctx := WithStore(r.Context(), store)
	ctx = WithPrefix(ctx, prefix)
	ctx = i18n.WithLocalizer(ctx, h.I18n.Localizer(lang))

	// Вошедший покупатель (по cookie сессии витрины) — для шапки и checkout.
	if h.Customers != nil {
		if c, err := r.Cookie(customerCookie); err == nil {
			if customer := h.Customers.LoadCustomer(ctx, store.ID, c.Value); customer != nil {
				ctx = WithCustomer(ctx, customer)
			}
		}
	}
	// Число позиций в корзине для бейджа шапки.
	if h.Cart != nil {
		if c, err := r.Cookie(cartCookie); err == nil {
			ctx = WithCartCount(ctx, h.Cart.Count(ctx, store.ID, c.Value))
		}
	}
	ctx = context.WithValue(ctx, chi.RouteCtxKey, chi.NewRouteContext())

	r2 := r.Clone(ctx)
	if cleanPath == "" {
		cleanPath = "/"
	}
	r2.URL.Path = cleanPath
	h.pages.ServeHTTP(w, r2)
}

// splitLang выделяет язык из первого сегмента пути (/kk, /en, /ru) и убирает его.
func splitLang(path, defLang string) (i18n.Lang, string) {
	trimmed := strings.TrimPrefix(path, "/")
	first, remainder, _ := strings.Cut(trimmed, "/")
	if l, ok := i18n.Parse(first); ok {
		return l, "/" + remainder
	}
	lang, ok := i18n.Parse(defLang)
	if !ok {
		lang = i18n.Default
	}
	return lang, path
}

// storeNotFound отдаёт страницу платформы, если магазин не найден/выключен.
func (h *Handler) storeNotFound(w http.ResponseWriter, r *http.Request) {
	if h.NotFound != nil {
		h.NotFound(w, r)
		return
	}
	http.NotFound(w, r)
}

// themeStaticFS отдаёт статику базовой темы: путь css/... ищется в base/static/... .
type themeStaticFS struct{ fsys fs.FS }

func (t themeStaticFS) Open(name string) (fs.File, error) {
	// Пока одна тема — статика берётся из base. Переопределения тем добавим позже.
	return t.fsys.Open("base/static/" + name)
}
