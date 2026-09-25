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
