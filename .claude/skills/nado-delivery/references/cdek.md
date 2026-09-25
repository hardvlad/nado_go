# СДЭК: API v2 (РФ и КЗ)

Код провайдера: `cdek`. Одна интеграция покрывает РФ и Казахстан. Вариант с ПВЗ и постаматами является основным сценарием для интернет-магазина.

Официальные порталы `api-docs.cdek.ru` и `confluence.cdek.ru` из нашей сети не открывались. Поэтому часть фактов ниже взята из официального SDK `cdek-it/sdk2.0` и открытых клиентов. Всё, что не подтверждено, помечено ⚠️. Перед реализацией сверьтесь с https://apidoc.cdek.ru/ и https://api-docs.cdek.ru/29923741.html.

## Окружения и авторизация

| | Prod | Тест |
|---|---|---|
| Базовый URL | `https://api.cdek.ru` | `https://api.edu.cdek.ru` |
| Учётные данные | Account/Secure из договора | общий тестовый аккаунт (см. ниже) |

Источник: [go-cdek](https://pkg.go.dev/github.com/build-monsters/go-cdek/v2), [riizeron/cdek-go-sdk](https://pkg.go.dev/github.com/riizeron/cdek-go-sdk/v2).

- Токен выдаётся по OAuth 2.0 client_credentials: `POST /v2/oauth/token`.
  - Параметры `grant_type=client_credentials`, `client_id=<Account>`, `client_secret=<Secure>` передаются в form-urlencoded (в старых примерах — в query).
  - Ответ содержит `access_token` и `expires_in`.
  - Дальше в каждом запросе заголовок `Authorization: Bearer <token>`.
- **Где продавец берёт ключи.** Account и Secure выдаются в ЛК СДЭК в разделе «Интеграция» либо по запросу менеджеру. Логин и пароль от ЛК не подходят ([infostart](https://forum.infostart.ru/forum15/topic271755/), [sdk docs](https://cdek-it-sdk20.readthedocs.io/ru/latest/)).
- **Время жизни токена.** Источники расходятся: SDK пишет 3600 с ([AntistressStore](https://github.com/AntistressStore/cdek-sdk-v2)), readthedocs — «6 минут». Поэтому берите фактическое `expires_in` из ответа, кэшируйте токен на каждую пару Account+Secure и обновляйте за 60 с до истечения. На 401 делайте один повторный запрос с новым токеном.
- **Тестовый аккаунт** (общий, опубликован в примерах; работает только с `api.edu.cdek.ru`):
  - `client_id=EMscd6r9JnFiQ3bLoyjJY6eM78JrJceI`
  - `client_secret=PjLZkKBHEiLK3YsjtNrt3TGNG0ahs3kG`

  Источник: поиск по [infostart](https://forum.infostart.ru/forum15/topic271755/) и [snipp.ru](https://snipp.ru/php/cdek-api). Данные на тестовом стенде отстают от прода, и стенд иногда отвечает 500 не по нашей вине ([AntistressStore](https://github.com/AntistressStore/cdek-sdk-v2)).

**Credentials в нашей модели:** `{account, secure, test bool}`. Хранятся зашифрованными. Токен держим в памяти, в БД его не пишем.

## Эндпоинты

| Назначение | Метод и путь | Метод Provider |
|---|---|---|
| Расчёт по одному тарифу | `POST /v2/calculator/tariff` | `Quote` |
| Расчёт по всем доступным тарифам | `POST /v2/calculator/tarifflist` | `Quote` |
| Поиск города | `GET /v2/location/cities?country_codes=RU,KZ&city=…&postal_code=…` | резолв адреса |
| Подсказки по городу ⚠️ | `GET /v2/location/suggest/cities?name=…` | автокомплит на витрине |
| Регионы | `GET /v2/location/regions` | справочник |
| ПВЗ и постаматы | `GET /v2/deliverypoints?city_code=…&type=PVZ\|POSTAMAT\|ALL&country_code=…` | `PickupPoints` |
| Создать заказ | `POST /v2/orders` | `CreateShipment` |
| Получить заказ | `GET /v2/orders/{uuid}` (либо `?cdek_number=`, `?im_number=`) | статус, трек |
| Удалить заказ | `DELETE /v2/orders/{uuid}` | `CancelShipment` |
| Отказ от заказа ⚠️ | `POST /v2/orders/{uuid}/refusal` | отмена после передачи |
| Ярлык (ШК места) | `POST /v2/print/barcodes`, затем `GET /v2/print/barcodes/{uuid}` | `Label` |
| Накладная | `POST /v2/print/orders`, затем `GET /v2/print/orders/{uuid}` | `Label` (тип invoice) |
| Вызов курьера на забор | `POST /v2/intakes`, `GET /v2/intakes/{uuid}` | вне интерфейса, v2 |
| Вебхуки | `POST /v2/webhooks`, `GET /v2/webhooks`, `DELETE /v2/webhooks/{uuid}` | подписка при подключении |

Пути сверены с [AntistressStore/cdek-sdk-v2](https://github.com/AntistressStore/cdek-sdk-v2) и [cdek-it/sdk2.0](https://github.com/cdek-it/sdk2.0).

### Расчёт стоимости (`Quote`)

- **Тело запроса.** Обязательны `from_location`, `to_location` и `packages[]`.
  - Локация задаётся полями `code` (код города СДЭК), `postal_code`, `country_code` или `address`.
  - Место: `weight` в **граммах**; `length`, `width`, `height` в **см**.
  - Для одного тарифа добавьте `tariff_code`. Необязательные поля: `currency`, `services[]` (например, страховка).
- **Ответ tarifflist:** `tariff_codes[]` с полями `tariff_code`, `tariff_name`, `delivery_mode`, `delivery_sum`, `period_min`, `period_max`.
- **Режимы доставки** определяются тарифом: склад→склад, склад→дверь, дверь→склад, дверь→дверь, склад→постамат.
  - Коды из примеров: 136 (посылка склад-склад), 137 (склад-дверь), 138, 139, 233/234 (экономичная), 368 (склад-постамат) ⚠️.
  - Набор тарифов зависит от договора и страны: в КЗ он другой ⚠️.
  - Не зашивайте коды в код. Берите tarifflist и фильтруйте по `delivery_mode`, который требуется: ПВЗ или курьер.
- Валюта ответа — валюта договора. Для КЗ ожидается KZT ⚠️. Параметр `currency` запрашивает другую валюту.

### ПВЗ (`PickupPoints`)

- Параметры: `city_code`, `postal_code`, `country_code`, `region_code`, `type` (`PVZ`, `POSTAMAT`, `ALL`), `have_cashless`, `have_cash`, `allowed_cod`, `is_dressing_room`, `weight_max`, `weight_min`, `take_only`, `is_handout`, `is_reception`, `lang` ([go-cdek](https://pkg.go.dev/github.com/build-monsters/go-cdek/v2)).
- Полный список по РФ и КЗ содержит десятки тысяч точек ⚠️. Не запрашивайте его на каждый заход покупателя. Держите в БД таблицу `carrier_pickup_points (provider, external_code, city_code, country, lat, lon, address, schedule, flags, updated_at)` и обновляйте её фоновой задачей раз в сутки (по стране или по городам). Витрина читает из БД.
- Поля ответа ⚠️: `code` (передаётся в заказ как `delivery_point`), `location{city_code,address,latitude,longitude}`, `work_time`, `type`, `weight_max`, `have_cash`, `allowed_cod`.

### Создание заказа (`CreateShipment`)

`POST /v2/orders`, тело:

- `type`: 1 — «интернет-магазин», наш случай. При этом типе обязательны `items` в местах.
- `number` — номер заказа у нас (у СДЭК это `im_number`). По нему ищут дубли, поэтому делайте его уникальным в пределах договора.
- `tariff_code`.
- Откуда: `shipment_point` (код ПВЗ приёма) **или** `from_location`.
- Куда: `delivery_point` (код ПВЗ) **или** `to_location{code|postal_code, address}`.
- `sender{name, company, phones[]}`, `recipient{name, phones[{number}], email}` — телефоны в формате E.164.
- `packages[]`: `number`, `weight` (г), `length`, `width`, `height` (см), `items[]`.
  - Поля позиции: `name`, `ware_key` (артикул), `cost` (объявленная ценность за единицу), `weight`, `amount`, `payment{value, vat_rate?}`.
  - `payment.value` — сумма к оплате получателем за единицу, то есть наложенный платёж. Для оплаченных заказов ставьте 0.
- `delivery_recipient_cost{value}` — стоимость доставки, которую берут с получателя при наложенном платеже.
- `services[]` — доп. услуги ⚠️.

Пример структуры: [cdek-it readthedocs](https://cdek-it-sdk20.readthedocs.io/ru/latest/).

**Регистрация заказа асинхронная.**
- Ответ содержит `entity.uuid` и `requests[]` с `state` ∈ `ACCEPTED`, `WAITING`, `SUCCESSFUL`, `INVALID` ([поиск](https://github.com/Kroch4ka/yamshik-cdek/pull/2)). `ACCEPTED` означает только, что запрос прошёл предварительную валидацию.
- Трек-номер `cdek_number` появляется позже. Получайте его через `GET /v2/orders/{uuid}` с backoff (1, 2, 4… с, максимум около минуты) или из вебхука.
- Если `INVALID`, покажите продавцу `requests[].errors[]`.

**Дубли.** Если запрос создания упал по таймауту, заказ всё равно мог создаться ([AntistressStore](https://github.com/AntistressStore/cdek-sdk-v2)). Перед повтором ищите заказ через `GET /v2/orders?im_number=<number>` ⚠️. `number` генерируйте детерминированно из нашего shipment ID.

### Отмена

`DELETE /v2/orders/{uuid}` работает, пока посылка не передана в СДЭК (статус `CREATED`/`ACCEPTED`) ⚠️. После передачи нужен отказ (refusal) или возврат. В интерфейсе возвращайте `ErrCannotCancel`, чтобы продавец решил вопрос вручную.

### Ярлыки (`Label`)

1. `POST /v2/print/barcodes {orders:[{order_uuid}], copy_count, format:"A4"|"A5"|"A6"}` возвращает uuid печатной формы.
2. Форма готовится асинхронно: опрашивайте `GET /v2/print/barcodes/{uuid}` до `statuses[].code=READY` ⚠️ или ждите вебхука `PRINT_FORM`.
3. В ответе приходит `url`. PDF скачивается с тем же Bearer-токеном. Отдавайте его как `Document{ContentType:"application/pdf"}`.

## Вебхуки (`ParseWebhook`)

- Подписка: `POST /v2/webhooks {type, url}`. Нужные типы:
  - `ORDER_STATUS` — смена статуса;
  - `PRINT_FORM` — готова печатная форма ([sdk2.0](https://cdek-it-sdk20.readthedocs.io/ru/latest/)).
  - Другие типы (`DOWNLOAD_PHOTO`, `PREALERT_CLOSED` и т.д.) не нужны ⚠️.
- Подписываемся при подключении продавца: один URL на подключение, например `https://<platform>/webhooks/delivery/cdek/{connection_id}`.
- **У вебхуков нет подписи** ⚠️. Поэтому не доверяйте телу: возьмите из него `uuid` и перечитайте заказ через `GET /v2/orders/{uuid}` с ключами этого подключения. Отвечайте 200 сразу, обработку ставьте в очередь.
- Страховка на случай потери вебхуков: фоновый опрос активных отправлений раз в 1–3 часа.

## Статусы → наши

Коды статусов заказа из «Приложения 1» документации. Список неполный ⚠️: сверить с [api-docs Order Details](https://api-docs.cdek.ru/33828849.html).

| СДЭК | Наш |
|---|---|
| `ACCEPTED` (принят, прошёл валидацию), `CREATED` | `created` |
| `RECEIVED_AT_SHIPMENT_WAREHOUSE`, `READY_TO_SHIP_AT_SENDING_OFFICE`, `TAKEN_BY_TRANSPORTER_FROM_SENDER_CITY` | `accepted` |
| `SENT_TO_TRANSIT_CITY`, `ACCEPTED_IN_TRANSIT_CITY`, `ACCEPTED_AT_TRANSIT_WAREHOUSE`, `SENT_TO_RECIPIENT_CITY`, `ACCEPTED_IN_RECIPIENT_CITY`, `ACCEPTED_AT_RECIPIENT_CITY_WAREHOUSE`, `TAKEN_BY_COURIER`, `READY_FOR_SHIPMENT_IN_TRANSIT_CITY`, `IN_CUSTOMS_*` | `in_transit` |
| `ACCEPTED_AT_PICK_UP_POINT`, `POSTOMAT_POSTED` | `ready_for_pickup` |
| `DELIVERED`, `POSTOMAT_RECEIVED` | `delivered` |
| `NOT_DELIVERED`, `RETURNED_TO_SENDER_CITY_WAREHOUSE`, `SENT_TO_SENDER_CITY`, `POSTOMAT_SEIZED` | `returned` (промежуточные состояния показывайте как «возврат в пути») |
| `REMOVED` ⚠️ | `canceled` |
| `INVALID` | `failed` |

Неизвестный код логируйте и не меняйте наш статус. Сырой код храните в `tracking_events.raw_status`.

## Модель локаций

- Основной ключ — `code` города СДЭК (int). Его определяют через `GET /v2/location/cities` по `postal_code` или названию и `country_codes`.
- В ответе есть `fias_guid` ⚠️; KATO для КЗ СДЭК не принимает ⚠️. Для адреса покупателя: индекс → `cities?postal_code=` → `code`. Если найдено несколько городов, отдайте выбор автокомплиту на витрине.
- Кэшируйте соответствие «индекс/город → code» в БД.

## Страны и валюты

- РФ и КЗ, плюс международные направления.
- Договор КЗ заключается с СДЭК КЗ ([cdek.kz/integration/api](https://cdek.kz/kz/integration/api/)). Тот же API-хост, но ключи и тарифы другие ⚠️.
- Продавец из КЗ подключает ключи своего договора. Поле `country` подключения определяет дефолтный `country_code` в запросах.

## Лимиты и подводные камни

- Публичных rate limit не нашёл ⚠️. Закладывайте клиентский лимит около 5 запросов в секунду на подключение и retry с backoff на 429/5xx.
- Все денежные поля — decimal в валюте договора, **не** копейки. Конвертируйте из наших minor units явно.
- Вес указывается в граммах целым числом, габариты — в см целым числом. Если у товара нет габаритов, подставляйте дефолтную упаковку из настроек магазина.
- Телефон получателя для РФ обязателен в формате `+7…` ⚠️.
- Тестовый стенд `api.edu` поддерживает не все города и тарифы.
