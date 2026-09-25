package kaspicabinet

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestOfferItemNormalize(t *testing.T) {
	raw := `{
		"sku":"061077339","masterSku":"m-123","masterTitle":"Наушники X","model":"Наушники X белые",
		"brand":"Acme","masterCategory":"Master - Electronics","images":["https://cdn/1.jpg","https://cdn/2.jpg"],
		"available":true,"price":0,"minPrice":24990,
		"availabilities":[
			{"storeId":"30297603_PP1","available":"yes","preOrder":0,"stockSpecified":true,"stockCount":3},
			{"storeId":"30297603_PP2","available":"yes","preOrder":5,"stockSpecified":false,"stockCount":0},
			{"storeId":"30297603_PP3","available":"no","stockSpecified":true,"stockCount":10}
		],
		"stocks":[{"stockLevel":{"30297603_PP1":{"value":7}}}]
	}`
	var it offerItem
	if err := json.Unmarshal([]byte(raw), &it); err != nil {
		t.Fatal(err)
	}
	p := it.normalize()

	if p.SKU != "061077339" || p.MasterSKU != "m-123" {
		t.Errorf("коды: %q %q", p.SKU, p.MasterSKU)
	}
	if p.Title != "Наушники X белые" { // model приоритетнее title/masterTitle
		t.Errorf("title = %q", p.Title)
	}
	if p.PriceMinor != 2499000 { // price=0 → minPrice=24990 → тиыны
		t.Errorf("price = %d", p.PriceMinor)
	}
	if len(p.Stocks) != 2 { // только available=yes
		t.Fatalf("ожидалось 2 доступные точки, получено %d", len(p.Stocks))
	}
	// PP1: точный остаток из stocks (7), не stockCount (3).
	if p.Stocks[0].StoreCode != "PP1" || p.Stocks[0].Qty != 7 || !p.Stocks[0].Specified {
		t.Errorf("PP1 неверно: %+v", p.Stocks[0])
	}
	// PP2: остаток не ведётся, есть предзаказ.
	if p.Stocks[1].StoreCode != "PP2" || p.Stocks[1].Qty != 0 || p.Stocks[1].Specified || p.Stocks[1].PreOrder != 5 {
		t.Errorf("PP2 неверно: %+v", p.Stocks[1])
	}
	if len(p.Images) != 2 || len(p.Raw) == 0 {
		t.Errorf("фото/raw: %d, %d", len(p.Images), len(p.Raw))
	}
}

func TestProductsPaginate(t *testing.T) {
	// Сервер отдаёт активные (2 страницы) и снятые (1 страница) товары.
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active := r.URL.Query().Get("a") == "true"
		page := r.URL.Query().Get("p")
		hits.Add(1)
		switch {
		case active && page == "0":
			w.Write([]byte(`{"total":26,"data":[` + repeatItems(25) + `]}`))
		case active && page == "1":
			w.Write([]byte(`{"total":26,"data":[{"sku":"a26","price":100}]}`))
		case !active && page == "0":
			w.Write([]byte(`{"total":1,"data":[{"sku":"x1","price":200}]}`))
		default:
			w.Write([]byte(`{"total":0,"data":[]}`))
		}
	}))
	defer srv.Close()

	c := New(withHosts(hosts{idmc: srv.URL, kaspi: srv.URL, mc: srv.URL}))
	sess := &Session{MerchantID: "30297603", AmpCookie: "amp_6e9c16=x", MCSession: "mc-session=s", MCSid: "mc-sid=s"}

	var got []CabinetProduct
	for p, err := range c.Products(sess) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p)
	}
	// 26 активных + 1 снятый = 27.
	if len(got) != 27 {
		t.Fatalf("ожидалось 27 товаров, получено %d", len(got))
	}
	if got[26].SKU != "x1" {
		t.Errorf("последний (снятый) товар: %q", got[26].SKU)
	}
}

func TestOfferListSessionExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()
	c := New(withHosts(hosts{idmc: srv.URL, kaspi: srv.URL, mc: srv.URL}))
	sess := &Session{MerchantID: "1", AmpCookie: "amp_6e9c16=x", MCSession: "s", MCSid: "s"}
	for _, err := range c.Products(sess) {
		if err != ErrSessionExpired {
			t.Fatalf("ожидалась ErrSessionExpired, получено %v", err)
		}
		break
	}
}

func repeatItems(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			s += ","
		}
		s += `{"sku":"a` + itoa(i) + `","price":100}`
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
