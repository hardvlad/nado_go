/* 0007 — коды подтверждения входа в кабинет Kaspi, прочитанные из общего
   почтового ящика служебных сотрудников (IMAP, пакет internal/mailbox).
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0007_kaspi_mfa_codes.sql

   В отличие от кодов покупателей (dbo.otp_codes, хранится HMAC), этот код
   хранится ОТКРЫТЫМ ТЕКСТОМ: воркер должен предъявить его форме Kaspi при входе
   служебного сотрудника. Поэтому код короткоживущий и убирается по used_at и по
   сроку; ящик — инфраструктура платформы (catch-all служебных адресов), к
   продавцам не привязан. */

IF OBJECT_ID(N'dbo.kaspi_mfa_codes', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.kaspi_mfa_codes
    (
        id          BIGINT IDENTITY (1, 1) NOT NULL,
        email       VARCHAR(320)           NOT NULL,  -- адрес-получатель служебного сотрудника (кому пришёл код)
        code        VARCHAR(12)            NOT NULL,  -- открытый текст, короткоживущий
        received_at DATETIME2(3)           NOT NULL CONSTRAINT DF_kaspi_mfa_codes_received DEFAULT (SYSUTCDATETIME()),
        used_at     DATETIME2(3)           NULL,
        CONSTRAINT PK_kaspi_mfa_codes PRIMARY KEY CLUSTERED (id)
    );

    -- Поиск свежего неиспользованного кода по адресу идёт от новых к старым.
    CREATE INDEX IX_kaspi_mfa_codes_email ON dbo.kaspi_mfa_codes (email, id DESC);
END;
GO
