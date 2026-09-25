# Halyk ePay (Казахстан) — справочник для `payment.Provider`

Сведения собраны 25.09.2026. Основной источник — https://epayment.kz/docs. Пометка «⚠️ не проверено» означает, что факт нужно сверить.

## Документация

| Тема | Ссылка |
|---|---|
| Оглавление | https://epayment.kz/docs |
| Получение токена | https://epayment.kz/docs/poluchenie-tokena |
| Платёжная страница, postLink | https://epayment.kz/docs/platezhnaya-stranica |
| Проверка статуса | https://epayment.kz/docs/check-status-payment |
| Подтверждение (charge) | https://epayment.kz/docs/podtverzhdenie-chastichnoe-podtverzhdenie-operacii |
| Отмена | https://epayment.kz/docs/cancel |
| Возврат | https://epayment.kz/docs/vozvrat-chastichnyi-vozvrat |
| Тестовые данные | https://epayment.kz/docs/Test-credentials |
| Тарифы | https://halykbank.kz/en/business/payments/internet-acquiring-epay |

## Подключение продавца

- У продавца должен быть договор интернет-эквайринга с Halyk Bank. OAuth для платформ нет.
- Продавец вводит в нашем кабинете три значения, которые банк выдаёт при подключении:
  - `client_id`;
  - `client_secret`;
  - `terminal_id`.
- `Credentials`: `{ClientID, ClientSecret, TerminalID}`, хранятся зашифрованными.
- Проверка при подключении: получите токен `client_credentials`. Ошибка 4xx означает неверные ключи.

## Хосты

| | Тест | Прод |
|---|---|---|
| OAuth | `https://test-epay-oauth.epayment.kz/oauth2/token` | `https://epay-oauth.homebank.kz/oauth2/token` |
| API | `https://test-epay-api.epayment.kz` | `https://epay-api.homebank.kz` |
| Платёжная форма JS | `https://test-epay.epayment.kz/payform/payment-api.js` | `https://epay.homebank.kz/payform/payment-api.js` |

- В других источниках встречается тестовый OAuth `https://testoauth.homebank.kz/epay2/oauth2/token` — видимо, старый. Держите все хосты в конфиге.

## Токен

`POST <oauth>/oauth2/token` (form-data):

```
grant_type=client_credentials
scope=webapi usermanagement email_send verification statement statistics payment
client_id=<...>
client_secret=<...>
invoiceID=<номер заказа>
secret_hash=<наш случайный секрет для этого платежа>
amount=<сумма>
currency=KZT
terminal=<terminal_id>
postLink=https://<наш-домен>/webhooks/payments/halyk/<connectionID>
failurePostLink=https://<наш-домен>/webhooks/payments/halyk/<connectionID>
```

- Ответ: `access_token`, `expires_in: 7200` (2 часа), `token_type: Bearer`.
- Токен для оплаты **привязан к конкретному счёту**: `invoiceID`, `amount`, `terminal`. Параметры платежа в запросе токена ⚠️ не проверено по официальной странице; подтверждено сторонними клиентами (например, https://github.com/tungatarov/epayment). Для служебных вызовов (статус, возврат) берите отдельный токен `client_credentials` без параметров платежа.
- `invoiceId` должен быть уникальным для каждого нового заказа. По сторонним клиентам — только цифры, длина 6–15 ⚠️ не проверено. Генерируйте числовой ID попытки, например из последовательности в БД, и храните связь с заказом.

## Оплата — платёжная страница (redirect/виджет)

- Покупатель проходит оплату на форме банка. Наш сервер отдаёт в браузер объект платежа, страница подключает `payment-api.js` и вызывает `halyk.pay(paymentObject)`.
- Обязательные поля `paymentObject`:
  - `invoiceId`;
  - `backLink`;
  - `postLink`;
  - `terminal`;
  - `amount`;
  - `currency`;
  - `description`;
  - `auth` — ответ токена целиком, как объект.
- Необязательные поля:
  - `failureBackLink`, `failurePostLink`;
  - `language` (`rus` / `kaz` / `eng`);
  - `email`, `phone`, `name`;
  - `accountId`;
  - `data`.
- `PaymentSession` для этого провайдера возвращает не `RedirectURL`, а данные для виджета, например `WidgetScript` + `WidgetPayload`. Шаблон checkout должен уметь оба варианта.
- Формат суммы в `amount` — число в тенге. Допустимы ли тиыны, ⚠️ не проверено; для KZT передавайте целые тенге, если заказ позволяет.
- Валюта: `KZT`. Другие валюты (USD) ⚠️ не проверено.

## postLink (webhook)

- Банк POST-ит JSON на `postLink` (или `failurePostLink` при неудаче). Поля:
  - `id` (ID транзакции), `invoiceId`, `amount`, `currency`, `terminal`;
  - `code` (`"ok"` / `"error"`), `reason`, `reasonCode`;
  - `cardMask`, `approvalCode`, `reference`, `dateTime`, `email`, `phone`;
  - `secret_hash`.
- Проверка подлинности:
  1. `secret_hash` из уведомления должен совпасть с секретом, который мы сохранили для этого `invoiceId` при запросе токена. Сравнивайте через `subtle.ConstantTimeCompare`.
  2. **Обязательно** перезапросите статус: `GET <api>/check-status/payment/transaction/:invoiceid` с токеном `client_credentials`. Применяйте `statusName` из ответа.
- Ожидаемый ответ банку в документации явно не описан ⚠️ не проверено. Отвечайте 200 после идемпотентной обработки.
- Сохраняйте `id` транзакции: он нужен для возврата и подтверждения.

## Статусы (`check-status` → `statusName`) и маппинг

| ePay | Смысл | Наш статус |
|---|---|---|
| `NEW` | промежуточный | `pending` |
| `AUTH` | сумма заблокирована | `waiting_for_capture` |
| `CHARGE` | сумма списана | `succeeded` |
| `CANCEL` | блокировка снята | `canceled` |
| `REFUND` | возврат | `refunded` или `partially_refunded` (сравнивайте суммы возвратов) ⚠️ не проверено: приходит ли `REFUND` при частичном возврате |
| `FAILED`, `REJECT`, `3D` | неуспех | `failed` |
| `VERIFIED` | верификация карты | не используется |

`resultCode`:
- `100` — ок, смотрите `statusName`;
- `102` — invoice не найден;
- `107` — операция в процессе, повторите позже;
- `101` / `103` / `104` / `106` / `109` — ошибки.

Источник: https://epayment.kz/docs/check-status-payment

## Двухстадийность, отмена, возврат

- Если терминал настроен на двухстадийную оплату, после оплаты статус `AUTH`, и нужен запрос подтверждения — см. страницу «Подтверждение/частичное подтверждение». Какой режим у терминала по умолчанию, ⚠️ не проверено. Узнавайте у продавца при подключении или обрабатывайте оба варианта.
- Отмена `AUTH` — см. https://epayment.kz/docs/cancel
- Возврат: `POST <api>/operation/:id/refund?amount=<сумма>&externalID=<наш ID возврата>`, `Authorization: Bearer <token client_credentials>`.
  - `:id` — ID транзакции из postLink или из `check-status`.
  - Без `amount` возвращается вся сумма. Частичный возврат — **минимум 10 тенге**.
  - **Возврат возможен только из статуса `CHARGE`.**
  - Ответ: 200 — успех, 400 — ошибка.
  - Источник: https://epayment.kz/docs/vozvrat-chastichnyi-vozvrat
- Идемпотентность: заголовка Idempotence-Key нет. Передавайте `externalID` и перед повтором проверяйте статус транзакции ⚠️ не проверено: дедуплицирует ли банк по `externalID`.

## Тестирование

- Тестовые `client_id=test`, `client_secret=yF587AV9Ms94qN2QShFzVR3vFnWkhjbAK3sG`, `terminal=67e34d63-102f-4bd1-898e-370781d0074d`.
- Карты:
  - `4405639704015096` (01/27, 321) — успех;
  - `5522042705066736` (01/27, 775) — успех;
  - `4003032704547597` — отказ.
- Сроки действия тестовых карт могут устареть. Актуальные данные: https://epayment.kz/docs/Test-credentials

## Подвохи

- Токен для оплаты и токен для служебных вызовов — разные. Не кешируйте платёжный токен между заказами.
- Уникальность `invoiceId` проверяет банк. Повторная оплата заказа — новая попытка с новым `invoiceId`.
- Комиссии (карты Halyk 2,5%, карты других банков 3%) касаются продавца, не платформы.
- Встроенной фискализации у ePay в найденных источниках нет ⚠️ не проверено. Чек ККМ формирует наш модуль `fiscal` через Webkassa.
