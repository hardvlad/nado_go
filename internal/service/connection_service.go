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
// подключении, если синхронизаций ещё не было.
const initialOrdersWindow = 30 * 24 * time.Hour

// ConnectionService подключает магазин к маркетплейсу и синхронизирует заказы.
type ConnectionService struct {
	stores *repository.StoreRepository
	orders *repository.MarketplaceOrderRepository
	box    *secrets.Box
	kaspi  *kaspi.Official
	jobs   *jobs.Repository
	log    *slog.Logger
}

func NewConnectionService(stores *repository.StoreRepository, orders *repository.MarketplaceOrderRepository, box *secrets.Box, k *kaspi.Official, jobsRepo *jobs.Repository, log *slog.Logger) *ConnectionService {
	return &ConnectionService{stores: stores, orders: orders, box: box, kaspi: k, jobs: jobsRepo, log: log}
}

// ConnectKaspi создаёт магазин с подключением к Kaspi по токену официального
// API, проверяет токен и ставит первый импорт заказов. Возвращает id подключения.
func (s *ConnectionService) ConnectKaspi(ctx context.Context, accountID int64, storeName, token string) (int64, error) {
	storeName = strings.TrimSpace(storeName)
	token = strings.TrimSpace(token)
	if storeName == "" {
		return 0, ErrStoreNameRequired
	}
	if token == "" {
		return 0, ErrKaspiTokenInvalid
	}
	_, connID, err := s.CreateKaspiStoreFromToken(ctx, accountID, storeName, token)
	return connID, err
}

// CreateKaspiStoreFromToken проверяет токен официального API, создаёт магазин с
// подключением и ставит первый импорт заказов. Используется и подключением по
// токену (ConnectKaspi), и кабинетным онбордингом (токен получен из кабинета).
func (s *ConnectionService) CreateKaspiStoreFromToken(ctx context.Context, accountID int64, storeName, token string) (storeID, connID int64, err error) {
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
			AccountID:        accountID,
			Name:             storeName,
			Slug:             genSlug(storeName),
			Country:          "KZ",
			BaseCurrency:     "KZT",
			Marketplace:      "kaspi",
			SecretCiphertext: cipher,
			PublicMeta:       string(meta),
			ConnectionName:   storeName,
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
