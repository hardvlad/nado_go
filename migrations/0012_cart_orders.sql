/* 0012 — корзина и заказы витрины (D-17). Скрипт идемпотентен.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0012_cart_orders.sql

   Корзину можно собрать без входа (по токену в cookie); при входе она
   привязывается к покупателю. Состав хранится на сервере, цены пересчитываются
   на каждом шаге из store_offers — клиенту не доверяем. Заказ фиксирует снимок
   позиций и сумм; оплата/доставка подключаются отдельно (заказ создаётся в
   статусе awaiting_payment). */

IF OBJECT_ID(N'dbo.carts', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.carts
    (
        id          BIGINT IDENTITY (1, 1) NOT NULL,
        store_id    BIGINT                 NOT NULL,
        token       VARCHAR(64)            NOT NULL,   -- случайный идентификатор из cookie
        customer_id BIGINT                 NULL,       -- заполняется при входе
        created_at  DATETIME2(3)           NOT NULL CONSTRAINT DF_carts_created DEFAULT (SYSUTCDATETIME()),
        updated_at  DATETIME2(3)           NOT NULL CONSTRAINT DF_carts_updated DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_carts PRIMARY KEY CLUSTERED (id),
        CONSTRAINT UX_carts_token UNIQUE (token),
        CONSTRAINT FK_carts_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id)
    );
END;
GO

IF OBJECT_ID(N'dbo.cart_items', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.cart_items
    (
        cart_id    BIGINT       NOT NULL,
        variant_id BIGINT       NOT NULL,
        qty        INT          NOT NULL CONSTRAINT DF_cart_items_qty DEFAULT (1),
        added_at   DATETIME2(3) NOT NULL CONSTRAINT DF_cart_items_added DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_cart_items PRIMARY KEY CLUSTERED (cart_id, variant_id),
        CONSTRAINT FK_cart_items_cart FOREIGN KEY (cart_id) REFERENCES dbo.carts (id),
        CONSTRAINT FK_cart_items_variant FOREIGN KEY (variant_id) REFERENCES dbo.variants (id),
        CONSTRAINT CK_cart_items_qty CHECK (qty > 0)
    );
END;
GO

IF OBJECT_ID(N'dbo.orders', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.orders
    (
        id             BIGINT IDENTITY (1, 1) NOT NULL,
        store_id       BIGINT                 NOT NULL,
        account_id     BIGINT                 NOT NULL,
        number         BIGINT                 NOT NULL,   -- порядковый номер в пределах магазина
        token          VARCHAR(64)            NOT NULL,   -- секрет для доступа к странице заказа
        status         VARCHAR(30)            NOT NULL CONSTRAINT DF_orders_status DEFAULT ('awaiting_payment'),
        customer_id    BIGINT                 NULL,
        customer_name  NVARCHAR(200)          NULL,       -- снимок на момент заказа
        customer_phone VARCHAR(20)            NULL,
        customer_email NVARCHAR(320)          NULL,
        address_json   NVARCHAR(MAX)          NULL CONSTRAINT CK_orders_address_json CHECK (address_json IS NULL OR ISJSON(address_json) = 1),
        comment        NVARCHAR(1000)         NULL,
        subtotal_minor BIGINT                 NOT NULL CONSTRAINT DF_orders_subtotal DEFAULT (0),
        total_minor    BIGINT                 NOT NULL CONSTRAINT DF_orders_total DEFAULT (0),
        currency       CHAR(3)                NOT NULL CONSTRAINT DF_orders_currency DEFAULT ('KZT'),
        lang           VARCHAR(2)             NOT NULL CONSTRAINT DF_orders_lang DEFAULT ('ru'),
        created_at     DATETIME2(3)           NOT NULL CONSTRAINT DF_orders_created DEFAULT (SYSUTCDATETIME()),
        CONSTRAINT PK_orders PRIMARY KEY CLUSTERED (id),
        CONSTRAINT UX_orders_number UNIQUE (store_id, number),
        CONSTRAINT FK_orders_stores FOREIGN KEY (store_id) REFERENCES dbo.stores (id),
        CONSTRAINT CK_orders_status CHECK (status IN
            ('new','awaiting_payment','paid','processing','shipped','delivered','completed','canceled','refunded'))
    );
    CREATE INDEX IX_orders_customer ON dbo.orders (customer_id, id DESC);
    CREATE INDEX IX_orders_store ON dbo.orders (store_id, created_at DESC);
END;
GO

IF OBJECT_ID(N'dbo.order_items', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.order_items
    (
        id            BIGINT IDENTITY (1, 1) NOT NULL,
        order_id      BIGINT                 NOT NULL,
        variant_id    BIGINT                 NOT NULL,
        title_snapshot NVARCHAR(1000)        NOT NULL,
        sku_snapshot  VARCHAR(100)           NULL,
        qty           INT                    NOT NULL,
        price_minor   BIGINT                 NOT NULL,   -- цена за единицу на момент заказа
        CONSTRAINT PK_order_items PRIMARY KEY CLUSTERED (id),
        CONSTRAINT FK_order_items_order FOREIGN KEY (order_id) REFERENCES dbo.orders (id)
    );
    CREATE INDEX IX_order_items_order ON dbo.order_items (order_id);
END;
GO
