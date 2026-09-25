/* 0006 — зеркало каталога маркетплейса (Kaspi, импорт из кабинета).
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0006_marketplace_products.sql

   Товары кабинета Kaspi зеркалятся к нам (уровень магазина, D-23). Это первый
   слой — импортированный каталог; превращение в продаваемые товары витрины с
   правилами цен (D-14) — отдельный шаг. */

IF OBJECT_ID(N'dbo.marketplace_products', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.marketplace_products
    (
        id            BIGINT IDENTITY (1, 1) NOT NULL,
        connection_id BIGINT                 NOT NULL,
        store_id      BIGINT                 NOT NULL,
        account_id    BIGINT                 NOT NULL,
        sku           VARCHAR(100)           NOT NULL,  -- код товара продавца
        master_sku    VARCHAR(100)           NULL,      -- код карточки-мастера Kaspi
        title         NVARCHAR(1000)         NULL,
        brand         NVARCHAR(300)          NULL,
        category_ext  VARCHAR(300)           NULL,      -- код категории Kaspi
        price_minor   BIGINT                 NULL,      -- цена маркетплейса, тиыны
        currency      CHAR(3)                NOT NULL CONSTRAINT DF_marketplace_products_currency DEFAULT ('KZT'),
        available     BIT                    NOT NULL CONSTRAINT DF_marketplace_products_available DEFAULT (0),
        images        NVARCHAR(MAX)          NULL CONSTRAINT CK_marketplace_products_images_json CHECK (images IS NULL OR ISJSON(images) = 1),
        content_hash  BINARY(32)             NULL,      -- для пропуска неизменившихся
        raw_json      NVARCHAR(MAX)          NULL,
        removed_at    DATETIME2(3)           NULL,      -- не встретился при полном обходе
        synced_at     DATETIME2(3)           NOT NULL CONSTRAINT DF_marketplace_products_synced_at DEFAULT (SYSUTCDATETIME()),
        created_at    DATETIME2(3)           NOT NULL CONSTRAINT DF_marketplace_products_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_marketplace_products PRIMARY KEY CLUSTERED (id),
        CONSTRAINT UX_marketplace_products_sku UNIQUE (connection_id, sku),
        CONSTRAINT FK_marketplace_products_conn FOREIGN KEY (connection_id) REFERENCES dbo.marketplace_connections (id)
    );

    CREATE INDEX IX_marketplace_products_store ON dbo.marketplace_products (store_id, available);
END;
GO

IF OBJECT_ID(N'dbo.marketplace_product_stocks', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.marketplace_product_stocks
    (
        product_id  BIGINT       NOT NULL,
        store_code  VARCHAR(100) NOT NULL,  -- код точки продавца
        qty         INT          NOT NULL CONSTRAINT DF_marketplace_product_stocks_qty DEFAULT (0),
        specified   BIT          NOT NULL CONSTRAINT DF_marketplace_product_stocks_specified DEFAULT (0),
        preorder    INT          NOT NULL CONSTRAINT DF_marketplace_product_stocks_preorder DEFAULT (0),
        updated_at  DATETIME2(3) NOT NULL CONSTRAINT DF_marketplace_product_stocks_updated DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_marketplace_product_stocks PRIMARY KEY CLUSTERED (product_id, store_code),
        CONSTRAINT FK_marketplace_product_stocks_product FOREIGN KEY (product_id) REFERENCES dbo.marketplace_products (id)
    );
END;
GO
