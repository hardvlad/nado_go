/* 0013 — бесплатный пробный период магазина и напоминания об оплате.
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0013_trial.sql

   При добавлении магазина ему даётся trial_ends_at = сейчас + N дней (N из
   переменной окружения). За 3 дня, за 1 день и в день окончания продавцу шлётся
   напоминание об оплате в WhatsApp; таблица trial_reminders хранит уже
   отправленные вехи (защита от повторной отправки). */

IF COL_LENGTH('dbo.stores', 'trial_ends_at') IS NULL
    ALTER TABLE dbo.stores ADD trial_ends_at DATETIME2(3) NULL;
GO

IF OBJECT_ID(N'dbo.trial_reminders', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.trial_reminders
    (
        store_id    BIGINT       NOT NULL,
        days_before INT          NOT NULL,   -- веха: 3 | 1 | 0
        sent_at     DATETIME2(3) NOT NULL CONSTRAINT DF_trial_reminders_sent DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_trial_reminders PRIMARY KEY CLUSTERED (store_id, days_before),
        CONSTRAINT FK_trial_reminders_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id)
    );
END;
GO
