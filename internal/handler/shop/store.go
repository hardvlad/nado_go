// Package shop — публичная витрина магазина (D-04): выбор магазина по Host в
// проде и по префиксу /shop/{slug} в разработке, затем страницы каталога,
// корзины и оформления. Магазин кладётся в контекст запроса middleware-резолвером.
package shop

import (
	"context"
	"strings"
	"sync"
	"time"

	"nado_go/internal/model"
)

type ctxKey int

const (
	storeKey ctxKey = iota
	prefixKey
	customerKey
	cartCountKey
)

// WithCustomer/CustomerFrom переносят вошедшего покупателя (или nil) в контексте.
func WithCustomer(ctx context.Context, c any) context.Context {
	return context.WithValue(ctx, customerKey, c)
}

// CustomerFrom возвращает покупателя из контекста (nil, если не вошёл).
func CustomerFrom(ctx context.Context) any { return ctx.Value(customerKey) }

// WithCartCount/CartCountFrom переносят число позиций в корзине для бейджа.
func WithCartCount(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, cartCountKey, n)
}

// CartCountFrom возвращает число позиций в корзине (0, если нет).
func CartCountFrom(ctx context.Context) int {
	n, _ := ctx.Value(cartCountKey).(int)
	return n
}

// WithPrefix кладёт префикс URL витрины (/shop/{slug} в dev, "" в проде) для
// построения ссылок в шаблонах.
func WithPrefix(ctx context.Context, prefix string) context.Context {
	return context.WithValue(ctx, prefixKey, prefix)
}

// PrefixFrom возвращает префикс URL витрины из контекста.
func PrefixFrom(ctx context.Context) string {
	p, _ := ctx.Value(prefixKey).(string)
	return p
}

// WithStore кладёт разрешённый магазин в контекст.
func WithStore(ctx context.Context, s *model.StorefrontStore) context.Context {
	return context.WithValue(ctx, storeKey, s)
}

// StoreFrom достаёт магазин витрины из контекста (nil, если не разрешён).
func StoreFrom(ctx context.Context) *model.StorefrontStore {
	s, _ := ctx.Value(storeKey).(*model.StorefrontStore)
	return s
}

// Resolver находит магазин по slug или домену (репозиторий).
type Resolver interface {
	ResolveBySlug(ctx context.Context, slug string) (*model.StorefrontStore, error)
	ResolveByHost(ctx context.Context, host string) (*model.StorefrontStore, error)
}

// storeCache кеширует разрешение магазина в памяти с TTL: на каждый запрос
// витрины иначе шёл бы запрос в БД. Инвалидация — по истечении TTL (60 с).
type storeCache struct {
	resolver Resolver
	ttl      time.Duration
	mu       sync.RWMutex
	bySlug   map[string]cacheEntry
	byHost   map[string]cacheEntry
}

type cacheEntry struct {
	store *model.StorefrontStore
	exp   time.Time
}

func newStoreCache(r Resolver, ttl time.Duration) *storeCache {
	return &storeCache{resolver: r, ttl: ttl, bySlug: map[string]cacheEntry{}, byHost: map[string]cacheEntry{}}
}

func (c *storeCache) lookup(m map[string]cacheEntry, key string) (*model.StorefrontStore, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := m[key]
	if !ok || time.Now().After(e.exp) {
		return nil, false
	}
	return e.store, true
}

func (c *storeCache) put(m map[string]cacheEntry, key string, s *model.StorefrontStore) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m[key] = cacheEntry{store: s, exp: time.Now().Add(c.ttl)}
}

func (c *storeCache) bySlugKey(ctx context.Context, slug string) (*model.StorefrontStore, error) {
	if s, ok := c.lookup(c.bySlug, slug); ok {
		return s, nil
	}
	s, err := c.resolver.ResolveBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	c.put(c.bySlug, slug, s)
	return s, nil
}

func (c *storeCache) byHostKey(ctx context.Context, host string) (*model.StorefrontStore, error) {
	if s, ok := c.lookup(c.byHost, host); ok {
		return s, nil
	}
	s, err := c.resolver.ResolveByHost(ctx, host)
	if err != nil {
		return nil, err
	}
	c.put(c.byHost, host, s)
	return s, nil
}

// normalizeHost приводит Host к нижнему регистру и отрезает порт.
func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host
}
