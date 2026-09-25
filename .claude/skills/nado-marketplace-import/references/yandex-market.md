# Яндекс Маркет — коннектор импорта

Данные проверены в сентябре 2026 года по официальной OpenAPI:
[yandex-market/yandex-market-partner-api](https://github.com/yandex-market/yandex-market-partner-api)
(`openapi/openapi.yaml`, `openapi/paths/*`, `openapi/components/schemas/*`).
Документация: <https://yandex.ru/dev/market/partner-api/doc/ru/>.

- Базовый URL: `https://api.partner.market.yandex.ru`.
- Кроме OpenAPI, есть готовые SDK от Яндекса. Для Go официального SDK нет ⚠️ не проверено.
  Клиент можно сгенерировать из OpenAPI (`oapi-codegen`) или написать вручную на нужные методы.

## Авторизация

- Заголовок `Api-Key: <token>`. Токен выпускает продавец в кабинете: «Настройки → API и модули → Токены»
  ⚠️ путь меню не проверен.
- Токен привязан к бизнес-аккаунту, **бессрочный**, при выпуске выбираются группы доступа (scopes).
- OAuth (`oauth.yandex.ru`, scope `market:partner-api`) устарел: токен живёт год
  ([авторизация](https://yandex.ru/dev/market/partner-api/doc/ru/concepts/authorization)).
- Какие scopes просить у продавца для импорта (минимально, только чтение):
  - `offers-and-cards-management:read-only` — каталог, карточки, остатки;
  - `pricing:read-only` — цены;
  - `inventory-and-order-processing:read-only` — склады.
  - Вместо трёх можно одним `all-methods:read-only`, но это избыточно.
- Идентификаторы:
  - `businessId` — кабинет; каталог общий для всех магазинов кабинета.
  - `campaignId` — магазин, то есть модель FBS/FBY/DBS/Express.
  - Оба берутся в `Verify`: `GET /v2/campaigns` возвращает магазины и их `business.id`. Сохраните их в `AccountInfo`.

## Лимиты и общие правила

- Не больше 4 параллельных запросов на токен, тело запроса до 512 КБ
  ([docs](https://yandex.ru/dev/market/partner-api/doc/ru/)).
- Лимиты методов заданы в OpenAPI (`x-resource-limit-config`). Считаются либо по числу запросов
  (`path: resp`), либо по числу элементов:

| Метод | Базовый тариф | Средний тариф |
|---|---|---|
| `offer-mappings`, `offer-cards` | 100 запросов/мин | 600 запросов/мин |
| `v3 …/offers/stocks` | 500 запросов/мин | 500 запросов/мин |
| `…/offer-prices` | 5 000 товаров/мин | 10 000 товаров/мин |

- При превышении лимита приходит **`420`** (не 429, так описано в OpenAPI). Обрабатывайте оба кода.

## Каталог: `POST /v2/businesses/{businessId}/offer-mappings`

- Параметры запроса: `page_token`, `limit` (1–100, по умолчанию 50; если больше 100, обрезается до 100), `language`.
- Тело необязательное. Фильтры: `offerIds[]`, `cardStatuses[]`, `categoryIds[]`, `vendorNames[]`, `tags[]`, `archived`.
- Без тела возвращается весь каталог постранично.
- Ответ: `result.paging.nextPageToken` и `result.offerMappings[] {offer, mapping, showcaseUrls}`.
- `offer` (GetOfferDTO / BaseOfferDTO) содержит:
  - `offerId`, `name`, `marketCategoryId`, `category`, `vendor`, `vendorCode`;
  - `description`, `pictures[]`, `videos[]`, `manuals[]`, `barcodes[]`, `tags[]`;
  - `weightDimensions {length, width, height` в **см**, `weight` в **кг брутто**`}`;
  - `params[]` — устарел, отключат **12.10.2026**, используйте `parameterValues`;
  - `basicPrice` (цена с `value`, `currencyId`, `discountBase`, `updatedAt`);
  - `groupId` — у вариантов одной карточки он одинаковый;
  - `cardStatus`, `archived`, `mediaFiles {pictures, videos, firstVideoAsCover, manuals}`, `campaigns[]`, `sellingPrograms[]`.
- `mapping` содержит `marketSkuName`, `marketModelName`, `marketCategoryId`, `marketCategoryName`.
- Архивные товары возвращаются только с `archived: true` в фильтре. Их нужно запрашивать для снятия товаров с витрины.

## Характеристики: `POST /v2/businesses/{businessId}/offer-cards`

- Тело: `{"offerIds": [...], "withRecommendations": false}`. Пагинация та же: `page_token`, `limit` до 100.
- Ответ: `offerCards[] {offerId, mapping, parameterValues[] {parameterId, unitId, valueId, value}, cardStatus, groupId}`.
- Названия параметров и единиц берутся из
  `POST /v2/category/{categoryId}/parameters` ⚠️ путь не сверен. Кешируйте его по `categoryId`.
- Дерево категорий: `POST /v2/categories/tree`.

## Цены: `POST /v2/businesses/{businessId}/offer-prices`

- Возвращает базовые цены для всех магазинов кабинета. Тело: `offerIds[]`, пагинация через `page_token`.
- Цены конкретного магазина: `POST /v2/campaigns/{campaignId}/offer-prices`.
- Валюта `currencyId`: **`RUR`, а не `RUB`**, также `KZT`, `BYN`, `UZS`. Переводите в ISO 4217 при маппинге.
- Цена для витрины: `basicPrice.value`. Зачёркнутая цена: `basicPrice.discountBase`.

## Остатки

- **Склады продавца (FBS/DBS/Express)**: `POST /v3/businesses/{businessId}/offers/stocks`.
  - Тело: `partnerWarehouseId`, `offerIds[]`, `archived`.
  - Ответ: остатки по складу кабинета.
  - Метод работает только если в кабинете **нет групп складов**. Если группы есть, используйте
    `POST /v2/campaigns/{campaignId}/offers/stocks`.
  - Список складов: `GET /v3/businesses/{businessId}/warehouses`.
- Типы остатков (`WarehouseStockType`): `FIT` (годный), `AVAILABLE` (доступный к заказу), `FREEZE`,
  `QUARANTINE`, `DEFECT`, `EXPIRED`, `UTILIZATION`. Для витрины берите `AVAILABLE`, а если его нет, то `FIT`.
- **Склады Маркета (FBY)**: асинхронный отчёт `POST /v2/reports/stocks-on-warehouses/generate`,
  дальше опрос статуса и скачивание. Используйте для `Fulfillment = "fbo"`.

## Маппинг

| Яндекс Маркет | Наша модель |
|---|---|
| `offer.offerId` | `ExternalProduct.ExternalID`, `ExternalVariant.ExternalID`, `SKU`, `VendorCode` (часто совпадает с `vendorCode`) |
| `offer.groupId` | `GroupID`; если пусто — `offerId` |
| `offer.vendorCode` | `VendorCode` (если есть; иначе `offerId`) |
| `offer.name`, `description`, `vendor` | `Title`, `Description`, `Brand` |
| `offer.marketCategoryId` / `mapping.marketCategoryName` | `Category.ExternalID` / `Category.Path` (путь — по дереву) |
| `offer-cards.parameterValues` → названия параметров | `Attributes[] {Name, Values}` (значения одного `parameterId` сгруппировать) |
| `offer.pictures[]` | `Images` |
| `offer.videos[]` | `Videos` |
| `weightDimensions.length/width/height` × 10 | `Dimensions.*MM` |
| `weightDimensions.weight` × 1000 | `Dimensions.WeightG` |
| `offer.barcodes[]` | `ExternalVariant.Barcodes` |
| параметры «Цвет», «Размер» из `parameterValues` | `ExternalVariant.Options` |
| `basicPrice.value` / `discountBase`, `currencyId` (RUR→RUB) | `Price` / `OldPrice` |
| остаток `AVAILABLE`, склад | `StockLevel`: `"fbs"` для складов продавца, `"fbo"` для FBY |
| объект `offerMapping` целиком | `Raw` |
| `basicPrice.updatedAt` / время карточки | `UpdatedAt` ⚠️ отдельного поля «товар изменён» в `offer-mappings` не нашёл |

## Подводные камни

- Поля «время изменения карточки» нет, поэтому инкрементальный импорт по `since` не работает.
  Делайте полный проход каталога, но редко (например, раз в сутки), а изменения определяйте по хешу контента.
- `params` отключают 12.10.2026. Сразу используйте `offer-cards.parameterValues`.
- Код ответа `420` при превышении лимита легко пропустить, если ретраить только `429`.
- Push-уведомления (`POST notification` на URL партнёра) касаются заказов, отмен, возвратов, чатов и отзывов, а не
  изменения каталога ([push](https://yandex.ru/dev/market/partner-api/doc/ru/push-notifications/)).
  Для импорта товаров не нужны.
- Каталог привязан к бизнесу, а цены и остатки могут отличаться между магазинами (campaign).
  Для витрины продавец выбирает один магазин-источник. Храните его `campaignId` в настройках подключения.

## Что не проверено

- ⚠️ Путь меню для выпуска Api-Key и точный путь справочника параметров категории.
- ⚠️ Есть ли у метода остатков поле `updatedAt` на уровне склада (в DTO есть `WarehouseOfferDTO.updatedAt`, семантику не проверял).
- ⚠️ Официальный Go SDK.
