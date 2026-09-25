package kaspi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVerify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") != "good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Accept") != "application/vnd.api+json" {
			t.Errorf("нет заголовка Accept JSON:API: %q", r.Header.Get("Accept"))
		}
		w.Write([]byte(`{"data":[],"meta":{"totalCount":42,"pageCount":42}}`))
	}))
	defer srv.Close()

	c := NewOfficial(WithBaseURL(srv.URL))
	info, err := c.Verify(context.Background(), "good")
	if err != nil || info.OrdersTotal != 42 {
		t.Fatalf("Verify: %+v, %v", info, err)
	}

	if _, err := c.Verify(context.Background(), "bad"); err != ErrUnauthorized {
		t.Fatalf("неверный токен: ожидалась ErrUnauthorized, получено %v", err)
	}
}

func TestOrdersPaginateAndNormalize(t *testing.T) {
	page0 := `{"data":[` +
		`{"type":"orders","id":"o1","attributes":{"code":"C1","state":"ARCHIVE","status":"COMPLETED","totalPrice":24990.0,"creationDate":1690000000000,"customer":{"firstName":"Аскар","lastName":"Аскаров","cellPhone":"+77011112233"}}}` +
		func() string {
			// добиваем до 100 элементов, чтобы клиент запросил вторую страницу
			s := ""
			for i := 0; i < 99; i++ {
				s += `,{"type":"orders","id":"x","attributes":{"code":"x","totalPrice":100.0}}`
			}
			return s
		}() +
		`],"meta":{"totalCount":101,"pageCount":2}}`
	page1 := `{"data":[{"type":"orders","id":"o2","attributes":{"code":"C2","state":"DELIVERY","status":"ACCEPTED_BY_MERCHANT","totalPrice":5000.5,"creationDate":1690000100000,"customer":{"name":"Батыр","cellPhone":"+77022223344"}}}],"meta":{"totalCount":101,"pageCount":2}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page[number]") == "0" {
			w.Write([]byte(page0))
		} else {
			w.Write([]byte(page1))
		}
	}))
	defer srv.Close()

	c := NewOfficial(WithBaseURL(srv.URL))
	var orders []Order
	for o, err := range c.Orders(context.Background(), "good", time.UnixMilli(1689000000000)) {
		if err != nil {
			t.Fatal(err)
		}
		orders = append(orders, o)
	}

	if len(orders) != 101 {
		t.Fatalf("ожидалось 101 заказ с двух страниц, получено %d", len(orders))
	}
	first := orders[0]
	if first.ExternalID != "o1" || first.Code != "C1" || first.TotalMinor != 2499000 {
		t.Errorf("первый заказ нормализован неверно: %+v", first)
	}
	if first.CustomerName != "Аскар Аскаров" || first.CustomerPhone != "+77011112233" {
		t.Errorf("покупатель нормализован неверно: %q %q", first.CustomerName, first.CustomerPhone)
	}
	if len(first.Raw) == 0 {
		t.Error("сырой JSON заказа не сохранён")
	}
	last := orders[100]
	if last.ExternalID != "o2" || last.TotalMinor != 500050 {
		t.Errorf("заказ со второй страницы неверен: %+v", last)
	}
}
