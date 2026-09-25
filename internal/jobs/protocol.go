package jobs

import "encoding/json"

// Протокол раздачи заданий удалённым воркерам. Одни и те же структуры
// использует и сервер (handler/jobsapi), и клиент (jobsclient).
//
// Базовый путь: /jobs-api/v1. Аутентификация — Authorization: Bearer <token>.
// Это не /api/v1: без CORS и CSRF, только для воркеров по токену.

// LeaseRequest — запрос задачи. POST /jobs-api/v1/lease.
type LeaseRequest struct {
	// Kinds — типы задач, которые готов взять воркер (по флагу -kind).
	// Сервер дополнительно сверяет их с разрешёнными для раннера.
	Kinds        []string `json:"kinds"`
	LeaseSeconds int      `json:"lease_seconds,omitempty"`
}

// LeasedJob — выданная задача. Пустой ответ (204) — задач нет.
type LeasedJob struct {
	ID          int64           `json:"id"`
	Kind        string          `json:"kind"`
	AccountID   int64           `json:"account_id,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
}

// CompleteRequest — успешный итог. POST /jobs-api/v1/jobs/{id}/complete.
type CompleteRequest struct {
	Data json.RawMessage `json:"data,omitempty"`
}

// FailRequest — неуспех. POST /jobs-api/v1/jobs/{id}/fail.
type FailRequest struct {
	Error             string `json:"error"`
	Permanent         bool   `json:"permanent,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}
