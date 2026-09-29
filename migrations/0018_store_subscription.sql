/* 0018 — подписка на уровне магазина (не аккаунта). Скрипт идемпотентен.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0018_store_subscription.sql

   Подписка оформляется на каждый магазин отдельно, срок продления привязан к
   магазину (как и пробный период trial_ends_at). Поэтому статус/срок/тариф
   переезжают в dbo.stores. Колонки accounts.subscription_* из 0016 остаются
   (не используются) — их удаление отдельной задачей при чистке схемы. */

IF COL_LENGTH('dbo.stores', 'subscription_status') IS NULL
    ALTER TABLE dbo.stores ADD subscription_status VARCHAR(20) NOT NULL
        CONSTRAINT DF_stores_subscription_status DEFAULT ('trialing');
GO
IF COL_LENGTH('dbo.stores', 'subscription_until') IS NULL
    ALTER TABLE dbo.stores ADD subscription_until DATETIME2(3) NULL;
GO
IF COL_LENGTH('dbo.stores', 'plan_code') IS NULL
    ALTER TABLE dbo.stores ADD plan_code VARCHAR(30) NULL;
GO
