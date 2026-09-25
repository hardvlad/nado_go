CREATE TABLE Settings
(
    ID int NOT NULL IDENTITY(1,1),
    Name text NOT NULL,
    Setting text NOT NULL,

    PRIMARY KEY(ID)
)

insert into Settings (Name,Setting) values ('SessionCookieName','PROFITBOTSESSIONCOOKIE')
insert into Settings (Name,Setting) values ('UserSessionLength','32')

CREATE TABLE Regions
(
    ID int NOT NULL IDENTITY(1,1),
    Name nvarchar(250) NOT NULL,
    CountryCode nvarchar(10) NOT NULL,

    PRIMARY KEY(ID)
)

insert into Regions (Name,CountryCode) values ('Kazakhstan','7')

CREATE TABLE Entries
(
    ID int NOT NULL IDENTITY(1,1),
    SessionID int NOT NULL,
    Name nvarchar(250) NULL,
    Phone nvarchar(250) NULL,
    Email nvarchar(250) NULL,

    PRIMARY KEY(ID)
)

CREATE TABLE Sources
(
    ID int NOT NULL IDENTITY(1,1),
    Name varchar(100) NOT NULL,

    PRIMARY KEY(ID)
)

SET IDENTITY_INSERT Sources ON
GO
SET IDENTITY_INSERT Sources ON
INSERT INTO Sources (ID, Name)VALUES (1, 'Web site')
INSERT INTO Sources (ID, Name)VALUES (2, 'iOS')
INSERT INTO Sources (ID, Name) VALUES (3, 'Android')
SET IDENTITY_INSERT Sources OFF
GO

create table UserRoles
(
    ID int NOT NULL IDENTITY(1,1),
    Name nvarchar(100) NOT NULL,
    Description nvarchar(500) NULL,

    PRIMARY KEY(ID)
)

insert into UserRoles (Name,Description) values ('admin',N'Администратор системы')
insert into UserRoles (Name,Description) values ('sales',N'Менеджер по продажам')

CREATE TABLE ReferralTerms
(
    ID int NOT NULL IDENTITY(1,1),
    [Percent] int NOT NULL,
    IsActive bit NOT NULL,
    IsDefault bit NOT NULL,

    PRIMARY KEY(ID)
)

insert into ReferralTerms ([Percent],IsActive,IsDefault) values (20,1,1)

CREATE TABLE Users
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Phone nvarchar(25) NOT NULL,
    FirstName nvarchar(150) DEFAULT NULL,
    LastName nvarchar(150) DEFAULT NULL,
    Email nvarchar(150) DEFAULT NULL,
    EmailConfirmed int DEFAULT NULL,
    LangSetting nvarchar(2) DEFAULT NULL,
    DeletedDate DateTime NULL,
    NotificationChannelPush bit NOT NULL DEFAULT 0,
    ExternalID varchar(40) NOT NULL,
    IsNameConfirmed bit DEFAULT NULL,
    AuthPhoneConfirmed bit DEFAULT NULL,
    AuthEmailConfirmed bit DEFAULT NULL,
    DisplayName nvarchar(250) DEFAULT NULL,
    PasswordHash nvarchar(250) DEFAULT NULL,
    TelegramConnectionString nvarchar(50) DEFAULT NULL,

    RoleID int NULL,

    ReferralCode nvarchar(100) NULL,
    RegisteredByRefUserID int NULL,
    ReferralTermID int NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(RoleID) REFERENCES UserRoles(ID),
    FOREIGN KEY(RegisteredByRefUserID) REFERENCES Users(ID),
    FOREIGN KEY(ReferralTermID) REFERENCES ReferralTerms(ID)
)

insert into Users (Phone,FirstName,LastName,Email,EmailConfirmed,LangSetting,DeletedDate,NotificationChannelPush,ExternalID,IsNameConfirmed,AuthPhoneConfirmed,AuthEmailConfirmed,DisplayName)
values ('+77000000000','Test','Test','test@test.com',1,'ru',NULL,1,'test',1,1,1,'Test Test')

insert into Users (Phone,FirstName,LastName,Email,EmailConfirmed,LangSetting,DeletedDate,NotificationChannelPush,ExternalID,IsNameConfirmed,AuthPhoneConfirmed,AuthEmailConfirmed,DisplayName)
values ('+77000000001','Test1','Test1','test1@test.com',1,'ru',NULL,1,'test',1,1,1,'Test1 Test1')

CREATE TABLE Sessions
(
    ID int NOT NULL IDENTITY(1,1),
    SessionID nvarchar(32) NOT NULL,
    Date datetime NOT NULL,
    LastActivityDate int NOT NULL,
    IP nvarchar(15) NOT NULL,
    CountryCode nvarchar(2) DEFAULT NULL,
    Continent_en nvarchar(50) DEFAULT NULL,
    Country_en nvarchar(50) DEFAULT NULL,
    City_en nvarchar(50) DEFAULT NULL,
    Lat decimal(15,9) DEFAULT NULL,
    Lon decimal(15,9) DEFAULT NULL,
    UserAgent text DEFAULT NULL,
    Version int DEFAULT NULL,
    PaymentFromArabic int DEFAULT NULL,
    RoistatVisitID int DEFAULT NULL,
    SourceID int DEFAULT NULL,
    AppVersion int DEFAULT NULL,
    UserID int DEFAULT NULL,
    LogoffDate int DEFAULT NULL,
    RegionID int DEFAULT NULL,

    AdminUserID int NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(SourceID) REFERENCES Sources(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
    FOREIGN KEY(RegionID) REFERENCES Regions(ID),
    FOREIGN KEY(AdminUserID) REFERENCES Users(ID)
)

CREATE NONCLUSTERED INDEX Sessions_SessionID_LogoffDate_index ON [dbo].[Sessions] ([SessionID],[LogoffDate])

CREATE TABLE SessionsRefererUser
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL default GetDate(),
    SessionID int NOT NULL,
    RefererUserID int NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(SessionID) REFERENCES Sessions(ID),
    FOREIGN KEY(RefererUserID) REFERENCES Users(ID)
)

CREATE TABLE ThrottlingMethods
(
    ID int NOT NULL IDENTITY(1,1),
    MethodName nvarchar(255) NOT NULL,
    PerSession int NOT NULL,
    PerSessionCount int NOT NULL,
    PerSessionPeriod int NOT NULL,
    PerIP int NOT NULL,
    PerIPCount int NOT NULL,
    PerIPPeriod int NOT NULL,
    ErrorEn nvarchar(1000) DEFAULT NULL,
    ErrorAr nvarchar(1000) DEFAULT NULL,

    PRIMARY KEY(ID)
)

CREATE TABLE ThrottlingMethodCalls
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    SessionID int NOT NULL,
    IP nvarchar(255) NOT NULL,
    MethodID int NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(SessionID) REFERENCES Sessions(ID),
    FOREIGN KEY(MethodID) REFERENCES ThrottlingMethods(ID),
)

create index ThrottlingMethodCalls_Date_Index on ThrottlingMethodCalls(Date)
create index ThrottlingMethodCalls_IP_Index on ThrottlingMethodCalls(IP)

create table KaspiCategories
(
    ID int NOT NULL IDENTITY(1,1),

    Date datetime NOT NULL DEFAULT GetDate(),
    KaspiID nvarchar(100) NOT NULL,
    Level INT NOT NULL,
    ParentID int NULL,
    Name nvarchar(1000) NOT NULL,
    CommissionStart decimal(15,3) NOT NULL,
    CommissionEnd decimal(15,3) NOT NULL

        PRIMARY KEY(ID)
)

alter table KaspiCategories add FOREIGN KEY(ParentID) REFERENCES KaspiCategories(ID)
create index KaspiCategories_KaspiID_Index on KaspiCategories(KaspiID)

create table KaspiMasterProducts
(
    ID int NOT NULL IDENTITY(1,1),

    Date datetime NOT NULL DEFAULT GetDate(),
    KaspiID nvarchar(100) NOT NULL,
    Code nvarchar(100) NOT NULL,
    Name nvarchar(2000) NOT NULL,
    FirstImage nvarchar(200) NULL,
    Images nvarchar(MAX) NULL,
    Brand nvarchar(200) NULL,

    LastPriceCheckDate datetime NULL,
    OffersCount int NULL,
    LastPriceUpdateDate datetime NULL,
    PreviousPriceUpdateDate datetime NULL,

    LastPriceNoDumpingCheckDate datetime NULL,

    PRIMARY KEY(ID)
)

create index KaspiMasterProducts_KaspiID_Index on KaspiMasterProducts(KaspiID)
create index KaspiMasterProducts_Code_Index on KaspiMasterProducts(Code)

create table KaspiMasterProductsVesions
(
    ID int NOT NULL IDENTITY(1,1),

    MasterProductID int NOT NULL,
    Date datetime NOT NULL DEFAULT GetDate(),
    KaspiID nvarchar(100) NOT NULL,
    Code nvarchar(100) NOT NULL,
    Name nvarchar(2000) NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(MasterProductID) REFERENCES KaspiMasterProducts(ID)
)

create table KaspiCustomers
(
    ID int NOT NULL IDENTITY(1,1),

    Date datetime NOT NULL DEFAULT GetDate(),
    KaspiID nvarchar(100) NOT NULL,
    Name nvarchar(200) NOT NULL,
    Phone nvarchar(200) NOT NULL,
    FirstName nvarchar(200) NOT NULL,
    LastName nvarchar(200) NOT NULL,

    PRIMARY KEY(ID)
)

create index KaspiCustomers_KaspiID_Index on KaspiCustomers(KaspiID)

create table KaspiCustomersVersions
(
    ID int NOT NULL IDENTITY(1,1),

    CustomerID int NOT NULL,
    Date datetime NOT NULL DEFAULT GetDate(),
    KaspiID nvarchar(100) NOT NULL,
    Name nvarchar(200) NOT NULL,
    Phone nvarchar(200) NOT NULL,
    FirstName nvarchar(200) NOT NULL,
    LastName nvarchar(200) NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(CustomerID) REFERENCES KaspiCustomers(ID)
)

create index KaspiCustomersVersions_KaspiID_Index on KaspiCustomersVersions(KaspiID)

create table KaspiTowns
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Name nvarchar(200) NOT NULL,

    PRIMARY KEY(ID)
)

create table KaspiCustomersAdresses
(
    ID int NOT NULL IDENTITY(1,1),

    CustomerID int NOT NULL,
    Date datetime NOT NULL DEFAULT GetDate(),
    StreetName nvarchar(400) NULL,
    StreetNumber nvarchar(400) NULL,
    TownID int NOT NULL,
    District nvarchar(400) NULL,
    Building nvarchar(400) NULL,
    Apartment nvarchar(400) NULL,
    FormattedAddress nvarchar(800) NULL,
    Latitude decimal(20,17) NULL,
    Longitude decimal(20,17) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(CustomerID) REFERENCES KaspiCustomers(ID),
    FOREIGN KEY(TownID) REFERENCES KaspiTowns(ID)
)

create index KaspiCustomersAdresses_FormattedAddress_Index on KaspiCustomersAdresses(FormattedAddress)

create table KaspiPaymentModes
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Name nvarchar(200) NOT NULL,

    PRIMARY KEY(ID)
)

insert into KaspiPaymentModes (Name) values ('PAY_WITH_CREDIT'), ('PREPAID');

create table KaspiDeliveryModes
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Name nvarchar(200) NOT NULL,

    PRIMARY KEY(ID)
)

insert into KaspiDeliveryModes (Name) values ('DELIVERY_LOCAL'), ('DELIVERY_PICKUP'), ('DELIVERY_REGIONAL_PICKUP'), ('DELIVERY_REGIONAL_TODOOR');

create table KaspiOrderStates
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Name nvarchar(200) NOT NULL,

    PRIMARY KEY(ID)
)

insert into KaspiOrderStates (Name) values ('NEW'), ('SIGN_REQUIRED'), ('PICKUP'), ('DELIVERY'), ('KASPI_DELIVERY'), ('ARCHIVE');

create table KaspiOrderStatuses
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Name nvarchar(200) NOT NULL,
    CountAsSold bit NOT NULL DEFAULT 1,
    CountAsReturn bit NOT NULL DEFAULT 0,

    PRIMARY KEY(ID)
)

insert into KaspiOrderStatuses (Name) values ('APPROVED_BY_BANK'), ('ACCEPTED_BY_MERCHANT'), ('COMPLETED'), ('CANCELLED'), ('CANCELLING'), ('KASPI_DELIVERY_RETURN_REQUESTED'), ('RETURNED');
update KaspiOrderStatuses set CountAsSold = 0 where Name in ('CANCELLED','CANCELLING')
update KaspiOrderStatuses set CountAsReturn = 1 where Name in ('KASPI_DELIVERY_RETURN_REQUESTED','RETURNED')

create table KaspiPickupPointIDS
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Name nvarchar(200) NOT NULL,

    PRIMARY KEY(ID)
)

create table UserKaspiShops
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    UserID INT NOT NULL,
    Name nvarchar(200) NOT NULL,
    Token nvarchar(200) NOT NULL,
    IsTokenValid bit NOT NULL,
    FirstImportFinished bit NOT NULL,
    FirstImportStartDate datetime NULL,
    FirstImportEndDate datetime NULL,
    CurrentImportOrdersDate datetime NULL,
    DeletedDate datetime NULL,
    UseLKAccess bit NULL,
    LKEmail nvarchar(200) NULL,
    LKPassword nvarchar(200) NULL,
    LKAccessData nvarchar(MAX) NULL,
    LKAccessDataRenewDate datetime NULL,
    LKAccessActive bit NULL,
    LKAccessLastDate datetime NULL,
    LKItemsDownloaded bit NULL,
    LKItemsDownloadedDate datetime NULL,
    KaspiDownloadCode nvarchar(200) NULL,
    KaspiMerchantID nvarchar(200) NULL,

    TaxPercent int NOT NULL default 3,

    DoNotSendMerchantHeader bit NULL,

    UseMarketingAccess bit NULL,
    MarketingLogin nvarchar(200) NULL,
    MarketingPassword nvarchar(200) NULL,
    MarketingSession nvarchar(MAX) NULL,
    MarketingLastUpdated datetime NULL,
    MarketingFirstImportFinished bit NULL,
    MarketingFirstImportStartDate datetime NULL,
    MarketingFirstImportEndDate datetime NULL,
    MarketingMerchantID nvarchar(200) NULL,

    GoodsLoadRequested bit NULL,
    GoodsLoadRequestDate datetime NULL,
    GoodsLoadAssignedDate datetime NULL,
    GoodsLoadTotalItems int NULL,
    GoodsLoadLoadedItems int NULL,
    GoodsLoadFinishedDate datetime NULL,
    GoodsLoadSuccess bit NULL,

    DoSendKaspiPrice bit NOT NULL default 0,
    LastKaspiPriceSendDate datetime NOT NULL default GetDate(),
    SendKaspiPriceAssignedDate datetime NULL,
    SendKaspiPriceFinishedDate datetime NULL,
    SendKaspiPriceSuccess bit NULL,
    SendKaspiPriceRequested bit NULL,

    First2YImportFinished bit NOT NULL default 0,
    First2YImportStartDate datetime NULL,
    First2YImportEndDate datetime NULL,

    DumpingGlobalExcludeMerchants nvarchar(max),

    DumpingGlobalFilterByDelivery bit NULL,
    DumpingGlobalMinDelivery int NULL,
    DumpingGlobalFilterByRating bit NULL,
    DumpingGlobalMinRating decimal(15,1) NULL,
    DumpingGlobalFilterByReviews bit NULL,
    DumpingGlobalMinReviews int NULL,

    DumpingAllowPriceIncreaseAlways int NULL

    PRIMARY KEY(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
)

create table UserKaspiShopsUploadPriceTasks
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    ShopID INT NOT NULL,
    FinishedDate datetime NULL,
    PriceID nvarchar(200) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID)
)

--insert into UserKaspiShops (UserID,Name,Token) values (1,'TehnoStyle','iaxDvLibXn4AhhBMnzavpqZ/3jW53SRgjHn6klZaVJk=')
--insert into UserKaspiShops (UserID,Name,Token) values (1,'Elite','bv98G2ZYfRhed8k3QjSYfPqZF4oGxPKPXrncwOpxuts=')
--insert into UserKaspiShops (UserID,Name,Token) values (2,'Mekom','k3SrR+HeZ9vhfX6q1GrbIVI6y86A7GmC0fx0/U/fQYw=')

create table UserKaspiShopsStores
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    ShopID INT NOT NULL,
    Code nvarchar(100) NULL,
    Name nvarchar(200) NOT NULL,
    DeletedDate datetime NULL,
    Address nvarchar(200) NOT NULL default '',
    AddressLink nvarchar(200) NOT NULL default '',
    CityName nvarchar(100) NOT NULL default '',
    StartOperationID INT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID)
)


create table OrderStatuses
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Name nvarchar(100) NOT NULL,

    PRIMARY KEY(ID)
)

insert into OrderStatuses (Name) values (N'Новый'), (N'Передача'), (N'Доставка'), (N'Отменен'),(N'Возврат'), (N'Доставлен')

create table KaspiOrders
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    ShopID INT NOT NULL,
    KaspiID nvarchar(100) NOT NULL,
    Code nvarchar(100) NULL,
    TotalPrice decimal (15,2) NULL,
    PaymentModeID INT NULL,
    PlannedDeliveryDate DateTime NULL,
    PlannedDeliveryDateTS int NULL,
    CreationDate DateTime NULL,
    CreationDateTS int NULL,
    DeliveryCostForSeller decimal (15,2) NULL,
    IsKaspiDelivery bit NULL,
    DeliveryModeID int NULL,
    SignatureRequired bit NULL,
    CreditTerm INT NULL,
    PreOrder bit NULL,
    PickupPointID int NULL,
    ApprovedByBankDate DateTime NULL,
    ApprovedByBankDateTS int NULL,
    StateID int NULL,
    StatusID int NULL,
    DeliveryCost decimal (15,2) NULL,
    CustomerID int NULL,
    CustomerVersionID int NULL,
    CustomerAddressID int NULL,
    StoreID int NULL,
    LastOrderResponseHash nvarchar(100) NULL,
    LastDetailsResponseHash nvarchar(100) NULL,

    Assembled bit NULL,
    AssembledDate datetime NULL,
    CourierTransmissionPlanningDateTS int NULL,
    CourierTransmissionPlanningDate datetime NULL,
    CourierTransmissionDateTS int NULL,
    CourierTransmissionDate datetime NULL,
    WaybillNumber nvarchar(100) NULL,
    WaybillNumberSetDate datetime NULL,
    Express bit NULL,
    ReturnedToWarehouse bit NULL,
    FirstMileCourier bit NULL,

    OrderStatusID int NULL,

    ActualDeliveryDate DateTime NULL,
    ActualDeliveryDateTS int NULL,

    ActualReturnDate DateTime NULL,
    ActualReturnDateTS int NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID),
    FOREIGN KEY(PaymentModeID) REFERENCES KaspiPaymentModes(ID),
    FOREIGN KEY(DeliveryModeID) REFERENCES KaspiDeliveryModes(ID),
    FOREIGN KEY(PickupPointID) REFERENCES KaspiPickupPointIDS(ID),
    FOREIGN KEY(StateID) REFERENCES KaspiOrderStates(ID),
    FOREIGN KEY(StatusID) REFERENCES KaspiOrderStatuses(ID),
    FOREIGN KEY(CustomerID) REFERENCES KaspiCustomers(ID),
    FOREIGN KEY(CustomerVersionID) REFERENCES KaspiCustomersVersions(ID),
    FOREIGN KEY(CustomerAddressID) REFERENCES KaspiCustomersAdresses(ID),
    FOREIGN KEY(StoreID) REFERENCES UserKaspiShopsStores(ID),
    FOREIGN KEY(OrderStatusID) REFERENCES OrderStatuses(ID)
)

create index KaspiOrders_KaspiID_Index on KaspiOrders(KaspiID)
create index KaspiOrders_Code_Index on KaspiOrders(Code)
create index KaspiOrders_CreationDate_Index on KaspiOrders(CreationDate)
create index KaspiOrders_CreditTerm_Index on KaspiOrders(CreditTerm)
create index KaspiOrders_ApprovedByBankDate_Index on KaspiOrders(ApprovedByBankDate)
CREATE NONCLUSTERED INDEX KaspiOrders_StateID_ActualDeliveryDateTS_Index ON KaspiOrders (StateID,ActualDeliveryDateTS) INCLUDE (ShopID,CreationDateTS)
CREATE NONCLUSTERED INDEX KaspiOrders_ActualDeliveryDate_Index ON [dbo].[KaspiOrders] ([ActualDeliveryDate]) INCLUDE ([ShopID],[TotalPrice],[StatusID])
CREATE NONCLUSTERED INDEX KaspiOrders_StateID_ActualReturnDateTS_Index ON KaspiOrders (StateID,ActualReturnDateTS) INCLUDE (ShopID,CreationDateTS)
CREATE NONCLUSTERED INDEX KaspiOrders_ActualReturnDate_Index ON [dbo].[KaspiOrders] ([ActualReturnDate]) INCLUDE ([ShopID],[TotalPrice],[StatusID])
CREATE NONCLUSTERED INDEX KaspiOrders_ShopID_Code_StatusID_ActualDeliveryDateTS_ActualReturnDateTS_Index ON [dbo].[KaspiOrders] ([ShopID]) INCLUDE ([Code],[StatusID],[ActualDeliveryDateTS],[ActualReturnDateTS])
CREATE NONCLUSTERED INDEX KaspiOrders_ShopID_CreationDateTS_StatusID_ActualDeliveryDateTS_ActualReturnDateTS_Index ON [dbo].[KaspiOrders] ([ShopID]) INCLUDE ([CreationDateTS],[StatusID],[ActualDeliveryDateTS],[ActualReturnDateTS])

create table KaspiOrdersStateHistory
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    OrderID INT NOT NULL,
    StateID int NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(OrderID) REFERENCES KaspiOrders(ID),
    FOREIGN KEY(StateID) REFERENCES KaspiOrderStates(ID)
)

create table KaspiOrdersStatusHistory
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    OrderID INT NOT NULL,
    StatusID int NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(OrderID) REFERENCES KaspiOrders(ID),
    FOREIGN KEY(StatusID) REFERENCES KaspiOrderStatuses(ID)
)

create table KaspiUnitTypes
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Name nvarchar(200) NOT NULL,

    PRIMARY KEY(ID)
)

create table KaspiMerchantProducts
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    UserID INT NULL,
    KaspiMasterProductID int NULL,
    Code nvarchar(2000) COLLATE Cyrillic_General_CS_AS NULL,
    Name nvarchar(2000) NULL,
    CurrentSebes decimal (15,2) NULL,
    Price decimal (15,2) NULL,
    ShopID INT NOT NULL,
    Brand nvarchar(200) NULL,

    Additional1 nvarchar(4000) NULL,
    Additional2 nvarchar(4000) NULL,
    Additional3 nvarchar(4000) NULL,
    Additional4 nvarchar(4000) NULL,
    Additional5 nvarchar(4000) NULL,
    Additional6 nvarchar(4000) NULL,
    Additional7 nvarchar(4000) NULL,
    Additional8 nvarchar(4000) NULL,
    Additional9 nvarchar(000) NULL,

    ParentMerchantID int NULL,

    LastUpdateCheckDate datetime NULL,
    IsAvailable bit NULL,

    IsDumpingOn bit NULL,
    DumpingStep int NULL,
    DumpingMinPrice int NULL,
    DumpingIncreasePriceToMax bit null,
    DumpingExcludeMerchants varchar(2000) NULL,
    DumpingMinimumPlace int NULL,
    DumpingMaximumPlace int NULL,
    DumpingMaxPrice int NULL,

    DumpingFilterByDelivery bit NULL,
    DumpingMinDelivery int NULL,
    DumpingFilterByRating bit NULL,
    DumpingMinRating decimal(15,1) NULL,
    DumpingFilterByReviews bit NULL,
    DumpingMinReviews int NULL,

    CategoryCommission decimal (15,2) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
    FOREIGN KEY(KaspiMasterProductID) REFERENCES KaspiMasterProducts(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID),
    FOREIGN KEY(ParentMerchantID) REFERENCES KaspiMerchantProducts(ID)
)

create index KaspiMerchantProducts_Code_Index on KaspiMerchantProducts(Code)

CREATE NONCLUSTERED INDEX KaspiMerchantProducts_IsAvailable_IsDumpingOn_Index ON KaspiMerchantProducts (IsAvailable,IsDumpingOn) INCLUDE (KaspiMasterProductID)

create table KaspiMerchantProductsPresents
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    MerchantProductID int NOT NULL,
    PresentMerchantProductID int NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(MerchantProductID) REFERENCES KaspiMerchantProducts(ID),
    FOREIGN KEY(PresentMerchantProductID) REFERENCES KaspiMerchantProducts(ID)
)

create table KaspiMerchantProductsVersions
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    MerchantProductID int NOT NULL,
    Code nvarchar(1000) NULL,
    Name nvarchar(2000) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(MerchantProductID) REFERENCES KaspiMerchantProducts(ID),
)

create table KaspiOrderItemCategories
(
    ID   int            NOT NULL IDENTITY (1,1),
    Code nvarchar(800) NOT NULL,
    Name nvarchar(800) NOT NULL

    PRIMARY KEY (ID)
)

create index KaspiOrderItemCategories_Code_Index on KaspiOrderItemCategories(Code)

create table KaspiCategoriesFromMenu
(
    ID int NOT NULL IDENTITY(1,1),

    Date datetime NOT NULL DEFAULT GetDate(),
    Code nvarchar(1000) NOT NULL,
    Name nvarchar(1000) NOT NULL,
    CategoryID int NULL,
    Level int NOT NULL,
    CommissionStart decimal(15,3) NULL,
    CommissionEnd decimal(15,3) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(CategoryID) REFERENCES KaspiCategoriesFromMenu(ID)
)

create index KaspiCategoriesFromMenu_Code_Index on KaspiCategoriesFromMenu(Code)

create table KaspiCategoriesFromMenuCommissionHistory
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    CategoryID int NOT NULL,
    CommissionStart decimal(15,3) NOT NULL,
    CommissionEnd decimal(15,3) NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(CategoryID) REFERENCES KaspiCategoriesFromMenu(ID)
)

insert into KaspiCategoriesFromMenuCommissionHistory(Date,CategoryID,CommissionStart,CommissionEnd)
select '1970-01-01',ID,CommissionStart,CommissionEnd from KaspiCategoriesFromMenu ORDER BY ID

CREATE NONCLUSTERED INDEX KaspiCategoriesFromMenuCommissionHistory_CategoryID_Index
    ON [dbo].[KaspiCategoriesFromMenuCommissionHistory] ([CategoryID])


create table KaspiOrderDetails
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    KaspiOrderID INT NOT NULL,
    ProductID int NOT NULL,
    MasterProductVersionID int NULL,
    MerchantProductVersionID int NULL,
    KaspiID nvarchar(100) NOT NULL,
    UnitTypeID int NULL,
    OfferCode nvarchar(200) NULL,
    OfferName nvarchar(200) NULL,
    Quant int NULL,
    TotalPrice decimal (15,2) NULL,
    BasePrice decimal (15,2) NULL,
    ItemWeight decimal (15,2) NULL,
    CategoryID int NULL,
    CategoryCommission decimal (15,2) NULL,
    DeliveryCost decimal (15,2) NULL,
    CurrentSebes decimal (15,2) NULL,

    MenuCategoryID int NULL,
    ItemCategoryID int NULL,

    OrderCreationDate datetime null,

    ParentProductID int NULL,

    OrderDeliveryCost decimal (15,2) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(KaspiOrderID) REFERENCES KaspiOrders(ID),
    FOREIGN KEY(ProductID) REFERENCES KaspiMerchantProducts(ID),
    FOREIGN KEY(UnitTypeID) REFERENCES KaspiUnitTypes(ID),
    FOREIGN KEY(CategoryID) REFERENCES KaspiCategories(ID),
    FOREIGN KEY(MasterProductVersionID) REFERENCES KaspiMasterProductsVesions(ID),
    FOREIGN KEY(MerchantProductVersionID) REFERENCES KaspiMerchantProductsVersions(ID),
    FOREIGN KEY(MenuCategoryID) REFERENCES KaspiCategoriesFromMenu(ID),
    FOREIGN KEY(ItemCategoryID) REFERENCES KaspiOrderItemCategories(ID)
)

create index KaspiOrderDetails_KaspiID_Index on KaspiOrderDetails(KaspiID)
CREATE NONCLUSTERED INDEX KaspiOrders_ShopID_CreationDate_StateID_StatusID_Index ON KaspiOrders(ShopID) INCLUDE (CreationDate,StateID,StatusID)
CREATE NONCLUSTERED INDEX KaspiOrderDetails_KaspiOrderID_ProductIDTotalPrice_BasePrice_CategoryCommission_DeliveryCost_CurrentSebes_Index ON KaspiOrderDetails(KaspiOrderID) INCLUDE (ProductID,TotalPrice,BasePrice,CategoryCommission,DeliveryCost,CurrentSebes)
CREATE NONCLUSTERED INDEX KaspiOrderDetails_ProductID_Index ON KaspiOrderDetails(ProductID)

create table KaspiAPICallsResponseHashes
(
    ID   int      NOT NULL IDENTITY (1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    UserID INT NOT NULL,
    ShopID INT NULL,
    URL nvarchar(1000) NULL,
    URLHash nvarchar(100) NULL,
    ResponseHash nvarchar(100) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
    index KaspiAPICallsResponseHashes_URLHash_Index (URLHash)
)

create table KaspiAPICalls
(
    ID   int      NOT NULL IDENTITY (1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    UserID INT NOT NULL,
    ShopID INT NULL,
    Method nvarchar(1000) NULL,
    Parameters nvarchar(1000) NULL,
    Response TEXT  NULL,
    ResponseCode INT NULL,
    Duration INT NULL,
    ResponseHash nvarchar(100) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID)
)

create table EmailChecks
(
    ID   int      NOT NULL IDENTITY (1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    SessionID INT NOT NULL,
    Email nvarchar(1000) NULL,
    IsValid bit NULL,

    PRIMARY KEY (ID),
    FOREIGN KEY (SessionID) REFERENCES Sessions (ID)
)

create table EmailSignUps
(
    ID   int      NOT NULL IDENTITY (1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    SessionID INT NOT NULL,
    UserID int NOT NULL,
    Email nvarchar(1000) NULL,
    Code nvarchar(40) NULL,
    SignUpDate datetime NULL,

    PRIMARY KEY (ID),
    FOREIGN KEY (SessionID) REFERENCES Sessions (ID),
    FOREIGN KEY (UserID) REFERENCES Users (ID)
)

create table PasswordRecoveries
(
    ID   int      NOT NULL IDENTITY (1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    SessionID INT NOT NULL,
    UserID int NOT NULL,
    Code nvarchar(40) NULL,
    RecoveryDate datetime NULL,

    PRIMARY KEY (ID),
    FOREIGN KEY (SessionID) REFERENCES Sessions (ID),
    FOREIGN KEY (UserID) REFERENCES Users (ID)
)

create index PasswordRecoveries_Code_Index on PasswordRecoveries(Code);

create table UserTelegramChats
(
    ID   int      NOT NULL IDENTITY (1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    SessionID INT NOT NULL,
    UserID int NOT NULL,
    ChatID nvarchar(40) NULL,
    FirstName nvarchar(200) NULL,
    UserName nvarchar(200) NULL,
    DeletedDate datetime NULL,

    PRIMARY KEY (ID),
    FOREIGN KEY (SessionID) REFERENCES Sessions (ID),
    FOREIGN KEY (UserID) REFERENCES Users (ID)
)

create table UserMessagesTypes
(
    ID      int           NOT NULL IDENTITY (1,1),
    Date    datetime      NOT NULL DEFAULT GetDate(),
    Message nvarchar(1000) NOT NULL,

    PRIMARY KEY (ID)
)

SET IDENTITY_INSERT UserMessagesTypes ON
insert into UserMessagesTypes (ID,Message)
values
    (1, N'Вы успешно зарегистрировались'),
    (2, N'Вы вошли в аккаунт'),
    (3, N'Вы вошли в аккаунт используя автоматическую ссылку'),
    (4, N'Вы успешно добавили Каспи магазин'),
    (5, N'Ключ API Каспи магазина успешно проверен и активен'),
    (6, N'Ключ API Каспи магазина не имеет доступа и был деактивирован'),
    (7, N'Вы успешно добавили Телеграм чат'),
    (8, N'Вы начали процедуру восстановления пароля'),
    (9, N'Вы успешно установили новый пароль через процедуру восстановления'),
    (10, N'Вы успешно изменили пароль'),
    (11, N'Неуспешная попытка входа в аккаунт');

insert into UserMessagesTypes (ID,Message)
values
    (12, N'Первоначальный импорт заказов Каспи магазина начат'),
    (13, N'Первоначальный импорт заказов Каспи магазина завершен');

insert into UserMessagesTypes (ID,Message)
values
    (14, N'Вы успешно зарегистрировались с аккаунтом Google'),
    (15, N'Вы успешно вошли с аккаунтом Google'),
    (16, N'Вы успешно зарегистрировались с аккаунтом Facebook'),
    (17, N'Вы успешно вошли с аккаунтом Facebook');

insert into UserMessagesTypes (ID,Message)
values
    (18, N'Вы вышли из аккаунта');

insert into UserMessagesTypes (ID,Message)
values
    (19, N'Оплачено подключение номера WhatsApp на 30 дней'),
    (20, N'Оплачено подключение номера WhatsApp на 1 год'),
    (21, N'Номер WhatsApp готов к подключению'),
    (22, N'Номер WhatsApp подключен');

insert into UserMessagesTypes (ID,Message)
values
    (23, N'Вы успешно отредактировали Каспи магазин');

insert into UserMessagesTypes (ID,Message)
values
    (24, N'Вы успешно добавили склад Каспи магазина'),
    (25, N'Вы успешно отредактировали склад Каспи магазина');

insert into UserMessagesTypes (ID,Message)
values
    (26, N'Доступ к личному кабинету проверен и активен'),
    (27, N'Доступ к личному кабинету не валиден и был деактивирован');

SET IDENTITY_INSERT UserMessagesTypes OFF

create table EmailsToSend
(
    ID                 int      NOT NULL IDENTITY (1,1),
    Date               datetime NOT NULL DEFAULT GetDate(),
    UserID             int      NULL,
    SessionID          int      NULL,
    Email              nvarchar(200) NOT NULL,
    Subject            nvarchar(200) NOT NULL,
    Message            TEXT NOT NULL,
    SentDate           datetime NULL,

    PRIMARY KEY (ID),
    FOREIGN KEY (UserID) REFERENCES Users (ID),
    FOREIGN KEY (SessionID) REFERENCES Sessions (ID)
)

create table SocialNetworks
(
    ID                 int      NOT NULL IDENTITY (1,1),
    Date               datetime NOT NULL DEFAULT GetDate(),
    Name               nvarchar(200) NOT NULL,

    PRIMARY KEY (ID)
)

insert into SocialNetworks (Name) values ('Google'), ('Facebook');

create table UserSocialNetworks
(
    ID                 int      NOT NULL IDENTITY (1,1),
    Date               datetime NOT NULL DEFAULT GetDate(),
    UserID             int      NOT NULL,
    SocialNetworkID    int      NOT NULL,
    SocialNetworkUserID nvarchar(200) NOT NULL,
    Email              nvarchar(200) NULL,
    FirstName          nvarchar(200) NULL,
    LastName           nvarchar(200) NULL,
    Picture             nvarchar(200) NULL,

    PRIMARY KEY (ID),
    FOREIGN KEY (UserID) REFERENCES Users (ID),
    FOREIGN KEY (SocialNetworkID) REFERENCES SocialNetworks (ID)
)

create table TildaLeads
(
    ID    int           NOT NULL IDENTITY (1,1),
    Date  datetime      NOT NULL DEFAULT GetDate(),
    Phone nvarchar(200) NOT NULL,
    Email nvarchar(200) NOT NULL,
    SentDate  datetime      NULL,

    PRIMARY KEY (ID)
)

create table KaspiCategoriesList
(
    ID int NOT NULL IDENTITY(1,1),

    Date datetime NOT NULL DEFAULT GetDate(),
    Code nvarchar(1000) NOT NULL,
    Name nvarchar(1000) NOT NULL,
    CategoryID int NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(CategoryID) REFERENCES KaspiCategories(ID),
)

create table ServiceTypes
(
    ID         int           NOT NULL IDENTITY(1,1),
    Date       datetime      NOT NULL DEFAULT GetDate(),
    Name       nvarchar(100) NOT NULL,

    PRIMARY KEY(ID),
)

insert into ServiceTypes (Name)
values
    (N'WhatsApp');

insert into ServiceTypes (Name)
values
    (N'ProfitBot');

create table Services
(
    ID         int           NOT NULL IDENTITY(1,1),
    Date       datetime      NOT NULL DEFAULT GetDate(),
    Name       nvarchar(100) NOT NULL,
    TypeID     int           NOT NULL,
    Code       nvarchar(100) NOT NULL,
    Price      int           NOT NULL,
    PeriodDays int           NOT NULL,
    ParamValue int           NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(TypeID) REFERENCES ServiceTypes(ID)
)

insert into Services (Name,Code,Price,PeriodDays,TypeID)
values
    (N'Подключение номера WhatsApp на 30 дней', 'whatsapp1', 15000, 30,1),
    (N'Подключение номера WhatsApp на 1 год', 'whatsapp12', 150000, 365,1)

insert into Services (Name,Code,Price,PeriodDays,TypeID,ParamValue)
values
    (N'Подписка ProfitBot на 30 дней', 'profitbot1-1', 15000, 30,2,1),
    (N'Подписка ProfitBot на 90 дней', 'profitbot3-1', 45000, 90,2,1),
    (N'Подписка ProfitBot на 1 год', 'profitbot12-1', 153000, 365,2,1)

insert into Services (Name,Code,Price,PeriodDays,TypeID,ParamValue)
values
    (N'Подписка ProfitBot на 30 дней 2 склада', 'profitbot1-2', 24000, 30,2,2),
    (N'Подписка ProfitBot на 90 дней 2 склада', 'profitbot3-2', 72000, 90,2,2),
    (N'Подписка ProfitBot на 1 год 2 склада', 'profitbot12-2', 288000, 365,2,2),
    (N'Подписка ProfitBot на 30 дней 3 склада', 'profitbot1-3', 30000, 30,2,3),
    (N'Подписка ProfitBot на 90 дней 3 склада', 'profitbot3-3', 90000, 90,2,3),
    (N'Подписка ProfitBot на 1 год 3 склада', 'profitbot12-3', 360000, 365,2,3),
    (N'Подписка ProfitBot на 30 дней 4 склада', 'profitbot1-4', 40000, 30,2,4),
    (N'Подписка ProfitBot на 90 дней 4 склада', 'profitbot3-4', 120000, 90,2,4),
    (N'Подписка ProfitBot на 1 год 4 склада', 'profitbot12-4', 480000, 365,2,4),
    (N'Подписка ProfitBot на 30 дней 5 складов', 'profitbot1-5', 50000, 30,2,5),
    (N'Подписка ProfitBot на 90 дней 5 складов', 'profitbot3-5', 150000, 90,2,5),
    (N'Подписка ProfitBot на 1 год 5 складов', 'profitbot12-5', 600000, 365,2,5),
    (N'Подписка ProfitBot на 30 дней 6 складов', 'profitbot1-6', 60000, 30,2,6),
    (N'Подписка ProfitBot на 90 дней 6 складов', 'profitbot3-6', 180000, 90,2,6),
    (N'Подписка ProfitBot на 1 год 6 складов', 'profitbot12-6', 720000, 365,2,6)

create table UserServices
(
    ID              int           NOT NULL IDENTITY(1,1),
    Date            datetime      NOT NULL DEFAULT GetDate(),
    UserID          int           NOT NULL,
    ServiceID       int           NOT NULL,
    PaidTillDate    datetime      NULL,
    IsActive        bit           NULL,
    DeletedDate     datetime      NULL,
    OperationKey    nvarchar(100) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
    FOREIGN KEY(ServiceID) REFERENCES Services(ID)
)

create table PaymentTypes
(
    ID int NOT NULL IDENTITY(1,1) primary key,
    Name nvarchar(100) NOT NULL
)

SET IDENTITY_INSERT PaymentTypes ON
insert into PaymentTypes (ID,Name) values (1, N'Kaspi Red'), (2, N'ePay')
SET IDENTITY_INSERT PaymentTypes OFF

create table PaymentRequests
(
    ID         int           NOT NULL IDENTITY(192610,1),
    Date       datetime      NOT NULL DEFAULT GetDate(),
    OrderID    nvarchar(100) NOT NULL,
    UserID     int           NOT NULL,
    ServiceID  int           NOT NULL,
    Price      int           NOT NULL,
    Status     nvarchar(100) NOT NULL,
    PaymentID  nvarchar(100) NULL,
    PaidDate   datetime      NULL,
    UserServiceID int        NULL,
    PaidTillDate    datetime      NULL,
    UseBonusSum   int        NULL,
    PaymentTypeID int        NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
    FOREIGN KEY(ServiceID) REFERENCES Services(ID),
    FOREIGN KEY(UserServiceID) REFERENCES UserServices(ID),
    FOREIGN KEY(PaymentTypeID) REFERENCES PaymentTypes(ID)
)

create table UserSavedCards
(
    ID         int           NOT NULL IDENTITY(1,1),
    Date       datetime      NOT NULL DEFAULT GetDate(),
    UserID     int           NOT NULL,
    CardID     nvarchar(100) NOT NULL,
    CardType   nvarchar(100) NOT NULL,
    CardNumber nvarchar(100) NOT NULL,
    ExpDate    nvarchar(100) NOT NULL,
    CardHolder nvarchar(100) NOT NULL,
    CardToken  nvarchar(800) NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID)
)

create table UserWhatsAppInstances
(
    ID         int           NOT NULL IDENTITY(1,1),
    Date       datetime      NOT NULL DEFAULT GetDate(),
    UserID     int           NOT NULL,
    Phone      nvarchar(100) NULL,
    Name       nvarchar(1000) NULL,
    Domain     nvarchar(100) NOT NULL,
    AuthToken  nvarchar(100) NOT NULL,
    InstanceID nvarchar(100) NOT NULL,
    Token      nvarchar(1000) NOT NULL,
    PaidTillDate datetime     NOT NULL,
    IsActive    bit null,
    DeletedDate datetime      NULL,
    UserServiceID int        NOT NULL,
    State     nvarchar(100) NULL,
    StateChangeDate datetime NULL,

    ShopID int NULL,
    IsTurnedOff bit NOT NULL default 0,
    IsTestMode bit NOT NULL default 0,
    TestPhone nvarchar(100) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
    FOREIGN KEY(UserServiceID) REFERENCES UserServices(ID),
    FOREIGN KEY (ShopID) REFERENCES UserKaspiShops(ID)
)

create table UserWhatsappIncomingWebhook
(
    ID         int           NOT NULL IDENTITY(1,1),
    Date       datetime      NOT NULL DEFAULT GetDate(),
    Data       varchar(max)  NOT NULL,
    InstanceID int           NULL,
    MessageID  int           NULL,

    PRIMARY KEY (ID)
)

create table UserMessages
(
    ID                 int      NOT NULL IDENTITY (1,1),
    Date               datetime NOT NULL DEFAULT GetDate(),
    UserID             int      NOT NULL,
    TypeID             int      NOT NULL,
    ShopID             int      NULL,
    StoreID            int      NULL,
    TelegramChatID     int      NULL,
    PasswordRecoveryID int      NULL,
    EmailSignupID      int      NULL,
    SessionID          int      NULL,
    WhatsappInstanceID int      NULL,
    ViewDate           datetime NULL,

    PRIMARY KEY (ID),
    FOREIGN KEY (UserID) REFERENCES Users (ID),
    FOREIGN KEY (TypeID) REFERENCES UserMessagesTypes (ID),
    FOREIGN KEY (ShopID) REFERENCES UserKaspiShops (ID),
    FOREIGN KEY (StoreID) REFERENCES UserKaspiShopsStores (ID),
    FOREIGN KEY (TelegramChatID) REFERENCES UserTelegramChats (ID),
    FOREIGN KEY (PasswordRecoveryID) REFERENCES PasswordRecoveries (ID),
    FOREIGN KEY (EmailSignupID) REFERENCES EmailSignUps (ID),
    FOREIGN KEY (SessionID) REFERENCES Sessions (ID),
    FOREIGN KEY (WhatsappInstanceID) REFERENCES UserWhatsappInstances (ID)
)

create table APICallsGreenApi
(
    ID                 int            NOT NULL IDENTITY (1,1),
    Date               datetime       NOT NULL DEFAULT GetDate(),
    UserID             INT            NULL,
    WhatsappInstanceID int            NULL,
    Method             nvarchar(1000) NULL,
    Parameters         varchar(max)   NULL,
    Response           varchar(max)   NULL,
    ResponseCode       INT            NULL,
    Duration           INT            NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(WhatsappInstanceID) REFERENCES UserWhatsappInstances (ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID)
)

create table PaymentResponse
(
    ID         int           NOT NULL IDENTITY(1,1),
    Date       datetime      NOT NULL DEFAULT GetDate(),
    OrderID    nvarchar(max) NULL,
    URL        nvarchar(max) NOT NULL,
    Status     nvarchar(100) NOT NULL,
    Response   nvarchar(max) NULL
)

alter table TelegramMessagesToSend add WhatsappInstanceID int NULL
alter table TelegramMessagesToSend add FOREIGN KEY (WhatsappInstanceID) REFERENCES UserWhatsAppInstances(ID)

create table TelegramMessagesToSend
(
    ID                 int      NOT NULL IDENTITY (1,1),
    Date               datetime NOT NULL DEFAULT GetDate(),
    UserID             int      NULL,
    SessionID          int      NULL,
    ChatID             nvarchar(200) NULL,
    Token              nvarchar(200) NULL,
    PaymentRequestID   int NULL,
    WhatsappInstanceID int NULL,
    Message            TEXT NOT NULL,
    ForUser            bit NOT NULL,
    SentDate           datetime NULL,

    PRIMARY KEY (ID),
    FOREIGN KEY (UserID) REFERENCES Users (ID),
    FOREIGN KEY (SessionID) REFERENCES Sessions (ID),
    FOREIGN KEY (PaymentRequestID) REFERENCES PaymentRequests(ID),
    FOREIGN KEY (WhatsappInstanceID) REFERENCES UserWhatsAppInstances(ID)
)

create table AnalyticsData
(
    ID         int           NOT NULL IDENTITY(1,1),
    Date       datetime      NOT NULL DEFAULT GetDate(),
    Name       nvarchar(100) NOT NULL,

    PRIMARY KEY(ID)
)

create table AnalyticsDataItems
(
    ID         int           NOT NULL IDENTITY(1,1),
    Date       datetime      NOT NULL DEFAULT GetDate(),
    DataID     int           NOT NULL,
    ProductName      nvarchar(1000) NULL,
    CategoryName1    nvarchar(1000) NULL,
    CategoryName2    nvarchar(1000) NULL,
    CategoryName3    nvarchar(1000) NULL,
    Brand            nvarchar(1000) NULL,
    TotalPrice       decimal(15,2)  NULL,
    NumOrders        int            NULL,
    NumSellers       int            NULL,
    CategoryID       int            NULL,
    MenuCategoryID   int            NULL,
    Code             nvarchar(100)  NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(DataID) REFERENCES AnalyticsData(ID),
    FOREIGN KEY(CategoryID) REFERENCES KaspiCategories(ID),
    FOREIGN KEY(MenuCategoryID) REFERENCES KaspiCategoriesFromMenu(ID)
)

create view KaspiOrderDetailsWithMerchantIDUserIDCreationDate
as
select
    s.UserID,ko.CreationDate,kod.ProductID as MerchantProductID,ko.StoreID,
    kod.* from KaspiOrderDetails kod
        inner join KaspiOrders ko on ko.ID=kod.KaspiOrderID
        inner join UserKaspiShops s on s.ID=ko.ShopID

create table ScheduledTasksTypes
(
    ID         int           NOT NULL IDENTITY(1,1),
    Date       datetime      NOT NULL DEFAULT GetDate(),
    Name       nvarchar(100) NOT NULL,

    PRIMARY KEY(ID)
)

insert into ScheduledTasksTypes (Name) values ('priceUpload'), ('priceNalUpload'),('lkAccessCheck'),('itemDescUpload'),('marketingAccessCheck')

create table ScheduledTasks
(
    ID                  int           NOT NULL IDENTITY(1,1),
    Date                datetime      NOT NULL DEFAULT GetDate(),
    TaskTypeID          int           NOT NULL,
    UserID              int           NULL,
    TaskToStartDate     datetime      NULL,
    TaskData            nvarchar(max) NULL,
    StartedDate         datetime      NULL,
    PercentDone         int           NULL,
    FinishedDate        datetime      NULL,

    DistrResult           nvarchar(max) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY (UserID) REFERENCES Users (ID),
    FOREIGN KEY(TaskTypeID) REFERENCES ScheduledTasksTypes(ID)
)

create table GAWebHooks
(
    ID                  int           NOT NULL IDENTITY(1,1),
    Date                datetime      NOT NULL DEFAULT GetDate(),
    Headers             nvarchar(max) NOT NULL,
    Payload             nvarchar(max) NOT NULL,
    ParsedDate          datetime      NULL,
    InstanceID          int           NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(InstanceID) REFERENCES UserWhatsappInstances(ID)
)

create table SessionAttributes
(
    ID         int           NOT NULL IDENTITY(1,1),
    Name    varchar(50)    NOT NULL,
    Code    varchar(50)    NOT NULL,
    LifetimeSeconds int NOT NULL default 0,

    PRIMARY KEY(ID)
)

create table SessionAttributesData
(
    ID         int           NOT NULL IDENTITY(1,1),
    Date datetime       not null DEFAULT GetDate(),
    SessionID int NOT NULL,
    AttributeID int NOT NULL,
    Value varchar(50) NULL,
    IsActive int NOT NULL default 1,

    PRIMARY KEY(ID),

    FOREIGN KEY (SessionID) REFERENCES Sessions(ID),
    FOREIGN KEY (AttributeID) REFERENCES SessionAttributes(ID)

)

insert into SessionAttributes (Name, Code, LifetimeSeconds) values ('startDate', 'startDate', 0)
insert into SessionAttributes (Name, Code, LifetimeSeconds) values ('endDate', 'endDate', 0)

insert into SessionAttributes (Name, Code, LifetimeSeconds) values ('reportNalList_ShopID', 'reportNalList_ShopID', 0)
insert into SessionAttributes (Name, Code, LifetimeSeconds) values ('reportNalList_Months', 'reportNalList_Months', 0)
insert into SessionAttributes (Name, Code, LifetimeSeconds) values ('reportNalList_OnPage', 'reportNalList_OnPage', 0)

insert into SessionAttributes (Name, Code, LifetimeSeconds) values ('reportClientsList_ShopID', 'reportClientsList_ShopID', 0)
insert into SessionAttributes (Name, Code, LifetimeSeconds) values ('reportClientsList_DateStart', 'reportClientsList_DateStart', 0)
insert into SessionAttributes (Name, Code, LifetimeSeconds) values ('reportClientsList_DateEnd', 'reportClientsList_DateEnd', 0)
insert into SessionAttributes (Name, Code, LifetimeSeconds) values ('reportClientsList_OnPage', 'reportClientsList_OnPage', 0)

insert into SessionAttributes (Name, Code, LifetimeSeconds) values ('metricsStartDate', 'metricsStartDate', 0)
insert into SessionAttributes (Name, Code, LifetimeSeconds) values ('metricsEndDate', 'metricsEndDate', 0)

create table StoreOperationsTypes
(
    ID   int      NOT NULL IDENTITY (1,1),
    Name nvarchar(100) NOT NULL,
    Coeff INT NOT NULL,

    PRIMARY KEY(ID)
)

SET IDENTITY_INSERT StoreOperationsTypes ON
insert into StoreOperationsTypes (ID,Name,Coeff) values (1, N'Приход', 1),(2, N'Расход', -1),(3, N'Ревизия', 1),(4, N'Возврат', 1),(5, N'Архив', 0)
SET IDENTITY_INSERT StoreOperationsTypes OFF

create table StoreOperationsSource
(
    ID   int      NOT NULL IDENTITY (1,1),
    Name nvarchar(100) NOT NULL,

    PRIMARY KEY(ID)
)

SET IDENTITY_INSERT StoreOperationsSource ON
insert into StoreOperationsSource (ID,Name)values (1, N'Заказ'),(2, N'Вручную'),(3, N'Telegram')
SET IDENTITY_INSERT StoreOperationsSource OFF

create table UserSuppliers
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    UserID INT NOT NULL,
    Name nvarchar(500) NOT NULL,
    DeletedDate datetime NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID)
)

create table StoreOperationsReasons
(
    ID   int      NOT NULL IDENTITY (1,1),
    Name nvarchar(100) NOT NULL,
    ForIncome bit NOT NULL,
    ForOutcome bit NOT NULL,

    PRIMARY KEY(ID)
)

SET IDENTITY_INSERT StoreOperationsReasons ON
insert into StoreOperationsReasons (ID,Name,ForIncome,ForOutcome)
values
       (1, N'Продажа', 0, 1),
       (2, N'Брак/непригодность', 0, 1),
       (3, N'Перемещение', 1, 1),
       (4, N'Возврат', 1, 1),
       (5, N'Корректировка остатков', 1, 1),
       (6, N'Потеря',0,1),
       (7, N'Покупка у поставщика',1,0),
       (8, N'Подарок',1,1),
       (9, N'Остатки при старте учёта',1,0),
       (10, N'Другое',1,1)

SET IDENTITY_INSERT StoreOperationsReasons OFF

create table UserKaspiShopsStoresOperations
(
    ID          int           NOT NULL IDENTITY(1,1),
    Date        datetime      not null DEFAULT GetDate(),
    ProductID   int           NOT NULL,
    StoreID     int           NOT NULL,
    Quant       int           NOT NULL,
    Price       decimal(15,2) NOT NULL,
    Sebes       decimal(15,2) NULL default 0.00,
    OperationTypeID int       NOT NULL,
    KaspiOrderDetailID int    NULL,
    SourceID int NULL,
    SupplierID int NULL,
    ReasonID int NULL,
    DeletedDate datetime NULL,
    OriginalOperationTypeID int NULL,
    DeletedUserID int NULL,
    UpdateSebes bit NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY (ProductID) REFERENCES KaspiMerchantProducts(ID),
    FOREIGN KEY (StoreID) REFERENCES UserKaspiShopsStores(ID),
    FOREIGN KEY (OperationTypeID) REFERENCES StoreOperationsTypes(ID),
    FOREIGN KEY (KaspiOrderDetailID) REFERENCES KaspiOrderDetails(ID),
    FOREIGN KEY (SourceID) REFERENCES StoreOperationsSource(ID),
    FOREIGN KEY (SupplierID) REFERENCES UserSuppliers(ID),
    FOREIGN KEY (ReasonID) REFERENCES StoreOperationsReasons(ID),
    FOREIGN KEY (OriginalOperationTypeID) REFERENCES StoreOperationsTypes(ID),
    FOREIGN KEY (DeletedUserID) REFERENCES Users(ID)
)

CREATE NONCLUSTERED INDEX UserKaspiShopsStoresOperations_ProductID_Index
    ON [dbo].[UserKaspiShopsStoresOperations] ([ProductID])

create table UserKaspiShopsStoresStartCount
(
    ID          int             NOT NULL IDENTITY(1,1),
    Date        datetime        not null DEFAULT GetDate(),
    StoreID     int             NOT NULL,
    OperationID int             NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY (StoreID) REFERENCES UserKaspiShopsStores(ID),
    FOREIGN KEY (OperationID) REFERENCES UserKaspiShopsStoresOperations(ID)
)

create table KaspiShopItems
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    UpdatedDate datetime NOT NULL DEFAULT GetDate(),

    MenuCategoryID int NULL,

    KaspiID nvarchar(100) NULL,
    Title nvarchar(300) NULL,
    Brand nvarchar(300) NULL,
    CategoryID nvarchar(300) NULL,
    ShopLink  nvarchar(300) NULL,
    Price decimal(15,2) NULL,
    CreatedTime datetime NULL,
    PreviewImage01 nvarchar(300) NULL,
    PreviewImage02 nvarchar(300) NULL,
    PreviewImage03 nvarchar(300) NULL,
    PreviewImage04 nvarchar(300) NULL,
    PreviewImage05 nvarchar(300) NULL,
    PreviewImage06 nvarchar(300) NULL,
    PreviewImage07 nvarchar(300) NULL,
    PreviewImage08 nvarchar(300) NULL,
    PreviewImage09 nvarchar(300) NULL,
    PreviewImage10 nvarchar(300) NULL,

    ReviewsLink nvarchar(300) NULL,
    Rating decimal(15,2) NULL,
    ReviewsQuantity int NULL,
    BaseProductCodes nvarchar(500) NULL,
    ItemWeight decimal(15,2) NULL,


    PRIMARY KEY(ID),
    FOREIGN KEY(MenuCategoryID) REFERENCES KaspiCategoriesFromMenu(ID),

    INDEX KaspiShopItems_KaspiID_Index (KaspiID)
)

create table KaspiAPILKCalls
(
    ID   int      NOT NULL IDENTITY (1,1),
    Date datetime NOT NULL DEFAULT GetDate(),

    URL nvarchar(2000) NOT NULL,
    Headers nvarchar(max) NOT NULL,
    Payload nvarchar(max) NOT NULL,
    Response nvarchar(max) NOT NULL,
    ResponseHeaders nvarchar(max) NOT NULL,
    ResponseCode int NOT NULL,
    Duration int NOT NULL,
)

CREATE NONCLUSTERED INDEX KaspiOrders_CreationDate_ShopID_StoreID_Index
    ON [dbo].[KaspiOrders] ([CreationDate])
    INCLUDE ([ShopID],[StoreID])


create table OrderMessageTypes
(
    ID   int      NOT NULL IDENTITY (1,1),
    Code nvarchar(100) NOT NULL,
    Name nvarchar(100) NOT NULL,
    Template nvarchar(MAX) NOT NULL,

    PRIMARY KEY(ID)
)

SET IDENTITY_INSERT OrderMessageTypes ON
insert into OrderMessageTypes (ID,Code,Name,Template)
values

    (1, 'NEW', N'Заказ создан',N'Сәлеметсіз бе, @Имя клиента!
@Название магазина дүкенін таңдағаныңыз үшін алғысымызды білдіреміз!

📦 Сіздің тапсырысыңыз:
@Товары заказа

🆔 Тапсырыс нөмірі: @Номер заказа

Біз тапсырысыңызды қабылдап, өңдеуге кірістік.
Сеніміңіз үшін рахмет!

Здравствуйте, @Имя клиента!
Спасибо, что выбрали магазин @Название магазина!

📦 Ваш заказ:
@Товары заказа

🆔 Номер заказа: @Номер заказа

Мы приняли ваш заказ и начали его обработку.
Благодарим за доверие!'),
    (2, 'PACK', N'Упаковка', N'Сәлеметсіз бе, @Имя клиента!
Сіздің тапсырысыңыз мұқият жинақталуда және жақында жолға шығады.

📦 Тапсырысыңыз:
@Товары заказа

🆔 Тапсырыс нөмірі: @Номер заказа

@Название магазина дүкенін таңдағаныңыз үшін рахмет!
Сеніміңізді жоғары бағалаймыз!

Здравствуйте, @Имя клиента!
Ваш заказ уже собирается и скоро будет отправлен.

📦 Вы заказали:
@Товары заказа

🆔 Номер заказа: @Номер заказа

Спасибо, что выбрали магазин @Название магазина!
Нам приятно работать для вас!'),
    (3, 'SELFPICKUP', N'Самовывоз', N'Сәлеметсіз бе, @Имя клиента!
@Название магазина дүкенін таңдағаныңыз үшін алғысымызды білдіреміз!

📦 Сіздің тапсырысыңыз:
@Товары заказа

🆔 Тапсырыс нөмірі: @Номер заказа

Тапсырысыңызды келесі мекенжайдан ала аласыз:
@Ссылка на адрес склада
(@Адрес склада)

Сеніміңізге рақмет, қуанышпен қызмет етеміз!

Здравствуйте, @Имя клиента!
Спасибо, что выбрали магазин @Название магазина!

📦 Вы заказали:
@Товары заказа

🆔 Номер заказа: @Номер заказа

Вы можете забрать ваш заказ по следующему адресу:
@Ссылка на адрес склада
(@Адрес склада)

Благодарим за доверие, рады быть полезными!'),
    (4, 'POSTOMAT', N'Отправлен в постомат', N'Сәлеметсіз бе, @Имя клиента!
Сіздің № @Номер заказа тапсырысыңыз постоматқа жіберілді!

📦 Тапсырыстың құрамы:
@Товары заказа

🚚 Жоспарланған жеткізу күні: @Дата доставки

Сеніміңізге үлкен рахмет! Біз әрдайым сізге қызмет көрсетуге дайынбыз!

Здравствуйте, @Имя клиента!
Ваш заказ № @Номер заказа был отправлен в постомат!

📦 Состав заказа:
@Товары заказа

🚚 Ожидаемая дата доставки: @Дата доставки

Спасибо за ваше доверие! Мы всегда рады вам!'),
    (5, 'SENT', N'Отправлен', N'Сәлеметсіз бе, @Имя клиента!
Сіздің № @Номер заказа тапсырысыңыз жолға шықты!

📦 Тапсырыстың құрамы:
@Товары заказа

🚚 Жоспарланған жеткізу күні: @Дата доставки

Сеніміңізге рақмет! Тапсырысыңыз жақын арада сізге жеткізіледі.

Здравствуйте, @Имя клиента!
Ваш заказ № @Номер заказа был отправлен!

📦 Состав заказа:
@Товары заказа

🚚 Предполагаемая дата доставки: @Дата доставки

Спасибо за доверие! Совсем скоро заказ будет у вас.'),
    (6, 'CANCELLED', N'Заказ отменен', N'Сәлеметсіз бе, @Имя клиента!
Өкінішке орай, сіздің № @Номер заказа тапсырысыңыз қабылданбады.

📦 Тапсырыстың құрамы:
@Товары заказа

Егер сұрақтарыңыз болса немесе бұл түсінбестік деп ойласаңыз, бізге хабарласыңыз — көмектесуге әрқашан дайынбыз.


@Название магазина дүкенін таңдағаныңыз үшін алғысымызды білдіреміз. Келесі тапсырыстарда жолыққанша!

Здравствуйте, @Имя клиента!
К сожалению, заказ № @Номер заказа отменен.

📦 Состав заказа:
@Товары заказа

Если у вас возникли вопросы или это ошибка — напишите нам

Спасибо, что выбрали магазин @Название магазина. Будем рады видеть вас снова!'),
    (7, 'DONE', N'Доставлен', N'Сәлеметсіз бе, @Имя клиента!
@Название магазина дүкенін таңдағаныңыз үшін үлкен рақмет!
Тапсырысыңыз сәтті жеткізілді деп үміттенеміз 💛

Егер бәрі көңіліңізден шықса, төмендегі сілтеме арқылы біздің дүкеннің атауын көрсетіп, пікір қалдыра аласыз ба? Бұл біз үшін өте маңызды ⤵️
@Ссылка на отзыв

💬 Сілтеме белсенді болуы үшін осы хабарламаға жауап беруіңіз керек.

Здравствуйте, @Имя клиента!
Спасибо, что выбрали магазин @Название магазина!
Надеемся, заказ вам понравился и всё прошло отлично 💛

Если несложно, пожалуйста, оставьте отзыв, указав название нашего магазина, перейдя по ссылке ниже — для нас это действительно важно ⤵️
@Ссылка на отзыв

💬 Чтобы ссылка стала активной, просто ответьте на это сообщение любым словом.')

SET IDENTITY_INSERT OrderMessageTypes OFF

create table UserWhatsappMessageTemplates
(
    ID   int      NOT NULL IDENTITY (1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    UserID int NOT NULL,
    InstanceID int NOT NULL,
    OrderMessageTypeID int NOT NULL,
    IsActive bit NOT NULL,
    Template nvarchar(MAX) NULL,
    UpdatedDate datetime NULL,

    IsDelayActive bit NOT NULL default 0,
    DelayTime int NOT NULL default 0,
    DelayInput int NULL,
    DelayInputType int NULL,

    TimeRestrictionActive bit NOT NULL default 0,
    TimeRestrictionStartTime int NULL,
    TimeRestrictionEndTime int NULL,
    TimeRestrictionHourFrom int NULL,
    TimeRestrictionMinuteFrom int NULL,
    TimeRestrictionHourTo int NULL,
    TimeRestrictionMinuteTo int NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
    FOREIGN KEY(InstanceID) REFERENCES UserWhatsappInstances(ID),
    FOREIGN KEY(OrderMessageTypeID) REFERENCES OrderMessageTypes(ID)
)

create table UserWhatsappOrderMessages
(
    ID   int      NOT NULL IDENTITY (1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    UserID int NOT NULL,
    OrderID int NOT NULL,
    ShopID int NULL,
    InstanceID int NOT NULL,
    OrderMessageTypeID int NOT NULL,
    FromPhone nvarchar(100) NULL,
    ToPhone nvarchar(100) NULL,
    IsTestMode bit NOT NULL default 0,
    MessageText nvarchar(MAX) NULL,
    MessageID nvarchar(100) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
    FOREIGN KEY(OrderID) REFERENCES KaspiOrders(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID),
    FOREIGN KEY(InstanceID) REFERENCES UserWhatsappInstances(ID),
    FOREIGN KEY(OrderMessageTypeID) REFERENCES OrderMessageTypes(ID),

    INDEX UserWhatsappOrderMessages_MessageID_Index NONCLUSTERED (MessageID)
)

create table UserWhatsappOrderProcessAfterPhoneUpdate
(
    ID       int      NOT NULL IDENTITY (1,1),
    Date     datetime NOT NULL DEFAULT GetDate(),
    OrderID  int      NOT NULL,
    StatusID int      NULL,
    JustCreated bit   NULL default 1,
    PhoneUpdateDate datetime NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(OrderID) REFERENCES KaspiOrders(ID)
)

create table TaskJobsRuns
(
    ID   int      NOT NULL IDENTITY (1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    UserID INT NULL,
    ShopID INT NULL,
    TaskID INT NULL,
    FunctionName nvarchar(300) NULL,
    FinishedDate datetime NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
    FOREIGN KEY(TaskID) REFERENCES ScheduledTasks(ID)
)

create table KaspiMerchantProductStorePreOrder
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    ProductID int NOT NULL,
    StoreID int NOT NULL,
    PreOrderDays int NULL,
    PreOrderIncomeDate datetime NULL,
    MinQuant int NULL,
    PreorderMinQuantOn int NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ProductID) REFERENCES KaspiMerchantProducts(ID),
    FOREIGN KEY(StoreID) REFERENCES UserKaspiShopsStores(ID),
)

CREATE NONCLUSTERED INDEX KaspiMerchantProductStorePreOrder_MinQuant_Index ON [dbo].[KaspiMerchantProductStorePreOrder] ([MinQuant]) INCLUDE ([ProductID],[StoreID])

create table ExecutionLog
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Message nvarchar(MAX) NOT NULL,

    PRIMARY KEY(ID)
)

create table WhatsappContacts
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Phone nvarchar(100) NOT NULL,
    Name nvarchar(500) NULL,
    AvatarURL nvarchar(2000) NULL,
    AvatarAvailable bit NULL,
    AvatarGotDate datetime NULL,
    IsActive bit NOT NULL default 1,

    PRIMARY KEY(ID),
    INDEX WhatsappContacts_Phone_Index NONCLUSTERED (Phone)
)

create table UserWhatsappContacts
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    UserID int NOT NULL,
    InstanceID int NOT NULL,
    ContactID int NOT NULL,
    ChatName nvarchar(500) NULL,
    SenderContactName nvarchar(500) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
    FOREIGN KEY(InstanceID) REFERENCES UserWhatsappInstances(ID),
    FOREIGN KEY(ContactID) REFERENCES WhatsappContacts(ID)
)

create table UserWhatsappDelayedMessages
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    OrderID int NOT NULL,
    OrderMessageTypeID int NOT NULL,
    ProcessedTime datetime NULL,
    RemovedByCancel bit NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(OrderID) REFERENCES KaspiOrders(ID),
    FOREIGN KEY(OrderMessageTypeID) REFERENCES OrderMessageTypes(ID)
)

CREATE NONCLUSTERED INDEX UserWhatsappOrderMessages_OrderID_Index
    ON [dbo].[UserWhatsappOrderMessages] ([OrderID])

CREATE NONCLUSTERED INDEX KaspiOrders_ShopID_CreationDate_Index
    ON [dbo].[KaspiOrders] ([ShopID],[CreationDate])
    INCLUDE ([Code],[DeliveryCostForSeller],[StateID],[StatusID])

CREATE NONCLUSTERED INDEX KaspiOrders_ShopID_CreationDate__And_Incl_Index
    ON [dbo].[KaspiOrders] ([ShopID],[CreationDate])
    INCLUDE ([Code],[DeliveryCostForSeller],[StateID],[StatusID],[OrderStatusID])

create table KaspiProductDailySales
(
    ID int NOT NULL IDENTITY(1,1),
    Date date NOT NULL DEFAULT GetDate(),
    ProductID int NOT NULL,
    SalesCount int NOT NULL,
    SalesAmount decimal(15,2) NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ProductID) REFERENCES KaspiMerchantProducts(ID)
)

CREATE NONCLUSTERED INDEX Sessions_UserID_LastActivityDate_Index
    ON [dbo].[Sessions] ([UserID])
    INCLUDE ([LastActivityDate])

create table UserWhatsappInstancesShops
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    InstanceID int NOT NULL,
    ShopID int NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(InstanceID) REFERENCES UserWhatsappInstances(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID)
)

create table UserWhatsappMessages
(
    ID         int           NOT NULL IDENTITY(1,1),
    Date       datetime      NOT NULL DEFAULT GetDate(),
    InstanceID int           NULL,
    KaspiOrderID int         NULL,
    KaspiCustomerID int      NULL,
    UserContactID int NULL,
    Phone      nvarchar(100) NULL,
    Message    nvarchar(max)  NOT NULL,
    SentDate   datetime      NULL,
    MessageID  nvarchar(100) NULL,
    QuotedMessageID  nvarchar(100) NULL,
    QuotedMessage    nvarchar(max)  NULL,
    QuotedContactID int NULL,
    IsIncoming bit           NOT NULL,
    Description nvarchar(max) NULL,
    Title nvarchar(4000) NULL,
    PreviewType nvarchar(100) NULL,
    jpegThumbnail nvarchar(max) NULL,
    ForwardingScore int NULL,
    IsForwarded bit NULL,
    DownloadUrl nvarchar(2000) NULL,
    Caption nvarchar(max) NULL,
    FileName nvarchar(500) NULL,
    IsAnimated bit NULL,
    MimeType nvarchar(100) NULL,
    NameLocation nvarchar(500) NULL,
    Address nvarchar(1000) NULL,
    Latitude decimal(10,8) NULL,
    Longitude decimal(11,8) NULL,

    StatusID int NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(InstanceID) REFERENCES UserWhatsappInstances(ID),
    FOREIGN KEY(KaspiOrderID) REFERENCES KaspiOrders(ID),
    FOREIGN KEY(KaspiCustomerID) REFERENCES KaspiCustomers(ID),
    FOREIGN KEY(UserContactID) REFERENCES UserWhatsappContacts(ID),
    FOREIGN KEY(QuotedContactID) REFERENCES UserWhatsappContacts(ID),

    INDEX UserWhatsappMessages_MessageID_INDEX NONCLUSTERED (MessageID)
)

CREATE NONCLUSTERED INDEX UserWhatsappMessages_UserContactID_Index ON [dbo].[UserWhatsappMessages] ([UserContactID]) INCLUDE ([SentDate])

create table OrderStatusesHistory
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    OrderID int NOT NULL,
    StatusID int NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(OrderID) REFERENCES KaspiOrders(ID),
    FOREIGN KEY(StatusID) REFERENCES OrderStatuses(ID)
)

create table ReferralPayments
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    UserID int NOT NULL,
    PaymentRequestID int NULL,
    Amount decimal(15,2) NOT NULL,
    IsPayOut bit NOT NULL default 0,

    PRIMARY KEY(ID),
    FOREIGN KEY(UserID) REFERENCES Users(ID),
    FOREIGN KEY(PaymentRequestID) REFERENCES PaymentRequests(ID)
)

create table UserDumpingCommands
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    ProductID int NOT NULL,
    Price int NOT NULL,
    AssignedToExecuteDate datetime NULL,
    ExecutedDate datetime NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ProductID) REFERENCES KaspiMerchantProducts(ID)
)

CREATE NONCLUSTERED INDEX UserDumpingCommands_AssignedToExecuteDate_Index ON [dbo].[UserDumpingCommands] ([AssignedToExecuteDate])
CREATE NONCLUSTERED INDEX UserDumpingCommands_AssignedToExecuteDate_ProductID_Index ON [dbo].[UserDumpingCommands] ([AssignedToExecuteDate]) INCLUDE ([ProductID])
CREATE NONCLUSTERED INDEX UserDumpingCommands_ProductID_AssignedToExecuteDate_Index ON [dbo].[UserDumpingCommands] ([ProductID],[AssignedToExecuteDate])

create table KaspiMasterProductPriceHistory
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    MasterProductID int NOT NULL,
    MerchantID varchar(100) NOT NULL,
    Place int NOT NULL,
    Price int NOT NULL,
    MerchantName varchar(1000) NULL,
    OffersCount int NULL,

    Rating decimal(15,2) NULL,
    Reviews int NULL,
    Preorder int NULL,
    Delivery nvarchar(100) NULL,

    CityCode nvarchar(100) NULL,
    CityID int NULL,
    DeliveryDays int NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(MasterProductID) REFERENCES KaspiMasterProducts(ID),
    FOREIGN KEY(CityID) REFERENCES KaspiCities(ID)
)

create index KaspiMasterProductPriceHistory_MasterProductID_MerchantID_Index on KaspiMasterProductPriceHistory (MasterProductID,MerchantID)
    include (OffersCount, Place)

create table KaspiMarketingCampaignStates
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    State nvarchar(100) NOT NULL,

    PRIMARY KEY(ID)
)

create table KaspiMarketingCampaigns
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    ShopID int NOT NULL,
    CampaignID varchar(100) NOT NULL,
    Name nvarchar(500) NULL,
    DailyBudget decimal(15,2) NULL,
    StartDate datetime NULL,
    BiddingType nvarchar(100) NULL,
    DefaultBid decimal(15,2) NULL,
    StateID int NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID),
    FOREIGN KEY(StateID) REFERENCES KaspiMarketingCampaignStates(ID)
)

create table KaspiMarketingCampaignDailyStats
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    CampaignID int NOT NULL,
    StatDate date NOT NULL,
    Views int NULL,
    Clicks int NULL,
    Favourites int NULL,
    Carts int NULL,
    NumOrders int NULL,
    OrdersSum int NOT NULL,
    Cost decimal(15,2) NOT NULL,
    RecordTS int NULL,
    RecordDate datetime NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(CampaignID) REFERENCES KaspiMarketingCampaigns(ID)
)

create table KaspiMarketingCampaignProductsDailyStats
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    CampaignID int NOT NULL,
    StatDate date NOT NULL,
    ProductID int NOT NULL,
    Score decimal(15,2) NOT NULL,
    Bid decimal(15,2) NOT NULL,
    Views int NULL,
    Clicks int NULL,
    Favourites int NULL,
    Carts int NULL,
    NumOrders int NULL,
    OrdersSum int NOT NULL,
    Cost decimal(15,2) NOT NULL,
    Price decimal(15,2) NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(CampaignID) REFERENCES KaspiMarketingCampaigns(ID),
    FOREIGN KEY(ProductID) REFERENCES KaspiMerchantProducts(ID)
)

CREATE NONCLUSTERED INDEX KaspiMarketingCampaignProductsDailyStats_StatDate_Index ON [dbo].[KaspiMarketingCampaignProductsDailyStats] ([StatDate]) INCLUDE ([ProductID],[Cost])

create table KaspiMerchantProductsDumpingSaveHistory
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    ProductID int NULL,

    IsDumpingOn bit NULL,
    DumpingStep int NULL,
    DumpingMinPrice int NULL,
    DumpingIncreasePriceToMax bit null,
    DumpingExcludeMerchants varchar(2000) NULL,
    DumpingMinimumPlace int NULL,
    DumpingMaximumPlace int NULL,
    DumpingMaxPrice int NULL,

    DumpingFilterByDelivery bit NULL,
    DumpingMinDelivery int NULL,
    DumpingFilterByRating bit NULL,
    DumpingMinRating decimal(15,1) NULL,
    DumpingFilterByReviews bit NULL,
    DumpingMinReviews int NULL,

    ShopID int NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ProductID) REFERENCES KaspiMerchantProducts(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID)
)

create table KaspiMarketingBonusesCampaigns
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    ShopID int NOT NULL,
    CampaignID varchar(100) NOT NULL,
    Name nvarchar(500) NULL,
    StartDate datetime NULL,
    StateID int NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID),
    FOREIGN KEY(StateID) REFERENCES KaspiMarketingCampaignStates(ID)
)

create table KaspiMarketingBonusesCampaignDailyStats
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    CampaignID int NOT NULL,
    StatDate date NOT NULL,
    Transactions int NULL,
    OrderSum decimal(15,2) NULL,
    BonusSum decimal(15,2) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(CampaignID) REFERENCES KaspiMarketingBonusesCampaigns(ID)
)

create table KaspiMarketingBonusCampaignProductsDailyStats
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    CampaignID int NOT NULL,
    StatDate date NOT NULL,
    ProductID int NOT NULL,
    Score decimal(15,2) NOT NULL,
    Transactions int NULL,
    OrderSum decimal(15,2) NULL,
    BonusSum decimal(15,2) NULL,
    Price decimal(15,2) NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(CampaignID) REFERENCES KaspiMarketingBonusesCampaigns(ID),
    FOREIGN KEY(ProductID) REFERENCES KaspiMerchantProducts(ID)
)

create table KaspiMarketingReviewsCampaigns
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    ShopID int NOT NULL,
    CampaignID varchar(100) NOT NULL,
    Name nvarchar(500) NULL,
    StartDate datetime NULL,
    StateID int NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(ShopID) REFERENCES UserKaspiShops(ID),
    FOREIGN KEY(StateID) REFERENCES KaspiMarketingCampaignStates(ID)
)

create table KaspiMarketingReviewsCampaignDailyStats
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    CampaignID int NOT NULL,
    StatDate date NOT NULL,
    Notifications int NULL,
    Reviews int NULL,
    BonusSum decimal(15,2) NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(CampaignID) REFERENCES KaspiMarketingReviewsCampaigns(ID)
)

create table KaspiMarketingReviewsCampaignProductsDailyStats
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    CampaignID int NOT NULL,
    StatDate date NOT NULL,
    ProductID int NOT NULL,
    Score decimal(15,2) NOT NULL,
    Notifications int NULL,
    Reviews int NULL,
    BonusSum decimal(15,2) NULL,
    Price decimal(15,2) NOT NULL,

    PRIMARY KEY(ID),
    FOREIGN KEY(CampaignID) REFERENCES KaspiMarketingReviewsCampaigns(ID),
    FOREIGN KEY(ProductID) REFERENCES KaspiMerchantProducts(ID)
)

CREATE NONCLUSTERED INDEX KaspiOrders_CreationDate_ShopID_DeliveryCostForSeller_StatusID_Index ON [dbo].[KaspiOrders] ([CreationDate]) INCLUDE ([ShopID],[DeliveryCostForSeller],[StatusID])
CREATE NONCLUSTERED INDEX KaspiOrders_StoreID_CreationDate_ShopID_Index ON [dbo].[KaspiOrders] ([StoreID],[CreationDate]) INCLUDE ([ShopID])
CREATE NONCLUSTERED INDEX TelegramMessagesToSend_SessionID_Index ON [dbo].[TelegramMessagesToSend] ([SessionID])

create table SMSTypes
(
    ID  int NOT NULL IDENTITY(1,1) primary key,
    Name         varchar(200)    NOT NULL,
    Description  varchar(500)    NULL
)

insert into SMSTypes (Name, Description) values ('registration', N'Код для регистрации пользователя')

create table SMSCodes
(
    ID  int NOT NULL IDENTITY(1,1) primary key,
    SessionID         int            NOT null,
    Date              datetime       not null DEFAULT GetDate(),
    Code              varchar(200)   NOT NULL,
    UseDate           datetime       null,
    Phone             varchar(50)    NOT null,
    SMSTypeID         int            NOT null,
    NumberOfTries     int NOT NULL default 0,
    SignupCode        varchar(50) NULL,

    FOREIGN KEY (SessionID) REFERENCES Sessions(ID),
    FOREIGN KEY (SMSTypeID) REFERENCES SMSTypes(ID),
    INDEX SignupCode_Index  (SignupCode)
)

create table ShortLinks
(
    ID  int NOT NULL IDENTITY(1,1) primary key,
    Date              datetime       not null DEFAULT GetDate(),
    Code              varchar(20) NOT NULL,
    URL               varchar(500) NOT NULL,

    INDEX Code_Index UNIQUE (Code),
    INDEX URL_Index (URL)
)

create table ShortLinksClicks
(
    ID  int NOT NULL IDENTITY(1,1) primary key,
    Date              datetime       not null DEFAULT GetDate(),
    LinkID            int            not null,
    SessionID         int            not null,

    FOREIGN KEY (LinkID) REFERENCES ShortLinks(ID),
    FOREIGN KEY (SessionID) REFERENCES Sessions(ID)
)

create table UserKaspiShopsOTPRegistration
(
    ID  int NOT NULL IDENTITY(1,1) primary key,
    UserID           int            not null,
    Date             datetime       not null DEFAULT GetDate(),
    Phone            nvarchar(50)   NOT null,
    BotAssigned      nvarchar(100)  NULL,
    OTPSendResult    bit            NULL,
    RequestSentDate  datetime       null,
    RequestData      nvarchar(max)  NULL,
    OTPCode          nvarchar(20)   null,
    Name             nvarchar(200)  null,
    Email            nvarchar(200)  null,
    CreateStartedDate datetime       null,
    UserCreateResult bit            NULL,
    ConfirmedDate    datetime       null,
    Password         nvarchar(100)  null,
    PasswordSetDate  datetime       null,
    APIToken         nvarchar(100)  null,
    ResultData       nvarchar(max)  null,
    MerchantsData    nvarchar(max)  null,
    MerchantSelected nvarchar(200)  null,

    FOREIGN KEY (UserID) REFERENCES Users(ID)
)

CREATE NONCLUSTERED INDEX KaspiMarketingReviewsCampaignProductsDailyStats_ProductID_StatDate_Index ON [dbo].[KaspiMarketingReviewsCampaignProductsDailyStats] ([ProductID],[StatDate])

CREATE NONCLUSTERED INDEX KaspiMarketingCampaignProductsDailyStats_ProductID_StatDate_Index ON [dbo].[KaspiMarketingCampaignProductsDailyStats] ([ProductID],[StatDate]) INCLUDE ([Cost])

CREATE NONCLUSTERED INDEX KaspiMarketingBonusCampaignProductsDailyStats_ProductID_StatDate_Index ON [dbo].[KaspiMarketingBonusCampaignProductsDailyStats] ([ProductID],[StatDate])

create index KaspiMerchantProductStorePreOrder_StoreID_ProductID_Index on KaspiMerchantProductStorePreOrder(StoreID, ProductID)
include (PreOrderDays, MinQuant, PreOrderIncomeDate)



update KaspiOrderDetails set OrderDeliveryCost=(select DeliveryCostForSeller from KaspiOrders a where a.ID=KaspiOrderDetails.KaspiOrderID)
where (select count(ID) from KaspiOrderDetails a where a.KaspiOrderID=KaspiOrderDetails.KaspiOrderID)=1
  and
    (
        OrderDeliveryCost IS NULL
            OR
        (select DeliveryCostForSeller from KaspiOrders a where a.ID=KaspiOrderDetails.KaspiOrderID) != (select sum(OrderDeliveryCost) from KaspiOrderDetails a where a.KaspiOrderID=KaspiOrderDetails.KaspiOrderID)
        )


update KaspiOrderDetails set OrderDeliveryCost=cast(((select DeliveryCostForSeller from KaspiOrders where ID=KaspiOrderDetails.KaspiOrderID)/(select count(ID) from KaspiOrderDetails a where a.KaspiOrderID=KaspiOrderDetails.KaspiOrderID)) as INT)
where (select count(ID) from KaspiOrderDetails a where a.KaspiOrderID=KaspiOrderDetails.KaspiOrderID)>1
  and
    (
        OrderDeliveryCost IS NULL
            OR
        (select DeliveryCostForSeller from KaspiOrders a where a.ID=KaspiOrderDetails.KaspiOrderID) != (select sum(OrderDeliveryCost) from KaspiOrderDetails a where a.KaspiOrderID=KaspiOrderDetails.KaspiOrderID)
        )

update KaspiOrderDetails set OrderDeliveryCost
                                 =
                                 (select DeliveryCostForSeller from KaspiOrders where ID=KaspiOrderDetails.KaspiOrderID)
                                     - (select sum(OrderDeliveryCost) from KaspiOrderDetails a where a.KaspiOrderID=KaspiOrderDetails.KaspiOrderID and a.ID!=KaspiOrderDetails.ID)

where (select count(ID) from KaspiOrderDetails a where a.KaspiOrderID=KaspiOrderDetails.KaspiOrderID)>1 and
    (select DeliveryCostForSeller from KaspiOrders a where a.ID=KaspiOrderDetails.KaspiOrderID) != (select sum(OrderDeliveryCost) from KaspiOrderDetails a where a.KaspiOrderID=KaspiOrderDetails.KaspiOrderID) and
    ID=(select max(ID) from KaspiOrderDetails a where a.KaspiOrderID=KaspiOrderDetails.KaspiOrderID)

select * from KaspiOrders o where (select sum(OrderDeliveryCost) from KaspiOrderDetails where KaspiOrderID=o.ID) IS NULL OR
    DeliveryCostForSeller!=(select sum(OrderDeliveryCost) from KaspiOrderDetails where KaspiOrderID=o.ID)

select * from KaspiOrderDetails where OrderDeliveryCost IS NULL
create table KaspiTransactions
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    IP nvarchar(100) NULL,
    Command nvarchar(100) NULL,
    Txn_ID nvarchar(100) NULL,
    Txn_Date nvarchar(100) NULL,
    Account nvarchar(100) NULL,
    TxnSum decimal(15,2) NULL,
    Response nvarchar(MAX) NULL,

    PRIMARY KEY(ID)
)

CREATE INDEX KaspiTransactions_Txn_ID_Index ON KaspiTransactions(Txn_ID) INCLUDE (Command)

CREATE NONCLUSTERED INDEX KaspiCustomers_Phone_Index ON [dbo].[KaspiCustomers] ([Phone])

CREATE NONCLUSTERED INDEX KaspiMerchantProductStorePreOrder_ProductID_index ON [dbo].[KaspiMerchantProductStorePreOrder] ([ProductID])

CREATE NONCLUSTERED INDEX KaspiMerchantProducts_IsAvailable_IsDumpingOn_KaspiMasterProductID_ShopID_Index ON [dbo].[KaspiMerchantProducts] ([IsAvailable],[IsDumpingOn]) INCLUDE ([KaspiMasterProductID],[ShopID])

CREATE NONCLUSTERED INDEX KaspiOrders_ShopID_StateID_ActualDeliveryDateTS_Index ON [dbo].[KaspiOrders] ([ShopID],[StateID],[ActualDeliveryDateTS])

create table KaspiCities
(
    ID int NOT NULL IDENTITY(1,1),
    Date datetime NOT NULL DEFAULT GetDate(),
    Code nvarchar(100) NOT NULL,
    Name nvarchar(1000) NOT NULL,

    PRIMARY KEY(ID)
)

create index KaspiCities_Code_Index on KaspiCities(Code) include (ID)

insert into KaspiCities (Code,Name) values ('195253200', 'Абай (Алмат.обл)')
insert into KaspiCities (Code,Name) values ('353220100', 'Абай (Караганд.обл)')
insert into KaspiCities (Code,Name) values ('515433100', 'Абай (Турк.Обл.)')
insert into KaspiCities (Code,Name) values ('194033100', 'Ават (Алм.Обл)')
insert into KaspiCities (Code,Name) values ('396430100', 'Айет')
insert into KaspiCities (Code,Name) values ('434430100', 'Айтеке Би')
insert into KaspiCities (Code,Name) values ('314033100', 'Айша-Биби (Жамб.Обл)')
insert into KaspiCities (Code,Name) values ('234230100', 'Аккистау')
insert into KaspiCities (Code,Name) values ('113220100', 'Акколь')
insert into KaspiCities (Code,Name) values ('116630100', 'Акмол (Малиновка)')
insert into KaspiCities (Code,Name) values ('273620100', 'Аксай')
insert into KaspiCities (Code,Name) values ('551610000', 'Аксу (Павлод.обл)')
insert into KaspiCities (Code,Name) values ('356430100', 'Аксу-Аюлы')
insert into KaspiCities (Code,Name) values ('515230100', 'Аксукент')
insert into KaspiCities (Code,Name) values ('471010000', 'Актау')
insert into KaspiCities (Code,Name) values ('151010000', 'Актобе')
insert into KaspiCities (Code,Name) values ('196837100', 'Алатау (Жетыген)')
insert into KaspiCities (Code,Name) values ('153220100', 'Алга (Актюб.обл)')
insert into KaspiCities (Code,Name) values ('196433100', 'Алдаберген')
insert into KaspiCities (Code,Name) values ('750000000', 'Алматы')
insert into KaspiCities (Code,Name) values ('634820100', 'Алтай')
insert into KaspiCities (Code,Name) values ('393631100', 'Аманкарагай')
insert into KaspiCities (Code,Name) values ('433220100', 'Аральск')
insert into KaspiCities (Code,Name) values ('196253200', 'Аркабай')
insert into KaspiCities (Code,Name) values ('391610000', 'Аркалык')
insert into KaspiCities (Code,Name) values ('113430100', 'Аршалы')
insert into KaspiCities (Code,Name) values ('511610000', 'Арысь')
insert into KaspiCities (Code,Name) values ('314030100', 'Аса (Жамб.обл.)')
insert into KaspiCities (Code,Name) values ('710000000', 'Астана')
insert into KaspiCities (Code,Name) values ('113630100', 'Астраханка')
insert into KaspiCities (Code,Name) values ('514443100', 'Асыката')
insert into KaspiCities (Code,Name) values ('514459100', 'Атакент (Турк.обл.)')
insert into KaspiCities (Code,Name) values ('113820100', 'Атбасар')
insert into KaspiCities (Code,Name) values ('231010000', 'Атырау')
insert into KaspiCities (Code,Name) values ('393630100', 'Аулиеколь')
insert into KaspiCities (Code,Name) values ('633420100', 'Аягоз')
insert into KaspiCities (Code,Name) values ('154030100', 'Бадамша')
insert into KaspiCities (Code,Name) values ('194043100', 'Байдибек бия')
insert into KaspiCities (Code,Name) values ('431910000', 'Байконыр')
insert into KaspiCities (Code,Name) values ('196847100', 'Байсерке (Дмитриевка)')
insert into KaspiCities (Code,Name) values ('194067100', 'Байтерек (Новоалексеевка)')
insert into KaspiCities (Code,Name) values ('196435100', 'Бактыбай Жолбарысулы')
insert into KaspiCities (Code,Name) values ('116430100', 'Балкашино')
insert into KaspiCities (Code,Name) values ('194830100', 'Балпык Би')
insert into KaspiCities (Code,Name) values ('351610000', 'Балхаш')
insert into KaspiCities (Code,Name) values ('475042100', 'Батыр')
insert into KaspiCities (Code,Name) values ('314230100', 'Бауржан Момышулы (Жамб.Обл.)')
insert into KaspiCities (Code,Name) values ('475235100', 'Баутино')
insert into KaspiCities (Code,Name) values ('553630100', 'Баянаул')
insert into KaspiCities (Code,Name) values ('473630100', 'Бейнеу')
insert into KaspiCities (Code,Name) values ('196235100', 'Белбулак')
insert into KaspiCities (Code,Name) values ('634037100', 'Белоусовка')
insert into KaspiCities (Code,Name) values ('195233100', 'Береке')
insert into KaspiCities (Code,Name) values ('196243100', 'Бесагаш')
insert into KaspiCities (Code,Name) values ('633600000', 'Бескарагай (Бурас)')
insert into KaspiCities (Code,Name) values ('595030100', 'Бесколь')
insert into KaspiCities (Code,Name) values ('116837100', 'Бозайгыр')
insert into KaspiCities (Code,Name) values ('395630100', 'Боровской')
insert into KaspiCities (Code,Name) values ('633830100', 'Бородулиха')
insert into KaspiCities (Code,Name) values ('354030100', 'Ботакара')
insert into KaspiCities (Code,Name) values ('593620100', 'Булаево')
insert into KaspiCities (Code,Name) values ('117035100', 'Бурабай (Боровое)')
insert into KaspiCities (Code,Name) values ('313635100', 'Бурыл')
insert into KaspiCities (Code,Name) values ('395439100', 'Владимировка')
insert into KaspiCities (Code,Name) values ('634030100', 'Глубокое')
insert into KaspiCities (Code,Name) values ('196249100', 'Гульдала')
insert into KaspiCities (Code,Name) values ('394030100', 'Денисовка')
insert into KaspiCities (Code,Name) values ('115420100', 'Державинск')
insert into KaspiCities (Code,Name) values ('271000200', 'Деркуль')
insert into KaspiCities (Code,Name) values ('235235100', 'Доссор')
insert into KaspiCities (Code,Name) values ('196849200', 'Екпинды')
insert into KaspiCities (Code,Name) values ('196255400', 'Енбекши (Алм.Обл.)')
insert into KaspiCities (Code,Name) values ('114600000', 'Ерейментау')
insert into KaspiCities (Code,Name) values ('231045100', 'Еркинкала')
insert into KaspiCities (Code,Name) values ('194020100', 'Есик')
insert into KaspiCities (Code,Name) values ('114820100', 'Есиль')
insert into KaspiCities (Code,Name) values ('196839200', 'Жайнак (Комсомол)')
insert into KaspiCities (Code,Name) values ('352035100', 'Жайрем')
insert into KaspiCities (Code,Name) values ('115230100', 'Жаксы')
insert into KaspiCities (Code,Name) values ('433630100', 'Жалагаш')
insert into KaspiCities (Code,Name) values ('113640100', 'Жалтыр')
insert into KaspiCities (Code,Name) values ('354430100', 'Жанаарка')
insert into KaspiCities (Code,Name) values ('434030100', 'Жанакорган')
insert into KaspiCities (Code,Name) values ('196247500', 'Жаналык')
insert into KaspiCities (Code,Name) values ('471810000', 'Жанаозен')
insert into KaspiCities (Code,Name) values ('316020100', 'Жанатас')
insert into KaspiCities (Code,Name) values ('194047100', 'Жанашар')
insert into KaspiCities (Code,Name) values ('274030100', 'Жангала')
insert into KaspiCities (Code,Name) values ('193230100', 'Жансугуров')
insert into KaspiCities (Code,Name) values ('196833200', 'Жапек Батыра')
insert into KaspiCities (Code,Name) values ('195620100', 'Жаркент')
insert into KaspiCities (Code,Name) values ('351810000', 'Жезказган')
insert into KaspiCities (Code,Name) values ('633845100', 'Жезкент')
insert into KaspiCities (Code,Name) values ('554230100', 'Железинка')
insert into KaspiCities (Code,Name) values ('474239100', 'Жетыбай')
insert into KaspiCities (Code,Name) values ('514420100', 'Жетысай')
insert into KaspiCities (Code,Name) values ('113433100', 'Жибек-Жолы (Акмол.обл)')
insert into KaspiCities (Code,Name) values ('515247200', 'Жибек-Жолы (Туркестан.обл)')
insert into KaspiCities (Code,Name) values ('195237100', 'Жибек-Жолы (Шамалган)')
insert into KaspiCities (Code,Name) values ('394420100', 'Житикара')
insert into KaspiCities (Code,Name) values ('434630100', 'Жосалы')
insert into KaspiCities (Code,Name) values ('191633100', 'Заречное (Алм.обл.)')
insert into KaspiCities (Code,Name) values ('271035100', 'Зачаганск')
insert into KaspiCities (Code,Name) values ('115630100', 'Зеренда')
insert into KaspiCities (Code,Name) values ('234030100', 'Индербор')
insert into KaspiCities (Code,Name) values ('195247100', 'Иргели')
insert into KaspiCities (Code,Name) values ('195233400', 'Исаево')
insert into KaspiCities (Code,Name) values ('193459100', 'Кабанбай (Алмат.Обл)')
insert into KaspiCities (Code,Name) values ('116665100', 'Кабанбай батыр (Акмол. Обл)')
insert into KaspiCities (Code,Name) values ('514030100', 'Казыгурт')
insert into KaspiCities (Code,Name) values ('314853200', 'Кайнар (Жамбыл.обл.)')
insert into KaspiCities (Code,Name) values ('634430100', 'Калбатау')
insert into KaspiCities (Code,Name) values ('394830100', 'Камысты')
insert into KaspiCities (Code,Name) values ('154820100', 'Кандыагаш')
insert into KaspiCities (Code,Name) values ('395030100', 'Карабалык')
insert into KaspiCities (Code,Name) values ('196430100', 'Карабулак')
insert into KaspiCities (Code,Name) values ('615253100', 'Карабулак  (Турк. Обл.)')
insert into KaspiCities (Code,Name) values ('196253400', 'Карабулак (Алм.Обл.)')
insert into KaspiCities (Code,Name) values ('351010000', 'Караганда')
insert into KaspiCities (Code,Name) values ('352010000', 'Каражал')
insert into KaspiCities (Code,Name) values ('116648700', 'Каражар (Акмол.Обл)')
insert into KaspiCities (Code,Name) values ('116648100', 'Караоткель')
insert into KaspiCities (Code,Name) values ('316220100', 'Каратау')
insert into KaspiCities (Code,Name) values ('153630100', 'Карауылкельды (Байганин)')
insert into KaspiCities (Code,Name) values ('354820100', 'Каркаралинск')
insert into KaspiCities (Code,Name) values ('512039100', 'Карнак')
insert into KaspiCities (Code,Name) values ('195220100', 'Каскелен')
insert into KaspiCities (Code,Name) values ('636230100', 'Касыма Кайсенова')
insert into KaspiCities (Code,Name) values ('392435100', 'Качар')
insert into KaspiCities (Code,Name) values ('554830100', 'Кашыр (Теренколь)')
insert into KaspiCities (Code,Name) values ('314837100', 'Кенен')
insert into KaspiCities (Code,Name) values ('551043100', 'Кенжеколь')
insert into KaspiCities (Code,Name) values ('612010000', 'Кентау')
insert into KaspiCities (Code,Name) values ('195233500', 'Кокозек')
insert into KaspiCities (Code,Name) values ('635030100', 'Кокпекты (Абай.обл.)')
insert into KaspiCities (Code,Name) values ('195247400', 'Коксай')
insert into KaspiCities (Code,Name) values ('515847100', 'Коксайек')
insert into KaspiCities (Code,Name) values ('111010000', 'Кокшетау')
insert into KaspiCities (Code,Name) values ('195255400', 'Кольди')
insert into KaspiCities (Code,Name) values ('191610000', 'Конаев (Капшагай)')
insert into KaspiCities (Code,Name) values ('314851205', 'Кордай')
insert into KaspiCities (Code,Name) values ('391010000', 'Костанай')
insert into KaspiCities (Code,Name) values ('116651100', 'Косшы')
insert into KaspiCities (Code,Name) values ('116645100', 'Коянды')
insert into KaspiCities (Code,Name) values ('633851100', 'Красный Яр')
insert into KaspiCities (Code,Name) values ('315030100', 'Кулан')
insert into KaspiCities (Code,Name) values ('233620100', 'Кульсары')
insert into KaspiCities (Code,Name) values ('231035300', 'Курмангазы (Ганюшкино)')
insert into KaspiCities (Code,Name) values ('353263100', 'Курминское')
insert into KaspiCities (Code,Name) values ('632210000', 'Курчатов')
insert into KaspiCities (Code,Name) values ('635230100', 'Курчум')
insert into KaspiCities (Code,Name) values ('431010000', 'Кызылорда')
insert into KaspiCities (Code,Name) values ('196253500', 'Кызылту')
insert into KaspiCities (Code,Name) values ('515820100', 'Ленгер')
insert into KaspiCities (Code,Name) values ('551045100', 'Ленинский')
insert into KaspiCities (Code,Name) values ('392010000', 'Лисаковск')
insert into KaspiCities (Code,Name) values ('636473100', 'Маканчи')
insert into KaspiCities (Code,Name) values ('235230100', 'Макат')
insert into KaspiCities (Code,Name) values ('114020100', 'Макинск')
insert into KaspiCities (Code,Name) values ('595220100', 'Мамлютка')
insert into KaspiCities (Code,Name) values ('475030100', 'Мангистау')
insert into KaspiCities (Code,Name) values ('154630100', 'Мартук')
insert into KaspiCities (Code,Name) values ('314847100', 'Масанчи (Жамбыл.обл.)')
insert into KaspiCities (Code,Name) values ('235630100', 'Махамбет')
insert into KaspiCities (Code,Name) values ('195255500', 'Мерей')
insert into KaspiCities (Code,Name) values ('315430100', 'Мерке')
insert into KaspiCities (Code,Name) values ('355657100', 'Молодёжный')
insert into KaspiCities (Code,Name) values ('234847100', 'Мукур')
insert into KaspiCities (Code,Name) values ('196833100', 'Мухаметжан Туймебаева')
insert into KaspiCities (Code,Name) values ('194257100', 'Мынбаево')
insert into KaspiCities (Code,Name) values ('514481100', 'Мырзакент')
insert into KaspiCities (Code,Name) values ('634835100', 'Новая Бухтарма')
insert into KaspiCities (Code,Name) values ('596630100', 'Новоишимское (СКО)')
insert into KaspiCities (Code,Name) values ('355230100', 'Нура (Караганд.обл.)')
insert into KaspiCities (Code,Name) values ('512649100', 'Орангай')
insert into KaspiCities (Code,Name) values ('355630100', 'Осакаровка')
insert into KaspiCities (Code,Name) values ('314851100', 'Отар')
insert into KaspiCities (Code,Name) values ('196830100', 'Отеген батыр')
insert into KaspiCities (Code,Name) values ('551010000', 'Павлодар')
insert into KaspiCities (Code,Name) values ('551041100', 'Павлодарское')
insert into KaspiCities (Code,Name) values ('196253100', 'Панфилово')
insert into KaspiCities (Code,Name) values ('591010000', 'Петропавловск')
insert into KaspiCities (Code,Name) values ('276253100', 'Подстепное')
insert into KaspiCities (Code,Name) values ('634049100', 'Прапорщиково')
insert into KaspiCities (Code,Name) values ('594630100', 'Пресновка')
insert into KaspiCities (Code,Name) values ('352110000', 'Приозeрск')
insert into KaspiCities (Code,Name) values ('195253100', 'Райымбек')
insert into KaspiCities (Code,Name) values ('156420100', 'Риддер')
insert into KaspiCities (Code,Name) values ('392410000', 'Рудный')
insert into KaspiCities (Code,Name) values ('234853100', 'Сагиз')
insert into KaspiCities (Code,Name) values ('433257100', 'Саксаульский')
insert into KaspiCities (Code,Name) values ('635063100', 'Самарское')
insert into KaspiCities (Code,Name) values ('352210000', 'Сарань')
insert into KaspiCities (Code,Name) values ('196020100', 'Сарканд')
insert into KaspiCities (Code,Name) values ('515420100', 'Сарыагаш')
insert into KaspiCities (Code,Name) values ('313630100', 'Сарыкемер')
insert into KaspiCities (Code,Name) values ('396230100', 'Сарыколь')
insert into KaspiCities (Code,Name) values ('194630100', 'Сарыозек')
insert into KaspiCities (Code,Name) values ('352310000', 'Сатпаев')
insert into KaspiCities (Code,Name) values ('316033100', 'Саудакент')
insert into KaspiCities (Code,Name) values ('593230100', 'Саумалколь')
insert into KaspiCities (Code,Name) values ('632810000', 'Семей')
insert into KaspiCities (Code,Name) values ('595620100', 'Сергеевка')
insert into KaspiCities (Code,Name) values ('634821100', 'Серебрянск')
insert into KaspiCities (Code,Name) values ('595830100', 'Смирново')
insert into KaspiCities (Code,Name) values ('552253100', 'Солнечный (Павл.обл.)')
insert into KaspiCities (Code,Name) values ('512635100', 'Староикан')
insert into KaspiCities (Code,Name) values ('111810000', 'Степногорск')
insert into KaspiCities (Code,Name) values ('636269100', 'Таврическое')
insert into KaspiCities (Code,Name) values ('111600100', 'Тайтобе')
insert into KaspiCities (Code,Name) values ('596020100', 'Тайынша')
insert into KaspiCities (Code,Name) values ('116672100', 'Талапкер (Акмол.Обл)')
insert into KaspiCities (Code,Name) values ('196220100', 'Талгар')
insert into KaspiCities (Code,Name) values ('191010000', 'Талдыкорган')
insert into KaspiCities (Code,Name) values ('311010000', 'Тараз')
insert into KaspiCities (Code,Name) values ('433259700', 'Тасбогет')
insert into KaspiCities (Code,Name) values ('276030100', 'Таскала (ЗКО)')
insert into KaspiCities (Code,Name) values ('192610000', 'Текели')
insert into KaspiCities (Code,Name) values ('514630100', 'Темирлановка')
insert into KaspiCities (Code,Name) values ('352410000', 'Темиртау')
insert into KaspiCities (Code,Name) values ('434830100', 'Теренозек')
insert into KaspiCities (Code,Name) values ('395430100', 'Тобыл (Затобольск)')
insert into KaspiCities (Code,Name) values ('316630100', 'Толе би (Жамбыл. обл)')
insert into KaspiCities (Code,Name) values ('353285100', 'Топар')
insert into KaspiCities (Code,Name) values ('514483400', 'Торткуль')
insert into KaspiCities (Code,Name) values ('196245100', 'Туздыбастау')
insert into KaspiCities (Code,Name) values ('516030100', 'Турар Рыскулов')
insert into KaspiCities (Code,Name) values ('233635100', 'Тургызба')
insert into KaspiCities (Code,Name) values ('512610000', 'Туркестан')
insert into KaspiCities (Code,Name) values ('234245100', 'Тущыкудык')
insert into KaspiCities (Code,Name) values ('516063100', 'Тюлькубас')
insert into KaspiCities (Code,Name) values ('634049400', 'Уварово')
insert into KaspiCities (Code,Name) values ('396630100', 'Узунколь')
insert into KaspiCities (Code,Name) values ('194230100', 'Узынагаш')
insert into KaspiCities (Code,Name) values ('271010000', 'Уральск')
insert into KaspiCities (Code,Name) values ('636430100', 'Урджар')
insert into KaspiCities (Code,Name) values ('631010000', 'Усть-Каменогорск')
insert into KaspiCities (Code,Name) values ('353641300', 'Ушарал')
insert into KaspiCities (Code,Name) values ('195020100', 'Уштобе')
insert into KaspiCities (Code,Name) values ('396830100', 'Федоровка (Кост.Обл)')
insert into KaspiCities (Code,Name) values ('475220100', 'Форт-Шевченко')
insert into KaspiCities (Code,Name) values ('156020100', 'Хромтау')
insert into KaspiCities (Code,Name) values ('273230100', 'Чапаев (ЗКО)')
insert into KaspiCities (Code,Name) values ('196855100', 'Чапаево')
insert into KaspiCities (Code,Name) values ('634069100', 'Черемшанка')
insert into KaspiCities (Code,Name) values ('196630100', 'Чунджа')
insert into KaspiCities (Code,Name) values ('634643300', 'Шалкар (Актюб.обл)')
insert into KaspiCities (Code,Name) values ('634421100', 'Шар (Чарск)')
insert into KaspiCities (Code,Name) values ('555259100', 'Шарбакты')
insert into KaspiCities (Code,Name) values ('616420100', 'Шардара')
insert into KaspiCities (Code,Name) values ('352835100', 'Шахан')
insert into KaspiCities (Code,Name) values ('352810000', 'Шахтинск')
insert into KaspiCities (Code,Name) values ('513630100', 'Шаян')
insert into KaspiCities (Code,Name) values ('194083100', 'Шелек')
insert into KaspiCities (Code,Name) values ('636820100', 'Шемонаиха')
insert into KaspiCities (Code,Name) values ('191637100', 'Шенгельды')
insert into KaspiCities (Code,Name) values ('474630100', 'Шетпе')
insert into KaspiCities (Code,Name) values ('552257100', 'Шидерты')
insert into KaspiCities (Code,Name) values ('117055900', 'Шиели')
insert into KaspiCities (Code,Name) values ('515630100', 'Шолаккорган')
insert into KaspiCities (Code,Name) values ('116830100', 'Шортанды')
insert into KaspiCities (Code,Name) values ('316621100', 'Шу')
insert into KaspiCities (Code,Name) values ('155630000', 'Шубаркудук')
insert into KaspiCities (Code,Name) values ('632865100', 'Шульбинск')
insert into KaspiCities (Code,Name) values ('511010000', 'Шымкент')
insert into KaspiCities (Code,Name) values ('117020100', 'Щучинск')
insert into KaspiCities (Code,Name) values ('552210000', 'Экибастуз')
insert into KaspiCities (Code,Name) values ('154823100', 'Эмба')
insert into KaspiCities (Code,Name) values ('353249100', 'Юбилейное(Карг.Обл)')
insert into KaspiCities (Code,Name) values ('594230100', 'Явленка')

CREATE NONCLUSTERED INDEX KaspiOrders_ShopID_CreationDate_TotalPrice_Index ON [dbo].[KaspiOrders] ([ShopID],[CreationDate]) INCLUDE ([TotalPrice])

CREATE TABLE KaspiCategoriesDocPercent
(
    ID int IDENTITY(1,1) NOT NULL PRIMARY KEY,
    Date DATETIME NOT NULL DEFAULT GetDate(),
    Cat1 nvarchar(500) NULL,
    Cat2 nvarchar(500) NULL,
    Cat3 nvarchar(500) NULL,
    Cat4 nvarchar(500) NULL,
    Cat5 nvarchar(500) NULL,
    CommissionNoNDS decimal(15,2) NULL,
    CommissionWithNDS decimal(15,2) NULL
)

create index KaspiCategoriesDocPercent_Cat5_Index on KaspiCategoriesDocPercent(Cat5) include (CommissionWithNDS)

CREATE TABLE MutexLocks
(
    ID   int IDENTITY (1,1) NOT NULL PRIMARY KEY,
    DateStart DATETIME           NOT NULL DEFAULT GetDate(),
    DateEnd DATETIME           NOT NULL DEFAULT GetDate(),
    Code nvarchar(100) NOT NULL
)

insert into MutexLocks (Code) values ('get_item_price_jobs')

CREATE TABLE InfraJobRunners
(
    ID   int IDENTITY (1,1) NOT NULL PRIMARY KEY,
    Date DATETIME           NOT NULL DEFAULT GetDate(),
    LastDate DATETIME       NULL,
    Code nvarchar(100) NOT NULL,
    LastSent DATETIME       NULL
)

insert into InfraJobRunners (Code) values ('MY_HOME'), ('OFFICE1'), ('OFFICE2')

insert into Settings (Name, Setting)
values
    ('AMO_SECRET_KEY', ''),
    ('AMO_CLIENT_ID', ''),
    ('AMO_ACCESS_TOKEN', ''),
    ('AMO_REFRESH_TOKEN', ''),
    ('AMO_REDIRECT_URL', 'https://my.profitbot.kz/oauth'),
    ('AMO_BASE_URL', 'https://amocrm.ru')

insert into Settings (Name, Setting)
values
    ('DoNotIncreasePricesSetting', '0')

CREATE TABLE UserEditLogTypes
(
    ID   int IDENTITY (1,1) NOT NULL PRIMARY KEY,
    Name nvarchar(500) NULL
)

insert into UserEditLogTypes (Name) values (N'Изменение даты подписки')

CREATE TABLE UserEditLog
(
    ID   int IDENTITY (1,1) NOT NULL PRIMARY KEY,
    Date DATETIME           NOT NULL DEFAULT GetDate(),
    UserID int NOT NULL,
    EditUserID int NOT NULL,
    EditTypeID int NOT NULL,
    ServiceID int NULL,
    NewValue nvarchar(500) NULL,

    FOREIGN KEY (EditTypeID) REFERENCES UserEditLogTypes(ID),
    FOREIGN KEY (UserID) REFERENCES Users(ID),
    FOREIGN KEY (EditUserID) REFERENCES Users(ID),
    FOREIGN KEY (ServiceID) REFERENCES UserServices(ID)
)

create function [dbo].[GetDeliveryCost](@Price decimal(15,2))
returns int
as
begin
    declare @Cost int

    if @Price < 1000
        set @Cost = 57
    else if @Price  < 3000
        set @Cost = 173
    else if @Price  < 5000
        set @Cost = 231
    else if @Price  < 10000
        set @Cost = 927
    else
        set @Cost = 2145

    return  @Cost
end

create table ProductEditsHistory
(
    ID int NOT NULL IDENTITY(1,1) PRIMARY KEY,
    Date datetime NOT NULL DEFAULT GetDate(),
    ProductID int NOT NULL,
    PriceEdit bit NOT NULL DEFAULT 0,
    NalEdit bit NOT NULL DEFAULT 0,
    ProcessedDate datetime NULL,

    FOREIGN KEY (ProductID) REFERENCES KaspiMerchantProducts(ID)
)

create index ProductEditsHistory_ProductID_ProcessedDate_Index on ProductEditsHistory(ProductID, ProcessedDate) include (ID)

alter table UserDumpingCommands add PriceEditHistoryID int NULL
alter table UserDumpingCommands add foreign key (PriceEditHistoryID) references ProductEditsHistory(ID)

CREATE NONCLUSTERED INDEX UserDumpingCommands_ProductID_Date_Index ON
    [dbo].[UserDumpingCommands] ([ProductID],[Date])
    INCLUDE ([Price],[AssignedToExecuteDate],[ExecutedDate],[PriceEditHistoryID])

create table KaspiMerchantProductsItemUnavailable
(
    ID int NOT NULL IDENTITY (1,1) PRIMARY KEY,
    Date DateTime NOT NULL default GetDate(),
    ItemID int NOT NULL,

    FOREIGN KEY (ItemID) references KaspiMerchantProducts(ID)

)

create table KaspiMerchantProductsSyncItemsData
(
    ID int NOT NULL IDENTITY (1,1) PRIMARY KEY,
    Date DateTime NOT NULL default GetDate(),
    ShopID int NOT NULL,
    ActiveItemsCount int NULL,
    InActiveItemsCount int NULL,
    TotalCount int null,
    CodesCount int NULL,
    ItemsDisabled int NULL,
    ItemsActivated int null,
    ItemsDeactivated int null,

    FOREIGN KEY (ShopID) references UserKaspiShops(ID)
)

create table KaspiEmailOTPCodes
(
    ID int NOT NULL IDENTITY (1,1) PRIMARY KEY,
    Date DateTime NOT NULL default GetDate(),
    Email nvarchar(500) NOT NULL,
    Code nvarchar(10) NOT NULL,
    UsedDate DateTime NULL,
)

create table KaspiShopCredentials
(
    ID int NOT NULL IDENTITY (1,1) PRIMARY KEY,
    Date DateTime NOT NULL default GetDate(),
    ShopID int NOT NULL references UserKaspiShops(ID),
    AccessData nvarchar(3000) NOT NULL,
    IsActive bit NOT NULL,
    DeactivateDate DateTime NULL,
)

create table UserDumpingLoadTracker
(
    ID int NOT NULL IDENTITY (1,1) PRIMARY KEY,
    Date DateTime NOT NULL default GetDate(),
    CommandID int NOT NULL references UserDumpingCommands(ID),
    FileID nvarchar(500) NOT NULL,
    CompletedDate DateTime NULL,
)