# ЮKassa (РФ) — справочник для реализации `payment.Provider`

Сведения собраны 25.09.2026. Пометка «⚠️ не проверено» означает, что факт взят из памяти или вторичного источника: сверьте его с документацией до реализации.

## Документация

| Тема | Ссылка |
|---|---|
| Справочник API | https://yookassa.ru/developers/api |
| Формат взаимодействия, Idempotence-Key | https://yookassa.ru/developers/using-api/interaction-format |
| Жизненный цикл платежа | https://yookassa.ru/developers/payment-acceptance/getting-started/payment-process |
| Входящие уведомления (webhooks) | https://yookassa.ru/developers/using-api/webhooks |
| Партнёрская программа, OAuth | https://yookassa.ru/developers/solutions-for-platforms/partners-api/basics |
| Получение токена | https://yookassa.ru/developers/solutions-for-platforms/partners-api/oauth/obtain-token |
| Отзыв токена | https://yookassa.ru/developers/solutions-for-platforms/partners-api/oauth/revoke-token |
| Тестирование | https://yookassa.ru/developers/payment-acceptance/testing-and-going-live/testing |
| Возвраты | https://yookassa.ru/developers/payment-acceptance/after-the-payment/refunds |

## Подключение продавца (BYO — деньги идут напрямую продавцу)

### Основной способ: OAuth, кнопка «Подключить ЮKassa»

Регистрация приложения платформы (делается один раз):
1. Приложение регистрируется на https://yookassa.ru/oauth/v2/client.
2. Поля: название, описание (его видит продавец), сайт, способ передачи кода «Передавать в Callback URL», Callback URL, набор прав.
3. На выходе платформа получает `client_id` и `client_secret`. Секрет хранится в конфиге платформы, не в БД.
4. Права, которые нужны: создание платежа, подтверждение, просмотр, отмена, создание возврата, просмотр возвратов. Права на рекуррентные платежи и на просмотр комиссий — по необходимости.
5. Чтобы получать вознаграждение, подайте заявку в партнёрскую программу; после неё с платформой свяжется менеджер ЮKassa. Для работы OAuth это не требуется.

Поток авторизации:
1. Продавца перенаправляют на `GET https://yookassa.ru/oauth/v2/authorize?client_id=<id>&response_type=code&state=<csrf>`.
   - `state` — до 1024 символов. Передавайте подписанный CSRF-токен, внутри которого ID магазина-арендатора.
2. ЮKassa возвращает на Callback URL:
   - `?code=...&state=...` — при успехе;
   - `?error=access_denied&state=...` — если продавец отказал.
3. **Код живёт 5 минут.** Обменивайте его сразу в обработчике callback.
4. Обмен: `POST https://yookassa.ru/oauth/v2/token`, `grant_type=authorization_code&code=<code>`.
   - Авторизация: `Authorization: Basic base64(client_id:client_secret)`. Второй вариант — передать `client_id` и `client_secret` в теле.
   - Ответ: `access_token` (от 32 до 512 символов) и `expires_in` в секундах.
5. **Токен живёт 5 лет. Refresh-токена нет.** Сохраняйте `expires_at`. Ближе к сроку просите продавца переподключиться.
6. После подключения:
   - вызовите `GET /v3/me` и сохраните `account_id` (это shopId) и флаг `test` ⚠️ не проверено (набор полей `/v3/me`);
   - зарегистрируйте webhooks (см. ниже).

Все запросы к API с OAuth-токеном отправляются с заголовком `Authorization: Bearer <token>`.

Отзыв доступа:
- Платформа отзывает токен так: `POST https://yookassa.ru/oauth/v2/revoke_token`, в теле `token=<token>`, авторизация Basic `client_id:client_secret`. Успешный ответ — `{}`.
- Продавец может отозвать доступ сам, в личном кабинете ЮKassa. **Уведомления об этом нет.** Узнать об отзыве можно только по ответу `401 invalid_credentials` на очередной запрос. При таком ответе переводите подключение в статус `revoked` и показывайте продавцу баннер «Переподключите ЮKassa».
- Ответ `403 forbidden` означает, что у токена не хватает прав.

### Запасной способ: shopId и секретный ключ

- Продавец вводит `shopId` и секретный ключ из ЛК: «Интеграция → Ключи API».
- Запросы идут с HTTP Basic `shopId:secret_key`.
- **Минус:** webhooks в этом режиме настраиваются только в ЛК продавца («Интеграция → HTTP-уведомления»). API `/v3/webhooks` доступен только с OAuth. Продавцу придётся вручную вписать наш URL уведомлений. Показывайте ему URL и инструкцию.

`Credentials` в Go: `{Mode: "oauth"|"basic", AccessToken, ShopID, SecretKey, ExpiresAt}`. Всё хранится зашифрованным.

## API: общие правила

- База: `https://api.yookassa.ru/v3/`. Тело — JSON, ответы тоже JSON.
- **Idempotence-Key** (заголовок) обязателен для POST и DELETE.
  - До 64 символов, рекомендуется UUID v4.
  - Ключ хранится 24 часа.
  - Тот же ключ с теми же данными вернёт тот же ответ.
  - Ключ генерируется **один раз на логическую операцию** (попытку оплаты или возврат) и сохраняется в БД до вызова API. При ретрае используйте сохранённый ключ.
- **HTTP 500 не значит «не прошло».** Повторите запрос с тем же ключом или проверьте объект через GET. Если 500 держится больше 30 минут, обращайтесь в поддержку.
- **HTTP 429 `too_many_requests`** — ставьте экспоненциальный backoff.
  - Источник по кодам ответа: https://yookassa.ru/developers/using-api/response-handling/http-codes

## Создание платежа

`POST /v3/payments`:

```json
{
  "amount": {"value": "1250.00", "currency": "RUB"},
  "capture": true,
  "confirmation": {"type": "redirect", "return_url": "https://shop.example/checkout/return?order=123"},
  "description": "Заказ №123",
  "metadata": {"order_id": "123", "tenant_id": "45", "attempt_id": "..."}
}
```

- **Сумма — строка** с точкой и двумя знаками после неё: `"1250.00"`. Конвертация из `money.Money{Minor: 125000}` делается строго целочисленно, без float: `fmt.Sprintf("%d.%02d", m/100, m%100)`.
- Валюта: принимаем только `RUB`. Список поддерживаемых валют в документации не перечислен явно ⚠️ не проверено. Мультивалютную витрину при оплате через ЮKassa пересчитывайте в RUB до создания платежа. Если валюта заказа не RUB, `Capabilities` возвращает её как неподдерживаемую.
- `capture: true` — одностадийная оплата, её используем по умолчанию.
  - `capture: false` — двухстадийная: `pending → waiting_for_capture`, дальше `POST /v3/payments/{id}/capture` или `/cancel`.
  - Холд держится от 2 часов до 7 дней в зависимости от способа оплаты. Точный срок — в поле `expires_at`. По его истечении платёж отменяется с причиной `expired_on_capture`.
- `confirmation.type = redirect`. В ответе приходит `confirmation.confirmation_url`, на него отправляем покупателя. Другие типы: `embedded` (виджет), `qr` (для СБП), `external`, `mobile_application`.
- Лимиты: `description` — до 128 символов, `metadata` — до 16 ключей ⚠️ не проверено (из памяти).
- **54-ФЗ:** по решению проекта чеки формирует отдельная облачная касса. Поэтому поле `receipt` в ЮKassa не передаём. **Подвох:** если у магазина продавца в ЛК ЮKassa включены «Чеки от ЮKassa» или сторонняя касса, API требует `receipt`, а без него возвращает ошибку. Кроме того, чеки выйдут дважды. При подключении проверьте настройки через `/v3/me` (флаг фискализации ⚠️ не проверено) и попросите продавца выключить фискализацию на стороне ЮKassa.
- `PaymentSession`: `ExternalID = payment.id`, `RedirectURL = confirmation.confirmation_url`, `ExpiresAt`.

## Статусы и маппинг

| ЮKassa | Наш статус | Примечание |
|---|---|---|
| `pending` | `pending` | ждём покупателя |
| `waiting_for_capture` | `waiting_for_capture` | только при `capture:false` |
| `succeeded` | `succeeded` | финальный |
| `succeeded` + `refunded_amount` < `amount` | `partially_refunded` | вычисляется из суммы возвратов |
| `succeeded` + `refunded_amount` == `amount` | `refunded` | |
| `canceled` | `canceled` | причина в `cancellation_details.reason` (например, `expired_on_confirmation`, `expired_on_capture`) |

- Отдельного статуса `failed` у ЮKassa нет. Отказ банка приходит как `canceled` с причиной. `canceled` с отказом банка можно показывать как `failed`, но это наше решение.
- `succeeded` и `canceled` — финальные, из них статус не меняется.
- Тестовые платежи отмечены полем `test: true`. Подробнее: https://yookassa.ru/developers/payment-acceptance/getting-started/payment-process

## Webhooks

- События:
  - `payment.waiting_for_capture`;
  - `payment.succeeded`;
  - `payment.canceled`;
  - `refund.succeeded`.
- Регистрация при OAuth: `POST /v3/webhooks` с телом `{"event": "payment.succeeded", "url": "https://<наш-домен>/webhooks/payments/yookassa/<connectionID>"}`.
  - Нужны Bearer-токен и Idempotence-Key.
  - На каждое событие — отдельный вызов.
  - Webhook привязан к токену, то есть к магазину. `connectionID` в URL нужен, чтобы сразу найти credentials.
- **Подписи у уведомлений нет.** Проверка подлинности:
  1. IP отправителя должен входить в allowlist:
     - `185.71.76.0/27`, `185.71.77.0/27`;
     - `77.75.153.0/25`, `77.75.156.11`, `77.75.156.35`;
     - `77.75.154.128/25`, `2a02:5180::/32`.
     - IP берите из `X-Real-IP` от нашего nginx, а не из присланного клиентом заголовка.
  2. **Всегда перезапрашивайте объект:** `GET /v3/payments/{object.id}` с credentials подключения. Статус берите из этого ответа, а не из тела уведомления. Этого одного уже достаточно: подделанный webhook не изменит реальный статус. IP-фильтр — дополнительная защита.
- Отвечать нужно **HTTP 200** (тело игнорируется). На любой другой код ЮKassa повторяет доставку до 24 часов. Сначала обработка идемпотентна (upsert статуса), потом 200. Если перезапрос не удался, отвечайте 500: ЮKassa повторит доставку.
- `ParseWebhook` возвращает `WebhookEvent{ExternalID, Kind}`. Итоговый статус определяет `GetPayment`.

## Возвраты

- `POST /v3/refunds`: `{"payment_id": "...", "amount": {"value": "300.00", "currency": "RUB"}, "description": "..."}` + Idempotence-Key.
- Частичные возвраты разрешены. Их может быть сколько угодно, пока сумма всех возвратов не больше суммы платежа. Минимум 1 ₽. После частичного возврата на платеже должно остаться не меньше 1 ₽ или 0 ₽. Источник: https://yookassa.ru/developers/payment-acceptance/after-the-payment/refunds
- Статус возврата: `pending` → `succeeded` / `canceled`. Событие `refund.succeeded` приходит в webhook.
- Чек возврата отправляет наш модуль `fiscal`, а не ЮKassa.

## Тестирование

- Тестовый магазин создаётся в ЛК, до 20 штук. Webhooks в нём работают.
- Карты без 3-D Secure (успешная оплата):
  - `5555555555554444` (MC);
  - `4111111111111111` (Visa);
  - `2202474301322987` (Мир).
- Карты с 3-D Secure:
  - `5555555555554477`;
  - `4793128161644804`;
  - `2200000000000004`.
- Карты с отказом, например `5555555555554592` (`3d_secure_failed`).
- Срок действия и CVC — любые.
- Источник: https://yookassa.ru/developers/payment-acceptance/testing-and-going-live/testing

## Подвохи

- Никогда не создавайте новый платёж при ретрае того же запроса. Ретраим с тем же Idempotence-Key.
- `return_url` — это не подтверждение оплаты. Покупатель может вернуться до `succeeded`. Пока статус `pending`, страница возврата опрашивает наш бэкенд.
- Токен живёт 5 лет, а продавец может отозвать доступ в любой момент. Обрабатывайте 401 централизованно.
- Двойные чеки, если у продавца включена фискализация в ЮKassa и одновременно работает наша облачная касса.
- СБП, SberPay и T-Pay доступны через `payment_method_data` или на странице ЮKassa. Набор методов зависит от договора продавца.
