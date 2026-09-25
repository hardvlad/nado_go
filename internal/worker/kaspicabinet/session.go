// Package kaspicabinet — доступ к личному кабинету продавца Kaspi для импорта
// каталога, которого нет в официальном Shop API.
//
// Это перенос рабочего кода пользователя (docs/project/kaspi_samples/lk_checker.php)
// на Go. Выполняется ТОЛЬКО на удалённых воркерах (cmd/nado-jobs), не на сервере.
// Учётные данные служебного сотрудника воркер получает в задании; к БД не ходит.
//
// Обход намеренно повторяет последовательность запросов кабинета: порядок,
// cookie и заголовки существенны. В отличие от образца, здесь не отключается
// проверка TLS, секреты не пишутся в лог, а роли служебного сотрудника
// минимальны (это задаётся на стороне сервера при создании сотрудника).
package kaspicabinet

import (
	"crypto/rand"
	"io"
	"net/http"
	"strings"
	"time"
)

// Хосты кабинета. Вынесены в переменные, чтобы подменять в тестах.
type hosts struct {
	idmc  string // https://idmc.shop.kaspi.kz
	kaspi string // https://kaspi.kz
	mc    string // https://mc.shop.kaspi.kz
}

func defaultHosts() hosts {
	return hosts{
		idmc:  "https://idmc.shop.kaspi.kz",
		kaspi: "https://kaspi.kz",
		mc:    "https://mc.shop.kaspi.kz",
	}
}

// Session — авторизованная сессия кабинета: cookie и выбранный merchant.
// Переиспользуется между запросами и задачами, пока не устареет (первый 401).
type Session struct {
	MerchantID string
	AmpCookie  string // "amp_6e9c16=..."
	MCSession  string // "mc-session=..." (строка вида name=value)
	MCSid      string // "mc-sid=..."
	// Merchants — все кабинеты, если у сотрудника их несколько (нужен выбор).
	Merchants []Merchant
}

// Merchant — кабинет продавца в ответе кабинета.
type Merchant struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}

// Valid сообщает, что сессия доведена до готовности (есть merchant и все cookie).
func (s *Session) Valid() bool {
	return s != nil && s.MerchantID != "" && s.AmpCookie != "" && s.MCSession != "" && s.MCSid != ""
}

// Client выполняет запросы к кабинету.
type Client struct {
	http  *http.Client
	hosts hosts
	// codeProvider отдаёт код подтверждения входа (MFA), пришедший на почту
	// служебного сотрудника. На воркере это обращение к серверу.
	codeProvider CodeProvider
}

// CodeProvider возвращает код MFA для адреса служебного сотрудника.
type CodeProvider func(email string) (string, error)

// Option настраивает Client.
type Option func(*Client)

func withHosts(h hosts) Option { return func(c *Client) { c.hosts = h } }

// WithCodeProvider задаёт источник кода подтверждения входа.
func WithCodeProvider(cp CodeProvider) Option { return func(c *Client) { c.codeProvider = cp } }

// New создаёт клиента кабинета. Редиректы НЕ следуются автоматически: коды 302
// и заголовок Location нужны в обходе явно.
func New(opts ...Option) *Client {
	c := &Client{
		http: &http.Client{
			Timeout: 50 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		hosts: defaultHosts(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// response — упрощённый результат запроса, как в образце.
type response struct {
	status    int
	body      []byte
	setCookie string // первая cookie ответа в виде name=value
	location  string // Location без части после ';'
}

// do выполняет один запрос кабинета. headers — готовые строки "Key: Value",
// как формирует образец. body отправляется как есть (уже сериализованный JSON).
func (c *Client) do(method, url string, headers []string, body []byte) (response, error) {
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return response{}, err
	}
	for _, h := range headers {
		if k, v, ok := strings.Cut(h, ": "); ok {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return response{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return response{}, err
	}

	out := response{status: resp.StatusCode, body: data}
	if sc := resp.Header.Get("Set-Cookie"); sc != "" {
		out.setCookie = firstCookie(sc)
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		out.location = strings.TrimSpace(strings.SplitN(loc, ";", 2)[0])
	}
	return out, nil
}

// firstCookie извлекает "name=value" из заголовка Set-Cookie (до первой ';').
func firstCookie(setCookie string) string {
	return strings.TrimSpace(strings.SplitN(setCookie, ";", 2)[0])
}

const cookieAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// randID возвращает случайную строку длины n из букв и цифр (аналог
// GenerateSessionID из образца).
func randID(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// rand.Read из crypto/rand не должен падать; на всякий случай — не пусто.
		return strings.Repeat("a", n)
	}
	for i := range buf {
		buf[i] = cookieAlphabet[int(buf[i])%len(cookieAlphabet)]
	}
	return string(buf)
}

// newAmpCookie строит cookie аналитики в формате образца:
// amp_6e9c16=<10>-<11>...<9>.<тот же 9>
func newAmpCookie() string {
	second := randID(9)
	return "amp_6e9c16=" + randID(10) + "-" + randID(11) + "..." + second + "." + second
}
