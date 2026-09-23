// Package model содержит доменные сущности, общие для всех слоёв.
package model

import "time"

// User — пример доменной сущности.
type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Допустимые статусы пользователя.
const (
	UserStatusActive   = "active"
	UserStatusBlocked  = "blocked"
	UserStatusArchived = "archived"
)

// UserFilter — параметры выборки списка пользователей.
type UserFilter struct {
	Search string // подстрока для поиска по имени и email
	Status string // пустое значение — без фильтра по статусу
	Limit  int
	Offset int
}
