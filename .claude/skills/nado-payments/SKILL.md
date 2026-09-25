---
name: nado-payments
description: Приём оплаты в магазинах nado через мерчант-аккаунты самих продавцов (BYO). Провайдеры — ЮKassa (OAuth-партнёрка), Т-Касса, Halyk ePay, Freedom Pay и Kaspi Pay в полуручном режиме, позже CloudPayments и Robokassa. Внутри — интерфейс payment.Provider, жизненный цикл платежа, проверка подлинности вебхуков, идемпотентность, возвраты, форматы сумм и связь с чеками и заказом. Загружай при любой работе с оплатой заказа, подключением платёжного способа в кабинете, вебхуками банков, возвратами, статусами payments и refunds. Загружай и для рекуррентной оплаты подписки самой платформы, даже если провайдер не назван.
---

# Оплата

**Порядок реализации (D-13, старт в КЗ):** Halyk ePay → Freedom Pay → Kaspi Pay
(полуручной режим). ЮKassa и Т-Касса реализуются при выходе в РФ, интерфейс
позволяет добавить их без переделок. Биллинг подписок платформы (юрлицо РК)
идёт через казахстанского провайдера с рекуррентными платежами.

Решения (nado-platform → decisions.md):
- D-01: деньги идут напрямую продавцу, платформа их не держит;
- D-07: списание в валюте провайдера;
- D-08: чеки через отдельную кассу, провайдеру данные чека не передаются.

## Пакеты

```
internal/integration/payment/
  payment.go     интерфейс Provider, типы, реестр
  yookassa/  tkassa/  halykepay/  freedompay/  kaspimanual/
internal/service/payments.go       жизненный цикл, связь с заказом, постановка чека
internal/handler/webhook/payment.go  /webhooks/payment/{provider}/{token}
internal/handler/shop/payment_return.go  возврат покупателя после оплаты
```

## Интерфейс

```go
package payment

type Provider interface {
    Code() string
    Capabilities() Capabilities // Refunds, PartialRefunds, TwoStage, Recurring, Currencies []money.Currency, Webhooks
    CreatePayment(ctx context.Context, creds Credentials, req PaymentRequest) (*PaymentSession, error)
    GetPayment(ctx context.Context, creds Credentials, externalID string) (*PaymentStatus, error)
    Refund(ctx context.Context, creds Credentials, req RefundRequest) (*RefundResult, error)
    // ParseWebhook проверяет подлинность и разбирает уведомление. Непроверенное
    // уведомление — ошибка, а не событие.
    ParseWebhook(ctx context.Context, creds Credentials, r *http.Request) (*WebhookEvent, error)
}

type PaymentRequest struct {
    OrderID        int64
    OrderNumber    string
    Amount         money.Money   // в валюте списания
    Description    string        // ≤ 128 символов, ASCII-безопасно обрезать
    ReturnURL      string        // куда вернуть покупателя
    CustomerEmail, CustomerPhone string
    IdempotencyKey string        // payments.idempotency_key
    Lang           string
}

type PaymentSession struct {
    ExternalID  string
    RedirectURL string       // для redirect-сценария
    QRPayload   string       // для QR/СБП, если есть
    Instructions string      // для полуручного Kaspi
    Status      Status
}
```

Детали провайдеров — `references/<провайдер>.md`: авторизация, формат суммы,
статусы, проверка подписи, песочница. **Прочитай файл перед реализацией или
изменением адаптера.**

## Статусы платежа

```
pending → waiting_for_capture → succeeded → (partially_)refunded
   │              └→ canceled
   ├→ canceled | failed
   └→ awaiting_manual_confirmation (Kaspi) → succeeded | canceled
```

Каждый адаптер переводит статусы провайдера в эти значения. Переход проверяется
в service, повтор того же статуса — no-op.

## Жизненный цикл

1. **Checkout.** Заказ создан (`awaiting_payment`). Service создаёт строку
   `payments` с `idempotency_key` **до** вызова провайдера и вызывает
   `CreatePayment` с этим ключом. Сбой сети при повторе не создаст второй платёж у
   провайдера. Затем `external_id` и редирект.
2. **Возврат покупателя** на `ReturnURL`. Статусу из query-параметров не
   доверяем: вызываем `GetPayment` и показываем результат. Если платёж ещё
   `pending`, страница опрашивает статус через htmx (`hx-trigger="every 3s"`, до 2
   минут).
3. **Вебхук.**
   - URL `/webhooks/payment/{provider}/{token}` → по токену находим магазин и
     учётные данные → `ParseWebhook` (проверка подписи или IP) → дедупликация
     через `webhook_events.dedup_key`;
   - затем **сверка через `GetPayment`**, если провайдер не подписывает
     уведомления (ЮKassa);
   - затем переход статуса в транзакции вместе с заказом (`paid`) и постановкой
     задачи `fiscal.sell`;
   - ответ 200 после коммита. Ошибка → 5xx, и провайдер повторит.
4. **Страховка.** Задача `payment.poll` проверяет `pending` старше 10 минут, а
   заказы без оплаты отменяет по таймауту магазина (по умолчанию 60 минут) с
   освобождением брони.
5. **Возврат.** Service вызывает `Refund`. Сумма возвратов не может превысить
   `succeeded`-сумму. После успеха ставится задача `fiscal.refund`.

## Обязательные свойства

- **Суммы** — `money.Money` в минорных единицах. Конвертация в формат провайдера
  выполняется только в адаптере и покрыта тестом. Строка `"100.00"` у ЮKassa,
  копейки целым у Т-Кассы, decimal у других — смотри reference.
- **Валюта** должна входить в `Capabilities().Currencies`, иначе способ оплаты
  скрыт на checkout.
- **Сумма из вебхука** сверяется с суммой заказа. Расхождение → статус не
  меняется, алерт в лог (уровень error) и `sync_issues`-аналог для платежей.
- **Не логировать** полные тела вебхуков: в них ПД покупателя и маскированные
  карты.
- **Данные карт никогда не проходят через наш сервер.** Только hosted-формы и
  редиректы провайдеров, иначе нужен PCI DSS.
- **Kaspi (полуручной):** покупатель видит инструкцию и контакты продавца, заказ в
  `awaiting_manual_confirmation`, продавец подтверждает оплату кнопкой в кабинете.
  Подтверждение пишется в `audit_log`.

## Подключение способа в кабинете

- ЮKassa: кнопка «Подключить» → OAuth-редирект → callback
  `/cabinet/oauth/yookassa/callback` с проверкой `state` (CSRF) → токены в
  `provider_credentials`.
- Остальные провайдеры: форма ввода ключей → `Verify`-вызов (тестовый запрос к
  API) → сохранение. Продавцу показывается URL вебхука, если провайдер требует
  указать его в своём кабинете.
- Ключи повторно не показываются, только маска `••••1234`.

## Биллинг платформы (подписки продавцов)

Это отдельный контур: платформа — мерчант, продавец — плательщик. Используются
тот же интерфейс `Provider` и учётные данные платформы из конфигурации (не из
`provider_credentials`). Нужны рекуррентные платежи: сохранённый способ оплаты
ЮKassa в РФ, в КЗ — провайдер с рекуррентом (Freedom Pay или CloudPayments KZ, ⚠️
подтвердить). Функция `Recurring` добавляется в Capabilities только тем, кто её
реально поддерживает.
