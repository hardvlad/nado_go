/* 0002 — аккаунты продавцов, вход в кабинет, обратная связь.
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0002_accounts_auth_feedback.sql */

/* Аккаунт — арендатор платформы: продавец, ИП или компания (D-11). */
IF OBJECT_ID(N'dbo.accounts', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.accounts
    (
        id         BIGINT IDENTITY (1, 1) NOT NULL,
        name       NVARCHAR(200)          NOT NULL,
        country    CHAR(2)                NOT NULL CONSTRAINT DF_accounts_country DEFAULT ('KZ'),
        /* В какой инсталляции живут ПД аккаунта (D-13): kz — сервер в Казахстане. */
        region     VARCHAR(10)            NOT NULL CONSTRAINT DF_accounts_region DEFAULT ('kz'),
        plan_code  VARCHAR(30)            NOT NULL,
        status     VARCHAR(20)            NOT NULL CONSTRAINT DF_accounts_status DEFAULT ('active'),
        created_at DATETIME2(3)           NOT NULL CONSTRAINT DF_accounts_created_at DEFAULT (SYSUTCDATETIME()),
        updated_at DATETIME2(3)           NOT NULL CONSTRAINT DF_accounts_updated_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_accounts PRIMARY KEY CLUSTERED (id),
        CONSTRAINT CK_accounts_status CHECK (status IN ('active', 'suspended', 'closed')),
        CONSTRAINT CK_accounts_country CHECK (country IN ('KZ', 'RU'))
    );
END;
GO

/* Пользователь кабинета: поля для входа по паролю и настройки интерфейса. */
IF COL_LENGTH(N'dbo.users', N'password_hash') IS NULL
    ALTER TABLE dbo.users ADD password_hash VARCHAR(255) NULL;
GO
IF COL_LENGTH(N'dbo.users', N'phone') IS NULL
    ALTER TABLE dbo.users ADD phone VARCHAR(20) NULL;
GO
IF COL_LENGTH(N'dbo.users', N'locale') IS NULL
    ALTER TABLE dbo.users ADD locale VARCHAR(5) NULL;
GO
IF COL_LENGTH(N'dbo.users', N'ui_theme') IS NULL
    ALTER TABLE dbo.users ADD ui_theme VARCHAR(10) NOT NULL
        CONSTRAINT DF_users_ui_theme DEFAULT ('system')
        CONSTRAINT CK_users_ui_theme CHECK (ui_theme IN ('system', 'light', 'dark'));
GO
IF COL_LENGTH(N'dbo.users', N'last_login_at') IS NULL
    ALTER TABLE dbo.users ADD last_login_at DATETIME2(3) NULL;
GO

/* Участник аккаунта. Роли проверяются в service. */
IF OBJECT_ID(N'dbo.account_members', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.account_members
    (
        account_id BIGINT       NOT NULL,
        user_id    BIGINT       NOT NULL,
        role       VARCHAR(20)  NOT NULL,
        created_at DATETIME2(3) NOT NULL CONSTRAINT DF_account_members_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_account_members PRIMARY KEY CLUSTERED (account_id, user_id),
        CONSTRAINT FK_account_members_accounts FOREIGN KEY (account_id) REFERENCES dbo.accounts (id),
        CONSTRAINT FK_account_members_users FOREIGN KEY (user_id) REFERENCES dbo.users (id),
        CONSTRAINT CK_account_members_role CHECK (role IN ('owner', 'admin', 'manager', 'viewer'))
    );

    /* Поиск аккаунтов пользователя при входе. */
    CREATE INDEX IX_account_members_user ON dbo.account_members (user_id);
END;
GO

/* Серверные сессии кабинета. id — SHA-256 токена из cookie: сам токен в БД
   не хранится, поэтому утечка таблицы не даёт войти под чужой сессией. */
IF OBJECT_ID(N'dbo.sessions', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.sessions
    (
        id         BINARY(32)     NOT NULL,
        user_id    BIGINT         NOT NULL,
        account_id BIGINT         NOT NULL,
        expires_at DATETIME2(3)   NOT NULL,
        ip         VARCHAR(45)    NULL,
        user_agent NVARCHAR(400)  NULL,
        created_at DATETIME2(3)   NOT NULL CONSTRAINT DF_sessions_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_sessions PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_sessions_users FOREIGN KEY (user_id) REFERENCES dbo.users (id),
        CONSTRAINT FK_sessions_accounts FOREIGN KEY (account_id) REFERENCES dbo.accounts (id)
    );

    CREATE INDEX IX_sessions_user ON dbo.sessions (user_id);
    /* Под периодическую очистку просроченных сессий. */
    CREATE INDEX IX_sessions_expires_at ON dbo.sessions (expires_at);
END;
GO

/* Обращения с формы обратной связи на лендинге. */
IF OBJECT_ID(N'dbo.feedback_messages', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.feedback_messages
    (
        id         BIGINT IDENTITY (1, 1) NOT NULL,
        name       NVARCHAR(150)          NOT NULL,
        contact    NVARCHAR(255)          NOT NULL,
        topic      VARCHAR(20)            NOT NULL,
        message    NVARCHAR(4000)         NOT NULL,
        lang       VARCHAR(5)             NOT NULL,
        status     VARCHAR(20)            NOT NULL CONSTRAINT DF_feedback_messages_status DEFAULT ('new'),
        ip         VARCHAR(45)            NULL,
        user_agent NVARCHAR(400)          NULL,
        created_at DATETIME2(3)           NOT NULL CONSTRAINT DF_feedback_messages_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_feedback_messages PRIMARY KEY CLUSTERED (id),
        CONSTRAINT CK_feedback_messages_topic CHECK (topic IN ('question', 'suggestion', 'partnership', 'other')),
        CONSTRAINT CK_feedback_messages_status CHECK (status IN ('new', 'read', 'answered', 'spam'))
    );

    CREATE INDEX IX_feedback_messages_status ON dbo.feedback_messages (status, created_at DESC);
END;
GO
