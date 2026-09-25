# Казпочта (Kazpost / QazPost): «Модуль доставки» (SOAP) + трекинг (REST), КЗ

Код провайдера: `kazpost`.

Два сервиса:
1. **«Модуль доставки» API** (SOAP): тарифы, генерация трек-номера (ШПИ), адресный ярлык, ф.103, отмена ШПИ, данные по наложенному платежу, поиск отделений. Описание: https://rates.kazpost.kz/.
2. **Tracking API** (REST/JSON): статус и маршрут по ШПИ, подписки на уведомления, справочники отделений и статусов. Описание: https://track.kazpost.kz/api/.

Подключение для интернет-магазинов: [sk.kz](https://sk.kz/press-center/news/57251/?lang=ru), [inform.kz](https://www.inform.kz/ru/kazpochta-razrabotala-api-modul-dlya-internet-magazinov_a2796401).

## Доступ и авторизация

- **SOAP-методы с данными клиента** (ШПИ, ярлык) требуют элемент `<Key>` — «идентификационный ключ клиента (32 символа)». Он выдаётся после регистрации на post.kz и заполнения профиля организации ([getParcelBarcode](https://rates.kazpost.kz/api/getParcelBarcode/prod/)). Вопросы по доступу: `postcode@kazpost.kz`.
- **Расчёт тарифа** (`GetPostRate`) ключа не требует. Опционален `Contract` — номер договора, например `1224/АК` ([postrate](https://rates.kazpost.kz/api/postrate/prod/)).
- **Tracking API:** публичный `GET`. Доступ к подпискам и расширенным методам: `api@kazpost.kz` ([track API](https://track.kazpost.kz/api/)).
- Credentials: `{key, contract?, sender_bin, sender_* (адрес отправки по умолчанию)}`.
- **Окружения:** для каждого сервиса есть пары `/api/<service>/test/` и `/api/<service>/prod/` ([rates.kazpost.kz](https://rates.kazpost.kz/)).
  - WSDL prod: `http://rates.kazpost.kz/postratesprod/postratesws.wsdl`.
  - WSDL test: по аналогии `…/postratestest/…` ⚠️.
  - В WSDL указан **http**. Проверьте, работает ли https, и используйте его, если работает ⚠️.

## Go-подход к SOAP

- Никакого codegen и сторонних SOAP-библиотек. Пишите вручную структуры `encoding/xml` для envelope/body и нужных операций, отправляйте `http.Post` с `Content-Type: text/xml; charset=utf-8` и заголовком `SOAPAction` ⚠️ (значение взять из WSDL).
- Namespace-префиксы из примеров: запрос `pos:` (`<pos:Key>`), ответ `ns2:` (`<ns2:Barcode>`). В структурах указывайте полный namespace URI из WSDL, а не префикс.
- Скачайте WSDL один раз в `internal/integration/delivery/kazpost/testdata/` и пишите golden-тесты на маршалинг и анмаршалинг.

## Операции «Модуля доставки»

| Назначение | Операция / сервис | Метод Provider |
|---|---|---|
| Тариф | `GetPostRate` (`/api/postrate/prod/`) | `Quote` |
| Трек-номер (ШПИ) | `GetParcelBarcode` (`/api/getParcelBarcode/prod/`) | `CreateShipment` |
| Адресный ярлык (PDF A5, base64) + ШПИ | `GetAddrLetter` (`/api/addrletter/prod/`) | `Label` (и может заменить `CreateShipment`) |
| Данные по ярлыку | `/api/barcodeinfo/prod/` | сверка |
| Ф.103 (реестр) | сервис «Электронная форма 103» ⚠️ | документ партии |
| Отмена ШПИ | сервис «Отмена ШПИ» ⚠️ | `CancelShipment` |
| Наложенный платёж: передача/получение реквизитов | сервисы COD ⚠️ | настройки COD |
| Поиск города/района/отделения | сервис поиска по названию ⚠️ | резолв адреса / `PickupPoints` |

### `GetPostRate`

Источник: [postrate](https://rates.kazpost.kz/api/postrate/prod/).

- **Запрос `GetPostRateInfo`:**
  - `SndrCtg` — категория отправителя, обязательно;
  - `Contract`;
  - `Product` — код продукта, обязательно;
  - `MailCat` — категория РПО, обязательно;
  - `SendMethod` — способ пересылки, обязательно;
  - `Weight` — **граммы**, обязательно;
  - `Dimension` — S/M/L;
  - `Value` — объявленная ценность, обязательно при `MailCat`=2 или 4;
  - `From` — индекс, обязательно;
  - `To` — индекс;
  - `ToCountry`, `PostMark`.
- **Ответ:** `Sum` (в тенге), `ResponseCode`, `ResponseText`, `ResponseGenTime`.
- Справочники кодов `Product`, `MailCat`, `SendMethod`, `SndrCtg` опубликованы на страницах сервиса ⚠️. Вынесите их в константы с комментариями. Типичный набор для интернет-магазина — посылка с объявленной ценностью или с наложенным платежом ⚠️.

### `GetParcelBarcode` / `GetAddrLetter`

Источники: [ШПИ](https://rates.kazpost.kz/api/getParcelBarcode/prod/), [ярлык](https://rates.kazpost.kz/api/addrletter/prod/).

- `Key` — 32 символа.
- **Получатель:** `RcpnName` (≤256), `RcpnPhone` (формат `7XXXXXXXXX`, 10 цифр), `RcpnEmail`, `RcpnIIN` (12 цифр, опционально), `RcpnCountry`, `RcpnIndex` (6 цифр), `RcpnCity`, `RcpnDistrict`, `RcpnStreet`, `RcpnHouse`.
- **Отправитель:** `SndrBIN` (12 цифр, для нерезидентов `000000000000`), `SndrName`, `SndrPhone`, `SndrEmail`, `SndrCountry`, `SndrIndex`, `SndrCity`, `SndrDistrict`, `SndrStreet`, `SndrHouse`.
- **Отправление:** `Weight`, `DeclaredValue`, `CashOnDelivery` (формат `xxxxxx.xx`, обязателен при `MailCtg=4`), `DeliverySum`, `ProductCode` (4 символа), `Marks`, `SendMethod`, `MailCtg`, `OrderNum` (наш ID), `MailCount`, `Pickup`.
- **Ответ:** `Barcode` (ШПИ) или `Barcodes[]`; у ярлыка ещё `AddrLetPdf` — base64 PDF формата A5. `ResponseCode=0` означает успех.
- ⚠️ **Единицы веса расходятся:** в `GetPostRate` вес в граммах, в описании `GetAddrLetter` — «в килограммах (4 цифры)». Сверьте на тестовом стенде и зафиксируйте тестом.
- **Идемпотентность:** повторный вызов генерирует **новый** ШПИ ⚠️. Сохраняйте ШПИ в той же транзакции, в которой меняется статус отправления, и не повторяйте вызов после таймаута, пока не проверите результат через `barcodeinfo` по `OrderNum` ⚠️. Если это невозможно, покажите продавцу кнопку «перевыпустить», а старый ШПИ отмените.

## Tracking API

Источник: [track.kazpost.kz/api](https://track.kazpost.kz/api/).

- `GET https://track.post.kz/api/v2/{ШПИ}`, пример: `https://track.post.kz/api/v2/RK070333447CN`. Отдаёт информацию об отправлении и историю маршрута.
- Ответ **кэшируется на стороне Казпочты на 1 час**. Опрашивать чаще нет смысла: ставьте фоновый опрос раз в 1–2 часа.
- Есть подписка на уведомления через WebHook, Push, email, SMS. Условия подключения — через `api@kazpost.kz` ⚠️. В MVP `Capabilities.Webhooks=false`.
- Справочники отделений и статусов: разделы «Справочники: Отделения, Статусы» на странице API ⚠️. Отделения используйте как `PickupPoints` для получения «до востребования».

## Статусы → наши

Коды статусов нужно взять из справочника статусов Tracking API ⚠️. До его загрузки маппинг ведём по смыслу:

| Казпочта (смысл) | Наш |
|---|---|
| ШПИ сгенерирован, отправление не принято | `created` |
| Приём в отделении | `accepted` |
| Сортировка, отправка, прибытие в промежуточные пункты, таможня | `in_transit` |
| Прибыло в отделение вручения / в постамат | `ready_for_pickup` |
| Вручено адресату | `delivered` |
| Возврат отправителю / вручено отправителю | `returned` |
| ШПИ отменён | `canceled` |

Держите таблицу маппинга в коде провайдера. Неизвестный код → лог warning и `raw_status` без смены нашего статуса.

## Локации и страны

- Адрес строится на **почтовом индексе** КЗ (6 цифр) плюс город, район, улица, дом.
- KATO не используется.
- Для подсказок по городу и отделению — сервис поиска по названию ⚠️.
- Страны: KZ, плюс международные отправления через `ToCountry` (в MVP не делаем).

## Подводные камни

- Телефон строго `7XXXXXXXXX`, то есть 10 цифр без `+` и без ведущей 8.
- `SndrBIN` обязателен. Для ИП это ИИН ⚠️. Берите из реквизитов магазина.
- Суммы — decimal в тенге с 2 знаками. Конвертируйте из minor units (тиынов) явно.
- WSDL на http: проверьте поддержку TLS, иначе ключ клиента уходит открытым текстом. Если https не работает, зафиксируйте это как риск в настройках интеграции.
