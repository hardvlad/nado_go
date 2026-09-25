package greenapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Management — управление инстансами GreenAPI: создание через партнёрский API,
// получение QR для авторизации, проверка состояния. Не использует пул: работает
// с переданными доменом/токеном напрямую (во время создания инстанса записи в
// пуле ещё нет). Используется админ-командой cmd/nado-admin.
type Management struct {
	http   *http.Client
	scheme string // https в проде; http подставляется в тестах
}

func NewManagement() *Management {
	return &Management{http: &http.Client{Timeout: 30 * time.Second}, scheme: "https"}
}

// CreateInstanceRequest — параметры создания инстанса. WebhookURL и WebhookToken
// направляют уведомления GreenAPI на наш сервер (/webhooks/greenapi/{token}).
type CreateInstanceRequest struct {
	PartnerDomain string
	PartnerToken  string
	WebhookURL    string
	WebhookToken  string
}

// CreatedInstance — результат создания.
type CreatedInstance struct {
	InstanceID string
	Token      string // apiTokenInstance
	APIDomain  string // домен для вызовов инстанса
}

// CreateInstance создаёт инстанс через партнёрский API и настраивает вебхуки.
func (m *Management) CreateInstance(ctx context.Context, req CreateInstanceRequest) (*CreatedInstance, error) {
	if req.PartnerDomain == "" || req.PartnerToken == "" {
		return nil, fmt.Errorf("greenapi: не заданы партнёрские домен и токен")
	}
	url := fmt.Sprintf(m.scheme+"://%s/partner/createInstance/%s", req.PartnerDomain, req.PartnerToken)

	// Набор уведомлений: нам важны изменения состояния инстанса и статусы
	// исходящих сообщений (доставлено/прочитано для OTP).
	body := map[string]any{
		"webhookUrl":                 req.WebhookURL,
		"webhookUrlToken":            req.WebhookToken,
		"outgoingAPIMessageWebhook":  "yes",
		"stateWebhook":               "yes",
		"incomingWebhook":            "no",
		"pollMessageWebhook":         "no",
		"markIncomingMessagesReaded": "no",
		"keepOnlineStatus":           "no",
	}

	var parsed struct {
		IDInstance       json.Number `json:"idInstance"`
		APITokenInstance string      `json:"apiTokenInstance"`
	}
	if err := m.do(ctx, url, body, &parsed); err != nil {
		return nil, err
	}
	if parsed.IDInstance.String() == "" || parsed.APITokenInstance == "" {
		return nil, fmt.Errorf("greenapi: createInstance вернул пустой ответ")
	}
	return &CreatedInstance{
		InstanceID: parsed.IDInstance.String(),
		Token:      parsed.APITokenInstance,
		// Инстанс-вызовы идут на тот же домен, что и партнёрские (как в исходном проекте).
		APIDomain: req.PartnerDomain,
	}, nil
}

// QR возвращает QR-код авторизации инстанса как PNG (декодированный из base64).
// Пустой результат без ошибки — инстанс уже авторизован (QR не нужен).
func (m *Management) QR(ctx context.Context, apiDomain, instanceID, token string) (pngBase64 string, err error) {
	url := fmt.Sprintf(m.scheme+"://%s/waInstance%s/qr/%s", apiDomain, instanceID, token)
	var parsed struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := m.get(ctx, url, &parsed); err != nil {
		return "", err
	}
	if parsed.Type == "qrCode" {
		return parsed.Message, nil // base64 PNG
	}
	// alreadyLogged или иной тип — QR не нужен.
	return "", nil
}

// State возвращает состояние инстанса (authorized, notAuthorized, blocked, ...).
func (m *Management) State(ctx context.Context, apiDomain, instanceID, token string) (string, error) {
	url := fmt.Sprintf(m.scheme+"://%s/waInstance%s/getStateInstance/%s", apiDomain, instanceID, token)
	var parsed struct {
		StateInstance string `json:"stateInstance"`
	}
	if err := m.get(ctx, url, &parsed); err != nil {
		return "", err
	}
	return parsed.StateInstance, nil
}

func (m *Management) do(ctx context.Context, url string, body, out any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return m.send(req, out)
}

func (m *Management) get(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return m.send(req, out)
}

func (m *Management) send(req *http.Request, out any) error {
	resp, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("greenapi: запрос: %w", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("greenapi: HTTP %d: %s", resp.StatusCode, trimTo(strings.TrimSpace(string(payload)), 300))
	}
	if out != nil {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("greenapi: разбор ответа: %w", err)
		}
	}
	return nil
}
