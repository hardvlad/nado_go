# Freedom Pay (бывш. PayBox, Казахстан) — справочник для `payment.Provider`

Сведения собраны 25.09.2026. Источник — https://freedompay.kz/docs/merchant-api/intro и его подстраницы. Пометка «⚠️ не проверено» означает, что факт нужно сверить.

## Документация

| Тема | Ссылка |
|---|---|
| Введение, подпись | https://freedompay.kz/docs/merchant-api/intro |
| Приём платежа (`init_payment`, `result_url`) | https://freedompay.kz/docs/merchant-api/pay |
| После платежа (статус, возврат, клиринг) | https://freedompay.kz/docs/merchant-api/payafter |
| Коды ошибок | https://freedompay.kz/docs/merchant-api/error-list |

## Подключение продавца

- Продавец заключает договор с Freedom Pay. OAuth для платформ нет.
- Продавец вводит в нашем кабинете данные магазина из ЛК Freedom Pay:
  - `merchant_id`;
  - `secret_key` — секретный ключ для приёма платежей ⚠️ не проверено (путь в ЛК).
- `Credentials`: `{MerchantID, SecretKey}`, хранятся зашифрованными.
- Проверка при подключении: `get_status3.php` по несуществующему заказу. Ошибка подписи означает неверный ключ ⚠️ не проверено.

## Подпись `pg_sig` (запросы, ответы и callback)

1. Возьмите все поля сообщения, кроме `pg_sig`. `pg_salt` участвует.
2. Отсортируйте их по имени в алфавитном порядке. Для вложенных структур (XML или массивов) — рекурсивно ⚠️ не проверено.
3. Соберите строку: `<имя скрипта>;<значение1>;<значение2>;...;<secret_key>`.
   - Имя скрипта — последний сегмент пути без query, например `init_payment.php`.
   - Для callback это последний сегмент **нашего** `pg_result_url`.
4. `pg_sig = md5(строка)` в hex нижним регистром (32 символа).
5. `pg_salt` — случайная строка из цифр и латиницы, новая для каждого сообщения.

Для callback: пересчитайте подпись по всем пришедшим полям, кроме `pg_sig`, и сравните через `subtle.ConstantTimeCompare`. Подпись ответа XML считается по той же схеме.

## Создание платежа

`POST https://api.freedompay.kz/init_payment.php` (form-urlencoded):

| Параметр | Значение |
|---|---|
| `pg_merchant_id` | из credentials |
| `pg_order_id` | ID попытки оплаты, до 50 символов, уникальный |
| `pg_amount` | число, например `25` или `1250.50` |
| `pg_currency` | `KZT` (также `USD`, `EUR`) |
| `pg_description` | «Заказ №123» |
| `pg_result_url` | `https://<наш-домен>/webhooks/payments/freedom/<connectionID>` |
| `pg_success_url` / `pg_failure_url` | страницы возврата на витрину |
| `pg_request_method` | `POST` (метод вызова `result_url`) |
| `pg_lifetime` | время жизни счёта, секунды: по умолчанию 86400, диапазон 300–604800 |
| `pg_auto_clearing` | `1` — одностадийная оплата; `0` — двухстадийная (нужен `do_capture.php`) |
| `pg_testing_mode` | `1` для теста |
| `pg_salt`, `pg_sig` | подпись |
| `pg_user_phone`, `pg_user_contact_email` | контакты покупателя, необязательные ⚠️ не проверено (имена полей) |

- Ответ: `pg_status`, `pg_payment_id`, `pg_redirect_url`, `pg_salt`, `pg_sig`.
  - `PaymentSession.ExternalID = pg_payment_id`;
  - `RedirectURL = pg_redirect_url`.
  - Подпись ответа проверяйте.
- Формат суммы — десятичное число с точкой. Из `money.Money{Minor}` форматируйте целочисленно (`%d.%02d`), без float.
- Валюта: `KZT`, `USD`, `EUR`. Если покупатель выбирает другой метод оплаты, Freedom Pay конвертирует автоматически. RUB в списке нет ⚠️ не проверено.
- `pg_check_url` (предварительная проверка возможности платежа) не используем. Если параметр пустой, проверка не выполняется.

## Callback `result_url`

- Поля:
  - `pg_payment_id`, `pg_order_id`, `pg_amount`, `pg_currency`;
  - `pg_result` — `1` успех, `0` неуспех, `2` незавершённый ⚠️ не проверено (значение `2`);
  - `pg_can_reject` — можно ли отказаться от платежа;
  - `pg_payment_date`, `pg_payment_method`, `pg_user_phone`, `pg_user_contact_email`;
  - `pg_salt`, `pg_sig`.
- Порядок обработки:
  1. Проверьте подпись; если не сошлась — 403.
  2. Сверьте `pg_order_id` и `pg_amount` с попыткой в БД.
  3. Для надёжности перезапросите `get_status3.php`.
  4. Идемпотентно примените статус.
- **Ответ обязателен в XML:**

```xml
<?xml version="1.0" encoding="utf-8"?>
<response>
  <pg_status>ok</pg_status>
  <pg_description>Принято</pg_description>
  <pg_salt>random</pg_salt>
  <pg_sig>md5(...)</pg_sig>
</response>
```

- Значения `pg_status`:
  - `ok` — принят;
  - `rejected` — отказ от платежа, только если `pg_can_reject=1`: Freedom Pay вернёт деньги;
  - `error` — временная ошибка, Freedom Pay повторит ⚠️ не проверено (политика повторов).
- Используйте `rejected`, если заказ уже отменён или товара нет, а платёж пришёл после истечения резерва.

## Статус, клиринг, возврат, отмена

- Статус: `POST https://api.freedompay.kz/get_status3.php`, поля `pg_merchant_id`, `pg_payment_id` или `pg_order_id`, `pg_salt`, `pg_sig`. В ответе поле `pg_payment_status`. Полный список значений на странице не приведён ⚠️ не проверено; ожидаемые варианты: `new`, `pending`, `success`, `failed`, `revoked`, `refunded` и частичные.

| Freedom Pay (ожидаемо) | Наш статус |
|---|---|
| `new`, `pending`, `process` | `pending` |
| авторизация без клиринга (`pg_auto_clearing=0`) | `waiting_for_capture` |
| `success` / `ok` | `succeeded` |
| `failed`, `error`, `incomplete` | `failed` |
| `revoked` / `refunded` (полный) | `refunded` |
| частичный возврат | `partially_refunded` |
| `canceled` | `canceled` |

Сверяйте этот маппинг с реальными ответами в тестовом режиме и фиксируйте в тестах.

- Клиринг (двухстадийная оплата): `POST https://api.freedompay.kz/do_capture.php`, поля `pg_merchant_id`, `pg_payment_id`, `pg_clearing_amount`, `pg_salt`, `pg_sig`. Провести клиринг можно в течение 5 дней.
- Возврат: `POST https://api.freedompay.kz/revoke.php`, поля `pg_merchant_id`, `pg_payment_id`, `pg_refund_amount`, `pg_salt`, `pg_sig`.
  - Если `pg_refund_amount` не передан или равен 0, возвращается вся сумма.
  - Частичный возврат — передайте сумму.
- Отмена неоплаченного счёта: `POST https://api.freedompay.kz/cancel.php`.
- Идемпотентность: заголовка нет. Перед повтором возврата проверяйте статус и суммы возвратов в `get_status3` ⚠️ не проверено.

## Тестирование

- `pg_testing_mode=1`.
- Тестовые номера телефонов для оплаты с кошелька: например, `+77017777777`, OTP `111111`.
- Тестовые карты — раздел «Тестовые карты» в документации; номера не извлечены ⚠️ не проверено.

## Подвохи

- **Встроенная фискализация Freedom Pay.** По решению проекта чеки формирует Webkassa. Если у продавца в Freedom Pay включена онлайн-фискализация (ОФД), чеки выйдут дважды. При подключении попросите продавца выключить её или используйте её вместо Webkassa, но это другое решение, согласуйте его.
- MD5-подпись чувствительна к порядку полей и к имени скрипта. Покройте её тестом на эталонном примере.
- `pg_order_id` — это ID попытки, а не заказа: повторная оплата создаёт новую попытку.
- Callback может прийти раньше, чем покупатель вернётся на `pg_success_url`. Истина — в статусе из БД.
