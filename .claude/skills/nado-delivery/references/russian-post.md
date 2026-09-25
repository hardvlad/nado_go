# Почта России: API «Отправка» и API трекинга (РФ)

Код провайдера: `russian_post`. Нужны два разных сервиса с разной авторизацией:

1. **«Отправка»** (REST/JSON): тарифы, заказы, партии, документы, ПВЗ. Спецификация: https://otpravka.pochta.ru/specification.
2. **Трекинг** (SOAP): история операций по ШПИ. Спецификация: https://tracking.pochta.ru/specification.

Официальная спецификация «Отправки» отдаётся SPA-страницей и плохо читается автоматически. Пути ниже сверены с SDK [lapaygroup/RussianPost](https://github.com/lapaygroup/RussianPost). Методы HTTP частично ⚠️: сверить с разделами спецификации (`orders-creating_order`, `batches-create_batch_from_N_orders`, `documents-create_forms`, `nogroup-rate_calculate`).

## Авторизация «Отправки»

- Базовый URL: `https://otpravka-api.pochta.ru` ([lapaygroup](https://github.com/lapaygroup/RussianPost)).
- Каждый запрос несёт **два** заголовка:
  - `Authorization: AccessToken <токен приложения>`;
  - `X-User-Authorization: Basic base64(<логин>:<пароль>)` — логин и пароль ЛК otpravka.pochta.ru.
- **Где продавец берёт токен:** ЛК otpravka.pochta.ru → Настройки → «API» ⚠️. Нужен договор с Почтой, API доступен юрлицам и ИП с договором.
- Credentials: `{access_token, login, password}`, все поля зашифрованы. Пароль ЛК хранится у нас: это требование API. Сообщите продавцу, что лучше завести отдельного пользователя ЛК для интеграции ⚠️ (если ЛК это позволяет).
- **Тестового стенда нет** ⚠️. Проверять можно на реальном договоре: создание заказа в «backlog» бесплатно, его можно удалить до формирования партии.

## Эндпоинты «Отправки»

| Назначение | Путь | Метод Provider |
|---|---|---|
| Расчёт тарифа | `POST /1.0/tariff` | `Quote` |
| Нормализация адреса | `POST /1.0/clean/address` | резолв адреса |
| Нормализация ФИО / телефона ⚠️ | `POST /1.0/clean/physical`, `/1.0/clean/phone` | валидация |
| Создать заказы (в «Новые») | `PUT /1.0/user/backlog` (массив) ⚠️ | `CreateShipment` |
| Удалить заказы из «Новых» | `DELETE /1.0/backlog` (массив id) ⚠️ | `CancelShipment` |
| Сформировать партию из заказов | `POST /1.0/user/shipment?sending-date=…` ⚠️ | после создания заказа |
| Документы партии / заказа | `GET /1.0/forms/{id}/forms` (zip/pdf), `…/f7pdf` ⚠️ | `Label` |
| ПВЗ ЕКОМ (пункты выдачи) | `GET /1.0/delivery-point/findAll` ⚠️ | `PickupPoints` |
| Отделение по индексу | `GET /postoffice/1.0/{index}` | `PickupPoints` (отделения) |
| Точки сдачи продавца | «settings-shipping_points» в спецификации | онбординг |
| Счётчик запросов | «nogroup-count_request_api» в спецификации | мониторинг лимита |

### Тариф (`Quote`)

`POST /1.0/tariff`. Поля ⚠️:
- `index-from`, `index-to` (6 цифр);
- `mail-category`: `ORDINARY`, `WITH_DECLARED_VALUE`, `WITH_DECLARED_VALUE_AND_CASH_ON_DELIVERY` и т.д.;
- `mail-type`: `POSTAL_PARCEL`, `ONLINE_PARCEL`, `EMS`, `ECOM`…;
- `mass` (г), `dimension{length,width,height}` (см), `declared-value` (копейки);
- `fragile`, `courier`.

Ответ: `total-rate`, `total-vat` (копейки), `delivery-time{min-days,max-days}`.

Альтернатива без авторизации — публичный калькулятор `tariff.pochta.ru` ([lapaygroup](https://github.com/lapaygroup/RussianPost)). Годится для витрины до подключения договора, но цены там по публичному прайсу, а не по договору.

### Заказ (`CreateShipment`)

`PUT /1.0/user/backlog`, массив заказов ⚠️. Ключевые поля:
- `order-num` — наш ID, он же идемпотентность;
- `mail-type`, `mail-category`, `mass` (г), `dimension`;
- адрес получателя в нормализованном виде: `index-to`, `region-to`, `place-to`, `street-to`, `house-to`, `room-to`. Предварительно прогоняйте адрес через `clean/address` и отклоняйте результат с `quality-code` хуже «GOOD»/«POSTAL_BOX» ⚠️;
- `recipient-name`, `given-name`, `surname`, `tel-address`;
- `insr-value` (объявленная ценность, копейки), `payment` (наложенный платёж, копейки);
- для ЕКОМ-ПВЗ — `ecom-data{delivery-point-index, services}` ⚠️.

В ответе приходят `result-ids[]` и `errors[]` поэлементно: часть заказов может создаться, часть нет. ШПИ (`barcode`) доступен после создания заказа или после включения в партию ⚠️.

**Партии.** Почта работает партиями. Заказ из «Новых» надо включить в партию на дату сдачи (`/1.0/user/shipment`). После этого его нельзя удалить через backlog. Практика: создавать заказ в backlog при подтверждении отправки, а партию формировать кнопкой «Сдать отправления» у продавца или по расписанию раз в день.

### Документы (`Label`)

`GET /1.0/forms/{id}/forms` отдаёт пакет документов: ярлык, ф.103, ф.7 и другие; есть отдельные варианты f7pdf/f22 ⚠️. Формат — zip или pdf ⚠️. Если пришёл zip, распакуйте его и отдайте PDF ярлыка.

### ПВЗ

- Точки ЕКОМ (Почта + партнёрские ПВЗ): `GET /1.0/delivery-point/findAll` ⚠️. Кэшируйте в `carrier_pickup_points` и обновляйте раз в сутки.
- Обычные отделения: `GET /postoffice/1.0/{index}` отдаёт отделение по индексу, для «до востребования».

## Трекинг (`Track`)

- SOAP, отдельные учётные данные (логин и пароль сервиса трекинга из ЛК tracking.pochta.ru) ([спецификация](https://tracking.pochta.ru/specification)).
- **Единичный доступ:** операция `getOperationHistory`, один ШПИ за запрос. Без договора лимит **100 запросов в сутки**; с договором на отправку — без лимита ([поиск, tracking.pochta.ru](https://tracking.pochta.ru/)).
- **Пакетный доступ:** `getTicket` принимает до **3000** ШПИ, результат забирается позже через `getResponseByTicket`. Только для клиентов с договором.
- **Go-подход:** без codegen. Пишите `encoding/xml`-структуры для envelope и трёх операций вручную, POST с `Content-Type: application/soap+xml` (SOAP 1.2) ⚠️.
- Результат — список операций: `OperType{Id,Name}`, `OperAttr{Id,Name}`, `OperDate`, `Index`. Статус определяется парой (тип, атрибут).
- Вебхуков нет, работаем фоновым опросом: пакет раз в 2–4 часа по активным ШПИ.

## Операции трекинга → наши

Коды типов операций сверить с [tracking.pochta.ru/specification](https://tracking.pochta.ru/specification) ⚠️:

| OperType | Наш |
|---|---|
| 1 Приём | `accepted` |
| 8 Обработка, 6 Вручение-отложено?, 9 Импорт, 10 Экспорт, 14 Таможенное оформление | `in_transit` |
| 8 + атрибут «Прибыло в место вручения» | `ready_for_pickup` |
| 2 Вручение (атрибуты: адресату, отправителю при возврате) | `delivered` (адресату) / `returned` (отправителю) |
| 3 Возврат | `returned` |
| 5 Невручение, 12 Неудачная попытка вручения | событие без смены статуса |
| 4 Досылка | `in_transit` |

До первой операции «Приём» заказ живёт только в «Отправке» и имеет статус `created`. Удаление из backlog даёт `canceled`.

## Локации и страны

- Всё строится на **почтовом индексе** (6 цифр) плюс нормализованный адрес. FIAS и KLADR не нужны.
- `Capabilities.Countries=["RU"]`. Международные отправления есть, но в MVP их не делаем.

## Лимиты и подводные камни

- Суточные лимиты «Отправки» есть. Для увеличения пишут на `support.parcel@russianpost.ru` ([lapaygroup](https://github.com/lapaygroup/RussianPost)). Текущий расход смотрите методом «count_request_api». Лимит общий на аккаунт продавца, а не на платформу.
- Суммы в **копейках**, вес в **граммах**.
- Ответы с поэлементными `errors[]` при HTTP 200: обязательно проверяйте каждый элемент.
- Нормализация адреса обязательна. Иначе Почта создаст заказ, но отправление потеряется на сортировке.
- Трекинг и «Отправка» — это разные учётки. В форме подключения продавца нужны обе.
