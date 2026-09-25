/* 0003 — очередь фоновых задач и удалённые воркеры.
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0003_jobs.sql

   Задача выполняется либо на сервере (execution='local', её берёт встроенный
   воркер), либо на удалённом воркере (execution='remote', её выдаёт HTTP-API
   раздачи заданий). Удалённо выполняется всё, что связано с личным кабинетом
   Kaspi и отправкой OTP, — там же, где это делает исходный проект. */

IF OBJECT_ID(N'dbo.jobs', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.jobs
    (
        id           BIGINT IDENTITY (1, 1) NOT NULL,
        kind         VARCHAR(100)           NOT NULL,
        /* local — берёт встроенный воркер; remote — выдаётся удалённым по HTTP. */
        execution    VARCHAR(10)            NOT NULL CONSTRAINT DF_jobs_execution DEFAULT ('local'),
        account_id   BIGINT                 NULL,
        payload      NVARCHAR(MAX)          NOT NULL CONSTRAINT CK_jobs_payload_json CHECK (ISJSON(payload) = 1),
        result       NVARCHAR(MAX)          NULL CONSTRAINT CK_jobs_result_json CHECK (result IS NULL OR ISJSON(result) = 1),
        status       VARCHAR(20)            NOT NULL CONSTRAINT DF_jobs_status DEFAULT ('queued'),
        priority     INT                    NOT NULL CONSTRAINT DF_jobs_priority DEFAULT (100),
        run_at       DATETIME2(3)           NOT NULL CONSTRAINT DF_jobs_run_at DEFAULT (SYSUTCDATETIME()),
        attempts     INT                    NOT NULL CONSTRAINT DF_jobs_attempts DEFAULT (0),
        max_attempts INT                    NOT NULL CONSTRAINT DF_jobs_max_attempts DEFAULT (10),
        locked_by    VARCHAR(100)           NULL,
        locked_until DATETIME2(3)           NULL,
        dedup_key    VARCHAR(200)           NULL,
        last_error   NVARCHAR(2000)         NULL,
        created_at   DATETIME2(3)           NOT NULL CONSTRAINT DF_jobs_created_at DEFAULT (SYSUTCDATETIME()),
        finished_at  DATETIME2(3)           NULL,
        CONSTRAINT PK_jobs PRIMARY KEY CLUSTERED (id),
        CONSTRAINT CK_jobs_status CHECK (status IN ('queued', 'running', 'done', 'failed')),
        CONSTRAINT CK_jobs_execution CHECK (execution IN ('local', 'remote'))
    );

    /* Выборка готовых к запуску: по типу выполнения, времени и приоритету. */
    CREATE INDEX IX_jobs_ready ON dbo.jobs (execution, status, run_at, priority, id) INCLUDE (kind);

    /* Одна активная задача на ключ: «синхронизировать подключение 42» не должна
       стоять в очереди дважды. Фильтрованный индекс не мешает истории завершённых. */
    CREATE UNIQUE INDEX UX_jobs_dedup ON dbo.jobs (dedup_key)
        WHERE dedup_key IS NOT NULL AND status IN ('queued', 'running');
END;
GO

/* Удалённый воркер. Аутентифицируется постоянным токеном (в БД — только SHA-256),
   аналог InfraJobRunners из исходного проекта. */
IF OBJECT_ID(N'dbo.job_runners', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.job_runners
    (
        id           BIGINT IDENTITY (1, 1) NOT NULL,
        code         VARCHAR(100)           NOT NULL,
        token_hash   BINARY(32)             NOT NULL,
        /* Список типов задач через запятую, которые раннер имеет право брать;
           пусто — любые remote-задачи. */
        kinds        VARCHAR(1000)          NULL,
        disabled     BIT                    NOT NULL CONSTRAINT DF_job_runners_disabled DEFAULT (0),
        last_seen_at DATETIME2(3)           NULL,
        last_ip      VARCHAR(45)            NULL,
        created_at   DATETIME2(3)           NOT NULL CONSTRAINT DF_job_runners_created_at DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_job_runners PRIMARY KEY CLUSTERED (id),
        CONSTRAINT UX_job_runners_code UNIQUE (code),
        CONSTRAINT UX_job_runners_token UNIQUE (token_hash)
    );
END;
GO
