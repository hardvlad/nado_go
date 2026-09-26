package service

import (
	"context"

	"nado_go/internal/model"
	"nado_go/internal/repository"
)

// StorefrontService — чтение каталога витрины (категории, листинги, карточка,
// поиск). Тонкая обёртка над CatalogRepository для слоя обработчиков.
type StorefrontService struct {
	catalog *repository.CatalogRepository
}

func NewStorefrontService(catalog *repository.CatalogRepository) *StorefrontService {
	return &StorefrontService{catalog: catalog}
}

// Categories возвращает категории магазина с числом видимых товаров.
func (s *StorefrontService) Categories(ctx context.Context, storeID int64, lang, defLang string) ([]model.Category, error) {
	return s.catalog.ListCategories(ctx, storeID, lang, defLang)
}

// Products возвращает страницу товаров и общее число подходящих (для пагинации).
func (s *StorefrontService) Products(ctx context.Context, storeID int64, lang, defLang string, categoryID int64, search string, page, perPage int) ([]model.Product, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage <= 0 || perPage > 60 {
		perPage = 24
	}
	return s.catalog.ListProducts(ctx, repository.ProductQuery{
		StoreID: storeID, Lang: lang, DefLang: defLang, CategoryID: categoryID,
		Search: search, Limit: perPage, Offset: (page - 1) * perPage,
	})
}

// Product возвращает карточку товара по id в пределах магазина.
func (s *StorefrontService) Product(ctx context.Context, storeID, id int64, lang, defLang string) (*model.Product, error) {
	return s.catalog.GetProduct(ctx, storeID, id, lang, defLang)
}
