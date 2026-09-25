# Wildberries — коннектор импорта

Данные проверены в сентябре 2026 года. Портал dev.wildberries.ru не отдаёт страницы автоматическим
загрузчикам. Поэтому пути и поля сверены по двум источникам:

- OpenAPI-зеркало [eslazarev/wildberries-sdk/specs](https://github.com/eslazarev/wildberries-sdk/tree/main/specs), для контента;
- npm-пакет `daytona-wildberries-typescript-sdk@4.6.0` (типы сгенерированы из спецификаций WB), для цен и остатков.

Перед реализацией сверьте всё с официальной документацией:
[работа с товарами](https://dev.wildberries.ru/openapi/work-with-products),
[аналитика](https://dev.wildberries.ru/openapi/analytics).

## Домены

| Категория токена | Домен | Что нужно коннектору |
|---|---|---|
| Контент | `https://content-api.wildberries.ru` | карточки, фото, характеристики, категории |
| Цены и скидки | `https://discounts-prices-api.wildberries.ru` | цены по размерам |
| Маркетплейс | `https://marketplace-api.wildberries.ru` | склады продавца, остатки FBS |
| Аналитика | `https://seller-analytics-api.wildberries.ru` | отчёты об остатках (FBS и FBW) |
| Общее | `https://common-api.wildberries.ru` | данные продавца, `ping`, проверка токена (`Verify`) |

Для тестов есть песочница: `content-api-sandbox.wildberries.ru` и `marketplace-api-sandbox.wildberries.ru`.
Она работает с тестовым токеном и отдаёт не больше 1 запроса в секунду на все методы контента
([спецификация](https://raw.githubusercontent.com/eslazarev/wildberries-sdk/main/specs/02-items.yaml)).

## Авторизация: что это значит для SaaS

- Токен — это JWT. Его передают в заголовке `Authorization: <token>`, без `Bearer`.
- Токен выпускается в ЛК: «Настройки → Доступ к API».
- При выпуске выбирают категории (Контент, Цены и скидки, Маркетплейс, Аналитика…) и флаг «только чтение».
- Срок жизни — **до 180 дней**. Механизма обновления нет, токен перевыпускают вручную
  ([deeone.dev](https://deeone.dev/blog/wildberries-api-etl-grabli-2026.html),
  [WB Partners](https://seller.wildberries.ru/instructions/ru/ru/material/api-integration-with-token)).
- Типов токенов четыре:
  - **Персональный** — для собственных программ продавца или коробочных ERP.
  - **Сервисный** — для облачного сервиса из «[Каталога решений WB](https://dev.wildberries.ru/en/business-solutions)».
  - **Базовый** — со сниженными лимитами, общими для всех базовых токенов.
  - **Тестовый** — для песочницы.
- **Облачная платформа должна работать через сервисный токен.** Получить его можно только после
  включения в Каталог решений. Брать у продавца его персональный токен — нарушение правил.
- С 01.01.2026 облачные сервисы из каталога платят за каждый запрос (pay-as-you-go, постоплата пакетами).
  Для продавцов API бесплатен ([CNews](https://www.cnews.ru/news/line/2026-01-21_wb_api_perevodit_vneshnie_servisy),
  [Oborot](https://oborot.ru/news/wildberries-vvodit-oplatu-za-fakticheskoe-ispolzovanie-svoego-api-i262234.html)).
  - **Следствие:** каждый лишний запрос стоит денег.
  - Инкрементальный курсор по `updatedAt` и редкий опрос контента здесь нужны не для удобства, а для экономии.
- Что делать в коде:
  - хранить тип токена в `Credentials`;
  - раз в сутки проверять срок `exp` из JWT;
  - за 14 дней до истечения показывать продавцу предупреждение.
  - ⚠️ не проверено: процедура подключения сервисного токена через каталог (OAuth-подобный flow или ручная передача) и формат его заголовка.

## Лимиты и ошибки

Лимиты работают по схеме token bucket: период, лимит, интервал, burst.

| Ответ | Что делать |
|---|---|
| заголовок `X-Ratelimit-Remaining` | следить за остатком |
| `429` | ждать `X-Ratelimit-Retry` секунд; `X-Ratelimit-Reset` — время до полного восстановления |
| `409` | считается как 5–10 запросов |
| `401` | токен истёк или отозван: пометить подключение `needs_reauth`, ретраи не нужны |
| `403` | нет нужной категории у токена или тарифа |

Источник — [deeone.dev](https://deeone.dev/blog/wildberries-api-etl-grabli-2026.html).

Лимиты по методам:

| Группа методов | Лимит |
|---|---|
| Контент | 100 запросов/мин, интервал 600 мс, burst 5 |
| Цены и скидки | 10 запросов / 6 с на аккаунт, burst 5 |
| Остатки FBS `/api/v3/stocks/*` | 300 запросов/мин, burst 20 |
| Отчёты остатков аналитики | 3 запроса/мин, интервал 20 с, burst 1 |

Схема ответов меняется без предупреждения. Неизвестные поля не должны ломать разбор,
поэтому кладите весь ответ в `Raw`.

## Каталог: `POST content-api/content/v2/get/cards/list`

Тело запроса:

```json
{"settings": {
  "sort":   {"ascending": true},
  "filter": {"withPhoto": -1},
  "cursor": {"limit": 100, "updatedAt": "<из прошлого ответа>", "nmID": <из прошлого ответа>}
}}
```

- `limit` — не больше 100.
- Следующая страница: передайте `cursor.updatedAt` и `cursor.nmID` из ответа.
- Выборка закончилась, когда `cursor.total < limit`.
- Если курсор не сдвинулся (много карточек с одинаковым `updatedAt`), остановите цикл,
  иначе он станет бесконечным.
- Инкрементальный импорт (`since`): сортировка `ascending: true`, стартовый `cursor.updatedAt = since`.
  ⚠️ не проверено, что фильтр по `updatedAt` работает как «не раньше».
- Карточки из корзины (удалённые) отдаёт отдельный метод `POST /content/v2/get/cards/trash`.
  Он нужен, чтобы снимать товары с витрины.

Поля карточки (одна карточка = один `nmID` = один цвет):

- `nmID`, `imtID`, `vendorCode`, `title`, `description`, `brand`, `subjectID`, `subjectName`;
- `characteristics[] {id, name, value}` — тип `value` зависит от характеристики: строка, число или массив;
- `photos[] {big, c246x328, c516x688, square, tm}` — URL на CDN WB;
- `video` — строка URL;
- `sizes[] {chrtID, techSize, wbSize, skus[]}` — `skus` это баркоды;
- `dimensions {length, width, height` в **см**, `weightBrutto` в **кг**, `isValid}`;
- `updatedAt`, `createdAt`.

Справочники:

- `GET /content/v2/object/parent/all` — родительские категории;
- `GET /content/v2/object/all` — предметы;
- `GET /content/v2/object/charcs/{subjectId}` — характеристики предмета.

## Цены: `GET discounts-prices-api/api/v2/list/goods/filter`

- Параметры запроса: `limit` (до 1000), `offset`, `filterNmID`.
- Ответ: `data.listGoods[] {nmID, vendorCode, currencyIsoCode4217, discount, sizes[] {sizeID, price, discountedPrice, clubDiscountedPrice, techSizeName}}`.
- `sizeID` здесь — это `chrtID` из контента.
- Если цены у размеров разные (`editableSizePrice`), используйте `GET /api/v2/list/goods/size/nm`.
- Пакетный вариант: `POST /api/v2/list/goods/filter` с `nmList` до 1000 штук
  ([WBSeller docs](https://github.com/Dakword/WBSeller/blob/master/docs/Prices.md)).
- Цены — целые числа в валюте кабинета. Учитывайте `currencyIsoCode4217`: у продавца из КЗ может быть KZT.
- Какую цену брать для витрины:
  - `discountedPrice` → `Price` (цена со скидкой продавца, без скидок WB Клуба);
  - `price` → `OldPrice`.

## Остатки

**Склады продавца (FBS/DBS).** Рекомендуемый метод — отчёт
`POST seller-analytics-api/api/analytics/v1/stocks-report/seller-warehouses`.

- Один запрос возвращает все склады.
- Необязательные фильтры: `nmIds` (до 1000), `chrtIds`, `limit` (до 250 000), `offset`.
- Строка ответа: `{nmId, chrtId, warehouseId, warehouseName, regionName, quantity}`.
- Данные обновляются раз в 30 минут. Метод доступен только для персональных и сервисных токенов (по SDK 4.6.0).

Старый путь — `GET marketplace-api/api/v3/warehouses`, затем `POST /api/v3/stocks/{warehouseId}` с `{"chrtIds": [...]}`.

- Ответ: `stocks[] {chrtId, amount}`.
- С 2026-05-20 параметр `skus` (баркоды) отключён и возвращает 400. Нужны только `chrtIds`
  ([npm SDK](https://www.npmjs.com/package/daytona-wildberries-typescript-sdk)).

**Склады WB (FBW).** `POST seller-analytics-api/api/analytics/v1/stocks-report/wb-warehouses`,
те же поля плюс `inWayToClient` и `inWayFromClient`.

- Лимит 3 запроса/мин.
- Заменяет `GET statistics-api/api/v1/supplier/stocks`, который отключён 23.06.2026.
- Асинхронный отчёт `GET /api/v1/warehouse_remains` (создать задачу → статус → download) тоже работает,
  но медленнее ([спецификация отчётов](https://raw.githubusercontent.com/eslazarev/wildberries-sdk/main/specs/12-reports.yaml)).

## Маппинг

| WB | Наша модель |
|---|---|
| `nmID` | `ExternalProduct.ExternalID` (строкой) |
| `imtID` | `GroupID`: цвета одной модели объединены через `imtID` |
| `vendorCode` | `VendorCode` |
| `title`, `description`, `brand` | `Title`, `Description`, `Brand` |
| `subjectID` / `subjectName` | `Category.ExternalID` / `Category.Path` (родитель берётся из `object/parent/all`) |
| `characteristics[].name`, `value` | `Attributes[] {Name, Values}`: скаляр приводится к `[]string` |
| `photos[].big` | `Images` (в исходном порядке; первое фото — главное) |
| `video` | `Videos` |
| `dimensions.length/width/height` × 10 | `Dimensions.LengthMM/WidthMM/HeightMM` |
| `dimensions.weightBrutto` × 1000 | `Dimensions.WeightG` |
| `sizes[].chrtID` | `ExternalVariant.ExternalID`, а также `ExternalOffer.VariantExternalID` |
| `sizes[].skus` | `ExternalVariant.Barcodes`; `SKU = vendorCode + "/" + techSize`, если размеров больше одного |
| `sizes[].techSize` / `wbSize` | `Options["size"]`; цвет — из характеристики «Цвет» |
| `listGoods.sizes[].discountedPrice` / `price` | `Price` / `OldPrice` в валюте `currencyIsoCode4217` |
| `stocks-report.quantity`, `warehouseId`, `warehouseName` | `StockLevel`: `Fulfillment` = `"fbs"` для складов продавца, `"fbo"` для складов WB |
| весь объект карточки | `Raw` |
| `updatedAt` | `UpdatedAt` |

## Подводные камни

- Безразмерный товар всё равно имеет один элемент `sizes` с пустым `techSize` и собственным `chrtID`.
- Фото с CDN WB нужно копировать в своё хранилище. Ссылки нельзя встраивать в витрину: URL меняются, а хотлинк запрещён правилами.
- Лимиты считаются на аккаунт продавца. Импорт двух магазинов одного продавца делит один бюджет запросов.
- Для продавца из КЗ цены могут приходить в тенге, для продавца из РФ — в рублях. Не смешивайте валюты при импорте.
- Вебхуки: в списке доменов есть `webhooks-api.wildberries.ru`
  ([deeone.dev](https://deeone.dev/blog/wildberries-api-etl-grabli-2026.html)). ⚠️ не проверено, какие события
  доступны. Пока опирайтесь на опрос API.

## Что не проверено

- ⚠️ Процесс подключения продавца к сервисному токену и стоимость пакетов pay-as-you-go.
- ⚠️ Точная семантика `cursor.updatedAt` для инкрементального импорта.
- ⚠️ Лимиты цен 10 запросов / 6 с взяты из вторичных источников.
- ⚠️ Отчёты `stocks-report/*` описаны по типам стороннего SDK. Сверьте тело и ответ с dev.wildberries.ru.
