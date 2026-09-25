package model

import "time"

// Статусы кабинетного онбординга Kaspi. Продавец проходит их по шагам; UI кабинета
// опрашивает статус и показывает нужный экран (ввод кода, выбор кабинета, готово).
const (
	OnboardingStarted       = "started"          // поставлена задача отправки SMS-кода владельцу
	OnboardingOTPSent       = "otp_sent"         // код отправлен, ждём ввода продавцом
	OnboardingVerifying     = "verifying"        // код отправлен на проверку (создание сотрудника)
	OnboardingNeedMerchant  = "need_merchant"    // у владельца несколько кабинетов — нужен выбор
	OnboardingEmployeeMade  = "employee_created" // сотрудник создан, магазин подключён, ждём письмо с паролем
	OnboardingCatalogQueued = "catalog_queued"   // пароль получен, поставлен импорт каталога
	OnboardingDone          = "done"             // каталог импортирован
	OnboardingFailed        = "failed"           // ошибка на одном из шагов
)

// KaspiOnboarding — попытка подключения магазина к Kaspi через кабинет со
// служебным сотрудником (D-19, D-28). Секреты (сессия входа владельца, пароль
// сотрудника) хранятся только в зашифрованном виде.
type KaspiOnboarding struct {
	ID            int64
	AccountID     int64
	StoreName     string
	Phone         string // телефон владельца (в формате кабинета)
	EmployeeName  string
	EmployeeEmail string
	Status        string
	Merchants     []KaspiMerchant // заполняется, когда нужен выбор кабинета
	MerchantID    string
	StoreID       int64
	ConnectionID  int64
	Error         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// KaspiMerchant — кабинет владельца для выбора продавцом.
type KaspiMerchant struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}
