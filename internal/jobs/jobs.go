// Package jobs — очередь фоновых задач в MS SQL Server (D-12) и раздача
// заданий удалённым воркерам.
//
// Задача выполняется либо на сервере (Execution=Local, её берёт встроенный
// Runner), либо на удалённом воркере (Execution=Remote, её выдаёт HTTP-API
// раздачи заданий). Удалённо выполняется всё, что связано с личным кабинетом
// Kaspi и отправкой OTP.
//
// Обработчик задачи обязан быть идемпотентным: задачу могут выполнить дважды
// (истекла аренда упавшего воркера, повторная доставка результата).
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// Execution — где выполняется задача.
type Execution string

const (
	Local  Execution = "local"  // встроенный воркер сервера
	Remote Execution = "remote" // удалённый воркер по HTTP
)

// Статусы задачи.
const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// Типы задач. Значение хранится в jobs.kind.
const (
	KindKaspiSendOTP      = "kaspi.send_otp"      // remote: отправка OTP владельцу кабинета
	KindKaspiVerifyOTP    = "kaspi.verify_otp"    // remote: подтверждение OTP, создание доступа
	KindKaspiSyncCatalog  = "kaspi.sync_catalog"  // remote: обход каталога кабинета
	KindKaspiImportOrders = "kaspi.import_orders" // remote: импорт заказов
	KindMediaFetch        = "media.fetch"         // local: скачивание фото
	KindPricingRecalc     = "pricing.recalc"      // local: пересчёт цен магазина
)

// Job — единица работы в очереди.
type Job struct {
	ID          int64
	Kind        string
	Execution   Execution
	AccountID   int64 // 0 — системная задача
	Payload     json.RawMessage
	Attempts    int
	MaxAttempts int
}

// Enqueue — параметры постановки задачи.
type Enqueue struct {
	Kind      string
	Execution Execution // пусто = Local
	AccountID int64
	Payload   any       // сериализуется в JSON
	RunAt     time.Time // ноль = сейчас
	// DedupKey: пока в очереди или выполняется задача с таким ключом, повторная
	// постановка игнорируется (не ошибка).
	DedupKey    string
	Priority    int // 0 = обычный (100); меньше — раньше
	MaxAttempts int // 0 = 10
}

// Result — итог выполнения задачи, который присылает воркер.
type Result struct {
	OK    bool
	Data  json.RawMessage // сохраняется в jobs.result
	Error string          // причина, если !OK
	// Permanent: не повторять даже при неисчерпанных попытках (неверный токен,
	// удалённое подключение — нужна реакция продавца, а не повтор).
	Permanent bool
	// RetryAfter: перенести на это время (например, из Retry-After провайдера).
	// Попытка при этом не засчитывается.
	RetryAfter time.Duration
}

// permanentError помечает ошибку как неповторяемую.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent оборачивает ошибку так, что задача сразу помечается failed.
func Permanent(err error) error { return permanentError{err: err} }

// IsPermanent сообщает, что ошибку не нужно повторять.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

// Handler выполняет задачу одного типа. Возвращаемые данные пишутся в
// jobs.result. Идемпотентность — на совести обработчика.
type Handler func(ctx context.Context, job Job) (json.RawMessage, error)

// backoff — задержка перед повтором: экспонента с потолком в час и джиттером,
// чтобы упавшие разом задачи не били по серверу одновременно.
func backoff(attempts int) time.Duration {
	const base, max = 5 * time.Second, time.Hour
	d := time.Duration(math.Min(float64(base)*math.Pow(2, float64(attempts-1)), float64(max)))
	jitter := time.Duration(rand.Int64N(int64(d) / 5)) // ±20%
	return d - d/10 + jitter
}

// payloadJSON сериализует payload в JSON или "{}" для nil.
func payloadJSON(v any) (json.RawMessage, error) {
	if v == nil {
		return json.RawMessage("{}"), nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("jobs: сериализация payload: %w", err)
	}
	return b, nil
}
