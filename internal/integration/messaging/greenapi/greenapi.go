// Package greenapi — отправка WhatsApp-сообщений через сервис GreenAPI.
//
// Реализует messaging.Sender. Инстансы берутся из общего пула платформы
// (Pool): выбирается активный незаблокированный, с него уходит сообщение.
// Токен инстанса и текст кода в лог не попадают.
package greenapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"nado_go/internal/integration/messaging"
)

// Instance — выбранный из пула инстанс с расшифрованным токеном.
type Instance struct {
	ID         int64
	APIDomain  string // api.green-api.com или домен партнёра
	InstanceID string // idInstance
	Token      string // apiTokenInstance (расшифрован)
}

// Call — запись о вызове API для журнала (без токена и текста кода).
type Call struct {
	InstanceID int64
	Method     string
	StatusCode int
	DurationMS int
	Request    string
	Response   string
}

// Pool — пул инстансов. Реализуется репозиторием (он же расшифровывает токен).
type Pool interface {
	// Pick выбирает активный незаблокированный инстанс. Нет такого —
	// messaging.ErrNoInstance.
	Pick(ctx context.Context) (*Instance, error)
	MarkSent(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, reason string) error
	LogCall(ctx context.Context, c Call)
}

// Client отправляет сообщения через GreenAPI.
type Client struct {
	pool Pool
	http *http.Client
	log  *slog.Logger
}

func New(pool Pool, log *slog.Logger) *Client {
	return &Client{
		pool: pool,
		http: &http.Client{Timeout: 20 * time.Second},
		log:  log,
	}
}

func (c *Client) Provider() string { return "greenapi" }

// Send отправляет текст на номер с активного инстанса пула.
func (c *Client) Send(ctx context.Context, msg messaging.Message) (*messaging.Sent, error) {
	inst, err := c.pool.Pick(ctx)
	if err != nil {
		return nil, err // в т.ч. messaging.ErrNoInstance
	}

	// chatId GreenAPI — только цифры номера плюс суффикс.
	chatID := onlyDigits(msg.Phone) + "@c.us"
	body, _ := json.Marshal(map[string]string{"chatId": chatID, "message": msg.Text})

	url := fmt.Sprintf("https://%s/waInstance%s/sendMessage/%s", inst.APIDomain, inst.InstanceID, inst.Token)

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		c.pool.LogCall(ctx, Call{InstanceID: inst.ID, Method: "sendMessage", Request: redactedRequest(chatID)})
		_ = c.pool.MarkFailed(ctx, inst.ID, err.Error())
		return nil, fmt.Errorf("greenapi: отправка: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	// В журнал — метод, статус, длительность и chatId; текст кода и токен не пишем.
	c.pool.LogCall(ctx, Call{
		InstanceID: inst.ID,
		Method:     "sendMessage",
		StatusCode: resp.StatusCode,
		DurationMS: int(time.Since(start).Milliseconds()),
		Request:    redactedRequest(chatID),
		Response:   trimTo(string(respBody), 2000),
	})

	if resp.StatusCode != http.StatusOK {
		reason := fmt.Sprintf("HTTP %d", resp.StatusCode)
		_ = c.pool.MarkFailed(ctx, inst.ID, reason)
		return nil, fmt.Errorf("greenapi: sendMessage → %s", reason)
	}

	var parsed struct {
		IDMessage string `json:"idMessage"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil || parsed.IDMessage == "" {
		_ = c.pool.MarkFailed(ctx, inst.ID, "ответ без idMessage")
		return nil, fmt.Errorf("greenapi: неожиданный ответ sendMessage")
	}

	_ = c.pool.MarkSent(ctx, inst.ID)
	return &messaging.Sent{MessageID: parsed.IDMessage, InstanceID: inst.ID}, nil
}

func redactedRequest(chatID string) string {
	b, _ := json.Marshal(map[string]string{"chatId": chatID, "message": "<redacted>"})
	return string(b)
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func trimTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
