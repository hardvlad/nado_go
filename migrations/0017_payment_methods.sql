/* 0017 — методы оплаты магазина: несколько провайдеров одновременно (D-43).
   Скрипт идемпотентен.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0017_payment_methods.sql

   Заменяет одиночные настройки оплаты магазина (dbo.store_payment_settings из
   ранней версии 0016) на таблицу методов: у магазина может быть несколько
   активных способов оплаты, покупатель выбирает при оформлении. У каждого метода
   свой мерчант-аккаунт продавца (D-01), включение и токен вебхука. Отдельная
   миграция (а не правка 0016), потому что 0016 уже мог быть применён. */

IF OBJECT_ID(N'dbo.store_payment_methods', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.store_payment_methods
    (
        id                 BIGINT IDENTITY (1, 1) NOT NULL,
        store_id           BIGINT        NOT NULL,
        account_id         BIGINT        NOT NULL,
        provider           VARCHAR(20)   NOT NULL,
        is_enabled         BIT           NOT NULL CONSTRAINT DF_store_payment_methods_enabled DEFAULT (0),
        merchant_id        NVARCHAR(200) NULL,          -- ID мерчанта (Halyk: ClientID)
        terminal_id        NVARCHAR(200) NULL,          -- терминал (Halyk ePay: TerminalID)
        secret_ciphertext  VARBINARY(4000) NULL,       -- секретный ключ мерчанта, зашифрован
        testing_mode       BIT           NOT NULL CONSTRAINT DF_store_payment_methods_testing DEFAULT (0),
        webhook_token      VARCHAR(64)   NULL,          -- секрет в пути вебхука /webhooks/payment/{provider}/{token}
        sort               INT           NOT NULL CONSTRAINT DF_store_payment_methods_sort DEFAULT (0),
        created_at         DATETIME2(3)  NOT NULL CONSTRAINT DF_store_payment_methods_created DEFAULT (SYSUTCDATETIME()),
        updated_at         DATETIME2(3)  NOT NULL CONSTRAINT DF_store_payment_methods_updated DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_store_payment_methods PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_store_payment_methods_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id),
        CONSTRAINT FK_store_payment_methods_accounts FOREIGN KEY (account_id) REFERENCES dbo.accounts (id),
        CONSTRAINT UX_store_payment_methods_store_provider UNIQUE (store_id, provider),
        CONSTRAINT CK_store_payment_methods_provider CHECK (provider IN ('dev', 'freedompay', 'kaspi', 'halyk'))
    );
    CREATE INDEX IX_store_payment_methods_token ON dbo.store_payment_methods (provider, webhook_token);
END;
GO

/* Перенос ранее настроенного одиночного метода (если старая таблица есть) и её
   удаление. Перенос — динамическим SQL: набор колонок в ранних версиях
   store_payment_settings отличался (terminal_id добавлялся позже, токен был
   webhook_token_hash до webhook_token), поэтому статические ссылки на колонки
   не годятся — они не скомпилировались бы при их отсутствии. */
IF OBJECT_ID(N'dbo.store_payment_settings', N'U') IS NOT NULL
BEGIN
    IF COL_LENGTH('dbo.store_payment_settings', 'webhook_token') IS NOT NULL
    BEGIN
        DECLARE @term NVARCHAR(20) = CASE WHEN COL_LENGTH('dbo.store_payment_settings', 'terminal_id') IS NULL THEN N'NULL' ELSE N's.terminal_id' END;
        DECLARE @sql NVARCHAR(MAX) = N'
            INSERT INTO dbo.store_payment_methods
                (store_id, account_id, provider, is_enabled, merchant_id, terminal_id, secret_ciphertext, testing_mode, webhook_token)
            SELECT s.store_id, s.account_id, s.provider, s.is_enabled, s.merchant_id, ' + @term + N',
                   s.secret_ciphertext, s.testing_mode, s.webhook_token
            FROM dbo.store_payment_settings s
            WHERE NOT EXISTS (SELECT 1 FROM dbo.store_payment_methods m WHERE m.store_id = s.store_id AND m.provider = s.provider);';
        EXEC sys.sp_executesql @sql;
    END;

    DROP TABLE dbo.store_payment_settings;
END;
GO
