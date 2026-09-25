/* 0008 — кабинетный онбординг Kaspi: nado входит в кабинет владельца по SMS-коду
   и создаёт служебного сотрудника, через которого импортирует каталог (D-19, D-28).
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0008_kaspi_onboarding.sql

   Секреты (сессия входа владельца с cookie, пароль сотрудника) хранятся ТОЛЬКО
   зашифрованными (AES-256-GCM, пакет secrets), как токены провайдеров. Пароль
   владельца не хранится вовсе. */

IF OBJECT_ID(N'dbo.kaspi_onboarding', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.kaspi_onboarding
    (
        id                  BIGINT IDENTITY (1, 1) NOT NULL,
        account_id          BIGINT                 NOT NULL,
        store_name          NVARCHAR(200)          NOT NULL,
        phone               VARCHAR(32)            NOT NULL,   -- телефон владельца
        employee_name       NVARCHAR(200)          NOT NULL,
        employee_email      VARCHAR(320)           NULL,       -- s{id}-{rand}@<домен>, задаётся после вставки
        status              VARCHAR(30)            NOT NULL
            CONSTRAINT CK_kaspi_onboarding_status CHECK (status IN
                ('started','otp_sent','verifying','need_merchant','employee_created','catalog_queued','done','failed')),
        session_ciphertext  VARBINARY(MAX)         NULL,       -- зашифрованная OwnerSession (cookie входа)
        merchants_json      NVARCHAR(MAX)          NULL
            CONSTRAINT CK_kaspi_onboarding_merchants_json CHECK (merchants_json IS NULL OR ISJSON(merchants_json) = 1),
        merchant_id         VARCHAR(100)           NULL,
        store_id            BIGINT                 NULL,
        connection_id       BIGINT                 NULL,
        password_ciphertext VARBINARY(MAX)         NULL,       -- зашифрованный пароль сотрудника из письма
        error               NVARCHAR(1000)         NULL,
        created_at          DATETIME2(3)           NOT NULL CONSTRAINT DF_kaspi_onboarding_created DEFAULT (SYSUTCDATETIME()),
        updated_at          DATETIME2(3)           NOT NULL CONSTRAINT DF_kaspi_onboarding_updated DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_kaspi_onboarding PRIMARY KEY CLUSTERED (id)
    );

    CREATE INDEX IX_kaspi_onboarding_account ON dbo.kaspi_onboarding (account_id, id DESC);

    -- Письмо с паролем сотрудника ищется по его адресу; адрес уникален среди
    -- незавершённых онбордингов (фильтрованный уникальный индекс).
    CREATE UNIQUE INDEX UX_kaspi_onboarding_email
        ON dbo.kaspi_onboarding (employee_email)
        WHERE employee_email IS NOT NULL;
END;
GO
