/* 0010 — домены магазинов: выбор витрины по Host (D-04, D-18).
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0010_store_domains.sql

   Каждому магазину при создании выдаётся поддомен <slug>.<домен платформы>.
   Свой домен продавца добавляется отдельно (kind='custom') с подтверждением. */

IF OBJECT_ID(N'dbo.store_domains', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.store_domains
    (
        id          BIGINT IDENTITY (1, 1) NOT NULL,
        store_id    BIGINT                 NOT NULL,
        host        VARCHAR(255)           NOT NULL,   -- нижний регистр, без порта
        kind        VARCHAR(10)            NOT NULL CONSTRAINT DF_store_domains_kind DEFAULT ('subdomain'),
        verify_token VARCHAR(64)           NULL,
        verified_at DATETIME2(3)           NULL,       -- для custom: подтверждён ли DNS
        is_primary  BIT                    NOT NULL CONSTRAINT DF_store_domains_primary DEFAULT (1),
        created_at  DATETIME2(3)           NOT NULL CONSTRAINT DF_store_domains_created DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_store_domains PRIMARY KEY CLUSTERED (id),
        CONSTRAINT UX_store_domains_host UNIQUE (host),
        CONSTRAINT FK_store_domains_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id),
        CONSTRAINT CK_store_domains_kind CHECK (kind IN ('subdomain', 'custom'))
    );
    CREATE INDEX IX_store_domains_store ON dbo.store_domains (store_id);
END;
GO
