package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/model"
	"nado_go/internal/money"
	"nado_go/internal/repository"
	"nado_go/internal/service"
	"nado_go/internal/tenant"
)

// Редактирование каталога магазина в кабинете продавца: список товаров, карточка
// товара (контент по всем языкам, изображения, цена и видимость), управление
// категориями. Правки товаров из Kaspi защищаются от перезаписи синхронизацией
// (service.CatalogEditService помечает изменённые поля).

const catalogPerPage = 30

// productStatuses — статусы товара для фильтра и селекта.
var productStatuses = []string{"active", "draft", "archived"}

// --- Пути в рамках магазина ---

func catalogBase(lang i18n.Lang, storeID int64) string {
	return i18n.Localize(lang, fmt.Sprintf("/stores/%d/catalog", storeID))
}
func categoriesBase(lang i18n.Lang, storeID int64) string {
	return i18n.Localize(lang, fmt.Sprintf("/stores/%d/categories", storeID))
}
func productEditPath(lang i18n.Lang, storeID, pid int64) string {
	return i18n.Localize(lang, fmt.Sprintf("/stores/%d/products/%d/edit", storeID, pid))
}
func categoryEditPath(lang i18n.Lang, storeID, cid int64) string {
	return i18n.Localize(lang, fmt.Sprintf("/stores/%d/categories/%d/edit", storeID, cid))
}

func idParam(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, httpx.ErrBadRequest("Некорректный идентификатор")
	}
	return id, nil
}

// requireStore проверяет вход и возвращает принципала и id магазина из пути.
func (h *PageHandler) requireStore(w http.ResponseWriter, r *http.Request) (*model.Principal, int64, bool) {
	l := h.localizer(r)
	p := tenant.FromContext(r.Context())
	if p == nil {
		redirect(w, r, i18n.Localize(l.Lang(), "/login")+"?next="+url.QueryEscape(r.URL.Path))
		return nil, 0, false
	}
	storeID, err := idParam(r, "id")
	if err != nil {
		return nil, 0, false
	}
	return p, storeID, true
}

// storeName возвращает имя магазина аккаунта (для заголовков), пусто — если не найден.
func (h *PageHandler) storeName(r *http.Request, accountID, storeID int64) string {
	if h.Connections == nil {
		return ""
	}
	stores, err := h.Connections.Stores(r.Context(), accountID)
	if err != nil {
		return ""
	}
	for _, s := range stores {
		if s.ID == storeID {
			return s.Name
		}
	}
	return ""
}

// LangValue — значение поля для одного языка витрины в форме.
type LangValue struct {
	Code        string
	Name        string // человекочитаемое имя языка
	Title       string
	Description string
	Slug        string
	SEOTitle    string
	SEODesc     string
}

// --- Товары ---

// CatalogProducts — GET /stores/{id}/catalog: список товаров магазина.
func (h *PageHandler) CatalogProducts(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	l := h.localizer(r)

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	categoryID, _ := strconv.ParseInt(r.URL.Query().Get("category"), 10, 64)
	status := r.URL.Query().Get("status")
	if !validProductStatus(status) {
		status = ""
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	products, total, err := h.Catalog.Products(r.Context(), p.AccountID, storeID, string(l.Lang()), categoryID, query, status, page, catalogPerPage)
	if err != nil {
		return err
	}
	cats, err := h.Catalog.Categories(r.Context(), p.AccountID, storeID)
	if err != nil {
		return err
	}

	totalPages := (total + catalogPerPage - 1) / catalogPerPage
	nums := make([]int, 0, totalPages)
	for i := 1; i <= totalPages; i++ {
		nums = append(nums, i)
	}
	base := catalogBase(l.Lang(), storeID) + "?q=" + url.QueryEscape(query)
	if categoryID > 0 {
		base += "&category=" + strconv.FormatInt(categoryID, 10)
	}
	if status != "" {
		base += "&status=" + url.QueryEscape(status)
	}
	base += "&page="

	data := h.page(r, "catalog.title").
		With("StoreID", storeID).
		With("StoreName", h.storeName(r, p.AccountID, storeID)).
		With("Products", products).
		With("Categories", cats).
		With("Statuses", productStatusOptions(l, status)).
		With("Query", query).
		With("CategoryID", categoryID).
		With("Status", status).
		With("Total", total).
		With("Page", page).
		With("TotalPages", totalPages).
		With("PageNums", nums).
		With("PageBase", base).
		With("FilterAction", catalogBase(l.Lang(), storeID)).
		With("CategoriesURL", categoriesBase(l.Lang(), storeID)).
		With("ProductEditBase", i18n.Localize(l.Lang(), fmt.Sprintf("/stores/%d/products", storeID))).
		With("AccountURL", i18n.Localize(l.Lang(), "/account")).
		With("Lang", string(l.Lang()))
	return h.Render.Render(w, http.StatusOK, "catalog_products", data)
}

// ProductEditForm — GET /stores/{id}/products/{pid}/edit.
func (h *PageHandler) ProductEditForm(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	pid, err := idParam(r, "pid")
	if err != nil {
		return err
	}
	e, err := h.Catalog.ProductForEdit(r.Context(), p.AccountID, storeID, pid)
	if errors.Is(err, service.ErrProductNotFound) {
		return httpx.ErrNotFound("Товар не найден")
	}
	if err != nil {
		return err
	}
	return h.renderProductEdit(w, r, http.StatusOK, storeID, e, newForm())
}

func (h *PageHandler) renderProductEdit(w http.ResponseWriter, r *http.Request, status int, storeID int64, e *repository.ProductEdit, form FormView) error {
	l := h.localizer(r)
	langs := make([]LangValue, 0, len(i18n.Supported()))
	for _, lang := range i18n.Supported() {
		tr := e.Trs[string(lang)]
		langs = append(langs, LangValue{
			Code: string(lang), Name: lang.Name(),
			Title: tr.Title, Description: tr.Description, Slug: tr.Slug,
			SEOTitle: tr.SEOTitle, SEODesc: tr.SEODesc,
		})
	}
	cats, err := h.Catalog.Categories(r.Context(), tenant.FromContext(r.Context()).AccountID, storeID)
	if err != nil {
		return err
	}

	data := h.page(r, "catalog.product_title").
		With("StoreID", storeID).
		With("StoreName", h.storeName(r, tenant.FromContext(r.Context()).AccountID, storeID)).
		With("Product", e).
		With("Langs", langs).
		With("Categories", cats).
		With("Statuses", productStatusOptions(l, e.Status)).
		With("Images", e.Images).
		With("FromKaspi", e.SourceSKU != "").
		With("Overridden", e.Overridden).
		With("ManualPriceInput", priceInputValue(e.ManualPriceMinor)).
		With("SourcePrice", money.Money{Minor: e.SourcePriceMinor, Currency: money.Currency(e.Currency)}.Format(string(l.Lang()))).
		With("OfferPrice", money.Money{Minor: e.OfferPriceMinor, Currency: money.Currency(e.Currency)}.Format(string(l.Lang()))).
		With("Form", form).
		With("Action", productEditPath(l.Lang(), storeID, e.ID)).
		With("PriceAction", i18n.Localize(l.Lang(), fmt.Sprintf("/stores/%d/products/%d/price", storeID, e.ID))).
		With("ResetAction", i18n.Localize(l.Lang(), fmt.Sprintf("/stores/%d/products/%d/reset", storeID, e.ID))).
		With("CatalogURL", catalogBase(l.Lang(), storeID)).
		With("Saved", r.URL.Query().Get("saved") == "1").
		With("Reset", r.URL.Query().Get("reset") == "1").
		With("Lang", string(l.Lang()))
	return h.Render.Render(w, status, "product_edit", data)
}

// ProductEditSubmit — POST /stores/{id}/products/{pid}/edit: контент товара.
func (h *PageHandler) ProductEditSubmit(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	pid, err := idParam(r, "pid")
	if err != nil {
		return err
	}
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)

	f := service.ProductForm{
		Brand:        r.PostFormValue("brand"),
		Status:       r.PostFormValue("status"),
		Titles:       map[string]string{},
		Descriptions: map[string]string{},
		Slugs:        map[string]string{},
		SEOTitles:    map[string]string{},
		SEODescs:     map[string]string{},
	}
	f.CategoryID, _ = strconv.ParseInt(r.PostFormValue("category"), 10, 64)
	for _, lang := range i18n.Supported() {
		code := string(lang)
		f.Titles[code] = r.PostFormValue("title_" + code)
		f.Descriptions[code] = r.PostFormValue("description_" + code)
		f.Slugs[code] = r.PostFormValue("slug_" + code)
		f.SEOTitles[code] = r.PostFormValue("seo_title_" + code)
		f.SEODescs[code] = r.PostFormValue("seo_description_" + code)
	}
	for _, u := range r.PostForm["image"] {
		f.Images = append(f.Images, u)
	}

	err = h.Catalog.SaveProduct(r.Context(), p.AccountID, storeID, pid, f)
	switch {
	case errors.Is(err, service.ErrProductNotFound):
		return httpx.ErrNotFound("Товар не найден")
	case errors.Is(err, service.ErrTitleRequired), errors.Is(err, service.ErrSlugTaken):
		e, gerr := h.Catalog.ProductForEdit(r.Context(), p.AccountID, storeID, pid)
		if gerr != nil {
			return gerr
		}
		form := newForm()
		if errors.Is(err, service.ErrSlugTaken) {
			form.Alert = l.T("catalog.err_slug_taken")
		} else {
			form.Alert = l.T("catalog.err_title_required")
		}
		return h.renderProductEdit(w, r, http.StatusUnprocessableEntity, storeID, e, form)
	case err != nil:
		return err
	}
	redirect(w, r, productEditPath(l.Lang(), storeID, pid)+"?saved=1")
	return nil
}

// ProductPriceSubmit — POST /stores/{id}/products/{pid}/price: цена и видимость.
func (h *PageHandler) ProductPriceSubmit(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	pid, err := idParam(r, "pid")
	if err != nil {
		return err
	}
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)

	mode := r.PostFormValue("price_mode")
	price := r.PostFormValue("price")
	visible := r.PostFormValue("visible") != ""

	err = h.Catalog.SetPrice(r.Context(), p.AccountID, storeID, pid, mode, price, visible)
	switch {
	case errors.Is(err, service.ErrProductNotFound):
		return httpx.ErrNotFound("Товар не найден")
	case errors.Is(err, service.ErrBadPrice):
		e, gerr := h.Catalog.ProductForEdit(r.Context(), p.AccountID, storeID, pid)
		if gerr != nil {
			return gerr
		}
		form := newForm()
		form.Alert = l.T("catalog.err_bad_price")
		return h.renderProductEdit(w, r, http.StatusUnprocessableEntity, storeID, e, form)
	case err != nil:
		return err
	}
	redirect(w, r, productEditPath(l.Lang(), storeID, pid)+"?saved=1")
	return nil
}

// ProductResetOverride — POST /stores/{id}/products/{pid}/reset: вернуть значение
// поля с маркетплейса.
func (h *PageHandler) ProductResetOverride(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	pid, err := idParam(r, "pid")
	if err != nil {
		return err
	}
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)

	if err := h.Catalog.ResetOverride(r.Context(), p.AccountID, storeID, pid, r.PostFormValue("field")); err != nil {
		if errors.Is(err, service.ErrProductNotFound) {
			return httpx.ErrNotFound("Товар не найден")
		}
		return err
	}
	redirect(w, r, productEditPath(l.Lang(), storeID, pid)+"?reset=1")
	return nil
}

// --- Категории ---

// CategoriesList — GET /stores/{id}/categories.
func (h *PageHandler) CategoriesList(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	l := h.localizer(r)
	cats, err := h.Catalog.Categories(r.Context(), p.AccountID, storeID)
	if err != nil {
		return err
	}
	data := h.page(r, "categories.title").
		With("StoreID", storeID).
		With("StoreName", h.storeName(r, p.AccountID, storeID)).
		With("Categories", cats).
		With("NewURL", categoriesBase(l.Lang(), storeID)+"/new").
		With("CatalogURL", catalogBase(l.Lang(), storeID)).
		With("EditBase", categoriesBase(l.Lang(), storeID)).
		With("Deleted", r.URL.Query().Get("deleted") == "1").
		With("NotEmpty", r.URL.Query().Get("notempty") == "1").
		With("Saved", r.URL.Query().Get("saved") == "1")
	return h.Render.Render(w, http.StatusOK, "categories", data)
}

// CategoryNewForm — GET /stores/{id}/categories/new.
func (h *PageHandler) CategoryNewForm(w http.ResponseWriter, r *http.Request) error {
	_, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	return h.renderCategoryForm(w, r, http.StatusOK, storeID, 0, nil, newForm())
}

// CategoryCreate — POST /stores/{id}/categories/new.
func (h *PageHandler) CategoryCreate(w http.ResponseWriter, r *http.Request) error {
	return h.saveCategory(w, r, 0)
}

// CategoryEditForm — GET /stores/{id}/categories/{cid}/edit.
func (h *PageHandler) CategoryEditForm(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	cid, err := idParam(r, "cid")
	if err != nil {
		return err
	}
	e, err := h.Catalog.CategoryForEdit(r.Context(), p.AccountID, storeID, cid)
	if errors.Is(err, service.ErrCategoryNotFound) {
		return httpx.ErrNotFound("Категория не найдена")
	}
	if err != nil {
		return err
	}
	return h.renderCategoryForm(w, r, http.StatusOK, storeID, cid, e, newForm())
}

// CategoryEditSubmit — POST /stores/{id}/categories/{cid}/edit.
func (h *PageHandler) CategoryEditSubmit(w http.ResponseWriter, r *http.Request) error {
	cid, err := idParam(r, "cid")
	if err != nil {
		return err
	}
	return h.saveCategory(w, r, cid)
}

// saveCategory обрабатывает создание (cid=0) и обновление категории.
func (h *PageHandler) saveCategory(w http.ResponseWriter, r *http.Request, cid int64) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)

	f := service.CategoryForm{Names: map[string]string{}, Slugs: map[string]string{}}
	f.ParentID, _ = strconv.ParseInt(r.PostFormValue("parent"), 10, 64)
	f.Sort, _ = strconv.Atoi(r.PostFormValue("sort"))
	for _, lang := range i18n.Supported() {
		code := string(lang)
		f.Names[code] = r.PostFormValue("name_" + code)
		f.Slugs[code] = r.PostFormValue("slug_" + code)
	}
	if f.ParentID == cid && cid != 0 {
		f.ParentID = 0 // категория не может быть родителем самой себе
	}

	_, err := h.Catalog.SaveCategory(r.Context(), p.AccountID, storeID, cid, f)
	switch {
	case errors.Is(err, service.ErrCategoryNotFound):
		return httpx.ErrNotFound("Категория не найдена")
	case errors.Is(err, service.ErrNameRequired), errors.Is(err, service.ErrSlugTaken):
		form := newForm()
		if errors.Is(err, service.ErrSlugTaken) {
			form.Alert = l.T("catalog.err_slug_taken")
		} else {
			form.Alert = l.T("categories.err_name_required")
		}
		// Показываем введённые значения повторно.
		e := categoryEditFromForm(cid, f)
		return h.renderCategoryForm(w, r, http.StatusUnprocessableEntity, storeID, cid, e, form)
	case err != nil:
		return err
	}
	redirect(w, r, categoriesBase(l.Lang(), storeID)+"?saved=1")
	return nil
}

// CategoryDelete — POST /stores/{id}/categories/{cid}/delete.
func (h *PageHandler) CategoryDelete(w http.ResponseWriter, r *http.Request) error {
	p, storeID, ok := h.requireStore(w, r)
	if !ok {
		return nil
	}
	cid, err := idParam(r, "cid")
	if err != nil {
		return err
	}
	l := h.localizer(r)
	err = h.Catalog.DeleteCategory(r.Context(), p.AccountID, storeID, cid)
	switch {
	case errors.Is(err, service.ErrCategoryNotFound):
		return httpx.ErrNotFound("Категория не найдена")
	case errors.Is(err, service.ErrCategoryNotEmpty):
		// Возвращаем на список с пояснением.
		redirect(w, r, categoriesBase(l.Lang(), storeID)+"?notempty=1")
		return nil
	case err != nil:
		return err
	}
	redirect(w, r, categoriesBase(l.Lang(), storeID)+"?deleted=1")
	return nil
}

func (h *PageHandler) renderCategoryForm(w http.ResponseWriter, r *http.Request, status int, storeID, cid int64, e *repository.CategoryEdit, form FormView) error {
	l := h.localizer(r)
	langs := make([]LangValue, 0, len(i18n.Supported()))
	for _, lang := range i18n.Supported() {
		v := LangValue{Code: string(lang), Name: lang.Name()}
		if e != nil {
			tr := e.Trs[string(lang)]
			v.Title = tr.Name // используем Title как «имя категории»
			v.Slug = tr.Slug
		}
		langs = append(langs, v)
	}
	// Список категорий для выбора родителя (без самой категории).
	cats, err := h.Catalog.Categories(r.Context(), tenant.FromContext(r.Context()).AccountID, storeID)
	if err != nil {
		return err
	}
	parents := make([]repository.CabinetCategory, 0, len(cats))
	for _, c := range cats {
		if c.ID != cid {
			parents = append(parents, c)
		}
	}

	titleKey := "categories.new_title"
	action := categoriesBase(l.Lang(), storeID) + "/new"
	var parentID int64
	sort := 0
	fromKaspi := false
	if e != nil {
		titleKey = "categories.edit_title"
		action = categoryEditPath(l.Lang(), storeID, cid)
		parentID = e.ParentID
		sort = e.Sort
		fromKaspi = e.FromKaspi
	}

	data := h.page(r, titleKey).
		With("StoreID", storeID).
		With("StoreName", h.storeName(r, tenant.FromContext(r.Context()).AccountID, storeID)).
		With("CategoryID", cid).
		With("Langs", langs).
		With("Parents", parents).
		With("ParentID", parentID).
		With("Sort", sort).
		With("FromKaspi", fromKaspi).
		With("IsNew", e == nil).
		With("Form", form).
		With("Action", action).
		With("DeleteAction", fmt.Sprintf("%s/%d/delete", categoriesBase(l.Lang(), storeID), cid)).
		With("CategoriesURL", categoriesBase(l.Lang(), storeID))
	return h.Render.Render(w, status, "category_form", data)
}

// categoryEditFromForm восстанавливает введённые значения для повторного показа.
func categoryEditFromForm(cid int64, f service.CategoryForm) *repository.CategoryEdit {
	e := &repository.CategoryEdit{ID: cid, ParentID: f.ParentID, Sort: f.Sort, Trs: map[string]repository.CategoryTr{}}
	for lang, name := range f.Names {
		e.Trs[lang] = repository.CategoryTr{Name: name, Slug: f.Slugs[lang]}
	}
	if cid == 0 {
		return e
	}
	return e
}

// --- Опции статусов ---

func validProductStatus(s string) bool {
	for _, x := range productStatuses {
		if x == s {
			return true
		}
	}
	return false
}

func productStatusOptions(l *i18n.Localizer, selected string) []statusOption {
	out := make([]statusOption, 0, len(productStatuses))
	for _, s := range productStatuses {
		out = append(out, statusOption{Value: s, Label: l.T("catalog.status_" + s), Selected: s == selected})
	}
	return out
}

// priceInputValue форматирует минорную цену в строку основных единиц для поля
// ввода (без символа валюты): 199000 → "1990", 199050 → "1990.50".
func priceInputValue(minor int64) string {
	if minor == 0 {
		return ""
	}
	whole := minor / 100
	cents := minor % 100
	if cents == 0 {
		return strconv.FormatInt(whole, 10)
	}
	return fmt.Sprintf("%d.%02d", whole, cents)
}
