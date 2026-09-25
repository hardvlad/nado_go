# Клиенты внешних API

Все интеграции (маркетплейсы, оплата, чеки, доставка) ходят наружу через общий
пакет `internal/integration/httpclient`. Так поведение одинаково: таймауты,
повторы, лимиты, логи.

## Клиент

```go
type Client struct {
    http    *http.Client        // Timeout = 30s; Transport с MaxIdleConnsPerHost
    limiter *keyedLimiter       // golang.org/x/time/rate по ключу
    log     *slog.Logger
    retry   RetryPolicy
}

// Do выполняет запрос. key — ключ лимитера, обычно provider + ":" + credentialsID:
// лимиты маркетплейсов считаются на токен, а не на наш сервер.
func (c *Client) Do(ctx context.Context, key string, req *http.Request) (*Response, error)
```

- Сначала `limiter.Wait(ctx)` — не отправлять запрос, заведомо обречённый на 429.
- Тело ответа читается целиком с ограничением (`io.LimitReader`, например 20 МБ)
  и закрывается.
- Ответы с кодом не из 2xx превращаются в `*APIError{Status, Code, Message, Body,
  RetryAfter}`. `Body` обрезается до 4 КБ для лога и диагностики.

## Повторы

- Повторяются только **идемпотентные** запросы: GET, а также POST/PUT, помеченные
  адаптером как идемпотентные. Пример: POST создания платежа с Idempotence-Key.
- Повод для повтора — сетевые ошибки, 429, 500, 502, 503, 504.
- Пауза: `Retry-After` (секунды или HTTP-дата), иначе экспонента 0,5 с → 8 с с
  джиттером, максимум 4 попытки.
- 401 и 403 не повторяются. Адаптер возвращает `integration.ErrUnauthorized`,
  service помечает учётные данные как `status='invalid'`, продавец видит
  «переподключите».
- Если ретраи внутри запроса исчерпаны на 429, наверх уходит ошибка с
  `RetryAfter`, а очередь задач переносит задачу (см. jobs-queue.md).

## Логирование

- На уровне debug логируются метод, хост, путь, статус, длительность, размер.
- Заголовки `Authorization`, `Api-Key`, `Client-Id`, `X-Auth-Token`, `Token`,
  `Cookie` и поля тела `password`, `secret`, `token`, `card*` **вырезаются**.
  Функция `redact` покрыта тестом.
- Полные тела не логируются никогда: в них персональные данные покупателей.

## Лимиты по провайдерам

Лимиты задаются в адаптере при регистрации ключа лимитера: `rate.Limit` и burst.
Значения берутся из reference-файла провайдера, а не придумываются. Если у
методов разные лимиты (у WB они по категориям API), ключ лимитера включает
категорию: `wb:content:<credID>`.

## Ошибки для service

Пакет `internal/integration` экспортирует общие ошибки:
```go
var (
    ErrUnauthorized   = errors.New("integration: учётные данные недействительны")
    ErrRateLimited    = errors.New("integration: превышен лимит запросов")
    ErrNotFound       = errors.New("integration: объект не найден у провайдера")
    ErrUnavailable    = errors.New("integration: провайдер недоступен")
)
```
Адаптер оборачивает их с деталями:
`fmt.Errorf("ozon: product/list: %w", integration.ErrRateLimited)`.

## Тесты адаптеров

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Проверяем, что адаптер шлёт правильные заголовки и тело...
    if r.Header.Get("Api-Key") != "test-key" { w.WriteHeader(401); return }
    w.Write(mustRead(t, "testdata/ozon/product_list_page1.json"))
}))
conn := ozon.New(httpclient.New(...), ozon.WithBaseURL(srv.URL))
```

Базовый URL каждого адаптера переопределяется опцией, в том числе ради песочниц
провайдеров.
