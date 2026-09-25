---
name: nado-marketplace-import
description: Импорт и синхронизация товаров из маркетплейсов в каталог nado. Покрывает Kaspi.kz (кабинет продавца через служебного сотрудника с MFA по e-mail, запасной путь — файл Excel/XML), Wildberries, Ozon, Яндекс Маркет, а также Halyk Market, Мегамаркет и Avito в будущем. Внутри — интерфейс Connector, нормализованная модель ExternalProduct/ExternalOffer, алгоритм синхронизации (upsert, хеш контента, защита правок продавца, склейка товаров по штрихкоду), скачивание фото, лимиты API и задачи очереди. Загружай при любой работе с подключением кабинета маркетплейса, токенами WB/Ozon/YM/Kaspi, импортом каталога, обновлением цен и остатков, карточками, вариантами, FBS/FBO и sync_runs. Загружай и при разборе ошибок синхронизации, даже если маркетплейс не назван.
---

# Импорт из маркетплейсов

Решения по области (nado-platform → decisions.md):
- **D-05**: синхронизация односторонняя, из маркетплейса к нам;
- **D-13**: старт в КЗ, поэтому первым идёт Kaspi, затем WB и Ozon (оба работают
  в КЗ), затем Яндекс Маркет;
- **D-14**: цены маркетплейсов хранятся по источникам, итоговую цену считают
  правила магазина;
- **D-19, D-20** (уточняют D-10): Kaspi через кабинет со служебным сотрудником,
  которого nado создаёт сам по SMS-коду владельца; файл — запасной путь;
- открытые вопросы №4 (сервисный токен WB для юрлица РК), №27 (описания товаров
  Kaspi), №28 (почта для служебных сотрудников), №29 (роли сотрудника).

## Пакеты

```
internal/integration/marketplace/
  marketplace.go        интерфейс Connector, типы, реестр, Capabilities
  wildberries/  ozon/  yandexmarket/  kaspi/
internal/service/catalog_sync.go   алгоритм синхронизации (не зависит от маркетплейса)
internal/service/connections.go    подключение и проверка учётных данных
```

## Интерфейс

```go
package marketplace

type Code string // "wildberries" | "ozon" | "yandex_market" | "kaspi"

type Capabilities struct {
    APICatalog  bool // каталог читается по API
    FileImport  bool // поддерживается загрузка файла выгрузки
    Offers      bool // цены и остатки читаются по API
    Warehouses  bool // остатки по складам с признаком FBS/FBO
}

type Connector interface {
    Code() Code
    Capabilities() Capabilities
    // Verify проверяет учётные данные при подключении и возвращает то, что
    // можно показать продавцу: имя кабинета, ID, список складов.
    Verify(ctx context.Context, creds Credentials) (*AccountInfo, error)
    // Products обходит каталог. since.IsZero() — полный обход.
    Products(ctx context.Context, creds Credentials, since time.Time) iter.Seq2[ExternalProduct, error]
    // Offers отдаёт цены и остатки — частая лёгкая синхронизация.
    Offers(ctx context.Context, creds Credentials) iter.Seq2[ExternalOffer, error]
}

// FileImporter — для маркетплейсов без API чтения каталога (Kaspi).
type FileImporter interface {
    ParseFile(ctx context.Context, name string, r io.Reader) iter.Seq2[ExternalProduct, error]
}
```

- Итераторы `iter.Seq2` (Go 1.23+) нужны, чтобы не держать в памяти каталог на
  100 тыс. SKU. Пагинация и курсоры спрятаны внутри адаптера.
- Если во время обхода возвращается ошибка, обход останавливается. Service решает,
  фиксировать частичный результат или нет (см. ниже).

## Нормализованная модель

```go
type ExternalProduct struct {
    ExternalID  string            // nmID (WB), product_id (Ozon), offerId (YM), sku (Kaspi)
    GroupID     string            // объединение вариантов: imtID (WB), model/merge (Ozon), familyId (Kaspi)
    VendorCode  string            // артикул продавца
    Title, Description, Brand string
    Category    ExternalCategory  // {ID, Path []string}
    Attributes  []Attribute       // {Name, Values []string}
    Images      []string          // URL в максимальном разрешении, по порядку
    Videos      []string
    Dimensions  Dimensions        // мм и граммы — наши единицы; адаптер переводит
    Variants    []ExternalVariant // минимум один
    Raw         json.RawMessage   // исходный ответ по карточке — в product_sources.raw_json
    UpdatedAt   time.Time
}

type ExternalVariant struct {
    ExternalID string            // chrtID/size (WB), sku/offer_id (Ozon), ...
    SKU        string
    Barcodes   []string
    Options    map[string]string // "size": "M", "color": "синий"
    Price, OldPrice money.Money  // если маркетплейс отдаёт цену в карточке
}

type ExternalOffer struct {
    VariantExternalID string
    Price, OldPrice   money.Money
    Stocks            []StockLevel // {WarehouseID, WarehouseName, Qty, Fulfillment: "fbs"|"fbo"}
}
```

Особенности каждого маркетплейса и таблицы соответствия полей лежат в
`references/<маркетплейс>.md`. **Прочитай нужный файл перед реализацией
адаптера.** Эндпоинты, которых там нет, сверяй с официальной документацией
по ссылке оттуда, а не пиши по памяти.

## Алгоритм синхронизации контента (catalog_sync.go)

1. Создаётся `sync_runs` со статусом `running`.
2. Обходится `Products(since)`. Для каждой карточки:
   - `hash = sha256(канонический JSON нормализованных полей)`; поле `Raw` в хеш
     не входит;
   - ищется `product_sources` по `(connection_id, external_id)`;
     - **нет** — склейка (п. 3), затем создание товара и вариантов, затем постановка
       загрузки медиа;
     - **есть, хеш не изменился** — только `synced_at`;
     - **есть, хеш изменился** — обновляются поля, **не входящие** в
       `products.overridden_fields`, и обновляется `raw_json`.
   - Пишется пачками по 100–500 карточек в транзакции: MERGE с HOLDLOCK, лимит
     2100 параметров.
3. **Склейка** одного товара с разных маркетплейсов:
   - вариант с тем же штрихкодом уже есть в аккаунте — источник привязывается к
     существующему варианту;
   - иначе совпал `VendorCode`, а у варианта те же `Options` — привязка
     помечается как «предложена», продавец подтверждает её в кабинете;
   - иначе создаётся новый товар.
   Молча склеивать по названию нельзя.
4. Карточки, не встреченные при **полном** обходе, получают
   `product_sources.removed_at`. Товар не удаляется, а скрывается на витрине по
   настройке магазина. Если обход прервался ошибкой, пропавшие не помечаются: по
   неполным данным удалять ничего нельзя.
5. `sync_runs.stats`: created/updated/unchanged/removed/failed. Проблемы отдельных
   карточек (нет фото, неизвестная категория) пишутся в `sync_issues`, а обход
   продолжается.

## Правки продавца

Когда продавец меняет в кабинете поле, пришедшее с маркетплейса (title,
description, images, attributes...), имя поля добавляется в
`products.overridden_fields`. Синхронизация эти поля не трогает. Кнопка «Вернуть
значение с маркетплейса» убирает поле из списка и применяет `raw_json`.

## Цены и остатки (Offers)

- Выполняются часто, раз в 15–60 минут по тарифу. Меняют только две таблицы:
  - `variant_stocks` (`source_key = conn:<id>:wh:<warehouseId>`);
  - `variant_marketplace_prices` — цена **этого** маркетплейса.
  В `store_offers` синхронизация **не пишет**. Если цены изменились, ставится
  задача `pricing.recalc` по затронутым вариантам, и правила магазина считают
  итоговую цену (D-14, nado-storefront → pricing-rules.md). Ручная цена продавца
  синхронизацией не трогается никогда.
- Остатки FBO/FBW сохраняются с признаком fulfillment, но **по умолчанию не
  участвуют** в доступном остатке витрины: такой товар продавец не может отгрузить
  сам. Учитывать ли их — настройка магазина.
- Сравнение идёт до записи: неизменившиеся строки не обновляются, чтобы не
  раздувать журнал транзакций.

## Медиа

Фото и видео скачиваются к нам (задача `media.fetch`), хотлинк на CDN
маркетплейса не используется: ссылки меняются, и это чужой трафик. Правила:
- дедупликация по sha256;
- максимальный размер ограничен;
- проверяется реальный MIME;
- ошибка загрузки одного фото не валит импорт, она пишется в `sync_issues`.

## Задачи очереди

| kind | dedup_key | Когда |
|---|---|---|
| `marketplace.verify` | `verify:<conn>` | при подключении и по кнопке |
| `marketplace.sync_content` | `sync_content:<conn>` | после подключения, раз в сутки, по кнопке |
| `marketplace.sync_offers` | `sync_offers:<conn>` | по расписанию из тарифа |
| `marketplace.import_file` | `import_file:<upload>` | после загрузки файла (Kaspi, запасной путь) |
| `kaspi.login` | `kaspi_login:<conn>` | вход служебного сотрудника в кабинет — строго один на подключение |
| `pricing.recalc` | `pricing:<store>` | после изменения цен маркетплейсов |
| `media.fetch` | `media:<acc>:<sha(url)>` | новые URL фото |

## Учётные данные

- Хранятся в `provider_credentials` в зашифрованном виде (nado-go-conventions).
- Для OAuth (Ozon, ЮKassa) хранятся access/refresh token и срок действия,
  обновление выполняется заранее.
- WB-токен живёт 180 дней: за 14 дней до истечения продавец получает
  предупреждение в кабинете.
- 401/403 переводит подключение в `status='invalid'` без повторов, продавец видит
  «переподключите».

## Особенности, которые легко пропустить

- **WB:** облачные сервисы из Каталога решений с 01.01.2026 платят за каждый
  запрос (pay-as-you-go). Считай запросы в `sync_runs.stats` и не делай лишних
  обходов. Остатки складов продавца запрашиваются по `chrtIds`
  (с 20.05.2026), старый `supplier/stocks` отключён 23.06.2026.
- **Яндекс Маркет:** при превышении лимита отвечает кодом **420**, а не 429,
  поэтому адаптер переводит его в `integration.ErrRateLimited`. Валюта приходит
  как `RUR` и переводится в `RUB`.
- **Ozon:** id атрибутов (бренд, аннотация, видео) берутся из справочника
  категорий, не хардкодятся.
- **Kaspi (D-19, D-20):** основной путь — кабинет продавца через **служебного
  сотрудника** платформы.
  - Онбординг: SMS-код владельцу → токен API → создание сотрудника с e-mail на
    нашем домене.
  - Пароль сотрудника и коды MFA приходят письмами и читаются по IMAP (пакет
    `internal/integration/mailbox`). Пароль владельца не хранится.
  - Все шаги, эндпоинты и поля `offer-view/list` взяты из референсного кода
    пользователя (`docs/project/kaspi_samples/`) и описаны в
    `references/kaspi.md`. Эндпоинтов сверх описанных не выдумывай.
  - Описаний и характеристик Kaspi не отдаёт (open-questions №27).
  - Небезопасные приёмы образца (выключенный TLS, пароли открытым текстом и в
    коде, широкие роли) не переносятся.

## Чеклист нового адаптера

- [ ] Прочитан `references/<маркетплейс>.md`, эндпоинты сверены с документацией.
- [ ] Лимиты заданы в ключе лимитера (по категориям API, если они разные).
- [ ] Единицы переведены: см → мм, кг → г, рубли → копейки.
- [ ] Варианты сгруппированы (`GroupID`), у каждого `ExternalVariant` есть
      стабильный `ExternalID`.
- [ ] Табличные тесты нормализации на реальных ответах из `testdata/`.
- [ ] Коннектор зарегистрирован в реестре (`marketplace.Register`) в `app.go`.
- [ ] Возможности (`Capabilities`) честно отражают, что умеет API.
