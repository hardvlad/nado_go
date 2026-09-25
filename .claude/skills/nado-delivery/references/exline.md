# Exline: курьерская служба (КЗ)

Код провайдера: `exline`. Курьерская доставка по Казахстану, от двери до двери.

Документация: https://exline.kz/docs/. Разделы:
- API заявок: `/docs/orders`;
- API накладных: `/docs/waybills`;
- API отслеживания: `/docs/tracking`;
- API населённых пунктов: `/docs/regions`;
- API предзаявок: `/docs/preorder`.

⚠️ **Страницы документации при проверке отдавали 404 для автоматической загрузки,** а хосты API не резолвились из нашей сети. Факты ниже взяты из выдачи поиска по этим страницам. **Перед реализацией откройте документацию в браузере и сверьте каждый путь.** Если у продавца есть договор с Exline, запросите у менеджера актуальную спецификацию.

## Авторизация

- Для всех запросов (GET, POST, PATCH, DELETE) передаются параметры `client_id` и `auth_token`. При неверных данных приходит 401 с `{"errors": "wrong credentials"}` (поиск по [exline.kz/docs](https://exline.kz/docs/)).
- API предзаявок авторизуется параметром `secret`, а не парой client_id/auth_token ([preorder](https://exline.kz/docs/preorder)).
- **Где продавец берёт ключи:** личный кабинет Exline или менеджер по договору ⚠️.
- Credentials: `{client_id, auth_token, secret?}`, зашифрованы.
- Ключи в параметрах запроса могут попасть в логи прокси. Поэтому в нашем HTTP-клиенте маскируйте query при логировании.
- Тестовой среды не нашёл ⚠️.

## Хосты

| Хост | Что там |
|---|---|
| `https://api.exline.systems/public/v1/…` | публичный расчёт и справочники ([поиск](https://api.exline.systems/docs)) |
| `https://cabinet-api.exline.kz/api/client/v1/…` | клиентское API: накладные, заявки ([waybills](https://exline.kz/docs/waybills)) |

## Эндпоинты

| Назначение | Путь | Метод Provider |
|---|---|---|
| Населённые пункты: откуда / куда | раздел «API населённых пунктов», пути ⚠️ (вероятно `…/public/v1/regions/origin`, `…/regions/destination`) | резолв адреса |
| Расчёт стоимости | `GET https://api.exline.systems/public/v1/calculate?origin_id=&destination_id=&weight=&service=` | `Quote` |
| Накладная: чтение | `GET https://cabinet-api.exline.kz/api/client/v1/waybills/{id}` | `Track` / сверка |
| Накладная: создание | раздел «API накладных», путь ⚠️ (вероятно `POST …/waybills`) | `CreateShipment` |
| Заявка на забор | раздел «API заявок» ⚠️ | вызов курьера (v2) |
| Предзаявка | раздел «API предзаявок» (`secret`) ⚠️ | альтернатива созданию накладной |
| Отслеживание | раздел «API отслеживания» ⚠️ | `Track` |
| Отмена | не найдено ⚠️ | `CancelShipment` → `ErrNotSupported`, если метода нет |
| Печать накладной/ярлыка | не найдено ⚠️ | `Label` |

### Расчёт (`Quote`)

- **Параметры:**
  - `origin_id` — ID города отправления;
  - `destination_id` — ID города доставки;
  - `weight` — **кг**, float или int;
  - `service` ∈ `standard`, `express_mail`, `express_parcels`.
- **Ответ по одному сервису:** `price`, `fuel_surplus` (топливная надбавка, прибавляйте к цене), `human_range` (диапазон дат текстом), `min`/`max` (рабочие дни).
- Без `service` ответ содержит расчёт и по standard, и по express ([поиск](https://api.exline.systems/docs)).
- Габариты и объявленная ценность в параметрах расчёта не упоминаются ⚠️: возможно, объёмный вес не учитывается.

### Накладная (`CreateShipment`)

Поля из ответа `GET waybills/{id}` ([waybills](https://exline.kz/docs/waybills)): `id`, `name`, `customer`, `sender`, `sender_contact_person`, `sender_phone`, `sender_address`, `recipient`, `recipient_phone`, `recipient_address`, `quantity`, `service`, `payment_method`, `declared_value` (строка, например `"20000.00"`), `note`.

Поля создания, вероятно, совпадают ⚠️. Наложенный платёж: поле не найдено ⚠️. Уточнить, поддерживает ли Exline COD для интернет-магазинов.

## Отслеживание и статусы

- API отслеживания отдаёт итоговую стоимость доставки в KZT и оплаченную сумму ([tracking](https://exline.kz/docs/tracking)).
- Найденные коды статусов: `registration_system` (зарегистрировано в системе), `successful_delivery` (успешно доставлено). Полный список ⚠️.
- Вебхуков не нашёл ⚠️. Работаем фоновым опросом раз в 1–2 часа.

| Exline | Наш |
|---|---|
| `registration_system` | `created` |
| забор курьером / приём на склад ⚠️ | `accepted` |
| промежуточные статусы ⚠️ | `in_transit` |
| `successful_delivery` | `delivered` |
| возврат ⚠️ | `returned` |
| отмена ⚠️ | `canceled` |

ПВЗ нет, это чистая курьерская служба: `Capabilities.PickupPoints=false`, `Courier=true`.

## Локации

- Города адресуются внутренними ID Exline (`origin_id`, `destination_id`) из API населённых пунктов.
- Храните справочник в БД (`carrier_locations`: provider, external_id, name, region, kind=origin|destination), обновляйте раз в сутки и связывайте с нашим городом по названию и региону.
- Направления «откуда» и «куда» — разные списки, так как отправка возможна не из всех городов.

## Страны и деньги

- Только KZ (`Capabilities.Countries=["KZ"]`).
- Суммы в KZT decimal (строка `"20000.00"`), вес в кг (float). Конвертируйте из наших minor units и граммов явно.

## Главные непроверенные пункты

Прежде чем писать код, закрыть:
1. Точные пути создания накладной, трекинга и справочников городов.
2. Поддержка отмены и печати ярлыка.
3. Поддержка наложенного платежа.
4. Полный список статусов.
5. Способ передачи `client_id` и `auth_token`: query или тело. Для POST это важно.
