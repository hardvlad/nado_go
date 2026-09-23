// Package service — бизнес-логика. Слой ничего не знает про HTTP и про
// конкретную СУБД: принимает контекст и доменные типы, возвращает доменные
// ошибки httpx.Error, пригодные для отдачи клиенту.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nado_go/internal/database"
	"nado_go/internal/httpx"
	"nado_go/internal/model"
)

// UserStore — то, что сервису нужно от хранилища. Интерфейс объявлен здесь,
// у потребителя: репозиторий о нём не знает, а в тестах подставляется заглушка.
type UserStore interface {
	GetByID(ctx context.Context, id int64) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	List(ctx context.Context, f model.UserFilter) ([]model.User, int64, error)
	Create(ctx context.Context, u *model.User) (*model.User, error)
	Update(ctx context.Context, u *model.User) (*model.User, error)
	Delete(ctx context.Context, id int64) error
}

type UserService struct {
	users UserStore
}

func NewUserService(users UserStore) *UserService {
	return &UserService{users: users}
}

// CreateUserInput — входные данные создания пользователя.
type CreateUserInput struct {
	Email string `json:"email" validate:"required,email,max=255"`
	Name  string `json:"name"  validate:"required,min=2,max=150"`
}

// UpdateUserInput — входные данные обновления.
type UpdateUserInput struct {
	Name   string `json:"name"   validate:"required,min=2,max=150"`
	Status string `json:"status" validate:"required,oneof=active blocked archived"`
}

func (s *UserService) Get(ctx context.Context, id int64) (*model.User, error) {
	user, err := s.users.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, httpx.ErrNotFound("Пользователь не найден").WithCause(err)
		}
		return nil, httpx.ErrInternal(err)
	}
	return user, nil
}

func (s *UserService) List(ctx context.Context, f model.UserFilter) ([]model.User, int64, error) {
	users, total, err := s.users.List(ctx, f)
	if err != nil {
		return nil, 0, httpx.ErrInternal(err)
	}
	return users, total, nil
}

func (s *UserService) Create(ctx context.Context, in CreateUserInput) (*model.User, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))

	// Предварительная проверка даёт понятное сообщение; гонку между проверкой
	// и вставкой всё равно ловит уникальный индекс ниже.
	switch _, err := s.users.GetByEmail(ctx, email); {
	case err == nil:
		return nil, httpx.ErrConflict("Пользователь с таким email уже существует")
	case !errors.Is(err, database.ErrNotFound):
		return nil, httpx.ErrInternal(err)
	}

	user, err := s.users.Create(ctx, &model.User{
		Email:  email,
		Name:   strings.TrimSpace(in.Name),
		Status: model.UserStatusActive,
	})
	if err != nil {
		if errors.Is(err, database.ErrConflict) {
			return nil, httpx.ErrConflict("Пользователь с таким email уже существует").WithCause(err)
		}
		return nil, httpx.ErrInternal(err)
	}
	return user, nil
}

func (s *UserService) Update(ctx context.Context, id int64, in UpdateUserInput) (*model.User, error) {
	user, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	user.Name = strings.TrimSpace(in.Name)
	user.Status = in.Status

	updated, err := s.users.Update(ctx, user)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, httpx.ErrNotFound("Пользователь не найден").WithCause(err)
		}
		return nil, httpx.ErrInternal(err)
	}
	return updated, nil
}

func (s *UserService) Delete(ctx context.Context, id int64) error {
	if err := s.users.Delete(ctx, id); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return httpx.ErrNotFound("Пользователь не найден").WithCause(err)
		}
		if errors.Is(err, database.ErrConflict) {
			return httpx.ErrConflict("Пользователя нельзя удалить: есть связанные записи").WithCause(err)
		}
		return httpx.ErrInternal(fmt.Errorf("удаление пользователя %d: %w", id, err))
	}
	return nil
}
