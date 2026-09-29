/* 0016 — онлайн-оплаты: настройки оплаты магазина, платежи и подписка аккаунта.
   Скрипт идемпотентен.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0016_payments.sql

   Слой оплат (internal/integration/payment) обслуживает и заказы витрины (деньги
   покупателя → мерчант продавца, D-01), и подписку платформе (деньги продавца →
   мерчант nado). Одна таблица payments с полем purpose. Секрет мерчанта хранится
   зашифрованным (AES-256-GCM), как токены провайдеров (nado-go-conventions). */

/* Настройки приёма оплат магазином (свой мерчант-аккаунт продавца). */
IF OBJECT_ID(N'dbo.store_payment_settings', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.store_payment_settings
    (
        store_id           BIGINT        NOT NULL,
        account_id         BIGINT        NOT NULL,
        provider           VARCHAR(20)   NOT NULL CONSTRAINT DF_store_payment_settings_provider DEFAULT ('dev'),
        is_enabled         BIT           NOT NULL CONSTRAINT DF_store_payment_settings_enabled DEFAULT (0),
        merchant_id        NVARCHAR(200) NULL,          -- ID мерчанта (Halyk: ClientID)
        terminal_id        NVARCHAR(200) NULL,          -- терминал (Halyk ePay: TerminalID)
        secret_ciphertext  VARBINARY(4000) NULL,       -- секретный ключ мерчанта, зашифрован
        testing_mode       BIT           NOT NULL CONSTRAINT DF_store_payment_settings_testing DEFAULT (0),
        webhook_token      VARCHAR(64)   NULL,          -- секрет в пути вебхука /webhooks/payment/{provider}/{token}
        public_meta        NVARCHAR(MAX) NULL CONSTRAINT CK_store_payment_settings_meta_json CHECK (public_meta IS NULL OR ISJSON(public_meta) = 1),
        created_at         DATETIME2(3)  NOT NULL CONSTRAINT DF_store_payment_settings_created DEFAULT (SYSUTCDATETIME()),
        updated_at         DATETIME2(3)  NOT NULL CONSTRAINT DF_store_payment_settings_updated DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_store_payment_settings PRIMARY KEY CLUSTERED (store_id),
        CONSTRAINT FK_store_payment_settings_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id),
        CONSTRAINT FK_store_payment_settings_accounts FOREIGN KEY (account_id) REFERENCES dbo.accounts (id),
        CONSTRAINT CK_store_payment_settings_provider CHECK (provider IN ('dev', 'freedompay', 'kaspi', 'halyk'))
    );
    CREATE INDEX IX_store_payment_settings_token ON dbo.store_payment_settings (provider, webhook_token);
END;
GO

/* Платежи: заказов витрины (purpose='order') и подписки (purpose='subscription').
   ref_token — наш идентификатор, который уходит провайдеру (pg_order_id) и
   возвращается в вебхуке; по нему находим платёж. */
IF OBJECT_ID(N'dbo.payments', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.payments
    (
        id            BIGINT IDENTITY (1, 1) NOT NULL,
        account_id    BIGINT        NOT NULL,
        store_id      BIGINT        NULL,          -- NULL для подписки платформе
        purpose       VARCHAR(20)   NOT NULL,       -- order | subscription
        order_id      BIGINT        NULL,           -- для purpose='order'
        provider      VARCHAR(20)   NOT NULL,
        provider_ref  NVARCHAR(200) NULL,           -- id платежа у провайдера
        ref_token     VARCHAR(64)   NOT NULL,       -- наш идентификатор (pg_order_id)
        amount_minor  BIGINT        NOT NULL,
        currency      CHAR(3)       NOT NULL CONSTRAINT DF_payments_currency DEFAULT ('KZT'),
        status        VARCHAR(20)   NOT NULL CONSTRAINT DF_payments_status DEFAULT ('pending'),
        plan_code     VARCHAR(30)   NULL,           -- для подписки: какой тариф оплачивается
        period_days   INT           NULL,           -- для подписки: на сколько дней
        meta          NVARCHAR(MAX) NULL CONSTRAINT CK_payments_meta_json CHECK (meta IS NULL OR ISJSON(meta) = 1),
        created_at    DATETIME2(3)  NOT NULL CONSTRAINT DF_payments_created DEFAULT (SYSUTCDATETIME()),
        updated_at    DATETIME2(3)  NOT NULL CONSTRAINT DF_payments_updated DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_payments PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_payments_accounts FOREIGN KEY (account_id) REFERENCES dbo.accounts (id),
        CONSTRAINT FK_payments_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id),
        CONSTRAINT FK_payments_orders FOREIGN KEY (order_id) REFERENCES dbo.orders (id),
        CONSTRAINT CK_payments_purpose CHECK (purpose IN ('order', 'subscription')),
        CONSTRAINT CK_payments_status CHECK (status IN ('pending', 'succeeded', 'failed', 'canceled'))
    );
    CREATE UNIQUE INDEX UX_payments_ref ON dbo.payments (ref_token);
    CREATE INDEX IX_payments_order ON dbo.payments (order_id);
    CREATE INDEX IX_payments_account ON dbo.payments (account_id, purpose);
END;
GO

/* Признак оплаты у заказа витрины: время оплаты и id успешного платежа. */
IF COL_LENGTH('dbo.orders', 'paid_at') IS NULL
    ALTER TABLE dbo.orders ADD paid_at DATETIME2(3) NULL;
GO
IF COL_LENGTH('dbo.orders', 'payment_id') IS NULL
    ALTER TABLE dbo.orders ADD payment_id BIGINT NULL;
GO

/* Подписка аккаунта на платформу (D-06). trialing → active/past_due. */
IF COL_LENGTH('dbo.accounts', 'subscription_status') IS NULL
    ALTER TABLE dbo.accounts ADD subscription_status VARCHAR(20) NOT NULL
        CONSTRAINT DF_accounts_subscription_status DEFAULT ('trialing');
GO
IF COL_LENGTH('dbo.accounts', 'subscription_until') IS NULL
    ALTER TABLE dbo.accounts ADD subscription_until DATETIME2(3) NULL;
GO
