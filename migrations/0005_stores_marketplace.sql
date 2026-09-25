/* 0005 — магазины, учётные данные провайдеров, подключения маркетплейсов и
   зеркало заказов Kaspi (официальный Shop API).
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0005_stores_marketplace.sql

   Магазин — единица всего (D-23): витрина + одно подключение к маркетплейсу,
   один-к-одному. Тариф, каталог, заказы привязаны к магазину. */

/* Магазин: витрина + подключение к маркетплейсу. */
IF OBJECT_ID(N'dbo.stores', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.stores
    (
        id            BIGINT IDENTITY (1, 1) NOT NULL,
        account_id    BIGINT                 NOT NULL,
        slug          VARCHAR(63)            NOT NULL,      -- поддомен <slug>.nado.kz
        name          NVARCHAR(200)          NOT NULL,
        country       CHAR(2)                NOT NULL CONSTRAINT DF_stores_country DEFAULT ('KZ'),
        base_currency CHAR(3)                NOT NULL CONSTRAINT DF_stores_base_currency DEFAULT ('KZT'),
        plan_code     VARCHAR(30)            NULL,
        /* active — работает; disabled — нет оплаты/выключен (D-23), витрина и
           синхронизация встают; archived — удалён продавцом. */
        status        VARCHAR(20)            NOT NULL CONSTRAINT DF_stores_status DEFAULT ('active'),
        created_at    DATETIME2(3)           NOT NULL CONSTRAINT DF_stores_created_at DEFAULT (SYSUTCDATETIME()),
        updated_at    DATETIME2(3)           NOT NULL CONSTRAINT DF_stores_updated_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_stores PRIMARY KEY CLUSTERED (id),
        CONSTRAINT UX_stores_slug UNIQUE (slug),
        CONSTRAINT FK_stores_accounts FOREIGN KEY (account_id) REFERENCES dbo.accounts (id),
        CONSTRAINT CK_stores_status CHECK (status IN ('active', 'disabled', 'archived'))
    );

    CREATE INDEX IX_stores_account ON dbo.stores (account_id);
END;
GO

/* Учётные данные провайдера (маркетплейс/оплата/доставка/чеки).
   Секрет — только в зашифрованном виде (secrets, AES-GCM). */
IF OBJECT_ID(N'dbo.provider_credentials', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.provider_credentials
    (
        id               BIGINT IDENTITY (1, 1) NOT NULL,
        account_id       BIGINT                 NOT NULL,
        store_id         BIGINT                 NULL,
        kind             VARCHAR(20)            NOT NULL,  -- marketplace | payment | fiscal | delivery
        provider_code    VARCHAR(30)            NOT NULL,  -- kaspi | wildberries | ...
        secret_ciphertext VARBINARY(4000)       NOT NULL,
        public_meta      NVARCHAR(MAX)          NULL CONSTRAINT CK_provider_credentials_meta_json CHECK (public_meta IS NULL OR ISJSON(public_meta) = 1),
        status           VARCHAR(20)            NOT NULL CONSTRAINT DF_provider_credentials_status DEFAULT ('active'),
        last_verified_at DATETIME2(3)           NULL,
        created_at       DATETIME2(3)           NOT NULL CONSTRAINT DF_provider_credentials_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_provider_credentials PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_provider_credentials_accounts FOREIGN KEY (account_id) REFERENCES dbo.accounts (id),
        CONSTRAINT FK_provider_credentials_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id)
    );

    CREATE INDEX IX_provider_credentials_store ON dbo.provider_credentials (store_id, kind);
END;
GO

/* Подключение магазина к маркетплейсу — один-к-одному с магазином (D-23). */
IF OBJECT_ID(N'dbo.marketplace_connections', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.marketplace_connections
    (
        id             BIGINT IDENTITY (1, 1) NOT NULL,
        store_id       BIGINT                 NOT NULL,
        account_id     BIGINT                 NOT NULL,
        marketplace    VARCHAR(20)            NOT NULL,  -- kaspi | wildberries | ozon | yandex_market
        credentials_id BIGINT                 NOT NULL,
        name           NVARCHAR(200)          NULL,      -- имя кабинета/merchant для показа
        status         VARCHAR(20)            NOT NULL CONSTRAINT DF_marketplace_connections_status DEFAULT ('active'),
        orders_sync_at  DATETIME2(3)          NULL,
        content_sync_at DATETIME2(3)          NULL,
        settings       NVARCHAR(MAX)          NULL CONSTRAINT CK_marketplace_connections_settings_json CHECK (settings IS NULL OR ISJSON(settings) = 1),
        created_at     DATETIME2(3)           NOT NULL CONSTRAINT DF_marketplace_connections_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_marketplace_connections PRIMARY KEY CLUSTERED (id),
        CONSTRAINT UX_marketplace_connections_store UNIQUE (store_id),
        CONSTRAINT FK_marketplace_connections_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id),
        CONSTRAINT FK_marketplace_connections_accounts FOREIGN KEY (account_id) REFERENCES dbo.accounts (id),
        CONSTRAINT FK_marketplace_connections_creds FOREIGN KEY (credentials_id) REFERENCES dbo.provider_credentials (id),
        CONSTRAINT CK_marketplace_connections_status CHECK (status IN ('active', 'invalid', 'paused'))
    );
END;
GO

/* Зеркало заказов маркетплейса (Kaspi, официальный API). Только чтение —
   источник истины остаётся на маркетплейсе. */
IF OBJECT_ID(N'dbo.marketplace_orders', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.marketplace_orders
    (
        id             BIGINT IDENTITY (1, 1) NOT NULL,
        connection_id  BIGINT                 NOT NULL,
        store_id       BIGINT                 NOT NULL,
        account_id     BIGINT                 NOT NULL,
        external_id    VARCHAR(100)           NOT NULL,  -- id заказа у Kaspi (JSON:API id)
        code           VARCHAR(100)           NULL,      -- номер заказа
        state          VARCHAR(50)            NULL,
        status         VARCHAR(50)            NULL,
        total_minor    BIGINT                 NULL,      -- сумма в тиынах
        currency       CHAR(3)                NOT NULL CONSTRAINT DF_marketplace_orders_currency DEFAULT ('KZT'),
        customer_name  NVARCHAR(300)          NULL,
        customer_phone VARCHAR(20)            NULL,
        ordered_at     DATETIME2(3)           NULL,
        raw_json       NVARCHAR(MAX)          NULL,
        imported_at    DATETIME2(3)           NOT NULL CONSTRAINT DF_marketplace_orders_imported_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_marketplace_orders PRIMARY KEY CLUSTERED (id),
        CONSTRAINT UX_marketplace_orders_ext UNIQUE (connection_id, external_id),
        CONSTRAINT FK_marketplace_orders_conn FOREIGN KEY (connection_id) REFERENCES dbo.marketplace_connections (id)
    );

    CREATE INDEX IX_marketplace_orders_store ON dbo.marketplace_orders (store_id, ordered_at DESC);
END;
GO
