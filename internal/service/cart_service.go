package service

import (
	"context"

	"nado_go/internal/httpx"
	"nado_go/internal/model"
	"nado_go/internal/repository"
)

// CartService — корзина витрины. Состав хранится на сервере по токену из cookie;
// цены пересчитываются из store_offers на каждом показе.
type CartService struct {
	carts *repository.CartRepository
}

func NewCartService(carts *repository.CartRepository) *CartService {
	return &CartService{carts: carts}
}

// Add добавляет товар в корзину (создаёт её при необходимости).
func (s *CartService) Add(ctx context.Context, storeID int64, token string, variantID int64, qty int) error {
	if qty <= 0 {
		qty = 1
	}
	cartID, err := s.carts.EnsureCart(ctx, storeID, token)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	if err := s.carts.AddItem(ctx, cartID, variantID, qty); err != nil {
		return httpx.ErrInternal(err)
	}
	return nil
}

// SetQty меняет количество позиции (0 — удалить).
func (s *CartService) SetQty(ctx context.Context, storeID int64, token string, variantID, qty int) error {
	cartID, err := s.carts.EnsureCart(ctx, storeID, token)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	if err := s.carts.SetQty(ctx, cartID, int64(variantID), qty); err != nil {
		return httpx.ErrInternal(err)
	}
	return nil
}

// Remove убирает позицию из корзины.
func (s *CartService) Remove(ctx context.Context, storeID int64, token string, variantID int64) error {
	cartID, err := s.carts.EnsureCart(ctx, storeID, token)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	if err := s.carts.RemoveItem(ctx, cartID, variantID); err != nil {
		return httpx.ErrInternal(err)
	}
	return nil
}

// View возвращает корзину с позициями и итогом (пустую, если корзины нет).
func (s *CartService) View(ctx context.Context, storeID int64, token, lang, defLang string) (*model.Cart, error) {
	cart := &model.Cart{Token: token}
	if token == "" {
		return cart, nil
	}
	cartID, err := s.carts.EnsureCart(ctx, storeID, token)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	cart.ID = cartID
	lines, err := s.carts.Lines(ctx, storeID, cartID, lang, defLang)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	cart.Lines = lines
	for _, l := range lines {
		cart.TotalMinor += l.LineMinor
		cart.Count += l.Qty
		if cart.Currency == "" {
			cart.Currency = l.Currency
		}
	}
	return cart, nil
}

// Count — число единиц в корзине для бейджа шапки.
func (s *CartService) Count(ctx context.Context, storeID int64, token string) int {
	if token == "" {
		return 0
	}
	n, err := s.carts.CountByToken(ctx, storeID, token)
	if err != nil {
		return 0
	}
	return n
}
