# Яндекс Доставка: API «Доставка в другой день» (РФ)

Код провайдера: `yandex_delivery`.

У Яндекс Доставки два разных API:
- **«Доставка в другой день»** (`/api/b2b/platform/*`) — склад→ПВЗ или курьер, через сортировочный центр. Подходит для интернет-магазина. **Реализуем его.**
- **«Экспресс / в течение дня»** (`/b2b/cargo/integration/v2/claims/*`) — курьер «прямо сейчас». Это отдельный провайдер на потом, если понадобится.

Документация: https://yandex.ru/support/delivery-profile/ru/api/other-day/ref/ (зеркало: https://yandex.ru/dev/logistics/delivery-api/doc/about/intro.html). Индекс для LLM: https://yandex.ru/support/delivery-profile/ru/llms.txt.

## Окружения и авторизация

| | Prod | Тест |
|---|---|---|
| Хост | `https://b2b-authproxy.taxi.yandex.net` | `https://b2b.taxi.tst.yandex.net` |
| Токен | из ЛК продавца | `y2_AgAAAAD04omrAAAPeAAAAAACRpC94Qk6Z5rUTgOcTgYFECJllXYKFx8` |

Источник: [доступ к API](https://yandex.ru/support/delivery-profile/ru/api/other-day/access).

- **Где продавец берёт токен:** ЛК Яндекс Доставки → «Интеграция» → «Получить токен» (Профиль компании → API-токен).
- Заголовок: `Authorization: Bearer <token>`.
- Токен **бессрочный**, но становится недействительным после смены пароля аккаунта. На 401 переводим подключение в статус `needs_reauth` и уведомляем продавца.
- На тестовом стенде работают только адреса в Москве, и для курьера, и для ПВЗ.
- Credentials: `{token, test bool}` плюс настройки подключения:
  - `source_station_id` — склад или точка самопривоза продавца, откуда уходят заказы;
  - ИНН — для `billing_details`.

## Эндпоинты

Все пути начинаются с `/api/b2b/platform/` ([список методов](https://yandex.ru/support/delivery-profile/ru/api/other-day/ref/)).

| Назначение | Метод и путь | Метод Provider |
|---|---|---|
| Определить `geo_id` по адресу | `POST location/detect` | резолв адреса |
| Список ПВЗ и точек самопривоза | `POST pickup-points/list` | `PickupPoints` |
| Калькулятор цены | `POST pricing-calculator` | `Quote` |
| Варианты доставки (слоты) | `POST offers/create`, затем `POST offers/confirm` | `Quote` → `CreateShipment` (двухшаговый флоу) |
| Интервалы для оффера | `GET/POST offers/info` | выбор слота курьера |
| Создание заявки в один шаг | `POST request/create` | `CreateShipment` |
| Инфо о заявке | `GET request/info?request_id=…`, `POST requests/info` (пачкой), `GET request/actual_info` | `Track` |
| История статусов | `GET request/history?request_id=…` | `Track` |
| Отмена | `POST request/cancel` | `CancelShipment` |
| Ярлыки | `POST request/generate-labels` | `Label` |
| Акт приёма-передачи | `POST request/get-handover-act` | документ для отгрузки |
| Склады продавца | `POST warehouses/create`, `warehouses/list`, `warehouses/retrieve` | онбординг |
| Заборы со склада | `POST pickups/pickup-options`, `pickups/create`, `pickups/cancel`, `pickups/retrieve` | v2 |

### ПВЗ

`POST pickup-points/list` ([doc](https://yandex.ru/support/delivery-profile/ru/api/other-day/ref/2.-Tochki-samoprivoza-i-PVZ/apib2bplatformpickup-pointslist-post)):
- **Фильтры:**
  - `geo_id`;
  - `type`: `pickup_point` (ПВЗ), `terminal` (постамат), `warehouse`;
  - `payment_method`: `already_paid`, `card_on_receipt`, `postpay`;
  - `latitude{from,to}`, `longitude{from,to}` — прямоугольник на карте;
  - `available_for_dropoff` — для `warehouse` обязательно `true`;
  - `operator_ids`: `market_l4g`, `5post`;
  - `pickup_services`: примерка, частичный отказ и т.д.
- С пустым телом возвращаются все точки. Список огромный: кэшируйте в `carrier_pickup_points` и обновляйте раз в сутки. На витрине фильтруйте по bbox карты из БД.
- **Поля ответа:** `id` (передаётся в заказ как `platform_station`), `address` (с индексом), `position{latitude,longitude}`, `schedule` (с таймзоной), `payment_methods[]`, `dayoffs`, `type`, `operator_id`.
- `geo_id` берут из `POST location/detect` по строке адреса ⚠️ (формат тела не проверен).

### Создание заказа

**Вариант A — два шага, рекомендуется для витрины с выбором слота.**
1. `offers/create` с составом заказа возвращает офферы: цена, интервал, `offer_id`.
2. Покупатель выбирает. После оплаты вызываем `offers/confirm {offer_id}` и получаем `request_id`.

Оффер живёт ограниченное время ⚠️. Если оплата заняла дольше, пересоздавайте его.

**Вариант B — один шаг:** `request/create` ([doc](https://yandex.ru/support/delivery-profile/ru/api/other-day/ref/3.-Osnovnye-zaprosy/apib2bplatformrequestcreate-post)). Обязательные поля:
- `info.operator_request_id` — наш уникальный ID отправления. Он же служит ключом идемпотентности.
- `source.platform_station.platform_id` (склад продавца) и `source.interval_utc`.
- `destination`:
  - `type=platform_station` (ПВЗ, `platform_id`)
  - или `type=custom_location` (адрес для курьера, с `interval` доставки).
- `items[]`: `count`, `name`, `article`, `place_barcode` (связь с местом), `billing_details{unit_price, assessed_unit_price, nds, inn}`.
- `places[]`: `barcode`, `physical_dims{weight_gross, dx, dy, dz}`.
- `billing_info{payment_method: already_paid|card_on_receipt|postpay, delivery_cost}`.
- `recipient_info{first_name, last_name?, patronymic?, phone, email?}`.
- `last_mile_policy`: `time_interval` (курьер) или `self_pickup` (ПВЗ).

Ответ: 200 с `request_id`. При ошибке 400 с кодом, например `no_delivery_options`.

**Единицы:** деньги **в копейках**, габариты в **сантиметрах**, вес в **граммах**.

**Наложенный платёж:** `payment_method=card_on_receipt` или `postpay`. Суммы берутся из `unit_price` × `count` плюс `delivery_cost` ⚠️.

### Ярлыки

`POST request/generate-labels` ([doc](https://yandex.ru/support/delivery-profile/ru/api/other-day/ref/4.-Yarlyki-i-akty-priema-peredachi/apib2bplatformrequestgenerate-labels-post.md)):
- Тело: `{request_ids[], generate_type: one|many, label_size_mm: "210x297"|"100x150"|"75x120"|"58x60"|"58x40", language:"ru"}`.
- Ответ **сразу** приходит как `application/pdf` в бинарном виде, без асинхронности.
- Есть лимит на число заказов в запросе, но в документации его значение не указано ⚠️. Бейте запрос по 50 ⚠️.

## Трекинг и вебхуков нет

- Для other-day API вебхуков/коллбэков в документации **не найдено** ([llms.txt](https://yandex.ru/support/delivery-profile/ru/llms.txt)). `ParseWebhook` не реализуем, `Capabilities.Webhooks=false`.
- Статусы берём фоновым опросом: `POST requests/info` пачкой по активным отправлениям каждые 30–60 минут, `request/history` — для подробной ленты событий.

## Статусы → наши

Источник: [статусная модель](https://yandex.ru/support/delivery-profile/ru/api/other-day/status-model.md).

| Яндекс | Наш |
|---|---|
| `CREATED`, `DELIVERY_PROCESSING_STARTED`, `SORTING_CENTER_LOADED` | `created` |
| `SORTING_CENTER_AT_START` (поступил в точку приёма/СЦ) | `accepted` |
| `SORTING_CENTER_PREPARED`, `SORTING_CENTER_TRANSMITTED`, `DELIVERY_AT_START`, `DELIVERY_AT_START_SORT`, `DELIVERY_TRANSPORTATION`, `DELIVERY_TRANSPORTATION_RECIPIENT`, `DELIVERY_TIME_INTERVALS_UPDATED`, `DELIVERY_ATTEMPT_FAILED` (событие без смены статуса) | `in_transit` |
| `DELIVERY_ARRIVED_PICKUP_POINT` | `ready_for_pickup` |
| `DELIVERY_TRANSMITTED_TO_RECIPIENT`, `DELIVERY_DELIVERED`, `CONFIRMATION_CODE_RECEIVED`, `PARTICULARLY_DELIVERED` (частично — пометить) | `delivered` |
| `SORTING_CENTER_RETURN_RETURNED`, `RETURN_TRANSPORTATION_STARTED`, `RETURN_ARRIVED_DELIVERY`, `RETURN_READY_FOR_PICKUP`, `RETURN_RETURNED` | `returned` |
| `CANCELLED` (с причиной: `SHOP_CANCELLED`, `USER_CHANGED_MIND`, `PICKUP_EXPIRED`, `ORDER_WAS_LOST`…) | `canceled` |
| `VALIDATING_ERROR` | `failed` |

`PICKUP_EXPIRED` означает, что покупатель не забрал заказ, и дальше будет возврат. Показывайте продавцу причину отмены.

## Локации и страны

- ПВЗ адресуются своим `platform_id`, адреса — строкой плюс `geo_id` или координатами ⚠️.
- **Страны:** в описании other-day указано «Доставка по России». Казахстан не заявлен ([llms.txt](https://yandex.ru/support/delivery-profile/ru/llms.txt)). `Capabilities.Countries=["RU"]`.
- Экспресс-доставка работает в городах КЗ, но это другой API ⚠️.

## Лимиты и подводные камни

- Rate limit в документации не нашёл ⚠️. Клиентский лимит около 5 запросов в секунду, backoff на 429.
- Копейки в денежных полях. Это отличается от СДЭК, где суммы decimal: не перепутайте при маппинге.
- `source.interval_utc` должен попадать в расписание склада или забора. Иначе будет ошибка или `no_delivery_options`.
- Склад продавца (`warehouses/create`) нужно создать один раз при подключении и сохранить `platform_id` в настройках подключения.
- Частичная выдача (`PARTICULARLY_DELIVERED`) влияет на возвраты денег. Заведите событие для продавца.
