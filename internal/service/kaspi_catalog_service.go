package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"nado_go/internal/database"
	"nado_go/internal/integration/marketplace/kaspi"
	"nado_go/internal/jobs"
	"nado_go/internal/model"
	"nado_go/internal/repository"
)

// ErrConnectionNotFound — подключение не найдено или неактивно.
var ErrConnectionNotFound = errors.New("service: подключение не найдено")

// KaspiCatalogService принимает импортированный из кабинета каталог, который
// шлёт удалённый воркер, и сохраняет его зеркалом (уровень магазина, D-23).
type KaspiCatalogService struct {
	stores   *repository.StoreRepository
	products *repository.MarketplaceProductRepository
	jobs     *jobs.Repository
	log      *slog.Logger
}

func NewKaspiCatalogService(stores *repository.StoreRepository, products *repository.MarketplaceProductRepository, jobsRepo *jobs.Repository, log *slog.Logger) *KaspiCatalogService {
	return &KaspiCatalogService{stores: stores, products: products, jobs: jobsRepo, log: log}
}

// Ingest сохраняет страницу товаров подключения. Final=true — конец обхода.
func (s *KaspiCatalogService) Ingest(ctx context.Context, connectionID int64, req kaspi.IngestRequest) (kaspi.IngestResponse, error) {
	storeID, accountID, err := s.stores.GetConnectionScope(ctx, connectionID)
	if errors.Is(err, database.ErrNotFound) {
		return kaspi.IngestResponse{}, ErrConnectionNotFound
	}
	if err != nil {
		return kaspi.IngestResponse{}, err
	}

	var resp kaspi.IngestResponse
	for i := range req.Products {
		m := mapProduct(connectionID, storeID, accountID, &req.Products[i])
		created, err := s.products.Upsert(ctx, m)
		if err != nil {
			return resp, err
		}
		if created {
			resp.Created++
		} else {
			resp.Updated++
		}
	}

	if req.Final {
		if err := s.stores.TouchContentSync(ctx, connectionID); err != nil {
			return resp, err
		}
		// Зеркало обновлено — перестраиваем продаваемый каталог витрины.
		if s.jobs != nil {
			if _, err := s.jobs.Enqueue(ctx, jobs.Enqueue{
				Kind:      jobs.KindCatalogBuild,
				Execution: jobs.Local,
				AccountID: accountID,
				Payload:   map[string]any{"connection_id": connectionID},
				DedupKey:  fmt.Sprintf("catalog_build:%d", connectionID),
			}); err != nil && s.log != nil {
				s.log.Warn("не удалось поставить сборку каталога", slog.Int64("connection_id", connectionID), slog.Any("error", err))
			}
		}
	}
	return resp, nil
}

// mapProduct переводит DTO приёма в доменную модель и считает хеш содержимого.
func mapProduct(connectionID, storeID, accountID int64, p *kaspi.ProductPayload) *model.MarketplaceProduct {
	currency := p.Currency
	if currency == "" {
		currency = "KZT"
	}
	var imagesJSON string
	if len(p.Images) > 0 {
		b, _ := json.Marshal(p.Images)
		imagesJSON = string(b)
	}

	stocks := make([]model.MarketplaceProductStock, len(p.Stocks))
	for i, st := range p.Stocks {
		stocks[i] = model.MarketplaceProductStock{
			StoreCode: st.StoreCode, Qty: st.Qty, Specified: st.Specified, PreOrder: st.PreOrder,
		}
	}

	m := &model.MarketplaceProduct{
		ConnectionID: connectionID, StoreID: storeID, AccountID: accountID,
		SKU: p.SKU, MasterSKU: p.MasterSKU, Title: p.Title, Brand: p.Brand, CategoryExt: p.CategoryID,
		PriceMinor: p.PriceMinor, Currency: currency, Available: p.Available, ImagesJSON: imagesJSON,
		Raw: p.Raw, Stocks: stocks,
	}
	m.ContentHash = productHash(m)
	return m
}

// productHash — SHA-256 значимых полей: по нему можно пропускать неизменившиеся
// товары (в v1 запись всё равно идёт, но хеш сохраняется на будущее).
func productHash(m *model.MarketplaceProduct) []byte {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%d|%t|%s", m.SKU, m.Title, m.Brand, m.CategoryExt, m.PriceMinor, m.Available, m.ImagesJSON)
	for _, st := range m.Stocks {
		fmt.Fprintf(h, "|%s:%d:%t:%d", st.StoreCode, st.Qty, st.Specified, st.PreOrder)
	}
	return h.Sum(nil)
}
