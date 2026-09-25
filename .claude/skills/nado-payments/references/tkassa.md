# Т-Касса (Т-Банк, бывш. Тинькофф Эквайринг) — справочник для `payment.Provider`

Сведения собраны 25.09.2026. Официальный портал developer.tbank.ru не открылся (ошибка сертификата при загрузке). Поэтому часть фактов подтверждена через Go-клиент с открытым кодом и выдачу поиска. Такие места помечены «⚠️ не проверено»; перед реализацией сверьтесь с порталом вручную.

## Документация

| Тема | Ссылка |
|---|---|
| API интернет-эквайринга | https://developer.tbank.ru/eacq/api |
| Введение, уведомления | https://developer.tbank.ru/eacq/intro |
| Подпись Token | https://developer.tbank.ru/eacq/intro/developer/token |
| Тестовая среда и тест-кейсы | https://developer.tbank.ru/eacq/intro/errors/test и https://developer.tbank.ru/eacq/intro/errors/test-cases |
| Go-клиент (справочно, не зависимость) | https://github.com/nikita-vanyasin/tinkoff |

## Подключение продавца

- **OAuth для SaaS не найден.** Продавец сам вводит в кабинете платформы:
  - `TerminalKey` — идентификатор терминала;
  - `Password` — пароль терминала.
  - Оба значения берутся в ЛК Т-Бизнеса: «Интернет-эквайринг → Магазины → Терминалы» ⚠️ не проверено (путь в меню).
- `Credentials`: `{TerminalKey, Password}`, хранятся зашифрованными. `Password` используется только для подписи и в запросах не передаётся.
- Проверка ключей при подключении: `GetState` по несуществующему `PaymentId` или тестовый `Init` на 1 ₽ с последующим `Cancel`. Ошибка подписи означает неверный пароль ⚠️ не проверено (какой метод удобнее).

## API: общие правила

- База: `https://securepay.tinkoff.ru/v2/` (так в Go-клиенте). Возможен новый домен `securepay.tbank.ru` ⚠️ не проверено. Сделайте базовый URL настраиваемым.
- Все методы — POST с JSON-телом:
  - `Init`;
  - `GetState`;
  - `Confirm`;
  - `Cancel`;
  - `CheckOrder` ⚠️ не проверено.
- **Сумма — целое число в копейках** (`Amount: 125000` = 1250,00 ₽). Прямо `money.Money.Minor`.
- Валюта: только RUB ⚠️ не проверено. Другие валюты отмечайте в `Capabilities` как неподдерживаемые.

### Подпись Token (запросы и уведомления)

Алгоритм подтверждён кодом клиента https://raw.githubusercontent.com/nikita-vanyasin/tinkoff/master/client.go:
1. Возьмите пары «ключ–значение» **корневого уровня** запроса. Вложенные объекты и массивы (`Receipt`, `DATA`, `Shops`) не участвуют ⚠️ не проверено (так описано в официальной документации, сам текст не открылся).
2. Добавьте пару `Password` = пароль терминала. `TerminalKey` уже есть среди полей.
3. Отсортируйте по ключу, в алфавитном порядке.
4. Склейте **только значения**, без разделителей.
5. Посчитайте `SHA-256` и запишите в hex нижним регистром. Результат передаётся в поле `Token`.

Значения переводятся в строки так:
- bool → `"true"` / `"false"`;
- числа — в десятичной записи без экспоненты.

Уведомление проверяется по тому же алгоритму:
- берутся все поля уведомления, кроме `Token` (и кроме вложенных объектов, например `Data`);
- добавляется `Password`;
- полученный хеш сравнивается с `Token` через `subtle.ConstantTimeCompare`.

## Создание платежа — `Init`

```json
{
  "TerminalKey": "...",
  "Amount": 125000,
  "OrderId": "123-a1",
  "Description": "Заказ №123",
  "PayType": "O",
  "SuccessURL": "https://shop.example/checkout/return?order=123",
  "FailURL": "https://shop.example/checkout/fail?order=123",
  "NotificationURL": "https://<наш-домен>/webhooks/payments/tkassa/<connectionID>",
  "Token": "<sha256>"
}
```

- `PayType`: `O` — одностадийная оплата, её используем по умолчанию; `T` — двухстадийная (`AUTHORIZED` → `Confirm`).
- `OrderId` — ваш идентификатор, до 36 символов ⚠️ не проверено. Делайте его уникальным для **каждой попытки оплаты**, например `<orderID>-<attempt>`. Механизма Idempotence-Key, как у ЮKassa, нет ⚠️ не проверено. Защита от дублей такая: сохраните попытку в БД до вызова и при ретрае сначала вызовите `GetState` или `CheckOrder`.
- `NotificationURL` передаётся в каждом `Init`, поэтому продавцу ничего не нужно настраивать в ЛК ⚠️ не проверено (наличие параметра — по памяти, проверьте в `/eacq/api`).
- `Receipt` не передаём: чеки формирует наша облачная касса. Если у терминала продавца включены онлайн-чеки Т-Банка, `Init` без `Receipt` может вернуть ошибку, а чеки выйдут дважды. Попросите продавца выключить фискализацию на стороне банка ⚠️ не проверено.
- Ответ: `Success`, `ErrorCode` (`"0"` = ок), `PaymentId`, `PaymentURL`, `Status`.
  - `PaymentSession.ExternalID = PaymentId` (число, храните как строку);
  - `RedirectURL = PaymentURL`.

## Статусы и маппинг

Список статусов взят из https://raw.githubusercontent.com/nikita-vanyasin/tinkoff/master/status.go.

| Т-Касса | Наш статус |
|---|---|
| `NEW`, `FORM_SHOWED`, `AUTHORIZING`, `3DS_CHECKING`, `3DS_CHECKED`, `PREAUTHORIZING` | `pending` |
| `AUTHORIZED` (при `PayType=T`) | `waiting_for_capture` |
| `CONFIRMING` | прежний статус (промежуточный) |
| `CONFIRMED` | `succeeded` |
| `PARTIAL_REVERSED` | `waiting_for_capture` (холд уменьшен) |
| `REVERSING`, `REFUNDING`, `ASYNC_REFUNDING` | прежний статус (промежуточный) |
| `REVERSED`, `CANCELED`, `DEADLINE_EXPIRED` | `canceled` |
| `REJECTED`, `AUTH_FAIL` | `failed` |
| `PARTIAL_REFUNDED` | `partially_refunded` |
| `REFUNDED` | `refunded` |

Промежуточные статусы не должны откатывать финальные. Применяйте переход только по разрешённому графу состояний.

## Уведомления (webhooks)

- Т-Касса отправляет POST с JSON на `NotificationURL` при смене статуса.
- Поля уведомления:
  - `TerminalKey`, `OrderId`, `Success`, `Status`, `PaymentId`;
  - `ErrorCode`, `Amount`;
  - `CardId`, `Pan`, `ExpDate`;
  - `RebillId`, `Data`;
  - `Token`.
- Порядок обработки:
  1. Найдите подключение по `connectionID` из URL.
  2. Проверьте `Token`. Если подпись не сошлась — 403, ничего не меняем.
  3. Сверьте `TerminalKey` с подключением.
  4. Для надёжности перезапросите `GetState` с `PaymentId` и примените статус из ответа.
  5. **Ответьте HTTP 200 с телом `OK`** (plain text, без JSON). Без этого Т-Касса будет повторять уведомление ⚠️ не проверено (точная формулировка; в Go-клиенте есть `GetNotificationSuccessResponse`).
- IP-allowlist Т-Банк в найденных источниках не публикует ⚠️ не проверено. Полагайтесь на подпись и `GetState`.

## Двухстадийная оплата и возвраты

- `Confirm`: `{TerminalKey, PaymentId, Amount?, Token}` — списание захолдированной суммы. Можно списать частично ⚠️ не проверено.
- `Cancel`: `{TerminalKey, PaymentId, Amount?, Token}`. Это единый метод:
  - для `AUTHORIZED` — отмена холда (reversal);
  - для `CONFIRMED` — возврат (refund).
  - `Amount` в копейках задаёт **частичный** возврат; без `Amount` возвращается вся сумма. Частичный `Cancel` подтверждён примером в Go-клиенте: https://github.com/nikita-vanyasin/tinkoff/blob/master/README.md
- `RefundResult.ExternalID = PaymentId`. Отдельного идентификатора возврата может не быть ⚠️ не проверено. Храните собственный ID возврата и сумму.
- Для возвратов по СБП есть асинхронный статус `ASYNC_REFUNDING`. Итог приходит уведомлением.

## Тестирование

- Тестовые кейсы проводятся на **боевом URL** с тестовым терминалом, у которого в названии суффикс `DEMO`. Номера карт показываются в ЛК на вкладке тест-кейсов. Срок действия у тестовой карты — любой.
  - Источник: https://developer.tbank.ru/eacq/intro/errors/test-cases (по выдаче поиска).
- Отдельная тестовая среда `rest-api-test.tinkoff.ru` открывается **только по запросу**, иначе отвечает 403. Источник: https://github.com/kravetsone/t-kassa-api
- Конкретные номера тестовых карт ⚠️ не проверено: берите их в ЛК.

## Подвохи

- Подпись чувствительна к представлению значений: `true` / `false` строчными буквами, числа без `.0`. Покройте подпись тестом на эталонном примере из документации.
- `OrderId` должен быть уникальным для каждой попытки. Повторная оплата того же заказа — это новая попытка с новым `OrderId`.
- Уведомления могут приходить не по порядку. Используйте граф состояний и `GetState`.
- С 2026 года на комиссию Т-Банка начисляется НДС 22% (кроме T-Pay и СБП). Это касается продавца, на код не влияет.
  - Источник: https://www.tbank.ru/business/help/business-payments/internet-acquiring/how-work/price/
