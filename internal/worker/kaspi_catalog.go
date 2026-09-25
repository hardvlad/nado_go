package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"nado_go/internal/integration/marketplace/kaspi"
	"nado_go/internal/jobs"
	"nado_go/internal/jobsclient"
	"nado_go/internal/worker/kaspicabinet"
)

// kaspiCatalogPayload — задание kaspi.sync_catalog. Сервер кладёт сюда учётные
// данные служебного сотрудника (расшифрованные) и параметры подключения.
type kaspiCatalogPayload struct {
	ConnectionID     int64  `json:"connection_id"`
	Login            string `json:"login"`
	Password         string `json:"password"`
	SelectedMerchant string `json:"selected_merchant"`
}

const ingestBatchSize = 200

// newKaspiCatalogHandler импортирует каталог из кабинета Kaspi и шлёт его на
// сервер постранично.
func newKaspiCatalogHandler(client *jobsclient.Client) jobsclient.Handler {
	return func(ctx context.Context, job jobs.LeasedJob) (json.RawMessage, error) {
		var p kaspiCatalogPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return nil, jobs.Permanent(fmt.Errorf("worker: payload kaspi.sync_catalog: %w", err))
		}
		if p.ConnectionID == 0 || p.Login == "" {
			return nil, jobs.Permanent(errors.New("worker: в задании нет connection_id/login"))
		}

		// Код подтверждения входа берём с сервера (он читает почту сотрудника).
		codeProvider := serverCodeProvider(ctx, client, p.Login)
		cab := kaspicabinet.New(kaspicabinet.WithCodeProvider(codeProvider))

		sess, err := cab.Login(p.Login, p.Password, p.SelectedMerchant)
		if err != nil {
			// Неверные данные/изменившийся кабинет — повтор не поможет.
			if errors.Is(err, kaspicabinet.ErrLogin) || errors.Is(err, kaspicabinet.ErrNeedCode) {
				return nil, jobs.Permanent(err)
			}
			var sel kaspicabinet.ErrSelectMerchant
			if errors.As(err, &sel) {
				return nil, jobs.Permanent(fmt.Errorf("нужно выбрать кабинет из %d", len(sel.Merchants)))
			}
			return nil, err
		}

		created, updated := 0, 0
		batch := make([]kaspi.ProductPayload, 0, ingestBatchSize)

		flush := func(final bool) error {
			if len(batch) == 0 && !final {
				return nil
			}
			var resp kaspi.IngestResponse
			path := fmt.Sprintf("/kaspi/catalog/%d/products", p.ConnectionID)
			if _, err := client.Call(ctx, http.MethodPost, path, kaspi.IngestRequest{Products: batch, Final: final}, &resp); err != nil {
				return err
			}
			created += resp.Created
			updated += resp.Updated
			batch = batch[:0]
			return nil
		}

		for prod, err := range cab.Products(sess) {
			if err != nil {
				if errors.Is(err, kaspicabinet.ErrSessionExpired) {
					// Сессия устарела — повторим задачу с нуля.
					return nil, err
				}
				if errors.Is(err, kaspicabinet.ErrFormatChanged) {
					return nil, jobs.Permanent(err)
				}
				return nil, err
			}
			batch = append(batch, toPayload(prod))
			if len(batch) >= ingestBatchSize {
				if err := flush(false); err != nil {
					return nil, err
				}
			}
		}
		if err := flush(true); err != nil {
			return nil, err
		}

		return json.Marshal(map[string]int{"created": created, "updated": updated})
	}
}

// serverCodeProvider опрашивает сервер о коде подтверждения входа для адреса
// служебного сотрудника (до ~2 минут).
func serverCodeProvider(ctx context.Context, client *jobsclient.Client, email string) kaspicabinet.CodeProvider {
	return func(addr string) (string, error) {
		path := "/kaspi/otp-code?email=" + url.QueryEscape(addr)
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			var resp struct {
				Code string `json:"code"`
			}
			status, err := client.Call(ctx, http.MethodGet, path, nil, &resp)
			if err != nil {
				return "", err
			}
			if status == http.StatusOK && resp.Code != "" {
				return resp.Code, nil
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
		return "", errors.New("worker: код подтверждения не пришёл вовремя")
	}
}

// toPayload переводит нормализованный товар кабинета в DTO приёма.
func toPayload(p kaspicabinet.CabinetProduct) kaspi.ProductPayload {
	stocks := make([]kaspi.StockPayload, len(p.Stocks))
	for i, st := range p.Stocks {
		stocks[i] = kaspi.StockPayload{StoreCode: st.StoreCode, Qty: st.Qty, Specified: st.Specified, PreOrder: st.PreOrder}
	}
	return kaspi.ProductPayload{
		SKU: p.SKU, MasterSKU: p.MasterSKU, Title: p.Title, Brand: p.Brand, CategoryID: p.CategoryID,
		Images: p.Images, Available: p.Available, PriceMinor: p.PriceMinor, Currency: "KZT",
		Stocks: stocks, Raw: p.Raw,
	}
}
