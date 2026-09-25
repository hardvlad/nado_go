package kaspicabinet

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// ErrNeedCode — не задан источник кода подтверждения (MFA), а он потребовался.
var ErrNeedCode = errors.New("kaspicabinet: нужен источник кода подтверждения (CodeProvider)")

// ErrLogin — вход не удался (неверные данные, изменился кабинет, недоступен).
var ErrLogin = errors.New("kaspicabinet: не удалось войти в кабинет")

// ErrSelectMerchant — у сотрудника несколько кабинетов, нужно выбрать merchant.
type ErrSelectMerchant struct{ Merchants []Merchant }

func (e ErrSelectMerchant) Error() string {
	return fmt.Sprintf("kaspicabinet: нужно выбрать один из %d кабинетов", len(e.Merchants))
}

// Фиксированные заголовки, имитирующие браузер (как в образце). Порядок и состав
// кабинет проверяет, поэтому воспроизводятся точно.
const (
	hUA        = "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0"
	hAccLang   = "Accept-Language: en-US,en;q=0.5"
	hAccAll    = "Accept: application/json, text/plain, */*"
	hAccXML    = "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	hAccEnc    = "Accept-Encoding: gzip, deflate"
	hCTJSON    = "Content-Type: application/json"
	hKeepAlive = "Connection: keep-alive"
)

func (c *Client) refIdmcLogin() string { return "Referer: " + c.hosts.idmc + "/login" }
func (c *Client) originIdmc() string   { return "Origin: " + c.hosts.idmc }
func (c *Client) refIdmc() string      { return "Referer: " + c.hosts.idmc + "/" }
func (c *Client) refKaspi() string     { return "Referer: " + c.hosts.kaspi + "/" }
func (c *Client) refKaspiMc() string   { return "Referer: " + c.hosts.kaspi + "/mc/" }
func (c *Client) originKaspi() string  { return "Origin: " + c.hosts.kaspi }

var (
	sec1 = []string{"DNT: 1", "Sec-GPC: 1", "Sec-Fetch-Dest: empty", "Sec-Fetch-Mode: cors", "Sec-Fetch-Site: same-origin", "Priority: u=0", "TE: trailers"}
	sec2 = []string{"DNT: 1", "Sec-GPC: 1", "Upgrade-Insecure-Requests: 1", "Sec-Fetch-Dest: document", "Sec-Fetch-Mode: navigate", "Sec-Fetch-Site: same-site", "Priority: u=0, i"}
	sec3 = []string{"Upgrade-Insecure-Requests: 1", "Sec-Fetch-Dest: document", "Sec-Fetch-Mode: navigate", "Sec-Fetch-Site: same-origin", "Sec-Fetch-User: ?1", "Priority: u=0, i", "TE: trailers"}
	sec4 = []string{"x-auth-version: 3", "DNT: 1", "Sec-GPC: 1", "Sec-Fetch-Dest: empty", "Sec-Fetch-Mode: cors", "Sec-Fetch-Site: same-site", "Priority: u=4", "TE: trailers"}
	sec5 = []string{"DNT: 1", "Sec-GPC: 1", "Sec-Fetch-Dest: empty", "Sec-Fetch-Mode: cors", "Sec-Fetch-Site: same-origin"}
	sec6 = []string{"DNT: 1", "Sec-GPC: 1", "X-Auth-Version: 3", "Sec-Fetch-Dest: empty", "Sec-Fetch-Mode: cors", "Sec-Fetch-Site: same-site", "TE: trailers"}
)

func cookie(parts ...string) string { return "Cookie: " + strings.Join(parts, "; ") }

// Login выполняет вход служебного сотрудника и возвращает готовую сессию.
// selectedMerchant — выбранный кабинет, если их несколько (иначе "").
// Порт getPersonalAccountCredentials из lk_checker.php.
func (c *Client) Login(login, password, selectedMerchant string) (*Session, error) {
	// Шаг 1: страница входа.
	if r, err := c.do("GET", c.hosts.idmc+"/login", []string{hUA, hAccAll, hAccLang, hAccEnc}, nil); err != nil {
		return nil, err
	} else if r.status != 200 {
		return nil, fmt.Errorf("%w: /login → %d", ErrLogin, r.status)
	}

	// Шаг 2: вход по логину и паролю.
	loginBody, _ := json.Marshal(map[string]any{"_u": login, "_p": password, "_r_d": false})
	hdr := append([]string{hUA, hAccAll, hAccLang, hAccEnc, hCTJSON, c.originIdmc(), hKeepAlive, c.refIdmcLogin()}, sec1...)
	resp, err := c.do("POST", c.hosts.idmc+"/api/p/login", hdr, loginBody)
	if err != nil {
		return nil, err
	}
	// Кабинет мог включить паузу против частых входов — подождать и повторить раз.
	if resp.status == 401 {
		if wait := floodWait(resp.body); wait > 0 {
			time.Sleep(wait)
			resp, err = c.do("POST", c.hosts.idmc+"/api/p/login", hdr, loginBody)
			if err != nil {
				return nil, err
			}
		}
	}
	if resp.status != 200 {
		return nil, fmt.Errorf("%w: неверный логин/пароль (%d)", ErrLogin, resp.status)
	}

	msAuthSSO := resp.setCookie
	var parsed struct {
		RedirectURL string `json:"redirectUrl"`
	}
	_ = json.Unmarshal(resp.body, &parsed)

	// Нет redirectUrl — требуется код подтверждения из почты сотрудника.
	if parsed.RedirectURL == "" {
		if c.codeProvider == nil {
			return nil, ErrNeedCode
		}
		code, err := c.codeProvider(login)
		if err != nil {
			return nil, fmt.Errorf("%w: код подтверждения: %v", ErrLogin, err)
		}
		codeBody, _ := json.Marshal(map[string]any{"_u": login, "_m_c": code, "_r_d": true})
		hdr = append([]string{"Cookie: " + msAuthSSO, hUA, hAccAll, hAccLang, hAccEnc, hKeepAlive, c.refIdmcLogin()}, sec3...)
		resp, err = c.do("POST", c.hosts.idmc+"/api/p/login", hdr, codeBody)
		if err != nil {
			return nil, err
		}
		if resp.status != 200 {
			return nil, fmt.Errorf("%w: код не принят (%d)", ErrLogin, resp.status)
		}
		msAuthSSO = resp.setCookie
		if err := json.Unmarshal(resp.body, &parsed); err != nil || parsed.RedirectURL == "" {
			return nil, fmt.Errorf("%w: нет redirectUrl после кода", ErrLogin)
		}
	}

	// Шаг 4: переход по redirectUrl (idmc → внешний location).
	hdr = append([]string{"Cookie: " + msAuthSSO, hUA, hAccXML, hAccLang, hAccEnc, hKeepAlive, c.refIdmcLogin()}, sec3...)
	r4, err := c.do("GET", c.hosts.idmc+parsed.RedirectURL, hdr, nil)
	if err != nil {
		return nil, err
	}
	if r4.status != 302 {
		return nil, fmt.Errorf("%w: redirectUrl → %d", ErrLogin, r4.status)
	}
	hdr = append([]string{hUA, hAccXML, hAccLang, hAccEnc, c.refIdmc(), hKeepAlive}, sec2...)
	if r5, err := c.do("GET", r4.location, hdr, nil); err != nil {
		return nil, err
	} else if r5.status != 200 {
		return nil, fmt.Errorf("%w: переход после idmc → %d", ErrLogin, r5.status)
	}

	// Шаг 5: cookie аналитики и «прогрев» kaspi.kz.
	amp := newAmpCookie()
	hdr = append([]string{hUA, hAccAll, hAccLang, hAccEnc, hKeepAlive, c.refKaspiMc(), cookie(amp + ".0.0.0")}, sec5...)
	if r, err := c.do("GET", c.hosts.kaspi+"/yml/ms/feat/p/ft/pre/e?d=true", hdr, nil); err != nil {
		return nil, err
	} else if r.status != 200 {
		return nil, fmt.Errorf("%w: прогрев kaspi → %d", ErrLogin, r.status)
	}

	// Шаг 6: два обращения к mc/s/m дают cookie mc-session (оба 401).
	hdr = append([]string{hUA, hAccAll, hAccLang, hAccEnc, c.originKaspi(), hKeepAlive, c.refKaspi(), cookie(amp + ".0.0.0")}, sec6...)
	r6, err := c.do("GET", c.hosts.mc+"/s/m", hdr, nil)
	if err != nil {
		return nil, err
	}
	if r6.status != 401 {
		return nil, fmt.Errorf("%w: mc/s/m(1) → %d", ErrLogin, r6.status)
	}
	mcSession := r6.setCookie
	hdr = append([]string{hUA, hAccAll, hAccLang, hAccEnc, c.originKaspi(), hKeepAlive, c.refKaspi(), cookie(amp+".1.0.1", mcSession)}, sec6...)
	r7, err := c.do("GET", c.hosts.mc+"/s/m", hdr, nil)
	if err != nil {
		return nil, err
	}
	if r7.status != 401 {
		return nil, fmt.Errorf("%w: mc/s/m(2) → %d", ErrLogin, r7.status)
	}
	mcSession = r7.setCookie

	// Шаг 7: oauth2 authorization → серия редиректов до cookie mc-sid.
	hdr = append([]string{hUA, hAccXML, hAccLang, hAccEnc, hKeepAlive, c.refKaspi(), cookie(amp+".1.0.1", mcSession)}, sec2...)
	r8, err := c.do("GET", c.hosts.mc+"/oauth2/authorization/1", hdr, nil)
	if err != nil {
		return nil, err
	}
	if r8.status != 302 {
		return nil, fmt.Errorf("%w: oauth2 → %d", ErrLogin, r8.status)
	}
	mcArr := r8.setCookie

	hdr = append([]string{hUA, hAccXML, hAccLang, hAccEnc, hKeepAlive, cookie(msAuthSSO, amp+".1.0.1")}, sec2...)
	r9, err := c.do("GET", r8.location, hdr, nil)
	if err != nil {
		return nil, err
	}
	if r9.status != 302 {
		return nil, fmt.Errorf("%w: oauth redirect(1) → %d", ErrLogin, r9.status)
	}
	hdr = append([]string{hUA, hAccXML, hAccLang, hAccEnc, hKeepAlive, cookie(amp+".1.0.1", mcArr, mcSession)}, sec2...)
	r10, err := c.do("GET", r9.location, hdr, nil)
	if err != nil {
		return nil, err
	}
	if r10.status != 302 {
		return nil, fmt.Errorf("%w: oauth redirect(2) → %d", ErrLogin, r10.status)
	}
	mcSid := r10.setCookie

	// Шаг 8: список кабинетов сотрудника.
	hdr = append([]string{hUA, "Accept: */*", hAccLang, hAccEnc, c.refKaspi(), hCTJSON, c.originKaspi(), hKeepAlive, cookie(amp+".2.0.2", mcSession, mcSid)}, sec4...)
	r11, err := c.do("GET", c.hosts.mc+"/s/m", hdr, nil)
	if err != nil {
		return nil, err
	}
	if r11.status != 200 {
		return nil, fmt.Errorf("%w: список кабинетов → %d", ErrLogin, r11.status)
	}
	var merchantsResp struct {
		Merchants []Merchant `json:"merchants"`
	}
	if err := json.Unmarshal(r11.body, &merchantsResp); err != nil {
		return nil, fmt.Errorf("%w: разбор кабинетов", ErrLogin)
	}

	merchantID := selectedMerchant
	if merchantID == "" {
		switch len(merchantsResp.Merchants) {
		case 0:
			return nil, fmt.Errorf("%w: у сотрудника нет кабинетов", ErrLogin)
		case 1:
			merchantID = merchantsResp.Merchants[0].UID
		default:
			// Нужен выбор кабинета продавцом.
			return nil, ErrSelectMerchant{Merchants: merchantsResp.Merchants}
		}
	}

	return &Session{
		MerchantID: merchantID,
		AmpCookie:  amp,
		MCSession:  mcSession,
		MCSid:      mcSid,
		Merchants:  merchantsResp.Merchants,
	}, nil
}

// floodWait возвращает паузу, если кабинет включил антифлуд по частым входам.
func floodWait(body []byte) time.Duration {
	var r struct {
		ErrorCode string `json:"errorCode"`
		ErrorData struct {
			BreakTimeSeconds int `json:"breakTimeSeconds"`
		} `json:"errorData"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return 0
	}
	flood := r.ErrorCode == "MFA_SEND_FLOOD" || r.ErrorCode == "MFA_CODE_TOO_MANY_SEND" || r.ErrorData.BreakTimeSeconds > 0
	if !flood {
		return 0
	}
	sec := r.ErrorData.BreakTimeSeconds
	if sec == 0 {
		sec = 60
	}
	return time.Duration(sec+10+rand.IntN(11)) * time.Second
}
