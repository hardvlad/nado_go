/* 0009 — каталог витрины: категории, товары, варианты, остатки, цены
   маркетплейса, итоговые цены магазина и правила цен (D-14, D-23).
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0009_catalog.sql

   Это продаваемый каталог витрины (шаги 2 и 4 дорожной карты). Он строится из
   зеркала marketplace_products фоновой задачей catalog.build_store: один товар
   Kaspi → товар + один вариант + остатки + цена источника + оффер магазина.
   Контент по языкам вынесен в *_translations. Все таблицы каталога арендуются по
   store_id с денормализованным account_id для проверки владения. */

/* Магазину нужны язык по умолчанию и настройки темы (целевая схема). */
IF COL_LENGTH('dbo.stores', 'default_lang') IS NULL
    ALTER TABLE dbo.stores ADD default_lang VARCHAR(2) NOT NULL CONSTRAINT DF_stores_default_lang DEFAULT ('ru');
GO
IF COL_LENGTH('dbo.stores', 'theme_code') IS NULL
    ALTER TABLE dbo.stores ADD theme_code VARCHAR(40) NOT NULL CONSTRAINT DF_stores_theme_code DEFAULT ('base');
GO
IF COL_LENGTH('dbo.stores', 'theme_settings') IS NULL
    ALTER TABLE dbo.stores ADD theme_settings NVARCHAR(MAX) NULL CONSTRAINT CK_stores_theme_settings_json CHECK (theme_settings IS NULL OR ISJSON(theme_settings) = 1);
GO

/* Категории магазина (дерево). Внешние коды Kaspi отображаются в категории при
   построении каталога. */
IF OBJECT_ID(N'dbo.categories', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.categories
    (
        id           BIGINT IDENTITY (1, 1) NOT NULL,
        store_id     BIGINT                 NOT NULL,
        account_id   BIGINT                 NOT NULL,
        parent_id    BIGINT                 NULL,
        external_code VARCHAR(300)          NULL,      -- код категории источника (Kaspi), для идемпотентного построения
        sort         INT                    NOT NULL CONSTRAINT DF_categories_sort DEFAULT (0),
        created_at   DATETIME2(3)           NOT NULL CONSTRAINT DF_categories_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_categories PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_categories_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id)
    );
    CREATE INDEX IX_categories_store ON dbo.categories (store_id, sort);
    -- Уникальность внешнего кода в пределах магазина (для построения из зеркала).
    CREATE UNIQUE INDEX UX_categories_external ON dbo.categories (store_id, external_code) WHERE external_code IS NOT NULL;
END;
GO

IF OBJECT_ID(N'dbo.category_translations', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.category_translations
    (
        category_id BIGINT       NOT NULL,
        store_id    BIGINT       NOT NULL,   -- денормализация для уникальности slug в магазине
        lang        VARCHAR(2)   NOT NULL,
        name        NVARCHAR(300) NOT NULL,
        slug        VARCHAR(160) NOT NULL,
        CONSTRAINT PK_category_translations PRIMARY KEY CLUSTERED (category_id, lang),
        CONSTRAINT FK_category_translations_category FOREIGN KEY (category_id) REFERENCES dbo.categories (id),
        CONSTRAINT UX_category_translations_slug UNIQUE (store_id, lang, slug)
    );
END;
GO

/* Товар витрины. Контент по языкам — в product_translations. source_sku связывает
   с зеркалом marketplace_products для идемпотентного построения. */
IF OBJECT_ID(N'dbo.products', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.products
    (
        id          BIGINT IDENTITY (1, 1) NOT NULL,
        store_id    BIGINT                 NOT NULL,
        account_id  BIGINT                 NOT NULL,
        category_id BIGINT                 NULL,
        brand       NVARCHAR(300)          NULL,
        source_sku  VARCHAR(100)           NULL,      -- sku в marketplace_products (источник)
        status      VARCHAR(20)            NOT NULL CONSTRAINT DF_products_status DEFAULT ('active'),
        created_at  DATETIME2(3)           NOT NULL CONSTRAINT DF_products_created_at DEFAULT (SYSUTCDATETIME()),
        updated_at  DATETIME2(3)           NOT NULL CONSTRAINT DF_products_updated_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_products PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_products_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id),
        CONSTRAINT FK_products_category FOREIGN KEY (category_id) REFERENCES dbo.categories (id),
        CONSTRAINT CK_products_status CHECK (status IN ('draft', 'active', 'archived'))
    );
    CREATE INDEX IX_products_store ON dbo.products (store_id, status);
    CREATE INDEX IX_products_category ON dbo.products (category_id);
    CREATE UNIQUE INDEX UX_products_source ON dbo.products (store_id, source_sku) WHERE source_sku IS NOT NULL;
END;
GO

IF OBJECT_ID(N'dbo.product_translations', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.product_translations
    (
        product_id      BIGINT        NOT NULL,
        store_id        BIGINT        NOT NULL,
        lang            VARCHAR(2)    NOT NULL,
        title           NVARCHAR(1000) NOT NULL,
        description     NVARCHAR(MAX) NULL,
        slug            VARCHAR(200)  NOT NULL,
        seo_title       NVARCHAR(300) NULL,
        seo_description NVARCHAR(600) NULL,
        CONSTRAINT PK_product_translations PRIMARY KEY CLUSTERED (product_id, lang),
        CONSTRAINT FK_product_translations_product FOREIGN KEY (product_id) REFERENCES dbo.products (id),
        CONSTRAINT UX_product_translations_slug UNIQUE (store_id, lang, slug)
    );
END;
GO

/* Изображения товара. Пока храним внешние URL (зеркало Kaspi отдаёт ссылки);
   скачивание к себе (media.fetch) — отдельный шаг. */
IF OBJECT_ID(N'dbo.product_images', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.product_images
    (
        id         BIGINT IDENTITY (1, 1) NOT NULL,
        product_id BIGINT                 NOT NULL,
        store_id   BIGINT                 NOT NULL,
        url        NVARCHAR(1000)         NOT NULL,
        sort       INT                    NOT NULL CONSTRAINT DF_product_images_sort DEFAULT (0),
        CONSTRAINT PK_product_images PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_product_images_product FOREIGN KEY (product_id) REFERENCES dbo.products (id)
    );
    CREATE INDEX IX_product_images_product ON dbo.product_images (product_id, sort);
END;
GO

/* Вариант товара: SKU, остатки и цены относятся к варианту. Для Kaspi-зеркала
   один товар = один вариант. */
IF OBJECT_ID(N'dbo.variants', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.variants
    (
        id         BIGINT IDENTITY (1, 1) NOT NULL,
        store_id   BIGINT                 NOT NULL,
        account_id BIGINT                 NOT NULL,
        product_id BIGINT                 NOT NULL,
        sku        VARCHAR(100)           NOT NULL,
        options    NVARCHAR(MAX)          NULL CONSTRAINT CK_variants_options_json CHECK (options IS NULL OR ISJSON(options) = 1),
        status     VARCHAR(20)            NOT NULL CONSTRAINT DF_variants_status DEFAULT ('active'),
        created_at DATETIME2(3)           NOT NULL CONSTRAINT DF_variants_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_variants PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_variants_product FOREIGN KEY (product_id) REFERENCES dbo.products (id),
        CONSTRAINT UX_variants_sku UNIQUE (store_id, sku)
    );
    CREATE INDEX IX_variants_product ON dbo.variants (product_id);
END;
GO

/* Остатки варианта по источникам. source_key = 'conn:<id>:pp:<code>' или 'manual'. */
IF OBJECT_ID(N'dbo.variant_stocks', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.variant_stocks
    (
        variant_id  BIGINT       NOT NULL,
        source_key  VARCHAR(100) NOT NULL,
        store_id    BIGINT       NOT NULL,
        qty         INT          NOT NULL CONSTRAINT DF_variant_stocks_qty DEFAULT (0),
        fulfillment VARCHAR(10)  NOT NULL CONSTRAINT DF_variant_stocks_fulfillment DEFAULT ('fbs'),
        updated_at  DATETIME2(3) NOT NULL CONSTRAINT DF_variant_stocks_updated DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_variant_stocks PRIMARY KEY CLUSTERED (variant_id, source_key),
        CONSTRAINT FK_variant_stocks_variant FOREIGN KEY (variant_id) REFERENCES dbo.variants (id),
        CONSTRAINT CK_variant_stocks_fulfillment CHECK (fulfillment IN ('fbs', 'fbo', 'manual'))
    );
END;
GO

/* Цены маркетплейсов по источникам — вход для правил цены (D-14). */
IF OBJECT_ID(N'dbo.variant_marketplace_prices', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.variant_marketplace_prices
    (
        variant_id      BIGINT       NOT NULL,
        connection_id   BIGINT       NOT NULL,
        store_id        BIGINT       NOT NULL,
        price_minor     BIGINT       NOT NULL,
        old_price_minor BIGINT       NULL,
        currency        CHAR(3)      NOT NULL CONSTRAINT DF_variant_marketplace_prices_currency DEFAULT ('KZT'),
        updated_at      DATETIME2(3) NOT NULL CONSTRAINT DF_variant_marketplace_prices_updated DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_variant_marketplace_prices PRIMARY KEY CLUSTERED (variant_id, connection_id),
        CONSTRAINT FK_variant_marketplace_prices_variant FOREIGN KEY (variant_id) REFERENCES dbo.variants (id)
    );
END;
GO

/* Итоговая цена варианта в базовой валюте магазина. manual не пересчитывается. */
IF OBJECT_ID(N'dbo.store_offers', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.store_offers
    (
        store_id           BIGINT       NOT NULL,
        variant_id         BIGINT       NOT NULL,
        price_minor        BIGINT       NOT NULL,
        old_price_minor    BIGINT       NULL,
        currency           CHAR(3)      NOT NULL CONSTRAINT DF_store_offers_currency DEFAULT ('KZT'),
        is_visible         BIT          NOT NULL CONSTRAINT DF_store_offers_visible DEFAULT (1),
        price_mode         VARCHAR(10)  NOT NULL CONSTRAINT DF_store_offers_mode DEFAULT ('rule'),
        manual_price_minor BIGINT       NULL,
        applied_rule_id    BIGINT       NULL,
        computed_at        DATETIME2(3) NOT NULL CONSTRAINT DF_store_offers_computed DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_store_offers PRIMARY KEY CLUSTERED (store_id, variant_id),
        CONSTRAINT FK_store_offers_variant FOREIGN KEY (variant_id) REFERENCES dbo.variants (id),
        CONSTRAINT CK_store_offers_mode CHECK (price_mode IN ('rule', 'manual'))
    );
END;
GO

/* Правила цен магазина (D-14): приоритет product > category > store. */
IF OBJECT_ID(N'dbo.store_price_rules', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.store_price_rules
    (
        id           BIGINT IDENTITY (1, 1) NOT NULL,
        store_id     BIGINT                 NOT NULL,
        scope        VARCHAR(10)            NOT NULL,   -- store | category | product
        scope_id     BIGINT                 NULL,
        adjust_kind  VARCHAR(10)            NOT NULL CONSTRAINT DF_store_price_rules_kind DEFAULT ('percent'),
        adjust_value DECIMAL(12, 4)         NOT NULL CONSTRAINT DF_store_price_rules_value DEFAULT (0),  -- -10 = -10%
        round_to     INT                    NOT NULL CONSTRAINT DF_store_price_rules_round_to DEFAULT (1),
        round_mode   VARCHAR(10)            NOT NULL CONSTRAINT DF_store_price_rules_round_mode DEFAULT ('nearest'),
        is_enabled   BIT                    NOT NULL CONSTRAINT DF_store_price_rules_enabled DEFAULT (1),
        created_at   DATETIME2(3)           NOT NULL CONSTRAINT DF_store_price_rules_created DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_store_price_rules PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_store_price_rules_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id),
        CONSTRAINT CK_store_price_rules_scope CHECK (scope IN ('store', 'category', 'product')),
        CONSTRAINT CK_store_price_rules_kind CHECK (adjust_kind IN ('percent', 'amount')),
        CONSTRAINT CK_store_price_rules_round_mode CHECK (round_mode IN ('up', 'down', 'nearest'))
    );
    CREATE INDEX IX_store_price_rules_store ON dbo.store_price_rules (store_id, scope);
END;
GO
