package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"nado_go/internal/database"
	"nado_go/internal/httpx"
	"nado_go/internal/jobs"
	"nado_go/internal/money"
	"nado_go/internal/repository"
)

// Ошибки редактирования каталога из кабинета.
var (
	ErrCategoryNotFound = errors.New("service: категория не найдена")
	ErrProductNotFound  = errors.New("service: товар не найден")
	ErrCategoryNotEmpty = errors.New("service: в категории есть подкатегории или товары")
	ErrSlugTaken        = errors.New("service: такой slug уже используется")
	ErrNameRequired     = errors.New("service: не указано название")
	ErrTitleRequired    = errors.New("service: не указано название товара")
	ErrBadPrice         = errors.New("service: некорректная цена")
)

// CatalogEditService — редактирование категорий и товаров продавцом в кабинете.
// Правки товаров из Kaspi защищаются от перезаписи синхронизацией: изменённые
// поля помечаются в products.overridden_fields (см. миграцию 0015 и BuildProduct).
type CatalogEditService struct {
	catalog *repository.CatalogRepository
	stores  *repository.StoreRepository
	jobs    *jobs.Repository
	log     *slog.Logger
}

func NewCatalogEditService(catalog *repository.CatalogRepository, stores *repository.StoreRepository, jobsRepo *jobs.Repository, log *slog.Logger) *CatalogEditService {
	return &CatalogEditService{catalog: catalog, stores: stores, jobs: jobsRepo, log: log}
}

// storeLocale возвращает язык по умолчанию и валюту магазина (с проверкой, что он
// вообще существует). Владение проверяется в самих запросах каталога по account_id.
func (s *CatalogEditService) storeLocale(ctx context.Context, storeID int64) (lang, currency string, err error) {
	lang, currency, err = s.stores.StoreLocale(ctx, storeID)
	if errors.Is(err, database.ErrNotFound) {
		return "", "", httpx.ErrNotFound("Магазин не найден")
	}
	if err != nil {
		return "", "", httpx.ErrInternal(err)
	}
	if lang == "" {
		lang = "ru"
	}
	if currency == "" {
		currency = string(money.KZT)
	}
	return lang, currency, nil
}

// DefaultLang отдаёт язык магазина по умолчанию (для подписей форм).
func (s *CatalogEditService) DefaultLang(ctx context.Context, storeID int64) (string, error) {
	lang, _, err := s.storeLocale(ctx, storeID)
	return lang, err
}

// --- Категории ---

// Categories возвращает список категорий магазина для кабинета.
func (s *CatalogEditService) Categories(ctx context.Context, accountID, storeID int64) ([]repository.CabinetCategory, error) {
	defLang, _, err := s.storeLocale(ctx, storeID)
	if err != nil {
		return nil, err
	}
	cats, err := s.catalog.CabinetCategories(ctx, storeID, accountID, defLang)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return cats, nil
}

// CategoryForEdit возвращает категорию с переводами.
func (s *CatalogEditService) CategoryForEdit(ctx context.Context, accountID, storeID, id int64) (*repository.CategoryEdit, error) {
	e, err := s.catalog.GetCategoryForEdit(ctx, storeID, accountID, id)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrCategoryNotFound
	}
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return e, nil
}

// CategoryForm — ввод формы категории. Ключи map — коды языков магазина.
type CategoryForm struct {
	ParentID int64
	Sort     int
	Names    map[string]string
	Slugs    map[string]string
}

// SaveCategory создаёт (id=0) или обновляет категорию. Возвращает id.
func (s *CatalogEditService) SaveCategory(ctx context.Context, accountID, storeID, id int64, f CategoryForm) (int64, error) {
	defLang, _, err := s.storeLocale(ctx, storeID)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(f.Names[defLang]) == "" {
		return 0, ErrNameRequired
	}

	trs := map[string]repository.CategoryTr{}
	for lang, name := range f.Names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue // язык без имени — витрина возьмёт перевод по умолчанию
		}
		slug := strings.TrimSpace(f.Slugs[lang])
		if slug == "" {
			slug = Slugify(name)
		} else {
			slug = Slugify(slug)
		}
		trs[lang] = repository.CategoryTr{Name: name, Slug: slug}
	}

	in := repository.CategoryInput{
		StoreID: storeID, AccountID: accountID, ID: id, ParentID: f.ParentID, Sort: f.Sort, Trs: trs,
	}
	if id == 0 {
		newID, err := s.catalog.CreateCategory(ctx, in)
		return newID, mapCatalogWriteErr(err)
	}
	return id, mapCatalogWriteErr(s.catalog.UpdateCategory(ctx, in))
}

// DeleteCategory удаляет пустую категорию.
func (s *CatalogEditService) DeleteCategory(ctx context.Context, accountID, storeID, id int64) error {
	err := s.catalog.DeleteCategory(ctx, storeID, accountID, id)
	switch {
	case errors.Is(err, database.ErrNotFound):
		return ErrCategoryNotFound
	case errors.Is(err, database.ErrConflict):
		return ErrCategoryNotEmpty
	case err != nil:
		return httpx.ErrInternal(err)
	}
	return nil
}

// --- Товары ---

// Products возвращает страницу товаров кабинета (любого статуса) с поиском и
// фильтром по категории/статусу. lang — язык подписей (для названия товара).
func (s *CatalogEditService) Products(ctx context.Context, accountID, storeID int64, lang string, categoryID int64, search, status string, page, perPage int) ([]repository.CabinetProduct, int, error) {
	defLang, _, err := s.storeLocale(ctx, storeID)
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if perPage <= 0 || perPage > 100 {
		perPage = 30
	}
	list, total, err := s.catalog.CabinetProducts(ctx, repository.CabinetProductQuery{
		StoreID: storeID, AccountID: accountID, Lang: lang, DefLang: defLang,
		CategoryID: categoryID, Search: search, Status: status,
		Limit: perPage, Offset: (page - 1) * perPage,
	})
	if err != nil {
		return nil, 0, httpx.ErrInternal(err)
	}
	return list, total, nil
}

// ProductForEdit возвращает товар для формы редактирования.
func (s *CatalogEditService) ProductForEdit(ctx context.Context, accountID, storeID, id int64) (*repository.ProductEdit, error) {
	e, err := s.catalog.GetProductForEdit(ctx, storeID, accountID, id)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrProductNotFound
	}
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return e, nil
}

// ProductForm — ввод формы контента товара. Ключи map — коды языков магазина.
type ProductForm struct {
	CategoryID   int64
	Brand        string
	Status       string
	Titles       map[string]string
	Descriptions map[string]string
	Slugs        map[string]string
	SEOTitles    map[string]string
	SEODescs     map[string]string
	Images       []string
}

// SaveProduct сохраняет контент товара и, для товаров из Kaspi, помечает
// изменённые поля переопределёнными, чтобы синхронизация их не перезаписала.
func (s *CatalogEditService) SaveProduct(ctx context.Context, accountID, storeID, id int64, f ProductForm) error {
	defLang, _, err := s.storeLocale(ctx, storeID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(f.Titles[defLang]) == "" {
		return ErrTitleRequired
	}
	if f.Status != "draft" && f.Status != "active" && f.Status != "archived" {
		f.Status = "active"
	}

	cur, err := s.catalog.GetProductForEdit(ctx, storeID, accountID, id)
	if errors.Is(err, database.ErrNotFound) {
		return ErrProductNotFound
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}

	trs := map[string]repository.ProductTr{}
	for lang, title := range f.Titles {
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		slug := strings.TrimSpace(f.Slugs[lang])
		if slug == "" {
			slug = Slugify(title)
		} else {
			slug = Slugify(slug)
		}
		trs[lang] = repository.ProductTr{
			Title:       title,
			Description: f.Descriptions[lang],
			Slug:        slug,
			SEOTitle:    strings.TrimSpace(f.SEOTitles[lang]),
			SEODesc:     strings.TrimSpace(f.SEODescs[lang]),
		}
	}

	images := make([]string, 0, len(f.Images))
	for _, u := range f.Images {
		if u = strings.TrimSpace(u); u != "" {
			images = append(images, u)
		}
	}

	// Для товаров из Kaspi защищаем изменённые поля от перезаписи синхронизацией.
	overrides := cur.Overridden
	if cur.SourceSKU != "" {
		if f.CategoryID != cur.CategoryID {
			overrides["category"] = true
		}
		if strings.TrimSpace(f.Brand) != cur.Brand {
			overrides["brand"] = true
		}
		if f.Status != cur.Status {
			overrides["status"] = true
		}
		if contentChanged(cur.Trs, trs) {
			overrides["content"] = true
		}
		if imagesChanged(cur.Images, images) {
			overrides["images"] = true
		}
	}

	err = s.catalog.UpdateProduct(ctx, repository.ProductUpdate{
		StoreID: storeID, AccountID: accountID, ID: id,
		CategoryID: f.CategoryID, Brand: strings.TrimSpace(f.Brand), Status: f.Status,
		Trs: trs, Images: images, Overrides: overrideList(overrides),
	})
	if errors.Is(err, database.ErrNotFound) {
		return ErrProductNotFound
	}
	return mapCatalogWriteErr(err)
}

// SetPrice задаёт цену и видимость оффера товара. mode: "manual" — цена из ввода,
// "rule" — цена считается по правилам магазина от цены маркетплейса (D-14).
func (s *CatalogEditService) SetPrice(ctx context.Context, accountID, storeID, id int64, mode, priceInput string, visible bool) error {
	_, storeCur, err := s.storeLocale(ctx, storeID)
	if err != nil {
		return err
	}
	cur, err := s.catalog.GetProductForEdit(ctx, storeID, accountID, id)
	if errors.Is(err, database.ErrNotFound) {
		return ErrProductNotFound
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}
	currency := cur.Currency
	if currency == "" {
		currency = storeCur
	}

	var priceMinor, ruleID int64
	if mode == "manual" {
		priceMinor, err = parsePriceMinor(priceInput)
		if err != nil {
			return ErrBadPrice
		}
	} else {
		mode = "rule"
		rules, err := s.catalog.PriceRules(ctx, storeID)
		if err != nil {
			return httpx.ErrInternal(err)
		}
		rule := SelectPriceRule(rules, id, cur.CategoryID)
		priceMinor = ApplyPriceRule(cur.SourcePriceMinor, rule)
		if rule != nil {
			ruleID = rule.ID
		}
	}

	err = s.catalog.SetProductOffer(ctx, storeID, accountID, id, mode, priceMinor, cur.OldPriceMinor, ruleID, currency, visible)
	if errors.Is(err, database.ErrNotFound) {
		return ErrProductNotFound
	}
	return mapCatalogWriteErr(err)
}

// ResetOverride убирает защиту поля и ставит перестроение каталога, чтобы вернуть
// значение с маркетплейса. Нет активного подключения — значение вернёт следующая
// плановая синхронизация.
func (s *CatalogEditService) ResetOverride(ctx context.Context, accountID, storeID, id int64, field string) error {
	if !isOverridableField(field) {
		return httpx.ErrBadRequest("Неизвестное поле")
	}
	err := s.catalog.ClearOverride(ctx, storeID, accountID, id, field)
	if errors.Is(err, database.ErrNotFound) {
		return ErrProductNotFound
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}

	connID, err := s.stores.ConnectionIDForStore(ctx, accountID, storeID)
	if errors.Is(err, database.ErrNotFound) {
		return nil // подключение неактивно — восстановит плановая сборка
	}
	if err != nil {
		s.log.Warn("сброс правки: подключение магазина не найдено", slog.Int64("store_id", storeID), slog.Any("error", err))
		return nil
	}
	if _, err := s.jobs.Enqueue(ctx, jobs.Enqueue{
		Kind: jobs.KindCatalogBuild, Execution: jobs.Local, AccountID: accountID,
		Payload: map[string]any{"connection_id": connID}, DedupKey: fmt.Sprintf("catalog_build:%d", connID),
	}); err != nil {
		s.log.Warn("сброс правки: не удалось поставить сборку каталога", slog.Int64("connection_id", connID), slog.Any("error", err))
	}
	return nil
}

// --- Вспомогательное ---

var overridableFields = map[string]bool{
	"category": true, "brand": true, "status": true, "content": true, "images": true,
}

func isOverridableField(f string) bool { return overridableFields[f] }

func overrideList(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for f := range set {
		if set[f] {
			out = append(out, f)
		}
	}
	return out
}

func contentChanged(cur map[string]repository.ProductTr, next map[string]repository.ProductTr) bool {
	if len(cur) != len(next) {
		return true
	}
	for lang, n := range next {
		c, ok := cur[lang]
		if !ok || c != n {
			return true
		}
	}
	return false
}

func imagesChanged(a, b []string) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}

// parsePriceMinor разбирает цену в основных единицах ("1990", "1990.50",
// "1 990,50") в минорные (тиыны/копейки, 2 знака). Отрицательная — ошибка.
func parsePriceMinor(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0, nil
	}
	neg := strings.HasPrefix(s, "-")
	if neg {
		return 0, ErrBadPrice
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	major, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, ErrBadPrice
	}
	cents := int64(0)
	if hasFrac {
		if len(frac) > 2 {
			frac = frac[:2]
		}
		for len(frac) < 2 {
			frac += "0"
		}
		cents, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, ErrBadPrice
		}
	}
	return major*100 + cents, nil
}

// mapCatalogWriteErr переводит ошибки записи каталога в доменные ошибки сервиса.
func mapCatalogWriteErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, database.ErrConflict):
		return ErrSlugTaken
	case errors.Is(err, database.ErrNotFound):
		return ErrCategoryNotFound
	default:
		return httpx.ErrInternal(err)
	}
}
