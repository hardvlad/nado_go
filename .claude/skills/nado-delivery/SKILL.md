---
name: nado-delivery
description: Доставка заказов магазинов nado. Покрывает ручные способы (самовывоз, свой курьер, зоны и фиксированные цены) и интеграции СДЭК (РФ+КЗ), Яндекс Доставки, Почты России, Казпочты и Exline, а также будущую собственную службу доставки. Внутри — интерфейс delivery.Provider, расчёт стоимости на checkout, выбор ПВЗ на карте, создание отправлений, этикетки, трекинг и вебхуки статусов, габариты и вес из карточек маркетплейсов, кеш пунктов выдачи. Загружай при любой работе со способами доставки, тарифами, ПВЗ, отправлениями, трек-номерами, статусами shipments и адресами покупателей, даже если служба не названа.
---

# Доставка

**Порядок реализации (D-13, старт в КЗ):** ручные способы → СДЭК (КЗ) →
Казпочта → Exline. Яндекс Доставка и Почта России — при выходе в РФ (API Яндекса,
описанное в reference, Казахстан не покрывает).

Решение D-09 (nado-platform): несколько служб в MVP за одним интерфейсом.
Ручные способы и будущая собственная служба — такие же реализации интерфейса.
Особенности каждой службы — в `references/<служба>.md`. **Прочитай файл перед
реализацией адаптера.**

## Пакеты

```
internal/integration/delivery/
  delivery.go     интерфейс Provider, типы, реестр
  manual/         самовывоз, свой курьер, зоны — без внешнего API
  cdek/  yandexdelivery/  russianpost/  kazpost/  exline/
internal/service/delivery.go     quote на checkout, создание отправлений, трекинг
internal/service/pickup_points.go  кеш ПВЗ
```

## Интерфейс

```go
package delivery

type Capabilities struct {
    Countries     []string // "RU", "KZ"
    Courier       bool
    PickupPoints  bool
    CashOnDelivery bool
    Labels        bool
    Webhooks      bool
}

type Provider interface {
    Code() string
    Capabilities() Capabilities
    Quote(ctx context.Context, creds Credentials, req QuoteRequest) ([]Quote, error)
    PickupPoints(ctx context.Context, creds Credentials, q PointsQuery) ([]PickupPoint, error)
    CreateShipment(ctx context.Context, creds Credentials, req ShipmentRequest) (*Shipment, error)
    CancelShipment(ctx context.Context, creds Credentials, externalID string) error
    Label(ctx context.Context, creds Credentials, externalID string) (*Document, error)
    Track(ctx context.Context, creds Credentials, externalIDs []string) ([]TrackingEvent, error)
}

// Необязательный интерфейс — для служб с вебхуками.
type WebhookParser interface {
    ParseWebhook(ctx context.Context, creds Credentials, r *http.Request) ([]TrackingEvent, error)
}

type QuoteRequest struct {
    From, To  Location   // адрес, координаты, код города службы (заполняет service)
    Parcels   []Parcel   // вес г, габариты мм, объявленная ценность
    Mode      Mode       // courier | pickup_point
    PickupPointID string // если выбран ПВЗ
}

type Quote struct {
    TariffCode string
    Name       string
    Price      money.Money
    MinDays, MaxDays int
    Mode       Mode
}
```

Методы, которые служба не поддерживает, возвращают `delivery.ErrNotSupported`.
Возможности честно отражены в `Capabilities`, а UI скрывает недоступное.

## Статусы отправления

```
created → accepted → in_transit → ready_for_pickup → delivered
                         │                    └→ returned
                         └→ failed / canceled
```

Каждый адаптер переводит статусы службы в эти значения. Исходный статус
сохраняется в `shipment_events.raw`. `delivered` передаёт заказ в `delivered` и
ставит чек «полный расчёт» (nado-fiscal-receipts, схема `two_step`).

## Checkout

- Габариты и вес: вариант (из маркетплейса или вручную) → товар → значения магазина
  по умолчанию. **Без веса служба не посчитает тариф.** Товары без веса
  подсвечиваются в кабинете, а на checkout используется дефолт магазина.
- Упаковка заказа из нескольких позиций: суммарный вес и габариты «коробки» по
  простому правилу (самая длинная сторона, сумма высот). Этого достаточно для
  тарифа, точность упаковки — не цель MVP.
- `Quote` вызывается параллельно по всем включённым службам с общим таймаутом
  5 с. Не ответившая служба просто не показывается. Результат кешируется на
  10 минут по (служба, откуда, куда, вес, габариты).
- Цену доставки на витрине можно переопределить правилом магазина: «бесплатно
  от суммы», «фиксированно», «наценка %».

## Пункты выдачи

- Списки ПВЗ большие: у СДЭК десятки тысяч. Задача `delivery.sync_points` раз в
  сутки загружает их в таблицу `pickup_points (provider, external_id, country,
  city_code, lat, lon, address, schedule, raw)`.
- Карта на checkout получает точки через наш API по bbox или городу, а не
  напрямую из службы.
- Карта — Яндекс Карты JS API или Leaflet с OSM-тайлами. Ключ карты — настройка
  платформы, CSP расширяется для этого домена.

## Создание отправления

- Кнопка «Создать отправление» в заказе, либо автоматически после `paid`, если
  магазин так настроил.
- Отправитель — склад или адрес магазина из настроек. Получатель — снимок из
  заказа.
- Наложенный платёж в MVP **не используем**: оплата онлайн, а наложенный платёж
  требует отдельных чеков и сверок. `CashOnDelivery` появится позже.
- Этикетка (PDF) сохраняется в медиа (`label_media_id`) для печати.

## Трекинг

Вебхуки (если есть) приходят на `/webhooks/delivery/{provider}/{token}` и
проходят дедупликацию. Задача `delivery.track` дополнительно опрашивает активные
отправления раз в 1–3 часа пачками, чтобы не пропустить потерянный вебхук.

## Своя служба доставки (будущее)

Реализуется пакетом `internal/integration/delivery/<имя>`. Если партнёр даёт
API, это обычный адаптер. Если своя логистика, то `Quote` считает по тарифной
сетке из БД, а `Track` берёт статусы из нашего приложения для курьеров. Интерфейс
для этого менять не нужно.
