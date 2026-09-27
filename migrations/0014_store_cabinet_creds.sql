/* 0014 — данные кабинета на подключении и «собранные, но не сохранённые» реквизиты
   онбординга. Скрипт идемпотентен.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0014_store_cabinet_creds.sql

   По новому флоу (как в проекте на PHP, AddKaspiShop.php) сначала собираются все
   данные (e-mail и пароль служебного сотрудника, токен API), продавец видит их в
   форме и сохраняет магазин с проверкой. Поэтому:
   - у онбординга появляется token_ciphertext (токен собран, но магазин ещё не создан)
     и статус 'ready' (данные собраны, ждём подтверждения продавцом);
   - у подключения сохраняются реквизиты кабинета (для повторной проверки и
     редактирования): логин (e-mail сотрудника), зашифрованный пароль, uid кабинета. */

IF COL_LENGTH('dbo.kaspi_onboarding', 'token_ciphertext') IS NULL
    ALTER TABLE dbo.kaspi_onboarding ADD token_ciphertext VARBINARY(MAX) NULL;
GO

-- Расширяем список статусов онбординга статусом 'ready'.
IF OBJECT_ID('dbo.CK_kaspi_onboarding_status', 'C') IS NOT NULL
    ALTER TABLE dbo.kaspi_onboarding DROP CONSTRAINT CK_kaspi_onboarding_status;
GO
IF OBJECT_ID('dbo.CK_kaspi_onboarding_status', 'C') IS NULL
    ALTER TABLE dbo.kaspi_onboarding ADD CONSTRAINT CK_kaspi_onboarding_status CHECK (status IN
        ('started','otp_sent','verifying','need_merchant','employee_created','ready','catalog_queued','done','failed'));
GO

IF COL_LENGTH('dbo.marketplace_connections', 'cabinet_login') IS NULL
    ALTER TABLE dbo.marketplace_connections ADD cabinet_login VARCHAR(320) NULL;
GO
IF COL_LENGTH('dbo.marketplace_connections', 'cabinet_password_ciphertext') IS NULL
    ALTER TABLE dbo.marketplace_connections ADD cabinet_password_ciphertext VARBINARY(MAX) NULL;
GO
IF COL_LENGTH('dbo.marketplace_connections', 'merchant_uid') IS NULL
    ALTER TABLE dbo.marketplace_connections ADD merchant_uid VARCHAR(100) NULL;
GO
