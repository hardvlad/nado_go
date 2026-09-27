package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"nado_go/internal/database"
	"nado_go/internal/httpx"
	"nado_go/internal/jobs"
	"nado_go/internal/model"
	"nado_go/internal/repository"
	"nado_go/internal/secrets"
)

// Ошибки кабинетного онбординга Kaspi.
var (
	ErrPhoneRequired        = errors.New("service: не указан телефон владельца")
	ErrOnboardingState      = errors.New("service: онбординг не в том состоянии для этого шага")
	ErrOnboardingNotFound   = errors.New("service: онбординг не найден")
	ErrMerchantChoice       = errors.New("service: выбран неизвестный кабинет")
	ErrMailboxNotConfigured = errors.New("service: не задан домен почты служебных сотрудников")
	ErrCodeRequired         = errors.New("service: не введён код подтверждения")
)

// KaspiOnboardingService ведёт подключение магазина к Kaspi через кабинет со
// служебным сотрудником (D-19, D-28): отправка SMS-кода владельцу, создание
// сотрудника, приём его пароля из почты и постановка импорта каталога.
type KaspiOnboardingService struct {
	repo        *repository.KaspiOnboardingRepository
	conn        *ConnectionService
	jobs        *jobs.Repository
	box         *secrets.Box
	log         *slog.Logger
	emailDomain string // домен адресов служебных сотрудников (kaspi.nado.kz)
}

func NewKaspiOnboardingService(repo *repository.KaspiOnboardingRepository, conn *ConnectionService, jobsRepo *jobs.Repository, box *secrets.Box, emailDomain string, log *slog.Logger) *KaspiOnboardingService {
	return &KaspiOnboardingService{repo: repo, conn: conn, jobs: jobsRepo, box: box, emailDomain: strings.TrimSpace(emailDomain), log: log}
}

// Start заводит онбординг: создаёт запись, генерирует адрес служебного сотрудника
// и ставит удалённую задачу отправки SMS-кода владельцу. Возвращает id онбординга.
func (s *KaspiOnboardingService) Start(ctx context.Context, accountID int64, storeName, phone string) (int64, error) {
	storeName = strings.TrimSpace(storeName)
	if storeName == "" {
		return 0, ErrStoreNameRequired
	}
	phone = formatKaspiPhone(phone)
	if phone == "" {
		return 0, ErrPhoneRequired
	}
	if s.emailDomain == "" {
		return 0, ErrMailboxNotConfigured
	}

	id, err := s.repo.Create(ctx, &model.KaspiOnboarding{
		AccountID: accountID, StoreName: storeName, Phone: phone, EmployeeName: storeName,
	})
	if err != nil {
		return 0, httpx.ErrInternal(err)
	}

	// Уникальный адрес: s{id}-{rand}@<домен>. По нему Kaspi шлёт пароль и коды.
	email := fmt.Sprintf("s%d-%s@%s", id, randToken(3), s.emailDomain)
	if err := s.repo.SetEmployeeEmail(ctx, id, email); err != nil {
		return 0, httpx.ErrInternal(err)
	}

	if _, err := s.jobs.Enqueue(ctx, jobs.Enqueue{
		Kind:      jobs.KindKaspiSendOTP,
		Execution: jobs.Remote,
		AccountID: accountID,
		Payload:   map[string]any{"onboarding_id": id, "phone": phone},
		DedupKey:  fmt.Sprintf("kaspi_send_otp:%d", id),
	}); err != nil {
		return 0, httpx.ErrInternal(err)
	}
	return id, nil
}

// Get возвращает онбординг аккаунта для отображения статуса в кабинете.
func (s *KaspiOnboardingService) Get(ctx context.Context, accountID, id int64) (*model.KaspiOnboarding, error) {
	o, err := s.repo.Get(ctx, accountID, id)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrOnboardingNotFound
	}
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	return o, nil
}

// SubmitCode передаёт введённый продавцом SMS-код на проверку (создание
// сотрудника). Допустимо только в статусе otp_sent.
func (s *KaspiOnboardingService) SubmitCode(ctx context.Context, accountID, id int64, otp string) error {
	o, err := s.Get(ctx, accountID, id)
	if err != nil {
		return err
	}
	if o.Status != model.OnboardingOTPSent {
		return ErrOnboardingState
	}
	otp = strings.TrimSpace(otp)
	if otp == "" {
		return ErrCodeRequired
	}
	return s.enqueueVerify(ctx, accountID, id, otp, "")
}

// ChooseMerchant ставит проверку для выбранного кабинета, когда их несколько.
func (s *KaspiOnboardingService) ChooseMerchant(ctx context.Context, accountID, id int64, merchantUID string) error {
	o, err := s.Get(ctx, accountID, id)
	if err != nil {
		return err
	}
	if o.Status != model.OnboardingNeedMerchant {
		return ErrOnboardingState
	}
	// Выбор должен быть из предложенного списка.
	ok := false
	for _, m := range o.Merchants {
		if m.UID == merchantUID {
			ok = true
			break
		}
	}
	if !ok {
		return ErrMerchantChoice
	}
	return s.enqueueVerify(ctx, accountID, id, "", merchantUID)
}

// enqueueVerify ставит удалённую задачу проверки кода/выбора кабинета, вложив в
// неё расшифрованную сессию входа и реквизиты сотрудника.
func (s *KaspiOnboardingService) enqueueVerify(ctx context.Context, accountID, id int64, otp, merchantUID string) error {
	v, err := s.repo.LoadForVerify(ctx, id)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	sess, err := s.box.Decrypt(v.SessionCipher)
	if err != nil {
		return httpx.ErrInternal(fmt.Errorf("сессия онбординга нечитаема: %w", err))
	}
	if _, err := s.jobs.Enqueue(ctx, jobs.Enqueue{
		Kind:      jobs.KindKaspiVerifyOTP,
		Execution: jobs.Remote,
		AccountID: accountID,
		Payload: map[string]any{
			"onboarding_id":     id,
			"session":           json.RawMessage(sess),
			"otp":               otp,
			"name":              v.EmployeeName,
			"email":             v.EmployeeEmail,
			"selected_merchant": merchantUID,
		},
		DedupKey: fmt.Sprintf("kaspi_verify_otp:%d", id),
	}); err != nil {
		return httpx.ErrInternal(err)
	}
	return s.repo.SetStatus(ctx, id, model.OnboardingVerifying)
}

// SaveSession сохраняет присланную воркером сессию входа владельца (шифруя её) и
// переводит онбординг в ожидание кода. Вызывается из jobs-API.
func (s *KaspiOnboardingService) SaveSession(ctx context.Context, id int64, session json.RawMessage) error {
	cipher, err := s.box.Encrypt(session)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	return s.repo.SaveSession(ctx, id, cipher, model.OnboardingOTPSent)
}

// SaveMerchants сохраняет обновлённую сессию (с mc-sid) и список кабинетов для
// выбора продавцом. Вызывается из jobs-API.
func (s *KaspiOnboardingService) SaveMerchants(ctx context.Context, id int64, session json.RawMessage, merchants []model.KaspiMerchant) error {
	cipher, err := s.box.Encrypt(session)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	mj, _ := json.Marshal(merchants)
	return s.repo.SaveMerchants(ctx, id, cipher, string(mj))
}

// CompleteEmployee сохраняет полученный из кабинета токен API и uid кабинета.
// Магазин пока НЕ создаётся — данные собираются в форму (флоу как в PHP);
// онбординг переходит в ожидание пароля сотрудника из письма. Из jobs-API.
func (s *KaspiOnboardingService) CompleteEmployee(ctx context.Context, id int64, merchantID, apiToken string) error {
	if strings.TrimSpace(apiToken) == "" {
		return jobs.Permanent(fmt.Errorf("service: кабинет не вернул токен API"))
	}
	cipher, err := s.box.EncryptString(apiToken)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	return s.repo.SetEmployeeToken(ctx, id, merchantID, cipher)
}

// SetEmployeePassword принимает пароль служебного сотрудника из почты: находит
// ожидающий онбординг по адресу, шифрует пароль и переводит его в статус 'ready'
// (данные собраны, ждём подтверждения продавцом — импорт не запускается до
// сохранения). Не наш адрес — тихо игнорируем. Вызывается почтовым поллером.
func (s *KaspiOnboardingService) SetEmployeePassword(ctx context.Context, employeeEmail, password string) error {
	employeeEmail = strings.ToLower(strings.TrimSpace(employeeEmail))
	p, err := s.repo.FindPendingByEmail(ctx, employeeEmail)
	if errors.Is(err, database.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	cipher, err := s.box.EncryptString(password)
	if err != nil {
		return err
	}
	if err := s.repo.SetPassword(ctx, p.ID, cipher); err != nil {
		return err
	}
	s.log.Info("получен пароль служебного сотрудника, данные магазина готовы к подтверждению",
		slog.Int64("onboarding_id", p.ID))
	return nil
}

// ReviewData — собранные данные для формы подтверждения магазина.
type ReviewData struct {
	StoreName  string
	Email      string
	Password   string
	Token      string
	MerchantID string
}

// Review возвращает собранные данные онбординга для формы (статус 'ready').
func (s *KaspiOnboardingService) Review(ctx context.Context, accountID, id int64) (*ReviewData, error) {
	o, err := s.Get(ctx, accountID, id)
	if err != nil {
		return nil, err
	}
	if o.Status != model.OnboardingReady {
		return nil, ErrOnboardingState
	}
	d, err := s.repo.LoadForSave(ctx, id)
	if err != nil {
		return nil, httpx.ErrInternal(err)
	}
	token, _ := s.box.DecryptString(d.TokenCipher)
	password, _ := s.box.DecryptString(d.PasswordCipher)
	return &ReviewData{
		StoreName: o.StoreName, Email: d.EmployeeEmail, Password: password, Token: token, MerchantID: d.MerchantID,
	}, nil
}

// SaveStore создаёт магазин по собранным и подтверждённым продавцом данным (с
// проверкой токена) и ставит импорт каталога. Значения берутся из формы, поэтому
// продавец может их поправить перед сохранением (как addKaspiShop.php).
func (s *KaspiOnboardingService) SaveStore(ctx context.Context, accountID, id int64, name, email, password, token string) error {
	o, err := s.Get(ctx, accountID, id)
	if err != nil {
		return err
	}
	if o.Status != model.OnboardingReady {
		return ErrOnboardingState
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrStoreNameRequired
	}
	email = strings.ToLower(strings.TrimSpace(email))
	token = strings.TrimSpace(token)
	if token == "" || email == "" || password == "" {
		return ErrOnboardingState
	}

	d, err := s.repo.LoadForSave(ctx, id)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	passCipher, err := s.box.EncryptString(password)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	// Проверка токена и создание магазина с реквизитами кабинета.
	storeID, connID, err := s.conn.CreateKaspiStoreFromToken(ctx, accountID, name, token, KaspiCabinetCreds{
		Login: email, PasswordCipher: passCipher, MerchantUID: d.MerchantID,
	})
	if err != nil {
		return err
	}
	if err := s.repo.SetStore(ctx, id, storeID, connID); err != nil {
		return httpx.ErrInternal(err)
	}

	// Импорт каталога воркером: вход служебного сотрудника по логину и паролю.
	if _, err := s.jobs.Enqueue(ctx, jobs.Enqueue{
		Kind:      jobs.KindKaspiSyncCatalog,
		Execution: jobs.Remote,
		AccountID: accountID,
		Payload: map[string]any{
			"connection_id":     connID,
			"login":             email,
			"password":          password,
			"selected_merchant": d.MerchantID,
		},
		DedupKey: fmt.Sprintf("kaspi_sync_catalog:%d", connID),
	}); err != nil {
		s.log.Warn("не удалось поставить импорт каталога", slog.Int64("connection_id", connID), slog.Any("error", err))
	}
	return nil
}

// Fail помечает онбординг ошибкой (для показа продавцу). Вызывается из jobs-API,
// когда шаг воркера завершился неустранимо.
func (s *KaspiOnboardingService) Fail(ctx context.Context, id int64, msg string) error {
	return s.repo.Fail(ctx, id, msg)
}

// formatKaspiPhone нормализует телефон к формату кабинета "+7 (7xx) xxx-xx-xx".
// Пустая строка — если распознать не удалось.
func formatKaspiPhone(raw string) string {
	var digits []rune
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	// 8XXXXXXXXXX → 7XXXXXXXXXX; допускаем 11 цифр, начинающихся с 7.
	if len(digits) == 11 && digits[0] == '8' {
		digits[0] = '7'
	}
	if len(digits) != 11 || digits[0] != '7' {
		return ""
	}
	d := string(digits)
	return fmt.Sprintf("+7 (%s) %s-%s-%s", d[1:4], d[4:7], d[7:9], d[9:11])
}

// randToken возвращает случайную hex-строку из n байт (2n символов).
func randToken(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
