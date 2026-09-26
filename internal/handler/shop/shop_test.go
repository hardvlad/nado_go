package shop

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nado_go/internal/database"
	"nado_go/internal/i18n"
	"nado_go/internal/model"
	"nado_go/internal/service"
	"nado_go/internal/theme"
	webassets "nado_go/web"
)

type fakeCatalog struct {
	cats     []model.Category
	products []model.Product
	product  *model.Product
}

func (f fakeCatalog) Categories(context.Context, int64, string, string) ([]model.Category, error) {
	return f.cats, nil
}
func (f fakeCatalog) Products(context.Context, int64, string, string, int64, string, int, int) ([]model.Product, int, error) {
	return f.products, len(f.products), nil
}
func (f fakeCatalog) Product(context.Context, int64, int64, string, string) (*model.Product, error) {
	if f.product == nil {
		return nil, database.ErrNotFound
	}
	return f.product, nil
}

type fakeCart struct{ cart *model.Cart }

func (f fakeCart) Add(context.Context, int64, string, int64, int) error  { return nil }
func (f fakeCart) SetQty(context.Context, int64, string, int, int) error { return nil }
func (f fakeCart) Remove(context.Context, int64, string, int64) error    { return nil }
func (f fakeCart) View(context.Context, int64, string, string, string) (*model.Cart, error) {
	if f.cart == nil {
		return &model.Cart{}, nil
	}
	return f.cart, nil
}
func (f fakeCart) Count(context.Context, int64, string) int { return 0 }

type fakeOrders struct{ order *model.Order }

func (f fakeOrders) Checkout(context.Context, service.CheckoutInput) (*model.Order, error) {
	return f.order, nil
}
func (f fakeOrders) Order(context.Context, int64, int64, string) (*model.Order, error) {
	if f.order == nil {
		return nil, service.ErrOrderNotFound
	}
	return f.order, nil
}
func (f fakeOrders) CustomerOrders(context.Context, int64, int64) ([]model.Order, error) {
	return nil, nil
}

type fakeCustomers struct{ verified *model.Customer }

func (fakeCustomers) RequestCode(_ context.Context, _ int64, phone, _, _ string) (string, error) {
	return phone, nil
}
func (f fakeCustomers) Verify(_ context.Context, _, _ int64, _, _, _, _ string) (*model.Customer, string, error) {
	return f.verified, "tok", nil
}
func (fakeCustomers) LoadCustomer(context.Context, int64, string) *model.Customer { return nil }
func (fakeCustomers) Logout(context.Context, string) error                        { return nil }
func (fakeCustomers) DeleteAccount(context.Context, int64, int64) error           { return nil }
func (fakeCustomers) SessionTTL() time.Duration                                   { return time.Hour }

type fakeResolver struct {
	stores map[string]*model.StorefrontStore
}

func (f fakeResolver) ResolveBySlug(_ context.Context, slug string) (*model.StorefrontStore, error) {
	if s, ok := f.stores[slug]; ok {
		return s, nil
	}
	return nil, database.ErrNotFound
}
func (f fakeResolver) ResolveByHost(_ context.Context, host string) (*model.StorefrontStore, error) {
	if s, ok := f.stores[host]; ok {
		return s, nil
	}
	return nil, database.ErrNotFound
}

func testHandler(t *testing.T, stores map[string]*model.StorefrontStore) *Handler {
	return testHandlerFull(t, stores, fakeCatalog{}, fakeCart{}, fakeOrders{}, fakeCustomers{})
}

func testHandlerFull(t *testing.T, stores map[string]*model.StorefrontStore, cat Catalog, cart CartOps, ord OrderOps, cust CustomerAuth) *Handler {
	t.Helper()
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatal(err)
	}
	themesFS, err := webassets.Themes(false)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := theme.New(themesFS)
	if err != nil {
		t.Fatal(err)
	}
	notFound := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }
	return New(Deps{Stores: fakeResolver{stores: stores}, Catalog: cat, Cart: cart, Orders: ord, Customers: cust,
		Render: renderer, ThemeStatic: themesFS, I18n: bundle, NotFound: notFound,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func activeStores() map[string]*model.StorefrontStore {
	return map[string]*model.StorefrontStore{
		"acme": {ID: 1, AccountID: 1, Slug: "acme", Name: "Acme", Status: model.StoreStatusActive, DefaultLang: "ru", BaseCurrency: "KZT"},
	}
}

func TestDevHandlerResolvesStore(t *testing.T) {
	h := testHandler(t, map[string]*model.StorefrontStore{
		"acme": {ID: 1, Slug: "acme", Name: "Acme", Status: model.StoreStatusActive, DefaultLang: "ru", BaseCurrency: "KZT"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shop/acme/", nil)
	h.DevHandler()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("статус %d, тело %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Acme") {
		t.Errorf("на витрине нет имени магазина: %s", rec.Body.String())
	}
}

func TestDevHandlerUnknownStore(t *testing.T) {
	h := testHandler(t, map[string]*model.StorefrontStore{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shop/nope/", nil)
	h.DevHandler()(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("неизвестный магазин: ожидался 404, получен %d", rec.Code)
	}
}

func TestDevHandlerDisabledStore(t *testing.T) {
	h := testHandler(t, map[string]*model.StorefrontStore{
		"off": {ID: 2, Slug: "off", Name: "Off", Status: model.StoreStatusDisabled, DefaultLang: "ru"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shop/off/", nil)
	h.DevHandler()(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("выключенный магазин: ожидался 404, получен %d", rec.Code)
	}
}

func TestHostHandlerResolvesByHost(t *testing.T) {
	h := testHandler(t, map[string]*model.StorefrontStore{
		"acme.nado.kz": {ID: 1, Slug: "acme", Name: "Acme", Status: model.StoreStatusActive, DefaultLang: "ru"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "acme.nado.kz:443"
	h.HostHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Acme") {
		t.Fatalf("витрина по Host: статус %d, тело %s", rec.Code, rec.Body.String())
	}
}

func TestSplitLang(t *testing.T) {
	cases := []struct {
		path, def string
		wantLang  i18n.Lang
		wantPath  string
	}{
		{"/c/foo", "ru", i18n.Lang("ru"), "/c/foo"},
		{"/kk/c/foo", "ru", i18n.Lang("kk"), "/c/foo"},
		{"/en/", "ru", i18n.Lang("en"), "/"},
		{"/p/x-1", "kk", i18n.Lang("kk"), "/p/x-1"}, // без префикса → язык магазина
	}
	for _, c := range cases {
		lang, path := splitLang(c.path, c.def)
		if lang != c.wantLang || path != c.wantPath {
			t.Errorf("splitLang(%q,%q) = (%q,%q), ожидалось (%q,%q)", c.path, c.def, lang, path, c.wantLang, c.wantPath)
		}
	}
}

func TestLoginFlow(t *testing.T) {
	h := testHandler(t, map[string]*model.StorefrontStore{
		"acme": {ID: 1, Slug: "acme", Name: "Acme", Status: model.StoreStatusActive, DefaultLang: "ru", BaseCurrency: "KZT"},
	})

	// GET формы входа.
	rec := httptest.NewRecorder()
	h.DevHandler()(rec, httptest.NewRequest(http.MethodGet, "/shop/acme/login", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "phone") {
		t.Fatalf("форма входа: статус %d", rec.Code)
	}

	// POST телефона → шаг ввода кода.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shop/acme/login", strings.NewReader("phone=%2B77001234567"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	h.DevHandler()(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "code") {
		t.Fatalf("шаг кода: статус %d, тело %s", rec.Code, rec.Body.String())
	}
}

func TestNormalizeHost(t *testing.T) {
	if got := normalizeHost("Acme.Nado.KZ:8080"); got != "acme.nado.kz" {
		t.Errorf("normalizeHost = %q", got)
	}
}

func TestProductPage(t *testing.T) {
	cat := fakeCatalog{product: &model.Product{
		ID: 5, Title: "Телефон", Slug: "telefon", VariantID: 9, PriceMinor: 250000, Currency: "KZT",
		Available: true, Images: []string{"https://cdn/x.jpg"},
	}}
	h := testHandlerFull(t, activeStores(), cat, fakeCart{}, fakeOrders{}, fakeCustomers{})
	rec := httptest.NewRecorder()
	h.DevHandler()(rec, httptest.NewRequest(http.MethodGet, "/shop/acme/p/telefon-5", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("карточка товара: статус %d, тело %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Телефон") || !strings.Contains(body, "cart/add") {
		t.Errorf("на карточке нет названия или кнопки в корзину: %s", body)
	}
}

func TestProductNotFound(t *testing.T) {
	h := testHandlerFull(t, activeStores(), fakeCatalog{}, fakeCart{}, fakeOrders{}, fakeCustomers{})
	rec := httptest.NewRecorder()
	h.DevHandler()(rec, httptest.NewRequest(http.MethodGet, "/shop/acme/p/nope-999", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("несуществующий товар: ожидался 404, получен %d", rec.Code)
	}
}

func TestCategoryPage(t *testing.T) {
	cat := fakeCatalog{
		cats:     []model.Category{{ID: 3, Name: "Электроника", Slug: "elektronika"}},
		products: []model.Product{{ID: 5, Title: "Телефон", Slug: "telefon", VariantID: 9, PriceMinor: 250000, Currency: "KZT", Available: true}},
	}
	h := testHandlerFull(t, activeStores(), cat, fakeCart{}, fakeOrders{}, fakeCustomers{})
	rec := httptest.NewRecorder()
	h.DevHandler()(rec, httptest.NewRequest(http.MethodGet, "/shop/acme/c/elektronika-3", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Электроника") {
		t.Fatalf("категория: статус %d, тело %s", rec.Code, rec.Body.String())
	}
}

func TestCartPageEmpty(t *testing.T) {
	h := testHandlerFull(t, activeStores(), fakeCatalog{}, fakeCart{}, fakeOrders{}, fakeCustomers{})
	rec := httptest.NewRecorder()
	h.DevHandler()(rec, httptest.NewRequest(http.MethodGet, "/shop/acme/cart", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("корзина: статус %d", rec.Code)
	}
}

func TestOrderPage(t *testing.T) {
	ord := fakeOrders{order: &model.Order{
		Number: 1001, Token: "tok", Status: model.OrderAwaitingPayment, TotalMinor: 250000, Currency: "KZT",
		Items: []model.OrderItem{{Title: "Телефон", Qty: 1, PriceMinor: 250000, LineMinor: 250000}},
	}}
	h := testHandlerFull(t, activeStores(), fakeCatalog{}, fakeCart{}, ord, fakeCustomers{})
	rec := httptest.NewRecorder()
	h.DevHandler()(rec, httptest.NewRequest(http.MethodGet, "/shop/acme/order/1001?t=tok", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "1001") {
		t.Fatalf("страница заказа: статус %d, тело %s", rec.Code, rec.Body.String())
	}
}
