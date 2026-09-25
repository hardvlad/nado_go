// Package worker — реестр обработчиков удалённого воркера (cmd/nado-jobs).
//
// Здесь живёт код, который выполняется НЕ на сервере, а на удалённых машинах:
// работа с личным кабинетом Kaspi и отправка OTP. Обработчики получают всё
// нужное (учётные данные, параметры) в payload задачи — сервер кладёт их туда
// при постановке. Результаты (каталог) обработчик шлёт на сервер через клиента.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"nado_go/internal/jobs"
	"nado_go/internal/jobsclient"
)

// knownKinds — типы задач, которые воркер умеет обрабатывать.
var knownKinds = []string{
	jobs.KindKaspiSyncCatalog,
	jobs.KindKaspiSendOTP,
	jobs.KindKaspiVerifyOTP,
	jobs.KindKaspiImportOrders,
}

// AvailableKinds — типы задач воркера (для подсказки в CLI).
func AvailableKinds() []string {
	out := append([]string(nil), knownKinds...)
	sort.Strings(out)
	return out
}

// Register привязывает к клиенту обработчики для запрошенных типов.
// Неизвестный тип — ошибка запуска, а не молчаливый простой.
func Register(client *jobsclient.Client, kinds []string) error {
	for _, kind := range kinds {
		h, err := handlerFor(client, kind)
		if err != nil {
			return err
		}
		client.Register(kind, h)
	}
	return nil
}

// handlerFor строит обработчик типа задачи, захватывая клиента для обращений
// к серверу (приём каталога, код MFA).
func handlerFor(client *jobsclient.Client, kind string) (jobsclient.Handler, error) {
	switch kind {
	case jobs.KindKaspiSyncCatalog:
		return newKaspiCatalogHandler(client), nil
	case jobs.KindKaspiSendOTP:
		return newKaspiSendOTPHandler(client), nil
	case jobs.KindKaspiVerifyOTP:
		return newKaspiVerifyOTPHandler(client), nil
	case jobs.KindKaspiImportOrders:
		// Импорт заказов идёт по официальному API на сервере (local job),
		// удалённому воркеру он не выдаётся.
		return notImplemented(kind), nil
	default:
		return nil, fmt.Errorf("worker: неизвестный тип задачи %q; доступны: %v", kind, AvailableKinds())
	}
}

// notImplemented — временная заглушка до реализации обработчика.
func notImplemented(kind string) jobsclient.Handler {
	return func(context.Context, jobs.LeasedJob) (json.RawMessage, error) {
		return nil, jobs.Permanent(fmt.Errorf("worker: обработчик %s ещё не реализован", kind))
	}
}
