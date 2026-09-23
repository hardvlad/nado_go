/* 0001_init — начальная схема.
   Скрипт идемпотентен: повторный запуск ничего не ломает. */

IF OBJECT_ID(N'dbo.users', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.users
    (
        id         BIGINT IDENTITY (1, 1) NOT NULL,
        email      NVARCHAR(255)          NOT NULL,
        name       NVARCHAR(150)          NOT NULL,
        status     VARCHAR(20)            NOT NULL CONSTRAINT DF_users_status DEFAULT ('active'),
        created_at DATETIME2(3)           NOT NULL CONSTRAINT DF_users_created_at DEFAULT (SYSUTCDATETIME()),
        updated_at DATETIME2(3)           NOT NULL CONSTRAINT DF_users_updated_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_users PRIMARY KEY CLUSTERED (id),
        CONSTRAINT CK_users_status CHECK (status IN ('active', 'blocked', 'archived'))
    );

    /* Уникальность email — на уровне БД: проверка в коде не спасает от гонки
       двух параллельных регистраций. */
    CREATE UNIQUE INDEX UX_users_email ON dbo.users (email);

    /* Индекс под сортировку списка (ORDER BY created_at DESC, id DESC). */
    CREATE INDEX IX_users_created_at ON dbo.users (created_at DESC, id DESC);
END;
GO

IF OBJECT_ID(N'dbo.audit_log', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.audit_log
    (
        id         BIGINT IDENTITY (1, 1) NOT NULL,
        entity     VARCHAR(50)            NOT NULL,
        entity_id  BIGINT                 NOT NULL,
        action     VARCHAR(50)            NOT NULL,
        actor      NVARCHAR(150)          NULL,
        created_at DATETIME2(3)           NOT NULL CONSTRAINT DF_audit_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_audit_log PRIMARY KEY CLUSTERED (id)
    );

    CREATE INDEX IX_audit_log_entity ON dbo.audit_log (entity, entity_id);
END;
GO

/* Демонстрационные данные — только если таблица пуста. */
IF NOT EXISTS (SELECT 1 FROM dbo.users)
BEGIN
    INSERT INTO dbo.users (email, name, status)
    VALUES (N'admin@example.com', N'Администратор', 'active'),
           (N'user@example.com', N'Тестовый пользователь', 'active');
END;
GO
