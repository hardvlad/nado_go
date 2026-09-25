/* 0004 — отправка OTP через WhatsApp (GreenAPI) и вход продавца по телефону.
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0004_messaging_otp.sql

   Инстансы WhatsApp — общий пул платформы ТОЛЬКО для OTP (D-23), к продавцам
   и магазинам не привязаны. Отправка идёт с активного незаблокированного
   инстанса; провайдер скрыт за интерфейсом messaging.Sender, чтобы позже
   заменить GreenAPI на официальный WABA. */

IF OBJECT_ID(N'dbo.whatsapp_instances', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.whatsapp_instances
    (
        id               BIGINT IDENTITY (1, 1) NOT NULL,
        provider         VARCHAR(20)            NOT NULL CONSTRAINT DF_whatsapp_instances_provider DEFAULT ('greenapi'),
        name             NVARCHAR(100)          NULL,
        api_domain       VARCHAR(100)           NOT NULL,  -- api.green-api.com или домен партнёра
        instance_id      VARCHAR(100)           NOT NULL,  -- idInstance
        token_ciphertext VARBINARY(512)         NOT NULL,  -- apiTokenInstance, AES-GCM (secrets)
        phone            VARCHAR(20)            NULL,      -- номер, привязанный к инстансу
        /* Состояние по данным GreenAPI: authorized, notAuthorized, blocked,
           sleepMode, starting, yellowCard. Отправка — только с authorized. */
        state            VARCHAR(30)            NULL,
        state_changed_at DATETIME2(3)           NULL,
        /* Ручное отключение администратором (например, номер под подозрением). */
        disabled         BIT                    NOT NULL CONSTRAINT DF_whatsapp_instances_disabled DEFAULT (0),
        last_sent_at     DATETIME2(3)           NULL,      -- для равномерного распределения
        last_error       NVARCHAR(1000)         NULL,
        fail_count       INT                    NOT NULL CONSTRAINT DF_whatsapp_instances_fail_count DEFAULT (0),
        created_at       DATETIME2(3)           NOT NULL CONSTRAINT DF_whatsapp_instances_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_whatsapp_instances PRIMARY KEY CLUSTERED (id),
        CONSTRAINT UX_whatsapp_instances_instance UNIQUE (provider, instance_id)
    );

    /* Выбор инстанса для отправки: активные, давно не отправлявшие. */
    CREATE INDEX IX_whatsapp_instances_pick ON dbo.whatsapp_instances (disabled, state, last_sent_at);
END;
GO

/* Журнал вызовов API мессенджера — разбор инцидентов и учёт стоимости.
   Тело запроса хранится БЕЗ кода OTP и токенов (их вырезает адаптер). */
IF OBJECT_ID(N'dbo.whatsapp_api_calls', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.whatsapp_api_calls
    (
        id          BIGINT IDENTITY (1, 1) NOT NULL,
        instance_id BIGINT                 NULL,
        method      VARCHAR(100)           NOT NULL,
        status_code INT                    NULL,
        duration_ms INT                    NULL,
        request     NVARCHAR(2000)         NULL,
        response    NVARCHAR(2000)         NULL,
        created_at  DATETIME2(3)           NOT NULL CONSTRAINT DF_whatsapp_api_calls_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_whatsapp_api_calls PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_whatsapp_api_calls_instance FOREIGN KEY (instance_id) REFERENCES dbo.whatsapp_instances (id)
    );

    CREATE INDEX IX_whatsapp_api_calls_created ON dbo.whatsapp_api_calls (created_at);
END;
GO

/* Одноразовые коды. Хранится только HMAC кода (secrets.Box.HMAC):
   утечка таблицы не раскрывает коды. */
IF OBJECT_ID(N'dbo.otp_codes', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.otp_codes
    (
        id           BIGINT IDENTITY (1, 1) NOT NULL,
        purpose      VARCHAR(20)            NOT NULL,  -- login | register
        phone        VARCHAR(20)            NOT NULL,  -- E.164
        code_hash    BINARY(32)             NOT NULL,
        channel      VARCHAR(20)            NOT NULL CONSTRAINT DF_otp_codes_channel DEFAULT ('whatsapp'),
        instance_id  BIGINT                 NULL,      -- с какого инстанса ушло
        message_id   VARCHAR(100)           NULL,      -- id сообщения у провайдера
        attempts     INT                    NOT NULL CONSTRAINT DF_otp_codes_attempts DEFAULT (0),
        max_attempts INT                    NOT NULL CONSTRAINT DF_otp_codes_max_attempts DEFAULT (5),
        expires_at   DATETIME2(3)           NOT NULL,
        used_at      DATETIME2(3)           NULL,
        ip           VARCHAR(45)            NULL,
        created_at   DATETIME2(3)           NOT NULL CONSTRAINT DF_otp_codes_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_otp_codes PRIMARY KEY CLUSTERED (id),
        CONSTRAINT CK_otp_codes_purpose CHECK (purpose IN ('login', 'register')),
        CONSTRAINT FK_otp_codes_instance FOREIGN KEY (instance_id) REFERENCES dbo.whatsapp_instances (id)
    );

    /* Поиск последнего действующего кода для номера и цели. */
    CREATE INDEX IX_otp_codes_phone ON dbo.otp_codes (phone, purpose, created_at DESC);
END;
GO

/* Подтверждённый телефон продавца — для входа по коду. */
IF COL_LENGTH(N'dbo.users', N'phone_verified_at') IS NULL
    ALTER TABLE dbo.users ADD phone_verified_at DATETIME2(3) NULL;
GO

/* Один подтверждённый номер — один пользователь: по номеру однозначно
   находится, кого пускать. Неподтверждённые номера могут совпадать. */
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'UX_users_verified_phone')
    CREATE UNIQUE INDEX UX_users_verified_phone ON dbo.users (phone)
        WHERE phone IS NOT NULL AND phone_verified_at IS NOT NULL;
GO

/* Регистрация по телефону: email становится необязательным (в PHP Users.Email
   тоже NULL, идентификатор — телефон). Уникальность email — только для
   заполненных значений. */
IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'UX_users_email' AND object_id = OBJECT_ID(N'dbo.users'))
    DROP INDEX UX_users_email ON dbo.users;
GO
IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID(N'dbo.users') AND name = N'email' AND is_nullable = 0)
    ALTER TABLE dbo.users ALTER COLUMN email NVARCHAR(255) NULL;
GO
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'UX_users_email' AND object_id = OBJECT_ID(N'dbo.users'))
    CREATE UNIQUE INDEX UX_users_email ON dbo.users (email) WHERE email IS NOT NULL;
GO
