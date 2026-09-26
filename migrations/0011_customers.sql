/* 0011 — покупатели магазина, их сессии и одноразовые коды входа (D-17).
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0011_customers.sql

   Покупатель существует в пределах магазина (UNIQUE store_id + phone). Вход только
   по телефону, паролей нет. Код хранится как HMAC (secrets), не открытым текстом:
   простой SHA от 6 цифр перебирается мгновенно. */

IF OBJECT_ID(N'dbo.customers', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.customers
    (
        id                BIGINT IDENTITY (1, 1) NOT NULL,
        store_id          BIGINT                 NOT NULL,
        account_id        BIGINT                 NOT NULL,   -- владелец магазина (для проверки владения ПД)
        phone_e164        VARCHAR(20)            NOT NULL,
        phone_verified_at DATETIME2(3)           NULL,
        name              NVARCHAR(200)          NULL,
        email             NVARCHAR(320)          NULL,
        preferred_channel VARCHAR(10)            NOT NULL CONSTRAINT DF_customers_channel DEFAULT ('whatsapp'),
        consent_at        DATETIME2(3)           NULL,
        status            VARCHAR(20)            NOT NULL CONSTRAINT DF_customers_status DEFAULT ('active'),
        created_at        DATETIME2(3)           NOT NULL CONSTRAINT DF_customers_created DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_customers PRIMARY KEY CLUSTERED (id),
        CONSTRAINT UX_customers_store_phone UNIQUE (store_id, phone_e164),
        CONSTRAINT FK_customers_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id),
        CONSTRAINT CK_customers_status CHECK (status IN ('active', 'deleted'))
    );
END;
GO

IF OBJECT_ID(N'dbo.customer_sessions', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.customer_sessions
    (
        token_hash  BINARY(32)   NOT NULL,   -- SHA-256 токена из cookie (сам токен не хранится)
        customer_id BIGINT       NOT NULL,
        store_id    BIGINT       NOT NULL,
        user_agent  NVARCHAR(400) NULL,
        expires_at  DATETIME2(3) NOT NULL,
        created_at  DATETIME2(3) NOT NULL CONSTRAINT DF_customer_sessions_created DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_customer_sessions PRIMARY KEY CLUSTERED (token_hash),
        CONSTRAINT FK_customer_sessions_customer FOREIGN KEY (customer_id) REFERENCES dbo.customers (id)
    );
    CREATE INDEX IX_customer_sessions_customer ON dbo.customer_sessions (customer_id);
END;
GO

IF OBJECT_ID(N'dbo.otp_challenges', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.otp_challenges
    (
        id           BIGINT IDENTITY (1, 1) NOT NULL,
        store_id     BIGINT                 NOT NULL,
        phone_e164   VARCHAR(20)            NOT NULL,
        channel      VARCHAR(10)            NOT NULL CONSTRAINT DF_otp_challenges_channel DEFAULT ('whatsapp'),
        code_hash    BINARY(32)             NOT NULL,   -- HMAC кода (secrets)
        attempts     INT                    NOT NULL CONSTRAINT DF_otp_challenges_attempts DEFAULT (0),
        max_attempts INT                    NOT NULL CONSTRAINT DF_otp_challenges_max DEFAULT (5),
        expires_at   DATETIME2(3)           NOT NULL,
        verified_at  DATETIME2(3)           NULL,
        ip           VARCHAR(45)            NULL,
        created_at   DATETIME2(3)           NOT NULL CONSTRAINT DF_otp_challenges_created DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_otp_challenges PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_otp_challenges_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id)
    );
    CREATE INDEX IX_otp_challenges_lookup ON dbo.otp_challenges (store_id, phone_e164, id DESC);
END;
GO
