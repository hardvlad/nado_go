/* 0015 — защита правок продавца от перезаписи при повторной синхронизации.
   Скрипт идемпотентен: повторный запуск ничего не ломает.

   Применение:
     sqlcmd -S <сервер> -d nado -U <пользователь> -P <пароль> -C -i migrations/0015_product_overrides.sql

   Продавец редактирует товары и категории в кабинете. Товары, пришедшие из
   зеркала Kaspi (products.source_sku IS NOT NULL), перестраиваются задачей
   catalog.build_store из marketplace_products. Чтобы правки не затирались,
   в products добавляется overridden_fields — JSON-массив имён «переопределённых»
   полей. Построение каталога (BuildProduct) не трогает поля из этого списка.

   Значения токенов: 'category', 'brand', 'status', 'content', 'images'.
   ('content' покрывает перевод целиком — название, описание, slug, SEO по всем
   языкам; отдельные языки одним флагом, потому что построение пишет перевод одной
   строкой на язык магазина по умолчанию.) */

IF COL_LENGTH('dbo.products', 'overridden_fields') IS NULL
    ALTER TABLE dbo.products
        ADD overridden_fields NVARCHAR(MAX) NULL
        CONSTRAINT CK_products_overridden_fields_json CHECK (overridden_fields IS NULL OR ISJSON(overridden_fields) = 1);
GO
