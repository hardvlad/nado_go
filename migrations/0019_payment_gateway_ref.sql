/* 0019 — читабельный номер, передаваемый платёжному шлюзу. Скрипт идемпотентен.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0019_payment_gateway_ref.sql

   Раньше шлюзу уходил внутренний ref_token вида 'o-<hex>'. Теперь передаём
   читабельный номер из цифр (номер заказа магазина; для подписки — id платежа).
   ref_token остаётся внутренним глобально-уникальным ключом (номера заказов
   повторяются между магазинами), а в шлюз идёт gateway_ref. Вебхук ищет платёж
   по (store_id, gateway_ref) для заказов и по gateway_ref для подписки. */

IF COL_LENGTH('dbo.payments', 'gateway_ref') IS NULL
    ALTER TABLE dbo.payments ADD gateway_ref VARCHAR(64) NULL;
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'IX_payments_gateway_ref' AND object_id = OBJECT_ID('dbo.payments'))
    CREATE INDEX IX_payments_gateway_ref ON dbo.payments (store_id, gateway_ref);
GO
