package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"nado_go/internal/jobs"
	"nado_go/internal/repository"
)

// CatalogBuildService строит продаваемый каталог витрины из зеркала маркетплейса
// (marketplace_products → products/variants/store_offers). Один товар Kaspi даёт
// товар + один вариант; цена оффера считается правилами магазина (D-14).
type CatalogBuildService struct {
	source  *repository.MarketplaceProductRepository
	catalog *repository.CatalogRepository
	stores  *repository.StoreRepository
	log     *slog.Logger
}

func NewCatalogBuildService(source *repository.MarketplaceProductRepository, catalog *repository.CatalogRepository, stores *repository.StoreRepository, log *slog.Logger) *CatalogBuildService {
	return &CatalogBuildService{source: source, catalog: catalog, stores: stores, log: log}
}

// Rebuild перестраивает витринный каталог магазина из зеркала подключения.
// Идемпотентна: повторный запуск обновляет уже созданные товары.
func (s *CatalogBuildService) Rebuild(ctx context.Context, connectionID int64) (int, error) {
	src, err := s.source.ListForConnection(ctx, connectionID)
	if err != nil {
		return 0, err
	}
	if len(src) == 0 {
		return 0, nil
	}

	storeID := src[0].StoreID
	accountID := src[0].AccountID
	lang, currency, err := s.stores.StoreLocale(ctx, storeID)
	if err != nil {
		return 0, jobs.Permanent(fmt.Errorf("магазин подключения %d не найден: %w", connectionID, err))
	}
	if lang == "" {
		lang = "ru"
	}

	rules, err := s.catalog.PriceRules(ctx, storeID)
	if err != nil {
		return 0, err
	}

	catCache := map[string]int64{} // external_code → category_id
	built := 0
	for i := range src {
		p := &src[i]

		catID, err := s.ensureCategory(ctx, storeID, accountID, lang, p.CategoryExt, catCache)
		if err != nil {
			return built, err
		}

		qty := 0
		for _, st := range p.Stocks {
			qty += st.Qty
		}
		// Kaspi не всегда отдаёт точное число: при признаке «в наличии» держим
		// хотя бы 1, чтобы витрина не считала товар отсутствующим.
		if p.Available && qty == 0 {
			qty = 1
		}

		rule := SelectPriceRule(rules, 0, catID) // товар ещё без id — правило по категории/магазину
		offerPrice := ApplyPriceRule(p.PriceMinor, rule)
		cur := p.Currency
		if cur == "" {
			cur = currency
		}
		ruleID := int64(0)
		if rule != nil {
			ruleID = rule.ID
		}

		in := repository.BuildProductInput{
			StoreID: storeID, AccountID: accountID, ConnectionID: connectionID, CategoryID: catID,
			SourceSKU: p.SKU, Brand: p.Brand, Lang: lang,
			Title: p.Title, Slug: Slugify(p.Title) + "-" + Slugify(p.SKU),
			Images:           parseImages(p.ImagesJSON),
			SKU:              p.SKU,
			SourcePriceMinor: p.PriceMinor, Currency: cur, Qty: qty, Available: p.Available,
			OfferPriceMinor: offerPrice, AppliedRuleID: ruleID,
		}
		if err := s.catalog.BuildProduct(ctx, in); err != nil {
			return built, err
		}
		built++
	}

	s.log.Info("витринный каталог построен",
		slog.Int64("store_id", storeID), slog.Int64("connection_id", connectionID), slog.Int("products", built))
	return built, nil
}

// ensureCategory находит/создаёт категорию по внешнему коду с кэшированием.
func (s *CatalogBuildService) ensureCategory(ctx context.Context, storeID, accountID int64, lang, extCode string, cache map[string]int64) (int64, error) {
	if extCode == "" {
		return 0, nil
	}
	if id, ok := cache[extCode]; ok {
		return id, nil
	}
	// Названия категорий Kaspi не отдаёт — используем код как имя (продавец
	// переименует позже). slug строится из кода.
	id, err := s.catalog.EnsureCategory(ctx, storeID, accountID, extCode, extCode, lang, Slugify(extCode))
	if err != nil {
		return 0, err
	}
	cache[extCode] = id
	return id, nil
}

// parseImages разбирает JSON-массив URL из зеркала; пустой/битый → nil.
func parseImages(raw string) []string {
	if raw == "" {
		return nil
	}
	var urls []string
	if err := json.Unmarshal([]byte(raw), &urls); err != nil {
		return nil
	}
	return urls
}
