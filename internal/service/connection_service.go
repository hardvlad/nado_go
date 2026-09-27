package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"nado_go/internal/database"
	"nado_go/internal/httpx"
	"nado_go/internal/integration/marketplace/kaspi"
	"nado_go/internal/jobs"
	"nado_go/internal/model"
	"nado_go/internal/repository"
	"nado_go/internal/secrets"
)

// Ошибки подключения магазина.
var (
	ErrKaspiTokenInvalid = errors.New("service: неверный токен Kaspi")
	ErrStoreNameRequired = errors.New("service: не указано имя магазина")
)

// initialOrdersWindow — за какой период импортируются заказы при первом
// подключении, если синхронизаций ещё не было. Kaspi ограничивает диапазон дат
// создания в запросе заказов ~14 днями, поэтому берём 14 дней.
const initialOrdersWindow = 14 * 24 * time.Hour

// ConnectionService подключает магазин к маркетплейсу и синхронизирует заказы.
type ConnectionService struct {
	stores     *repository.StoreRepository
	orders     *repository.MarketplaceOrderRepository
	box        *secrets.Box
	kaspi      *kaspi.Official
	jobs       *jobs.Repository
	shopSuffix string // домен платформы для поддомена витрины (<slug>.<suffix>)
	trialDays  int    // бесплатный пробный период нового магазина, дней
	log        *slog.Logger
}

func NewConnectionService(stores *repository.StoreRepository, orders *repository.MarketplaceOrderRepository, box *secrets.Box, k *kaspi.Official, jobsRepo *jobs.Repository, shopSuffix string, trialDays int, log *slog.Logger) *ConnectionService {
	return &ConnectionService{stores: stores, orders: orders, box: box, kaspi: k, jobs: jobsRepo, shopSuffix: shopSuffix, trialDays: trialDays, log: log}
}

// KaspiCabinetCreds — реквизиты кабинета, сохраняемые на подключении для
// повторной проверки и редактирования. PasswordCipher уже зашифрован.
type KaspiCabinetCreds struct {
	Login          string
	PasswordCipher []byte
	MerchantUID    string
}

// CreateKaspiStoreFromToken проверяет токен официального API, создаёт магазин с
// подключением (сохраняя реквизиты кабинета) и ставит первый импорт заказов.
// Вызывается кабинетным онбордингом при сохранении магазина продавцом.
func (s *ConnectionService) CreateKaspiStoreFromToken(ctx context.Context, accountID int64, storeName, token string, cab KaspiCabinetCreds) (storeID, connID int64, err error) {
	info, err := s.kaspi.Verify(ctx, token)
	if err != nil {
		if errors.Is(err, kaspi.ErrUnauthorized) {
			return 0, 0, ErrKaspiTokenInvalid
		}
		return 0, 0, httpx.ErrInternal(err)
	}

	cipher, err := s.box.EncryptString(token)
	if err != nil {
		return 0, 0, httpx.ErrInternal(err)
	}
	meta, _ := json.Marshal(map[string]any{"orders_total": info.OrdersTotal})

	params := func() repository.NewConnection {
		return repository.NewConnection{
			AccountID:             accountID,
			Name:                  storeName,
			Slug:                  genSlug(storeName),
			Country:               "KZ",
			BaseCurrency:          "KZT",
			Marketplace:           "kaspi",
			SecretCiphertext:      cipher,
			PublicMeta:            string(meta),
			ConnectionName:        storeName,
			ShopSuffix:            s.shopSuffix,
			TrialDays:             s.trialDays,
			CabinetLogin:          cab.Login,
			CabinetPasswordCipher: cab.PasswordCipher,
			MerchantUID:           cab.MerchantUID,
		}
	}

	storeID, connID, err = s.stores.CreateStoreWithConnection(ctx, params())
	if errors.Is(err, database.ErrConflict) {
		// Крайне маловероятное совпадение slug — повторим один раз с новым.
		storeID, connID, err = s.stores.CreateStoreWithConnection(ctx, params())
	}
	if err != nil {
		return 0, 0, httpx.ErrInternal(err)
	}

	// Первый импорт заказов — фоновой задачей на сервере.
	if _, err := s.jobs.Enqueue(ctx, jobs.Enqueue{
		Kind:      jobs.KindKaspiImportOrders,
		Execution: jobs.Local,
		AccountID: accountID,
		Payload:   map[string]any{"connection_id": connID},
		DedupKey:  fmt.Sprintf("kaspi_orders:%d", connID),
	}); err != nil {
		s.log.Warn("не удалось поставить импорт заказов", slog.Int64("connection_id", connID), slog.Any("error", err))
	}

	return storeID, connID, nil
}

// ErrStoreNotFound — магазин не найден или чужой.
var ErrStoreNotFound = errors.New("service: магазин не найден")

// KaspiStoreEdit — данные магазина для формы редактирования.
type KaspiStoreEdit struct {
	StoreID      int64
	Name         string
	Token        string // расшифрованный токен API
	CabinetLogin string
	MerchantUID  string
}

// StoreForEdit возвращает магазин аккаунта для формы редактирования.
func (s *ConnectionService) StoreForEdit(ctx context.Context, accountID, storeID int64) (*KaspiStoreEdit, error) {
	e, err := s.stores.GetStoreForEdit(ctx, accountID, storeID)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrStoreNotFound
	}
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	token, err := s.box.DecryptString(e.SecretCiphertext)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return &KaspiStoreEdit{StoreID: e.StoreID, Name: e.Name, Token: token, CabinetLogin: e.CabinetLogin, MerchantUID: e.MerchantUID}, nil
}

// UpdateKaspiStore проверяет новый токен, обновляет магазин и реквизиты кабинета
// и переставляет синхронизацию, если реквизиты кабинета заданы. Пустой пароль —
// не менять. Как ветка редактирования в AddKaspiShop.php: проверка при сохранении.
func (s *ConnectionService) UpdateKaspiStore(ctx context.Context, accountID, storeID int64, name, token, cabinetLogin, cabinetPassword string) error {
	name = strings.TrimSpace(name)
	token = strings.TrimSpace(token)
	cabinetLogin = strings.ToLower(strings.TrimSpace(cabinetLogin))
	if name == "" {
		return ErrStoreNameRequired
	}
	if token == "" {
		return ErrKaspiTokenInvalid
	}

	e, err := s.stores.GetStoreForEdit(ctx, accountID, storeID)
	if errors.Is(err, database.ErrNotFound) {
		return ErrStoreNotFound
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}

	// Проверка токена официальным API (как validateToken в PHP).
	if _, err := s.kaspi.Verify(ctx, token); err != nil {
		if errors.Is(err, kaspi.ErrUnauthorized) {
			return ErrKaspiTokenInvalid
		}
		return httpx.ErrInternal(err)
	}

	tokenCipher, err := s.box.EncryptString(token)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	var passCipher []byte
	if cabinetPassword != "" {
		if passCipher, err = s.box.EncryptString(cabinetPassword); err != nil {
			return httpx.ErrInternal(err)
		}
	}

	if err := s.stores.UpdateStore(ctx, repository.UpdateStoreForEdit{
		StoreID: e.StoreID, ConnectionID: e.ConnectionID, CredentialsID: e.CredentialsID,
		Name: name, TokenCipher: tokenCipher, CabinetLogin: cabinetLogin,
		CabinetPasswordCipher: passCipher, MerchantUID: e.MerchantUID,
	}); err != nil {
		return httpx.ErrInternal(err)
	}

	// Обновить заказы всегда; каталог — если есть логин кабинета (и новый пароль,
	// либо уже сохранённый).
	if _, err := s.jobs.Enqueue(ctx, jobs.Enqueue{
		Kind: jobs.KindKaspiImportOrders, Execution: jobs.Local, AccountID: accountID,
		Payload: map[string]any{"connection_id": e.ConnectionID}, DedupKey: fmt.Sprintf("kaspi_orders:%d", e.ConnectionID),
	}); err != nil {
		s.log.Warn("edit: не удалось поставить импорт заказов", slog.Int64("connection_id", e.ConnectionID), slog.Any("error", err))
	}
	if cabinetLogin != "" && cabinetPassword != "" {
		if _, err := s.jobs.Enqueue(ctx, jobs.Enqueue{
			Kind: jobs.KindKaspiSyncCatalog, Execution: jobs.Remote,
			Payload:  map[string]any{"connection_id": e.ConnectionID, "login": cabinetLogin, "password": cabinetPassword, "selected_merchant": e.MerchantUID},
			DedupKey: fmt.Sprintf("kaspi_sync_catalog:%d", e.ConnectionID),
		}); err != nil {
			s.log.Warn("edit: не удалось поставить импорт каталога", slog.Int64("connection_id", e.ConnectionID), slog.Any("error", err))
		}
	}
	return nil
}

// ImportKaspiOrders — тело фоновой задачи kaspi.import_orders.
func (s *ConnectionService) ImportKaspiOrders(ctx context.Context, connectionID int64) (created, updated int, err error) {
	conn, err := s.stores.GetConnectionForSync(ctx, connectionID)
	if errors.Is(err, database.ErrNotFound) {
		return 0, 0, jobs.Permanent(fmt.Errorf("подключение %d не найдено", connectionID))
	}
	if err != nil {
		return 0, 0, err
	}

	token, err := s.box.DecryptString(conn.SecretCiphertext)
	if err != nil {
		return 0, 0, jobs.Permanent(fmt.Errorf("токен подключения %d нечитаем: %w", connectionID, err))
	}

	since := conn.OrdersSyncAt
	if since.IsZero() {
		since = time.Now().Add(-initialOrdersWindow)
	}

	for order, err := range s.kaspi.Orders(ctx, token, since) {
		if err != nil {
			if errors.Is(err, kaspi.ErrUnauthorized) {
				_ = s.stores.MarkConnectionInvalid(ctx, connectionID)
				return created, updated, jobs.Permanent(ErrKaspiTokenInvalid)
			}
			return created, updated, err
		}
		isNew, uerr := s.orders.Upsert(ctx, &model.MarketplaceOrder{
			ConnectionID: conn.ConnectionID, StoreID: conn.StoreID, AccountID: conn.AccountID,
			ExternalID: order.ExternalID, Code: order.Code, State: order.State, Status: order.Status,
			TotalMinor: order.TotalMinor, Currency: order.Currency,
			CustomerName: order.CustomerName, CustomerPhone: order.CustomerPhone,
			OrderedAt: order.OrderedAt, Raw: order.Raw,
		})
		if uerr != nil {
			return created, updated, uerr
		}
		if isNew {
			created++
		} else {
			updated++
		}
	}

	if err := s.stores.TouchOrdersSync(ctx, connectionID); err != nil {
		return created, updated, err
	}
	return created, updated, nil
}

// Stores возвращает магазины аккаунта для кабинета.
func (s *ConnectionService) Stores(ctx context.Context, accountID int64) ([]model.Store, error) {
	stores, err := s.stores.ListStores(ctx, accountID)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return stores, nil
}

// genSlug строит slug из имени: латиница/цифры плюс случайный суффикс, чтобы
// адрес был уникальным без обработки конфликтов. Продавец сменит позже.
func genSlug(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case (r == ' ' || r == '-' || r == '_') && b.Len() > 0 && !prevDash:
			b.WriteByte('-')
			prevDash = true
		}
	}
	base := strings.Trim(b.String(), "-")
	if len(base) < 3 {
		base = "shop"
	}
	if len(base) > 40 {
		base = base[:40]
	}
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return base + "-" + hex.EncodeToString(buf)
}
