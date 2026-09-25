package service

import (
	"bytes"
	"testing"

	"nado_go/internal/integration/marketplace/kaspi"
)

func TestMapProduct(t *testing.T) {
	p := &kaspi.ProductPayload{
		SKU: "a1", MasterSKU: "m1", Title: "Товар", Brand: "Acme", CategoryID: "cat",
		Images: []string{"u1", "u2"}, Available: true, PriceMinor: 2499000, Currency: "",
		Stocks: []kaspi.StockPayload{{StoreCode: "PP1", Qty: 3, Specified: true}},
	}
	m := mapProduct(7, 11, 5, p)

	if m.ConnectionID != 7 || m.StoreID != 11 || m.AccountID != 5 {
		t.Errorf("область не проставлена: %+v", m)
	}
	if m.Currency != "KZT" { // пустая валюта → KZT
		t.Errorf("валюта = %q", m.Currency)
	}
	if m.ImagesJSON != `["u1","u2"]` {
		t.Errorf("images JSON = %q", m.ImagesJSON)
	}
	if len(m.Stocks) != 1 || m.Stocks[0].StoreCode != "PP1" {
		t.Errorf("остатки: %+v", m.Stocks)
	}
	if len(m.ContentHash) != 32 {
		t.Errorf("хеш содержимого должен быть 32 байта, получено %d", len(m.ContentHash))
	}
}

func TestProductHashSensitivity(t *testing.T) {
	base := mapProduct(1, 1, 1, &kaspi.ProductPayload{SKU: "a", Title: "X", PriceMinor: 100})
	same := mapProduct(1, 1, 1, &kaspi.ProductPayload{SKU: "a", Title: "X", PriceMinor: 100})
	if !bytes.Equal(base.ContentHash, same.ContentHash) {
		t.Error("одинаковые товары должны давать одинаковый хеш")
	}
	diff := mapProduct(1, 1, 1, &kaspi.ProductPayload{SKU: "a", Title: "X", PriceMinor: 200})
	if bytes.Equal(base.ContentHash, diff.ContentHash) {
		t.Error("разная цена должна менять хеш")
	}
}
