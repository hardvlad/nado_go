# Kaspi.kz — коннектор импорта

**Содержание:**
- Официальный Shop API: что есть;
- Путь 1: импорт из файла (Excel, XML-прайс) — запасной;
- Путь 2: кабинет через служебного сотрудника — основной:
  - модель доступа;
  - онбординг (SMS-код владельцу, создание сотрудника);
  - приём почты;
  - рабочий вход;
  - запросы кабинета и поля `offer-view/list`;
  - что не повторять из образца;
  - реализация в nado;
- маппинг и что не проверено.

Данные проверены в сентябре 2026 года по [Kaspi Гиду для партнёров](https://guide.kaspi.kz/partner/ru/shop/api/general/q3193).

**Главное:** у официального Kaspi Shop API **нет методов чтения каталога, цен и остатков продавца.**
Есть только два направления:

- заказы — чтение и обработка;
- товары — только **запись** (импорт новых карточек в Kaspi).

Поэтому импорт с Kaspi идёт двумя путями: кабинет через служебного сотрудника (основной, по образцу пользователя) и файл (запасной). Во всех случаях
`Capabilities()` отражает, что чтения каталога через официальный API нет.

## Официальный Shop API: что есть

- Токен: ЛК продавца → «Настройки → Токен API» → «Сформировать». Заголовок `X-Auth-Token: <token>`.
  ⚠️ не проверено: срок жизни токена и лимиты.
- Заказы: `GET https://kaspi.kz/shop/api/v2/orders`.
  - Формат JSON:API (`Accept: application/vnd.api+json`).
  - Фильтры по state/status и дате; пагинация `page[number]` и `page[size]`, размер страницы до 100.
  - Позиции заказа: `/orderentries/{id}/product` ([q3201](https://guide.kaspi.kz/partner/ru/shop/api/orders/q3201)).
- Товары (запись): `POST https://kaspi.kz/shop/api/products/import`.
  - Поля: `sku`, `title`, `brand`, `category`, `description`, `images[{url}]`, `attributes[{code, value}]`.
  - Справочники: категории, атрибуты категории, значения атрибутов, JSON-схема импорта, статус импорта
    ([q3219](https://guide.kaspi.kz/partner/ru/shop/api/goods/q3219)).
- **Для импорта в nado официальный API полезен только в `Verify`**: запрос списка заказов с `page[size]=1`
  проверяет токен. Ещё он пригодится позже, если понадобится синхронизация заказов.

## Путь 1: импорт из файла (официальный, запасной)

Продавец выгружает файл из кабинета (`kaspi.kz/mc`) и загружает его в nado. Коннектор разбирает файл.
Метод `Products` получает не API, а загруженный файл (`io.Reader` из хранилища загрузок).

### Excel «Прайс-лист → Скачать в Excel»

Источник формата — [q3348](https://guide.kaspi.kz/partner/ru/shop/goods/price_list/q3348).

| Колонка | Смысл | Куда мапится |
|---|---|---|
| `SKU` | артикул продавца | `ExternalID`, `VendorCode`, `ExternalVariant.ExternalID/SKU` |
| `model` | название и модификация | `Title` |
| `brand` | бренд или «Без бренда» | `Brand` |
| `price` | цена в тенге | `Price` (KZT) |
| `PP1`…`PP5` | склады или точки выдачи: число (остаток) либо `yes`/`no` | `StockLevel{WarehouseID: "PP1"…, Qty}`, `Fulfillment: "fbs"`; при `yes` без числа `Qty` неизвестен |
| `preorder` | дни предзаказа, до 30 | в `Raw`, для отображения сроков |

Правила формата:

- данные только на первом листе;
- названия колонок не меняются;
- все пять колонок PP присутствуют, даже пустые.

Разбор: `github.com/xuri/excelize/v2`. Ищите колонки **по заголовку, а не по индексу**.
Нужен golden-тест на реальной выгрузке (попросите пример у пользователя).

### XML-прайс-лист

Этот формат Kaspi сам забирает по URL раз в 60 минут при изменениях
([q3251](https://guide.kaspi.kz/partner/ru/shop/goods/price_list/q3251)). Продавцы, у которых он уже есть,
могут дать nado ссылку на свой XML. Тогда импорт цен и остатков идёт по расписанию, без ручных загрузок.

```xml
<?xml version="1.0" encoding="utf-8"?>
<kaspi_catalog date="string" xmlns="kaspiShopping"
               xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
               xsi:schemaLocation="kaspiShopping http://kaspi.kz/kaspishopping.xsd">
  <company>CompanyName</company>
  <merchantid>CompanyID</merchantid>
  <offers>
    <offer sku="232130213">
      <model>iphone 5s white 32gb</model>
      <brand>Apple</brand>
      <availabilities>
        <availability available="yes" storeId="PP1" preOrder="5" stockCount="10"/>
      </availabilities>
      <price>192000</price>
      <!-- либо цены по городам: -->
      <cityprices><cityprice cityId="750000000">192000</cityprice></cityprices>
    </offer>
  </offers>
</kaspi_catalog>
```

Атрибут `schemaLocation` приведён по памяти, ⚠️ не проверено. Требования к полям:

- `sku` — до 20 символов, буквы и цифры, уникален;
- `available` — `yes` или `no`;
- `storeId` — код склада из кабинета;
- `preOrder` — от 0 до 30 дней;
- `stockCount` — остаток;
- цена — целое число, без пробелов и десятичной части.

Если есть `cityprices`, для витрины берите цену главного города продавца (настройка подключения) или минимальную.

Разбирайте потоково через `encoding/xml` (`Decoder.Token`). Namespace `kaspiShopping` нужно учитывать
при сравнении имён элементов.

### Чего в файлах нет

В файлах нет описаний, фото, характеристик и категорий. Импорт из файла даёт **скелет товара**:
название, бренд, цену и остатки. Контент продавец заполняет в nado вручную или подтягивает склейкой
по штрихкоду или артикулу с товаром из WB, Ozon или YM (см. алгоритм склейки в SKILL.md).
Об этом нужно честно сказать продавцу в UI.

## Путь 2: кабинет продавца через служебного сотрудника (неофициальный, основной)

Источник — референсный код пользователя из его проекта ProfitBot в
`docs/project/kaspi_samples/`:

| Файл | Что в нём |
|---|---|
| `lk_otp_checker.php` | онбординг: вход владельца по телефону и SMS-коду, получение API-токена, создание служебного сотрудника |
| `lk_otp_email_checker.php` | чтение почтового ящика: пароль нового сотрудника и коды MFA |
| `lk_checker.php` | вход служебного сотрудника и рабочие запросы (каталог, точки, заказы, цены) |
| `KaspiImport.php` | разбор ответа `offer-view/list` (`updateShopItemsFromPersonalCabinetData`) |
| `DistrLKAccessTasks.php`, `AddKaspiShop.php` | раздача задач агентам и форма подключения магазина |

Ниже только то, что **видно в этом коде**. Остальное помечено ⚠️. Своих
эндпоинтов не придумывай.

### Модель доступа

1. **Онбординг (один раз).** Владелец магазина вводит в nado свой телефон,
   получает от Kaspi SMS-код и вводит его в nado. Эта сессия владельца живёт
   несколько секунд. За это время nado:
   - получает токен официального API;
   - создаёт в кабинете **служебного сотрудника** с e-mail на домене платформы.

   После этого сессия владельца выбрасывается. Пароль владельца nado не видит
   никогда.
2. **Пароль сотрудника** генерирует Kaspi и присылает письмом на этот e-mail.
   Сервис приёма почты nado его сохраняет в зашифрованном виде.
3. **Рабочие входы** (импорт по расписанию) идут под служебным сотрудником:
   логин и пароль, затем код MFA, который тоже приходит на почту платформы.
4. **Отзыв доступа:** продавец удаляет сотрудника в кабинете Kaspi. Нужно
   показать это в UI.

В ProfitBot логины сотрудников — `pbsrv_*@service.pbot.kz`, и код входит только
под такими логинами (`str_starts_with($login, 'pbsrv_')`). Это защита от случайного
входа под чужим аккаунтом, её стоит сохранить. В nado адреса будут вида
`nado_<id>@<почтовый домен платформы>`, например `kaspi.nado.kz`.

### Онбординг: SMS-код владельцу (`lk_otp_checker.php` → `sendOTP`)

Заголовки браузера Firefox — как во входе ниже. `amp`-cookie генерируется так же,
счётчик `.0.0.0`.

1. `GET https://kaspi.kz/mc/` → 200.
2. `GET https://kaspi.kz/yml/ms/feat/p/ft/pre/e?d=true` (cookie `amp`) → 200.
3. `GET https://mc.shop.kaspi.kz/s/m` → 401, cookie ответа — `mc-session`.
4. `GET https://mc.shop.kaspi.kz/oauth2/authorization/1` (amp + mc-session) → 302.
   Cookie ответа — `mc_arr`, `location` — `redirectURL` (запомнить).
5. `GET {location}` (amp) → 302, cookie ответа — `MS_AUTH_SSO`.
6. `GET https://idmc.shop.kaspi.kz/login` (amp + MS_AUTH_SSO) → 200.
7. `POST https://idmc.shop.kaspi.kz/api/p/login` `{"_ph": "+7 (7XX) XXX-XX-XX"}` → 200.
   Kaspi отправляет SMS на телефон владельца. Формат телефона именно такой, с
   пробелами и скобками (`formatPhone`).

**Состояние между шагами** (нужно дождаться ввода кода): `{redirectUrl + "&continue",
ampCookie, mc_session_cookie, mc_arr_cookie, MS_AUTH_SSO_Cookie, phone}`. Хранится
зашифрованным, с TTL в несколько минут, и удаляется после использования.

### Онбординг: проверка кода и создание сотрудника (`verifyOTP`)

8. `POST https://idmc.shop.kaspi.kz/api/p/login` `{"_c": "<код из SMS>"}` (amp + MS_AUTH_SSO) → 200.
   Cookie ответа — новый `MS_AUTH_SSO`.
9. `GET {redirectUrl}&continue` (amp + новый MS_AUTH_SSO) → 302 → `GET {location}`
   (amp + mc-session + mc_arr) → 302. Cookie ответа — `mc-sid`, затем `GET {location}` → 200.
10. `GET https://mc.shop.kaspi.kz/s/m` (amp `.2.0.2` + mc-session + mc-sid) → 200,
    `{"merchants":[{"uid": ...}]}`. Если магазинов больше одного, продавец
    выбирает `uid` в UI. Сессию сохранить и продолжить через
    `verifyOTPwithMerchantID` с шага 11.
11. `GET https://mc.shop.kaspi.kz/user-assignments/api/v1/mc/users/registered?m={merchantUid}` → 200:
    список пользователей кабинета. Перед созданием сотрудника проверь, нет ли там
    уже нашего служебного e-mail: повторный онбординг не должен создавать дубль.
    ⚠️ формат ответа в образце не разбирается.
12. Токен официального API: GraphQL `getTokenApi`; если токена нет —
    mutation `tokenGenerate` (см. таблицу запросов ниже).
13. `POST https://mc.shop.kaspi.kz/user-assignments/api/v1/mc/users/add-email`:

    ```json
    {"name": "nado", "email": "nado_123@kaspi.nado.kz", "cityId": null,
     "roles": ["MANAGE_OFFERS"], "pointName": null, "contactPhone": null,
     "merchantUid": "30xxxxxx"}
    ```

    → 200, сотрудник создан, и Kaspi отправляет ему письмо с логином и паролем.

**Роли.** ProfitBot выдаёт широкий набор: `ACCEPT_ORDER_PICKUP`,
`COMPLETE_ORDER_PICKUP`, `ACCEPT_ORDER_DELIVERY`, `COMPLETE_ORDER_DELIVERY`,
`ACCEPT_KASPI_DELIVERY_ORDER`, `RETURN_ORDER`, `KASPI_DELIVERY_RETURN`,
`MANAGE_OFFERS`, `MANAGE_QUALITY_CONTROL`, `DOWNLOAD_ACTIVE_ARCHIVE_ORDERS`,
`KASPI_MARKETING`, `MANAGE_SETTINGS`. nado только читает каталог, остатки и
точки, поэтому **выдавай минимум**. Начни с `MANAGE_OFFERS` и проверь на тестовом
кабинете, что проходят `offer-view/list`, `getPointList` и вход. Недостающую роль
добавляй по одной. ⚠️ точный минимальный набор не проверен. Широкие роли (заказы,
возвраты, настройки) дают платформе доступ, которого продавец не ждёт.

### Приём почты (`lk_otp_email_checker.php`)

В ProfitBot все служебные адреса попадают в один ящик (catch-all
`service@service.pbot.kz` на своём почтовом сервере). Скрипт раз в секунду читает
его по IMAP:
- берёт непрочитанные (`UNSEEN`) письма, тело декодирует из base64;
- **код MFA**: если в тексте есть `Ваш код подтверждения: `, следующие 6
  символов — это код. Сохраняется пара (адрес `To`, код);
- **письмо с доступом нового сотрудника**: тело делится по `<br>`, из строк
  `Логин: …` и `Пароль: …` берутся учётные данные. Они принимаются, только если
  логин совпадает с ожидаемым адресом из незавершённого онбординга;
- письмо помечается прочитанным.

Вход сотрудника (`getEmailOtpCode`) ждёт код до 120 с, опрашивая раз в 5 с, и
берёт последний неиспользованный код для своего адреса, помечая его
использованным.

**В nado:**
- пакет `internal/integration/mailbox`: IMAP-клиент на Go
  (`github.com/emersion/go-imap/v2`), горутина-опросчик под управлением
  `app` (graceful shutdown);
- коды пишутся в таблицу `kaspi_mfa_codes (email, code_hash, received_at,
  used_at)` с TTL 10 минут. Пароль сотрудника пишется сразу в
  `provider_credentials` (шифрование `secrets`);
- проверяется отправитель письма: принимаются только письма от домена Kaspi
  (⚠️ точный адрес отправителя уточнить на реальном письме) с корректным
  DKIM, если почтовый сервер его проверяет. Иначе любой, кто знает наш адрес,
  подсунет «код» или «пароль»;
- TLS к IMAP с проверкой сертификата. У ProfitBot `novalidate-cert` на localhost —
  для внешнего сервера так нельзя. Учётные данные ящика берутся из окружения
  (`KASPI_MAILBOX_*`), а не из кода;
- почтовый домен (MX) и ящик — инфраструктура платформы, см. open-questions.

### Рабочий вход сотрудника (idmc → mc), по `getPersonalAccountCredentials`

Все запросы имитируют браузер Firefox: заголовки `User-Agent`, `Accept*`,
`Sec-Fetch-*`, `Origin`/`Referer` (`https://kaspi.kz/`, `https://idmc.shop.kaspi.kz/`).
Cookie из ответа берётся из `set-cookie` (до первого `;`).

1. `GET https://idmc.shop.kaspi.kz/login` → 200.
2. `POST https://idmc.shop.kaspi.kz/api/p/login`
   `{"_u": login, "_p": password, "_r_d": false}` (JSON).
   - 401 с `errorCode` `MFA_SEND_FLOOD` / `MFA_CODE_TOO_MANY_SEND` или
     `errorData.breakTimeSeconds` — флуд-контроль: ждать `breakTimeSeconds` плюс
     10–20 с (по умолчанию 60) и повторить один раз.
   - 200 **без** `redirectUrl` — нужен код MFA из почты.
3. Код MFA: `POST /api/p/login` `{"_u": login, "_m_c": code, "_r_d": true}` с
   cookie шага 2, в ответе `redirectUrl`. Cookie ответа — `MS_AUTH_SSO`, он нужен
   на шаге 8.
4. `GET https://idmc.shop.kaspi.kz{redirectUrl}` → 302 → `GET {location}` → 200.
5. Клиент генерирует cookie аналитики
   `amp_6e9c16=<10 симв.>-<11 симв.>...<9 симв.>.<те же 9 симв.>` и дописывает
   к нему счётчик `.0.0.0`, `.1.0.1`, `.2.0.2`, `.3.0.3`.
   `GET https://kaspi.kz/yml/ms/feat/p/ft/pre/e?d=true` → 200.
6. `GET https://mc.shop.kaspi.kz/s/m` дважды. Оба раза 401, cookie ответа — `mc-session`.
7. `GET https://mc.shop.kaspi.kz/oauth2/authorization/1` → 302, cookie ответа сохранить.
8. `GET {location}` с cookie `MS_AUTH_SSO` → 302 → `GET {location}` с cookie
   шагов 6–7 → 302, cookie ответа — `mc-sid`.
9. `GET https://mc.shop.kaspi.kz/s/m` → 200, `{"merchants":[{"uid": ...}]}`.
10. Проверочный запрос `GET /bff/offer-view/list?...`.

**Сессия** = `{merchantId, ampCookie, mc_session_cookie, mc_sid_cookie}`. Она
валидна, только если вход дошёл до шага 9 (`step == 13` в образце), и
переиспользуется между задачами. При 401 выполняется **один** повторный вход и
повтор запроса. Повторный 401 — ошибка.

**Блокировка:** не больше одного входа на подключение одновременно. В образце
это lock на 180 с в Redis. Параллельные входы вызывают `MFA_SEND_FLOOD`. В nado —
`dedup_key` задачи `kaspi_login:<conn>` или `sp_getapplock`.

### Запросы кабинета

Общие заголовки: `Cookie: {amp}.{счётчик}; {mc-session}; {mc-sid}`,
`X-Auth-Version: 3`, `Origin: https://kaspi.kz`, `Referer: https://kaspi.kz/`,
`Content-Type: application/json`.

| Назначение | Запрос | Ответ |
|---|---|---|
| Список товаров | `GET https://mc.shop.kaspi.kz/bff/offer-view/list?m={merchantUid}&p={page}&l={size}&a={true\|false}[&t={поиск}]` | `{total, data:[…]}`, поля — ниже. `a=true` — активные, `a=false` — снятые с продажи. В образце `l=25`, обход до пустой страницы |
| Точки / склады | `POST /mc/facade/graphql?opName=getPointList`, `merchant(id){points{id enabled city{name} address{streetName streetNumber building phone name} type schedule{…}}}` | `points[].id` = `{merchantUid}_{код точки}` (например `…_PP1`) |
| API-токен | `POST /mc/facade/graphql?opName=getTokenApi` → `data.merchant.integration.token`; нет токена — mutation `tokenGenerate(merchantId)` → `data.tokenGenerate` | токен официального Shop API |
| Пользователи кабинета | `GET /user-assignments/api/v1/mc/users/registered?m=` / `POST …/users/add-email` | онбординг, см. выше |
| Заказы | GraphQL `getOrders` / `getOrderDetails` | в nado не нужны |
| Запись цены / XML-прайса | `POST /pricefeed/upload/merchant/process`, `…/upload?merchantUid=` | запись в Kaspi — nado не делает (D-05) |

**Элемент `offer-view/list` → `data[]`** (поля, которые читает
`KaspiImport::updateShopItemsFromPersonalCabinetData`):

| Поле | Смысл |
|---|---|
| `sku` | артикул продавца (merchant product code) |
| `masterSku` | код мастер-товара Kaspi (карточка каталога, общая для всех продавцов) |
| `masterTitle` | название мастер-товара |
| `model` / `title` | название у продавца. Приоритет: `model` → `title` → `masterTitle` |
| `brand` | бренд (может отсутствовать) |
| `images` | массив строк, `images[0]` — главное фото. ⚠️ полный URL или путь — проверить на реальном ответе |
| `available` | bool — товар в продаже |
| `price` | цена, ₸. Если `price == 0` и `minPrice > 0`, образец берёт `minPrice` (цены по городам) |
| `minPrice` | минимальная цена |
| `masterCategory` | код категории Kaspi (ProfitBot сопоставляет с таблицей комиссий; префикс `Master - ` отрезается) |
| `availabilities[]` | `{storeId: "<merchant>_<код точки>", available: "yes"/"no", preOrder, stockSpecified, stockCount}` |
| `stocks[]` | `{stockLevel: {"<merchant>_<код точки>": {value}}}` — фактический остаток, если `stockSpecified` |

Остаток точки: если `available == "yes"` и `stockSpecified`, то
`stocks[].stockLevel[storeId].value`. Если остаток не ведётся (`stockSpecified =
false`), ProfitBot подставляет условные 5 штук. В nado сохраняй «в наличии без
количества», а трактовку оставь настройке магазина.

**Чего в ответе нет:** описания и характеристик. Kaspi-карточка — общий
мастер-товар, его контент живёт на публичной странице
`https://kaspi.kz/shop/p/-{masterSku}/`. В образце есть только публичный запрос
предложений продавцов `POST https://kaspi.kz/yml/offer-view/offers/{masterSku}`
`{"cityId":"750000000","id":…,"limit":5,"page":0,"sortOption":"PRICE"}`, он отдаёт
цены конкурентов, не контент. Варианты получить описание:
1. склейка по штрихкоду или артикулу с карточкой WB или Ozon того же продавца;
2. ручное заполнение в nado;
3. разбор публичной страницы товара — ⚠️ кода нет, это парсинг чужого сайта, только
   с согласия пользователя.

### Что не повторять из образца

Образец рабочий, но небезопасный. В nado:
- **не отключать проверку TLS** (`CURLOPT_SSL_VERIFYPEER=false`, IMAP
  `novalidate-cert`);
- **не хранить пароли открытым текстом** (`LKPassword`, пароль сотрудника в
  `UserKaspiShopsOTPRegistration.Password`) — шифровать через `secrets`;
- **не держать учётные данные в коде** (пароль IMAP в `lk_otp_email_checker.php`) —
  только окружение;
- **не передавать учётные данные** через HTTP-API между сервером и агентами и не
  класть токены в query-строку. В nado всё выполняет наш воркер очереди;
- **не печатать ответы кабинета** (`print_r($data)`, лог цен) в лог;
- **не выдавать сотруднику широкие роли** — только нужные для чтения;
- не собирать телефоны покупателей из заказов Kaspi.

### Реализация в nado

- Пакет `internal/integration/marketplace/kaspi/cabinet` реализует `Connector`:
  - `Verify` — вход и `merchants`;
  - `Products` / `Offers` — обход `offer-view/list` для `a=true` и `a=false`;
  - точки — `getPointList`.
- Онбординг — отдельный сервис `kaspi_onboarding`: состояния
  `phone_sent → code_verified → merchant_selected → employee_created →
  password_received → first_login_ok`, каждое с таймаутом и понятной ошибкой в UI.
- HTTP-клиент без `cookiejar`, cookie выставляются явно по шагам, как в образце
  (порядок и состав важны). Редиректы **не** следовать автоматически
  (`CheckRedirect` возвращает `http.ErrUseLastResponse`): коды 302 и `location`
  нужны явно. `Accept-Encoding` вручную не ставить.
- Темп консервативный: последовательные запросы, пауза 300–500 мс между
  страницами. Каталог раз в сутки, остатки раз в 15–60 минут по тарифу (в образце
  раз в 15 минут).
- Смена формата (HTML вместо JSON, пропали `data`/`total`) → `sync_runs.status =
  'source_changed'`, товары не удаляются.
- Полнота обхода: число sku должно совпасть с `total` для `a=true` и `a=false`,
  иначе пропавшие товары **не** помечаются удалёнными (так же в образце).
- Путь 1 (файл) остаётся запасным.

## Маппинг: итог

| Kaspi | Наша модель |
|---|---|
| `SKU` / `offer@sku` / `data[].sku` | `ExternalID`, `VendorCode`, `Variants[0].ExternalID/SKU` (один вариант на товар) |
| `data[].masterSku` | `GroupID` и `Raw`; ссылка на карточку Kaspi. Товары разных продавцов с одним `masterSku` — один и тот же товар каталога |
| `model` / `title` / `masterTitle` | `Title` (в этом порядке) |
| `brand` | `Brand` |
| `images[]` | `Images` → задача `media.fetch` |
| `masterCategory` | `Category.ID` |
| `price` (или `minPrice` при `price == 0`) / `cityprice` | `variant_marketplace_prices` (KZT) → правило цены магазина (D-14) |
| `availabilities` + `stocks` / `PP*` | `StockLevel{WarehouseID: код точки, Qty, Fulfillment: "fbs"}` |
| `available`, `a=true/false` | активность; снятые с продажи не показываются на витрине по умолчанию |
| объект целиком | `Raw` |

## Что не проверено

- ⚠️ Срок жизни и лимиты `X-Auth-Token`; вебхуков Kaspi нет.
- ⚠️ Точный `schemaLocation` XSD прайс-листа.
- ⚠️ Формат `images` (URL или путь) и ответа `users/registered`.
- ⚠️ Минимальный набор ролей служебного сотрудника.
- ⚠️ Адрес отправителя писем Kaspi (для проверки подлинности) и точные тексты писем:
  образец ищет `Ваш код подтверждения: `, `Логин: `, `Пароль: `. Kaspi может
  поменять шаблон — разбор покрыть тестами на сохранённых письмах.
- ⚠️ Срок жизни сессии кабинета: в образце её переиспользуют до первого 401.
