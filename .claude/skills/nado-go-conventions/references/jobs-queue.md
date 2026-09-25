# Очередь фоновых задач в MS SQL Server

## Таблица

```sql
CREATE TABLE dbo.jobs
(
    id           BIGINT IDENTITY (1, 1) NOT NULL,
    kind         VARCHAR(100)           NOT NULL,  -- 'marketplace.sync_content', 'payment.poll', ...
    account_id   BIGINT                 NULL,      -- NULL для системных задач
    payload      NVARCHAR(MAX)          NOT NULL CONSTRAINT CK_jobs_payload_json CHECK (ISJSON(payload) = 1),
    status       VARCHAR(20)            NOT NULL CONSTRAINT DF_jobs_status DEFAULT ('queued'),
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
    CONSTRAINT CK_jobs_status CHECK (status IN ('queued', 'running', 'done', 'failed'))
);

-- Выборка готовых к запуску.
CREATE INDEX IX_jobs_ready ON dbo.jobs (status, run_at, id) INCLUDE (kind);

-- Одна активная задача на ключ: «синхронизировать подключение 42» не должна
-- стоять в очереди дважды. Фильтрованный индекс не мешает истории завершённых.
CREATE UNIQUE INDEX UX_jobs_dedup ON dbo.jobs (dedup_key)
    WHERE dedup_key IS NOT NULL AND status IN ('queued', 'running');
```

## Захват задачи

```sql
WITH next AS (
    SELECT TOP (1) *
    FROM dbo.jobs WITH (UPDLOCK, READPAST, ROWLOCK)
    WHERE (status = 'queued' AND run_at <= SYSUTCDATETIME())
       OR (status = 'running' AND locked_until < SYSUTCDATETIME())  -- упавший воркер
    ORDER BY run_at, id
)
UPDATE next
SET status = 'running',
    locked_by = @worker,
    locked_until = DATEADD(SECOND, @lease_seconds, SYSUTCDATETIME()),
    attempts = attempts + 1
OUTPUT INSERTED.id, INSERTED.kind, INSERTED.account_id, INSERTED.payload, INSERTED.attempts, INSERTED.max_attempts;
```

- `READPAST` пропускает строки, захваченные другими воркерами, без ожидания, а
  `UPDLOCK` не даёт двум воркерам взять одну строку.
- Долгая задача продлевает аренду (`locked_until`) через heartbeat, если работает
  дольше `lease/2`. Иначе после истечения аренды её подхватит второй воркер.
- Когда задач нет, опрашивай с интервалом 1–2 с. Отдельный механизм уведомлений
  не нужен.

## Завершение

- Успех: `status='done', finished_at=SYSUTCDATETIME(), locked_by=NULL`.
- Ошибка:
  - если `attempts < max_attempts`, то `status='queued'`,
    `run_at = now + backoff(attempts)` и `last_error`;
  - иначе `status='failed'`.
- Backoff: `min(2^attempts * 5s, 1h)` плюс джиттер ±20%.
- Для 429 от внешнего API задача переносится на время из `Retry-After`, а
  попытка не засчитывается.
- `jobs.Permanent(err)` сразу ставит `failed`. Применяй для неверного токена,
  удалённого подключения и подобного: повтор не поможет, нужна реакция
  продавца, поэтому покажи ошибку в кабинете.

## Постановка

```go
// В той же транзакции, что и изменение данных — задача не потеряется при
// падении между коммитом и отправкой в брокер (transactional outbox).
err := s.db.WithTx(ctx, nil, func(ctx context.Context, tx *sql.Tx) error {
    if err := s.conns.CreateTx(ctx, tx, accountID, conn); err != nil {
        return err
    }
    return s.jobs.EnqueueTx(ctx, tx, jobs.Job{
        Kind:      "marketplace.sync_content",
        AccountID: accountID,
        Payload:   map[string]any{"connection_id": conn.ID},
        DedupKey:  fmt.Sprintf("sync_content:%d", conn.ID),
    })
})
```

Нарушение `UX_jobs_dedup` при постановке означает, что задача уже стоит в очереди.
Это не ошибка: `EnqueueTx` возвращает nil.

## Периодические задачи

Отдельный cron не нужен. Планировщик — горутина, которая раз в минуту:
- ставит `marketplace.sync_offers` для подключений с `offers_sync_at <= now`, затем
  сдвигает `offers_sync_at` на интервал из тарифа;
- делает то же для контента, курсов валют (ежедневно), обновления ПВЗ служб
  доставки (ежедневно) и очистки старых `jobs`, `sync_runs` и `webhook_events`.

Благодаря `dedup_key` несколько экземпляров приложения не создадут дублей.

## Остановка

`jobs.Runner.Run(ctx)` работает до отмены ctx. После отмены он перестаёт брать
новые задачи и ждёт текущие до `JOBS_SHUTDOWN_TIMEOUT`. `app.shutdown` сначала
останавливает HTTP-сервер и воркеры, потом закрывает БД. Незавершённая задача
вернётся в очередь по истечении аренды.

## Тесты

Обработчики тестируются как обычные функции `func(ctx, Job) error` с
заглушками зависимостей. Сама очередь (SQL) проверяется интеграционным тестом с
тегом `integration`.
