# Вход покупателя по телефону: OTP через WhatsApp и Telegram

Решение D-17: гостевого заказа нет, покупатель входит по номеру телефона. Код отправляют официальные каналы:
WhatsApp Business Platform (Cloud API) и Telegram Gateway API. Отправитель один на все магазины — платформа nado.
SMS — только возможный запасной канал. Сведения собраны 2026-09-25. «⚠️ не проверено» — перепроверить до кода.

## 1. WhatsApp Business Platform (Cloud API, Meta)

**Подключение (один раз на платформу):**
- Business portfolio в Meta Business Manager → WhatsApp Business Account (WABA) → приложение на
  developers.facebook.com с продуктом WhatsApp. Номер не должен быть занят в приложении WhatsApp и должен
  принимать SMS или звонок. Регистрация номера в Cloud API: `POST /{phone-number-id}/register` с PIN
  двухшаговой проверки ⚠️ не проверено. Display name «nado» проходит модерацию ⚠️ не проверено.
- **Верификация бизнеса обязательна на практике.** Без неё лимит — **250 уникальных получателей за скользящие
  24 ч** вне окна обслуживания. После верификации — 2 000, затем автоматический рост до 10K / 100K / Unlimited
  при хорошем качестве и использовании ≥50% лимита за 7 дней. Лимит общий на портфель. Текущий лимит — поле
  `whatsapp_business_manager_messaging_limit` (старое `messaging_limit_tier` устарело).
  https://developers.facebook.com/documentation/business-messaging/whatsapp/messaging-limits
- **Токен:** постоянный токен System User с правами `whatsapp_business_messaging` и
  `whatsapp_business_management` ⚠️ не проверено. Заголовок `Authorization: Bearer <token>`. Хранится
  зашифрованным, как секреты провайдеров (nado-go-conventions).

**Шаблон категории AUTHENTICATION.** Текст задаёт Meta: `<VERIFICATION_CODE> is your verification code.`
Meta переводит его на язык шаблона. Можно добавить строку безопасности (`add_security_recommendation`) и футер
«expires in N minutes» (`code_expiration_minutes`: 1–90). **URL, медиа и эмодзи запрещены.** Код — не длиннее
15 символов. Текст кнопки — до 25 символов. `message_send_ttl_seconds` задаёт, сколько пытаться доставить.
https://developers.facebook.com/docs/whatsapp/business-management-api/authentication-templates ·
https://developers.facebook.com/docs/whatsapp/business-management-api/authentication-templates/copy-code-button-authentication-templates

Кнопки `{"type":"OTP","otp_type":...}`:
- `COPY_CODE` — копирует код. Работает везде; это наш вариант для веба.
- `ONE_TAP` — автозаполнение в Android-приложении.
- `ZERO_TAP` — только Android. Нужны `supported_apps` (package_name + 11-символьный хеш подписи, до 5 штук),
  handshake перед отправкой и `zero_tap_terms_accepted: true`. Если что-то не так, WhatsApp показывает
  autofill или copy code. Пригодится гибридным приложениям магазинов.
  https://developers.facebook.com/docs/whatsapp/business-management-api/authentication-templates/zero-tap-authentication-templates

Создание шаблона: `POST /{waba-id}/message_templates`. Нужны языки `ru`, `kk`, `en`; поддержка `kk` в
authentication ⚠️ не проверено. Для одного языка вместо `languages` передают `language` ⚠️ не проверено.
```json
{"name":"nado_otp","languages":["ru","kk","en"],"category":"AUTHENTICATION","message_send_ttl_seconds":300,
 "components":[{"type":"BODY","add_security_recommendation":true},
  {"type":"FOOTER","code_expiration_minutes":5},
  {"type":"BUTTONS","buttons":[{"type":"OTP","otp_type":"COPY_CODE"}]}]}
```

**Отправка:** `POST https://graph.facebook.com/v{ver}/{phone-number-id}/messages`. Версия — в конфиге
(`WHATSAPP_GRAPH_VERSION`), актуальную взять из changelog. Код передаётся **дважды**: в body и в кнопке.
```json
{"messaging_product":"whatsapp","recipient_type":"individual","to":"77011234567","type":"template",
 "template":{"name":"nado_otp","language":{"code":"ru"},"components":[
   {"type":"body","parameters":[{"type":"text","text":"482913"}]},
   {"type":"button","sub_type":"url","index":"0","parameters":[{"type":"text","text":"482913"}]}]}}
```
Ответ содержит `messages[0].id` (wamid) — сохраняем в `provider_msg_id`. **Успешный ответ не значит, что у номера
есть WhatsApp.** Недоставку сообщает вебхук `failed` (обычно ошибка 131026 ⚠️ не проверено). Синхронной
проверки «есть ли WhatsApp» нет.

**Вебхуки:** `statuses[].status` = `sent` | `delivered` | `read` | `failed` (+ `errors[]`).
- Подписка: GET с `hub.mode=subscribe` и `hub.verify_token` — сверить токен и вернуть `hub.challenge`.
- Подлинность POST: `X-Hub-Signature-256: sha256=<hex>` = HMAC-SHA256(App Secret, сырое тело), сравнение
  через `hmac.Equal`. https://developers.facebook.com/docs/graph-api/webhooks/getting-started

**Пропускная способность:** 80 msg/s на номер. 1 000 msg/s — при Unlimited и 100K+ получателях в сутки.
Превышение → 130429. https://developers.facebook.com/documentation/business-messaging/whatsapp/throughput
Pair rate limit (много сообщений одному номеру) → 131056, порядка 1 сообщения в 6 с ⚠️ не проверено.

**Цены (2026)** — https://developers.facebook.com/documentation/business-messaging/whatsapp/pricing
- С 2025-07-01 оплата за сообщение. Платим только за **доставленный** шаблон, цена зависит от категории и кода
  страны получателя. Для authentication есть объёмные уровни по портфелю, они сбрасываются ежемесячно.
- **Казахстан с 2026-10-01** выделен из «Rest of Central & Eastern Europe» в отдельный рынок. Тарифы utility и
  authentication выросли, и появился тариф authentication-international.
- **Authentication-international** применяется, когда бизнес зарегистрирован в **другой** стране, чем
  получатель, и отправил >750K таких сообщений за 30 дней. Meta предупреждает за 30 дней.
  https://developers.facebook.com/documentation/business-messaging/whatsapp/pricing/authentication-international-rates
  nado — компания РК, поэтому для покупателей из КЗ ждём обычный тариф ⚠️ не проверено (страну бизнеса Meta
  определяет по публичным данным).
- Точные цены KZ/RU — в CSV рейт-карте: https://business.whatsapp.com/products/platform-pricing. В статьях
  встречалось ~$0.018 за authentication в KZ ⚠️ не проверено. Россия — отдельный рынок или «Rest of CEE»
  ⚠️ не проверено. Код +7 общий, рынок определяется по номеру.
- С 2026-10-01 свободные (сервисные) сообщения платные после 1 000 в месяц на номер
  (https://ominiflow.com/blog/whatsapp-api-pricing-update-october-2026, ⚠️ не сверено с Meta). На OTP не влияет.
- BSP (360dialog, Twilio, Infobip) нужны, только если не удастся верифицироваться в Meta самим.

## 2. Telegram Gateway API

Справочник https://core.telegram.org/gateway/api · обзор https://core.telegram.org/gateway ·
туториал https://core.telegram.org/gateway/verification-tutorial
- База `https://gatewayapi.telegram.org/{method}`. GET или POST (query, form, JSON).
  `Authorization: Bearer <token>`, токен — на gateway.telegram.org/account/api.
- Ответ `{"ok":true,"result":{...}}` или `{"ok":false,"error":"..."}`. Каталог строк ошибок ⚠️ не проверено.
- **checkSendAbility**(`phone_number` E.164) → `RequestStatus` с `request_id`. Если Telegram у номера нет,
  приходит ошибка, и запрос бесплатный. Если отправка возможна, сразу списывается цена одного сообщения, а
  первый `sendVerificationMessage` с этим `request_id` не тарифицируется повторно.
- **sendVerificationMessage**:
  - `phone_number`* и `request_id`;
  - `sender_username` — подтверждённый канал, например канал nado;
  - `code` (4–8 цифр, свой код) **или** `code_length` (4–8, код генерирует Telegram);
  - `callback_url` (HTTPS, ≤256 байт);
  - `payload` (≤128 байт, пользователь не видит — кладём `public_id` challenge);
  - `ttl` (30–3600 с; если за TTL не доставлено и не прочитано — возврат денег).
- **checkVerificationStatus**(`request_id`, `code`) → `code_valid` | `code_invalid` |
  `code_max_attempts_exceeded` | `expired`. Telegram умеет сам генерировать и проверять код; сколько попыток
  он даёт ⚠️ не проверено. Даже со своим кодом Telegram просит вызывать этот метод ради статистики.
- **revokeVerificationMessage**(`request_id`) — отзыв кода. Доставленное или прочитанное не удаляется.
- `RequestStatus`: `request_id`, `phone_number`, `request_cost`, `is_refunded`, `remaining_balance`,
  `delivery_status{status: sent|delivered|read|expired|revoked, updated_at}`, `verification_status`, `payload`.
- **Колбэк:** POST с `RequestStatus`. Отвечаем 200, иначе до 10 повторов. Проверка подписи:
  `key = SHA256(api_token)`, `sig = hex(HMAC-SHA256(key, X-Request-Timestamp + "\n" + raw_body))`, сравнить с
  `X-Request-Signature`. Timestamp не старше ±5 мин (защита от replay).
- **Цена:** $0.01 за доставленный код, недоставленное возвращается, отправка на свой номер бесплатна. Баланс —
  предоплата через Fragment (TON), без вывода и переводов
  (https://telegram.org/blog/star-messages-gateway-2-0-and-more). Оплата с юрлица РК ⚠️ вопрос к бухгалтерии.
- Код приходит в служебный чат «Verification Codes». Нужен opt-in пользователя: https://telegram.org/tos/gateway

## 3. Рекомендуемый поток в nado (Go)

Пакеты: `internal/otp` — логика; `integration/messaging/{whatsapp,telegramgateway}` — отправители.
```go
type Sender interface {
    Channel() string // "whatsapp" | "telegram"
    CanSend(ctx context.Context, phoneE164 string) (reqID string, ok bool, err error) // WA: всегда ok
    Send(ctx context.Context, m OTPMessage) (providerMsgID string, err error)        // Code, Lang, TTL, ReqID
}
```
1. **Нормализация.** `phonenumbers.Parse(raw, storeCountry)` (github.com/nyaruka/phonenumbers). Проверить
   `IsValidNumber` и тип MOBILE / FIXED_LINE_OR_MOBILE, затем `Format(num, phonenumbers.E164)`. Код +7 общий
   для KZ и RU: `GetRegionCodeForNumber` различает их по диапазонам. В WhatsApp `to` передаётся без `+`.
2. **Канал выбирает покупатель** кнопками WhatsApp / Telegram. Для Telegram сначала `checkSendAbility`; при
   отказе — «у номера нет Telegram, попробуйте WhatsApp». Для WhatsApp после кулдауна предлагаем
   «не пришёл код? отправить в Telegram»; если пришёл вебхук `failed` — сразу.
3. **Код:** 6 цифр из `crypto/rand` (`rand.Int(rand.Reader, big.NewInt(1_000_000))`, `%06d`). Генерируем сами
   для обоих каналов (в Telegram через `code`), чтобы проверка была одна. `checkVerificationStatus` вызываем
   асинхронно, только для статистики.
4. **Хранение:** только `HMAC-SHA256(OTP_PEPPER, public_id + ":" + code)`. Голый SHA от 6 цифр перебирается
   мгновенно. Сравнение через `hmac.Equal`. TTL 5 мин, 5 попыток → `locked`. Повторная отправка не раньше
   чем через 60 с, новый challenge переводит прежний в `superseded`.
5. **Антифрод (toll fraud / pumping).** Каждое сообщение стоит денег и тратит лимит уникальных получателей WA.
   - Лимиты: телефон — 5 в час и 10 в сутки (по **всем** магазинам); IP — 10 в час и 30 в сутки; магазин и
     страна — суточный бюджет из конфига с алертом.
   - Отправка только на страны из белого списка (KZ, RU).
   - CAPTCHA со второй отправки с IP (Turnstile ⚠️ не выбрано).
   - Одинаковый ответ, есть покупатель или нет — без перечисления номеров.
   - Счётчики — `COUNT` по индексам `otp_challenges`.
6. **Проверка** одним запросом:
   `UPDATE ... SET attempts += 1 OUTPUT INSERTED.* WHERE public_id=@id AND status='pending'
   AND expires_at > SYSUTCDATETIME() AND attempts < max_attempts`, затем сверка хеша. При совпадении —
   `verified` и upsert `customers(store_id, phone_e164, phone_verified_at)` в той же транзакции.
7. **Сессия:** 32 байта `crypto/rand` в cookie, в `customer_sessions` лежит SHA-256 токена. Cookie
   `HttpOnly; Secure; SameSite=Lax`, host-only (у своего домена магазина и поддомена nado разные cookie). Срок
   30 дней со скользящим продлением. При входе ротировать сессию и CSRF-токен. «Выйти везде» удаляет строки.
8. **Логи:** код, хеш, токены и тела запросов к провайдерам **не логировать**. Телефон маскировать
   (`+7701***4567`). В `redact` (external-http) добавить поля `code`, `text`, `parameters`.

```sql
CREATE TABLE dbo.otp_challenges
(
    id               BIGINT IDENTITY (1, 1) NOT NULL,
    public_id        UNIQUEIDENTIFIER NOT NULL CONSTRAINT DF_otp_challenges_public_id DEFAULT (NEWID()),
    store_id         BIGINT           NOT NULL,
    phone_e164       VARCHAR(16)      NOT NULL,
    channel          VARCHAR(20)      NOT NULL,
    purpose          VARCHAR(20)      NOT NULL CONSTRAINT DF_otp_challenges_purpose DEFAULT ('login'),
    code_hash        BINARY(32)       NOT NULL,
    status           VARCHAR(20)      NOT NULL CONSTRAINT DF_otp_challenges_status DEFAULT ('pending'),
    attempts         INT              NOT NULL CONSTRAINT DF_otp_challenges_attempts DEFAULT (0),
    max_attempts     INT              NOT NULL CONSTRAINT DF_otp_challenges_max_attempts DEFAULT (5),
    provider_msg_id  VARCHAR(200)     NULL,  -- wamid или Telegram request_id
    delivery_status  VARCHAR(20)      NULL,  -- sent/delivered/read/failed/expired/revoked
    cost_micros_usd  BIGINT           NULL,  -- request_cost Telegram / оценка по рейт-карте
    client_ip        VARCHAR(45)      NOT NULL,
    expires_at       DATETIME2(3)     NOT NULL,
    created_at       DATETIME2(3)     NOT NULL CONSTRAINT DF_otp_challenges_created_at DEFAULT (SYSUTCDATETIME()),
    verified_at      DATETIME2(3)     NULL,
    CONSTRAINT PK_otp_challenges PRIMARY KEY CLUSTERED (id),
    CONSTRAINT CK_otp_challenges_channel CHECK (channel IN ('whatsapp', 'telegram', 'sms')),
    CONSTRAINT CK_otp_challenges_status CHECK (status IN ('pending','verified','expired','locked','superseded','failed'))
);
CREATE UNIQUE INDEX UX_otp_challenges_public_id ON dbo.otp_challenges (public_id);
CREATE INDEX IX_otp_challenges_phone_e164_created_at ON dbo.otp_challenges (phone_e164, created_at);
CREATE INDEX IX_otp_challenges_client_ip_created_at ON dbo.otp_challenges (client_ip, created_at);
CREATE INDEX IX_otp_challenges_provider_msg_id ON dbo.otp_challenges (provider_msg_id) WHERE provider_msg_id IS NOT NULL;
```
Клиенту отдаём `public_id`. Строки старше 30 дней удаляет фоновая задача (минимизация ПД). `store_id` нужен,
потому что покупатели изолированы по магазину (D-17).

**Персональные данные.** Телефон — ПД и в РК (Закон № 94-V, https://adilet.zan.kz/rus/docs/Z1300000094), и в
РФ (152-ФЗ). Оператор — продавец, nado обрабатывает ПД по его поручению.
- До отправки кода — явный чекбокс: «Согласен на обработку номера телефона магазином <Название> и его передачу
  WhatsApp (Meta) / Telegram для отправки кода» со ссылкой на политику магазина.
- Передача номера Meta или Telegram — **трансграничная**. РК: согласие или страна с адекватной защитой (ст. 16)
  ⚠️ не проверено. РФ: уведомление РКН; с 01.09.2025 согласие — отдельный документ ⚠️ не проверено.
- Локализация: ПД граждан РК — в базах на территории РК (ст. 12), граждан РФ — первичная запись в РФ. Для
  региона РФ это влияет на то, где хранить `otp_challenges` и `customers` ⚠️ нужен юрист.

## 4. Подводные камни и что проверить до реализации

- **250 получателей в сутки** у непроверенного портфеля Meta: без верификации бизнеса вход через WhatsApp
  упрётся в лимит в первый же день. Верификацию начинать заранее (дни–недели ⚠️).
- **WhatsApp в РФ** с февраля 2026 ограничен: домены исключены из НСДИ, доступ через VPN
  (https://prim.rbc.ru/prim/12/02/2026/698d79189a7947ffe922a0ef). Telegram в РФ тоже частично замедляют. Для
  региона РФ нужен третий канал (SMS или Max) — open-questions №22.
- В authentication-шаблоне нельзя упомянуть магазин: покупатель увидит отправителя «nado». Объяснить это на
  экране ввода кода.
- Шаблон могут отклонить или приостановить за низкое качество. Следить за статусом (вебхук
  `message_template_status_update` ⚠️ не проверено) и тогда переключать UI на Telegram.
- Telegram: нулевой баланс = нет входа. Мониторить `remaining_balance` и ставить алерт.
- Вебхуки обоих провайдеров принимать на `app.nado.kz`, не на доменах магазинов. Подпись проверять по сырому
  телу до разбора JSON. Статус `delivered`/`read` — не вход; вход — только совпадение кода.
- Тесты без сети: фейковый `Sender`, фиксированные часы и pepper, эталонные векторы подписей Meta и Telegram.
- Проверить: версию Graph API; права System User; `kk` в authentication-шаблонах; код ошибки «нет WhatsApp»;
  цены KZ/RU в CSV после 2026-10-01; лимит попыток Telegram; юридическую схему согласия и трансграничной
  передачи.
