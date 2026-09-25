# Модель данных

Это целевая схема. Таблицы создаются миграциями по мере реализации модулей, без
опережения. Соглашения по типам и именам описаны в nado-go-conventions.

**Условные обозначения:** 🔑 PK; `acc` — `account_id BIGINT NOT NULL` с FK на
`accounts`. Все арендные таблицы содержат `acc`.

## Аккаунты и доступ

| Таблица | Ключевые поля | Примечания |
|---|---|---|
| `accounts` | 🔑id, name, country (`RU`/`KZ`), region, plan_code, status, created_at | Арендатор: продавец, ИП или компания. `region` определяет, в какой инсталляции хранятся ПД (см. open-questions) |
| `users` | 🔑id, email (unique), password_hash, name, locale, ui_theme (`system`/`light`/`dark`), status | Пользователь кабинета; может состоять в нескольких аккаунтах |
| `account_members` | 🔑(account_id, user_id), role (`owner`/`admin`/`manager`/`viewer`) | Роли проверяются в service |
| `sessions` | 🔑id (случайные 32 байта, хранится хеш), user_id, account_id, expires_at, ip, user_agent | Серверные сессии кабинета, cookie `HttpOnly; Secure; SameSite=Lax` |
| `subscriptions` | 🔑id, acc, plan_code, status, current_period_end, provider, external_id | Биллинг платформы (D-06) |
| `plans` | 🔑code, limits (JSON: stores, products, connections, custom_domain...) | Справочник тарифов |

## Магазины

| Таблица | Ключевые поля | Примечания |
|---|---|---|
| `stores` | 🔑id, acc, slug (unique), name, country, base_currency, default_lang, status, theme_code, theme_settings (JSON по схеме манифеста темы: default_mode `system`/`light`/`dark`, primary_light, primary_dark (NULL — вычисляется), шрифт, logo_light_media_id, logo_dark_media_id, баннеры), settings (JSON) | `slug` — поддомен `<slug>.nado.kz` |
| `store_price_rules` | 🔑id, store_id, scope (`store`/`category`/`product`), scope_id NULL, base_source (`connection:<id>`/`min`/`max`), adjust_kind (`percent`/`amount`), adjust_value (DECIMAL; −10 = −10%), round_to (1/10/100), round_mode (`up`/`down`/`nearest`), is_enabled | D-14. Приоритет: product > category > store; ручная цена выше любого правила |
| `store_apps` | 🔑id, store_id, platform (`ios`/`android`), bundle_id, status, store_url, developer_account_owner | Гибридные приложения (D-16); модель уточнится после open-questions №20 |
| `store_languages` | 🔑(store_id, lang) | `ru`, `kk`, `en` |
| `store_currencies` | 🔑(store_id, currency), markup_bp | Валюты отображения; наценка в базисных пунктах |
| `store_domains` | 🔑id, store_id, host (unique), kind (`subdomain`/`custom`), verify_token, verified_at, is_primary | Выбор магазина по Host |
| `store_payment_methods` | 🔑id, store_id, provider_code, credentials_id, settings, is_enabled, sort | |
| `store_delivery_methods` | 🔑id, store_id, provider_code, credentials_id, settings (зоны, фикс. цены), is_enabled, sort | |
| `store_fiscal_settings` | 🔑store_id, provider_code, credentials_id, tax_system, default_vat, receipt_scheme (`two_step`/`single`) | |

## Учётные данные провайдеров

| Таблица | Ключевые поля | Примечания |
|---|---|---|
| `provider_credentials` | 🔑id, acc, kind (`marketplace`/`payment`/`fiscal`/`delivery`), provider_code, secret_ciphertext VARBINARY, key_version, public_meta (JSON), expires_at, status, last_verified_at | Секрет — только в зашифрованном виде; `public_meta` — то, что можно показать (shopId, имя кабинета) |

## Каталог (уровень аккаунта)

| Таблица | Ключевые поля | Примечания |
|---|---|---|
| `products` | 🔑id, acc, brand, category_id, status (`draft`/`active`/`archived`), overridden_fields (JSON-массив), created_at, updated_at | Контент по языкам — в `product_translations` |
| `product_translations` | 🔑(product_id, lang), title, description, seo_title, seo_description, slug | Уникальность `(acc, lang, slug)` через денормализацию `acc` |
| `variants` | 🔑id, acc, product_id, sku, barcodes (JSON), options (JSON: size, color...), weight_g, length_mm, width_mm, height_mm, status | `UNIQUE (acc, sku)` |
| `variant_stocks` | 🔑(variant_id, source_key), acc, qty, fulfillment (`fbs`/`fbo`/`manual`), updated_at | `source_key` = `manual` или `conn:<id>:wh:<warehouseId>`; какие источники учитывать — настройка магазина |
| `variant_marketplace_prices` | 🔑(variant_id, connection_id), acc, price_minor, old_price_minor, currency, updated_at | Цены маркетплейсов по источникам — вход для правил цены (D-14) |
| `categories` | 🔑id, acc, parent_id, sort | Своё дерево аккаунта; внешние категории — в `category_mappings` |
| `category_translations` | 🔑(category_id, lang), name, slug | |
| `attributes` | 🔑id, acc, code, kind | Характеристики для фильтров витрины |
| `product_attribute_values` | product_id, attribute_id, lang, value | |
| `media` | 🔑id, acc, sha256 (unique в пределах acc), storage_key, mime, width, height, bytes | Скачанные к нам файлы, без хотлинка |
| `product_media` | 🔑(product_id, media_id), variant_id NULL, sort, alt | |

## Связь с маркетплейсами

| Таблица | Ключевые поля | Примечания |
|---|---|---|
| `marketplace_connections` | 🔑id, acc, marketplace (`wildberries`/`ozon`/`yandex_market`/`kaspi`), credentials_id, name, status, content_sync_at, offers_sync_at, settings (JSON: склады, курсы, правила цен) | Одно подключение на кабинет |
| `product_sources` | 🔑id, acc, connection_id, product_id, variant_id NULL, external_id, external_group_id, content_hash, raw_json, removed_at, synced_at | `UNIQUE (connection_id, external_id)`; хранится только последний снимок |
| `sync_runs` | 🔑id, acc, connection_id, kind (`content`/`offers`/`file`), status, started_at, finished_at, stats (JSON), error | История для кабинета; старые записи чистятся |
| `sync_issues` | 🔑id, sync_run_id, external_id, severity, code, message | Проблемы по конкретным карточкам |

## Витрина и заказы (уровень магазина)

| Таблица | Ключевые поля | Примечания |
|---|---|---|
| `store_offers` | 🔑(store_id, variant_id), price_minor, old_price_minor, currency, is_visible, price_mode (`rule`/`manual`), manual_price_minor NULL, applied_rule_id NULL, computed_at | Итоговая цена в базовой валюте магазина; `manual` не пересчитывается синхронизацией |
| `customers` | 🔑id, store_id, phone_e164, phone_verified_at, name, email NULL, preferred_channel (`whatsapp`/`telegram`), consent_at, ui_theme, status | `UNIQUE (store_id, phone_e164)`; вход только по телефону (D-17), паролей нет |
| `customer_sessions` | 🔑id (хеш токена), customer_id, store_id, expires_at, user_agent, app_platform NULL | Отдельно от сессий кабинета; `app_platform` — вход из гибридного приложения |
| `customer_addresses` | 🔑id, customer_id, label, address (JSON), is_default | Для доставки |
| `carts` / `cart_items` | cart: 🔑id (случайный), store_id, customer_id NULL (до входа), currency, expires_at | Корзину можно собрать до входа; при входе она привязывается к покупателю |
| `orders` | 🔑id, store_id, acc, number (`UNIQUE (store_id, number)`), status, customer snapshot, display_currency, charge_currency, fx_rate DECIMAL(19,8), subtotal/delivery/discount/total_minor, lang, created_at | Все суммы — снимок |
| `order_items` | 🔑id, order_id, variant_id, title_snapshot, sku_snapshot, qty, price_minor, vat_code, marking_codes (JSON) NULL | |
| `payments` | 🔑id, order_id, provider_code, external_id, status, amount_minor, currency, idempotency_key, raw_last_event | `UNIQUE (provider_code, external_id)` |
| `refunds` | 🔑id, payment_id, external_id, amount_minor, status | |
| `receipts` | 🔑id, order_id, kind (`prepayment`/`full`/`refund`), provider_code, external_id, status, payload (JSON), fiscal_data (JSON) | |
| `shipments` | 🔑id, order_id, provider_code, external_id, tracking_number, status, pickup_point (JSON), label_media_id | |
| `shipment_events` | 🔑id, shipment_id, status, occurred_at, raw | |

## Инфраструктурные таблицы

| Таблица | Назначение |
|---|---|
| `jobs` | Очередь фоновых задач (nado-go-conventions → jobs-queue.md) |
| `fx_rates` | 🔑(rate_date, base, quote), rate, source (`cbr`/`nbrk`) |
| `otp_challenges` | 🔑id, store_id, phone_e164, channel, code_hash, provider_request_id, attempts, expires_at, verified_at, ip — одноразовые коды входа (nado-storefront → customer-auth-otp.md) |
| `webhook_events` | 🔑id, provider_code, dedup_key (unique), received_at, payload, processed_at — дедупликация и аудит входящих вебхуков |
| `audit_log` | Уже есть в 0001; расширить полем `account_id` |

## Статусы заказа

```
new → awaiting_payment → paid → processing → shipped → delivered → completed
          │                 │                                  └→ returned
          ├→ awaiting_manual_confirmation (Kaspi полуручной) → paid
          └→ canceled (по таймауту неоплаты или вручную)
paid/processing → refunded | partially_refunded
```

Переходы проверяются в service через таблицу допустимых переходов. Установить
произвольный статус через API нельзя.
