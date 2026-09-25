# Ozon — коннектор импорта

Данные проверены в сентябре 2026 года. Порталы docs.ozon.ru и dev.ozon.ru не отдают страницы
автоматическим загрузчикам. Поэтому пути и поля сверены по двум источникам:

- типам, сгенерированным из официальной OpenAPI 2.1: [artefactby/ozon-seller-api `src/generated/types.ts`](https://github.com/artefactby/ozon-seller-api);
- поисковым выдержкам dev.ozon.ru.

Официальная документация: <https://docs.ozon.ru/api/seller/>. Есть готовый Go-клиент
[diphantxm/ozon-api-client](https://pkg.go.dev/github.com/diphantxm/ozon-api-client/ozon), его можно взять как образец.
Свой клиент на `net/http` тоже подойдёт.

Базовый URL: `https://api-seller.ozon.ru`. Все методы — `POST` с JSON-телом.
⚠️ не проверено: у Seller API нет публичной песочницы.

## Авторизация

Для SaaS правильный вариант — **OAuth**. Схема Client-Id + Api-Key оставлена как запасной путь.

**1. OAuth (частное приложение).** Приложение заводится на <https://dev.ozon.ru/apps/private/>.

- В приложении указывают scope и Redirect URL, получают `client_id` и `client_secret`.
- Продавец проходит authorization code flow. Параметры: `response_type=code`, `access_type=offline`,
  `scope`, `state`, `prompt=select_company`.
- Код авторизации живёт 5 минут. Токен выдаётся в формате JWT.
- Заголовок запроса: `Authorization: Bearer <token>`.
- Токен обновляется через `grant_type=refresh_token` без участия продавца.
- Если refresh-токен перестал работать (продавец отозвал доступ), нужна повторная авторизация продавцом.

Источники: [OAuth-процесс](https://dev.ozon.ru/start/450-Protsess-OAuth-avtorizatsii-dlia-dostupa-k-Seller-API-Ozon/),
[частное приложение](https://dev.ozon.ru/start/447-Zavedenie-chastnogo-prilozheniia-v-Seller-API/).

- ⚠️ не проверено: точные URL `authorize`/`token`, срок жизни access-токена, ограничения частного
  приложения по числу продавцов и требования для публикации в «Магазине приложений».
- Новые методы появляются в scope не сразу. Известен случай, когда методы FBO-поставок отвечали 403
  при OAuth ([community](https://dev.ozon.ru/community/2166-Novye-metody-FBO-postavki-otsutstvuiut-v-OAuth-scope-chastnogo-prilozheniia-403/)).
  На каждый используемый метод нужен интеграционный тест с реальным токеном.

**2. Client-Id + Api-Key.** Продавец создаёт ключ в ЛК: «Настройки → API-ключи». Лучше выбирать роль только
для чтения. Заголовки: `Client-Id`, `Api-Key`. Годится для MVP и для продавцов, которые не хотят проходить OAuth.
В `Credentials` храните оба варианта.

Для `Verify` подойдёт любой дешёвый запрос, например `/v3/product/list` с `limit: 1`.
⚠️ не проверено: отдельного метода «кто я» не нашёл.

## Лимиты

- 50 запросов/с на все методы одного Client-Id
  ([выдержка dev.ozon.ru](https://dev.ozon.ru/news/584-Vnedriaem-novye-limity-v-Seller-API-s-19-maia/)).
- Методы записи товаров ограничены отдельно: 30 000 товарных операций в минуту.
  На импорт (чтение) это не влияет ([avoshop](https://avoshop.ru/company/news/2026/limity_api_ozon_s_24_fevralya_2026_goda/)).
- Ответ `429`: делайте экспоненциальный backoff и выполняйте запросы к одному Client-Id последовательно.

## Каталог: шаги импорта

**1. `POST /v3/product/list`.** Тело: `{"filter": {"visibility": "ALL"}, "last_id": "", "limit": 1000}`.

- `limit` от 1 до 1000.
- Следующая страница: `last_id` из ответа. Выборка закончилась, когда пришёл пустой `items`.
- Элемент ответа: `{product_id, offer_id, sku, archived, has_fbo_stocks, has_fbs_stocks, is_discounted, quants[]}`.
- Архивные товары в `visibility: "ALL"` не попадают. Их отдаёт фильтр `"ARCHIVED"`, он нужен для снятия товаров с витрины.
- `result.total` отключают 23.11.2026, используйте `total_items`.

**2. `POST /v3/product/info/list`.** Тело: `{"product_id": ["..."]}`, до 1000 товаров. Также можно передать `offer_id[]` или `sku[]`.

- Ответ: `items[]` с полями
  - `id` (product_id), `offer_id`, `sku`, `name`, `barcodes[]`;
  - `images[]`, `primary_image[]`, `color_image[]`;
  - `description_category_id`, `type_id`;
  - `price`, `old_price`, `min_price` (строки), `currency_code`, `vat`;
  - `model_info {model_id, count}`, `stocks {has_stock, stocks[] {present, reserved, sku, source}}`;
  - `created_at`, `updated_at`, `is_archived`.

**3. `POST /v4/product/info/attributes`.** Тело: `{"filter": {"product_id": [...], "visibility": "ALL"}, "limit": 1000, "last_id": ""}`.

- Ответ: `result[]` с полями
  - `id`, `offer_id`, `sku`, `name`, `barcode`, `barcodes`;
  - `description_category_id`, `type_id`;
  - `height`, `depth`, `width`, `dimension_unit` (`mm`/`cm`/`in`), `weight`, `weight_unit` (`g`/`kg`/`lb`);
  - `images`, `primary_image`, `color_image`, `pdf_list`, `model_info`;
  - `attributes[] {id, complex_id, values[] {dictionary_value_id, value}}`, `complex_attributes[]`.
- Пагинация через `last_id`.

**4. `POST /v1/product/info/description`.** Тело: `{"product_id": N}` или `{"offer_id": "..."}`, **по одному товару**.

- Ответ: `result {id, offer_id, name, description}`.
- На большом каталоге это самый дорогой шаг. Запрашивайте описание, только когда `updated_at` товара изменился.
- ⚠️ не проверено: описание может также приходить атрибутом с id 4191 («Аннотация»). Если это так, отдельный запрос не нужен.

**5. Названия атрибутов** берутся из справочника `POST /v1/description-category/attribute`
(`description_category_id` + `type_id`). Кешируйте его на сутки: в `attributes` приходят только `id`.
Дерево категорий — `POST /v1/description-category/tree`.

**6. Видео.** Это комплексный атрибут: 21841 — ссылка, 21837 — название, связаны общим `complex_id`
([выдержка seller-edu/habr](https://habr.com/ru/articles/872228/)). ⚠️ не проверено по официальной документации.

## Цены: `POST /v5/product/info/prices`

- Тело: `{"filter": {"visibility": "ALL"}, "limit": 1000, "cursor": ""}`. Пагинация через `cursor`.
- Элемент ответа: `price {price, old_price, marketing_seller_price, min_price, retail_price, currency_code, vat, ...}`.
- Цена для витрины: `price` (цена продавца до акций Ozon). Зачёркнутая цена: `old_price`.

## Остатки: `POST /v4/product/info/stocks`

- Тело: `{"filter": {"visibility": "ALL"}, "limit": 1000, "cursor": ""}`.
- Ответ: `items[] {product_id, offer_id, stocks[] {type, present, reserved, sku, warehouse_ids[]}}`.
- Значения `type`: `fbs` (склад продавца, доставка Ozon), `rfbs` (склад продавца, доставка продавца),
  `fbo` (склад Ozon), `fbp` (склад партнёра).
- Доступный остаток считается как `present - reserved`.
- Разбивку по конкретным складам отдают:
  - `POST /v2/product/info/stocks-by-warehouse/fbs` (FBS);
  - `POST /v1/product/info/stocks-by-warehouse/fbo` (FBO);
  - список складов продавца — `POST /v1/warehouse/list`.

## Маппинг

| Ozon | Наша модель |
|---|---|
| `id` / `product_id` | `ExternalProduct.ExternalID`, одновременно `ExternalVariant.ExternalID` (у Ozon товар = вариант) |
| `model_info.model_id` | `GroupID`: товары с одинаковым `model_id` объединены на одной карточке |
| `offer_id` | `VendorCode` и `ExternalVariant.SKU` |
| `name` | `Title` |
| `/v1/product/info/description.description` | `Description` (HTML-подобная разметка, нужна санитизация) |
| атрибут «Бренд» (id 85 ⚠️) | `Brand` |
| `description_category_id` + `type_id` | `Category.ExternalID` = `"<dcid>:<type_id>"`, `Path` — из `/v1/description-category/tree` |
| `attributes[].id` → название из справочника; `values[].value` | `Attributes[] {Name, Values}` |
| `primary_image` + `images` | `Images` (сначала главное; убрать дубли) |
| атрибут 21841 | `Videos` |
| `depth` / `width` / `height` + `dimension_unit` | `Dimensions.LengthMM/WidthMM/HeightMM` (перевести в мм) |
| `weight` + `weight_unit` | `Dimensions.WeightG` |
| `barcodes[]` | `ExternalVariant.Barcodes` |
| атрибуты «Цвет товара», «Размер» (id ⚠️) | `ExternalVariant.Options["color"/"size"]` |
| `price.price` / `old_price`, `currency_code` | `Price` / `OldPrice` (строки или числа → `money.Money`) |
| `stocks[].present - reserved`, `type` | `StockLevel.Qty`; `Fulfillment` = `"fbo"` для `fbo`/`fbp`, `"fbs"` для `fbs`/`rfbs` |
| `updated_at` | `UpdatedAt` |

## Подводные камни

- Карточка Ozon может быть общей для нескольких продавцов. Контент и фото могли загрузить не вы.
  Покажите продавцу источник контента.
- `price` в `/v3/product/info/list` — строка, в `/v5/product/info/prices` — число. Разбирайте оба варианта.
- Продавец из РФ, который продаёт в КЗ, работает в том же кабинете. Валюта в `currency_code` бывает RUB, KZT, CNY и другие.
- `sku` (Ozon SKU) и `product_id` — разные идентификаторы. Для связи с остатками и ценами используйте `product_id`.
- Отменённые или удалённые товары не приходят в `visibility: "ALL"`. Для сверки запрашивайте `ARCHIVED` отдельно.
- Push-уведомления: `/v1/notification/set`, `/v1/notification/list`, `/v1/notification/check`
  (есть в OpenAPI, [types.ts](https://github.com/artefactby/ozon-seller-api)). ⚠️ не проверено, какие события
  относятся к товарам. Пока опирайтесь на опрос API.

## Что не проверено

- ⚠️ URL и сроки OAuth-токенов, ограничения частного приложения и порядок публикации публичного.
- ⚠️ id атрибутов «Бренд», «Цвет», «Размер», «Аннотация». Берите их из `/v1/description-category/attribute`, а не хардкодьте.
- ⚠️ Отдельные лимиты у `/v1/product/info/description` и `/v4/product/info/attributes`.
- ⚠️ Песочница Seller API.
