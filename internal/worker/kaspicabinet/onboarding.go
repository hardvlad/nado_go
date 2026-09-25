package kaspicabinet

import (
	"encoding/json"
	"fmt"
)

// Онбординг служебного сотрудника: nado входит в кабинет как ВЛАДЕЛЕЦ по его
// номеру телефона (Kaspi шлёт владельцу SMS-код), затем, получив код от продавца,
// создаёт в кабинете служебного сотрудника с почтой на нашем домене и забирает
// токен официального Shop API.
//
// Порт lk_otp_checker.php (sendOTP / verifyOTP / verifyOTPwithMerchantID). Две
// фазы разделены действием человека (владелец читает SMS), поэтому состояние
// сессии сериализуется и хранится на сервере между двумя заданиями воркера.
// Пароль владельца при этом не запрашивается и не хранится (D-19).

// DefaultEmployeeRoles — роли служебного сотрудника в кабинете. По решению
// пользователя используется полный набор из образца (заказы, возвраты, доставка,
// настройки), чтобы сотрудник закрывал не только импорт каталога, но и работу с
// заказами через кабинет (открытый вопрос №29 закрыт этим выбором). Сервер может
// передать свой список ролей параметром.
var DefaultEmployeeRoles = []string{
	"ACCEPT_ORDER_PICKUP", "COMPLETE_ORDER_PICKUP",
	"ACCEPT_ORDER_DELIVERY", "COMPLETE_ORDER_DELIVERY",
	"ACCEPT_KASPI_DELIVERY_ORDER", "RETURN_ORDER", "KASPI_DELIVERY_RETURN",
	"MANAGE_OFFERS", "MANAGE_QUALITY_CONTROL",
	"DOWNLOAD_ACTIVE_ARCHIVE_ORDERS", "KASPI_MARKETING", "MANAGE_SETTINGS",
}

// OwnerSession — состояние входа владельца между отправкой и проверкой SMS-кода.
// Поля сериализуются: сервер хранит их между заданиями kaspi.send_otp и
// kaspi.verify_otp.
type OwnerSession struct {
	Phone       string `json:"phone"`
	RedirectURL string `json:"redirect_url"`
	AmpCookie   string `json:"amp_cookie"`
	MCSession   string `json:"mc_session"`
	MCArr       string `json:"mc_arr"`
	MSAuthSSO   string `json:"ms_auth_sso"`
	// Заполняются, если после проверки кода у владельца оказалось несколько
	// кабинетов и нужен выбор (тогда следующий шаг — CreateEmployeeForMerchant).
	MCSid     string     `json:"mc_sid,omitempty"`
	Merchants []Merchant `json:"merchants,omitempty"`
}

// EmployeeResult — итог создания служебного сотрудника.
type EmployeeResult struct {
	MerchantID string
	APIToken   string // токен официального Shop API выбранного кабинета
}

// SendOwnerOTP выполняет вход владельца по телефону до шага, на котором Kaspi
// отправляет SMS-код на его номер. Порт sendOTP. phone — в формате кабинета
// ("+7 (7xx) xxx-xx-xx"). Возвращает состояние для последующей проверки кода.
func (c *Client) SendOwnerOTP(phone string) (*OwnerSession, error) {
	// Шаг 1: прогрев витрины кабинета.
	if r, err := c.do("GET", c.hosts.kaspi+"/mc/", []string{hUA, hAccAll, hAccLang, hAccEnc}, nil); err != nil {
		return nil, err
	} else if r.status != 200 {
		return nil, fmt.Errorf("%w: kaspi/mc → %d", ErrLogin, r.status)
	}

	// Шаг 2: cookie аналитики и предзагрузка фич.
	amp := newAmpCookie()
	hdr := append([]string{hUA, hAccAll, hAccLang, hAccEnc, hKeepAlive, c.refKaspiMc(), cookie(amp + ".0.0.0")}, sec5...)
	if r, err := c.do("GET", c.hosts.kaspi+"/yml/ms/feat/p/ft/pre/e?d=true", hdr, nil); err != nil {
		return nil, err
	} else if r.status != 200 {
		return nil, fmt.Errorf("%w: прогрев kaspi → %d", ErrLogin, r.status)
	}

	// Шаг 3: mc/s/m без авторизации даёт cookie mc-session (401).
	hdr = append([]string{hUA, hAccAll, hAccLang, hAccEnc, c.originKaspi(), hKeepAlive, c.refKaspi(), cookie(amp + ".0.0.0")}, sec6...)
	r3, err := c.do("GET", c.hosts.mc+"/s/m", hdr, nil)
	if err != nil {
		return nil, err
	}
	if r3.status != 401 {
		return nil, fmt.Errorf("%w: mc/s/m → %d", ErrLogin, r3.status)
	}
	mcSession := r3.setCookie

	// Шаг 4: oauth2 authorization → 302 с redirectURL и cookie mc-arr.
	hdr = append([]string{hUA, hAccXML, hAccLang, hAccEnc, hKeepAlive, c.refKaspi(), cookie(amp+".0.0.0", mcSession)}, sec2...)
	r4, err := c.do("GET", c.hosts.mc+"/oauth2/authorization/1", hdr, nil)
	if err != nil {
		return nil, err
	}
	if r4.status != 302 {
		return nil, fmt.Errorf("%w: oauth2 → %d", ErrLogin, r4.status)
	}
	mcArr := r4.setCookie
	redirectURL := r4.location

	// Шаг 5: переход по redirectURL даёт cookie MS_AUTH_SSO (302).
	hdr = append([]string{hUA, hAccXML, hAccLang, hAccEnc, hKeepAlive, cookie(amp + ".0.0.0")}, sec2...)
	r5, err := c.do("GET", redirectURL, hdr, nil)
	if err != nil {
		return nil, err
	}
	if r5.status != 302 {
		return nil, fmt.Errorf("%w: redirect oauth → %d", ErrLogin, r5.status)
	}
	msAuthSSO := r5.setCookie

	// Шаг 6: страница входа idmc с полученной cookie.
	hdr = []string{hUA, hAccAll, hAccLang, hAccEnc, cookie(amp+".0.0.0", msAuthSSO)}
	if r, err := c.do("GET", c.hosts.idmc+"/login", hdr, nil); err != nil {
		return nil, err
	} else if r.status != 200 {
		return nil, fmt.Errorf("%w: idmc/login → %d", ErrLogin, r.status)
	}

	// Шаг 7: запрос кода по номеру владельца — Kaspi отправляет SMS.
	body, _ := json.Marshal(map[string]any{"_ph": phone})
	hdr = append([]string{hUA, hAccAll, hAccLang, hAccEnc, hCTJSON, hKeepAlive, c.originIdmc(), c.refIdmcLogin(), cookie(amp+".0.0.0", msAuthSSO)}, sec1...)
	r7, err := c.do("POST", c.hosts.idmc+"/api/p/login", hdr, body)
	if err != nil {
		return nil, err
	}
	if r7.status != 200 {
		return nil, fmt.Errorf("%w: запрос SMS-кода владельца → %d", ErrLogin, r7.status)
	}

	return &OwnerSession{
		Phone:       phone,
		RedirectURL: redirectURL + "&continue",
		AmpCookie:   amp,
		MCSession:   mcSession,
		MCArr:       mcArr,
		MSAuthSSO:   msAuthSSO,
	}, nil
}

// CreateEmployee проверяет SMS-код владельца и создаёт служебного сотрудника.
// Порт verifyOTP. Если у владельца несколько кабинетов, sess дополняется MCSid и
// Merchants и возвращается ErrSelectMerchant — продавец выбирает кабинет, после
// чего вызывается CreateEmployeeForMerchant. roles пусто → DefaultEmployeeRoles.
func (c *Client) CreateEmployee(sess *OwnerSession, otp, name, email string, roles []string) (*EmployeeResult, error) {
	amp := sess.AmpCookie

	// Шаг 9: подтверждение кода.
	body, _ := json.Marshal(map[string]any{"_c": otp})
	hdr := append([]string{hUA, hAccAll, hAccLang, hAccEnc, hCTJSON, hKeepAlive, c.originIdmc(), c.refIdmcLogin(), cookie(amp+".0.0.0", sess.MSAuthSSO)}, sec1...)
	r9, err := c.do("POST", c.hosts.idmc+"/api/p/login", hdr, body)
	if err != nil {
		return nil, err
	}
	if r9.status != 200 {
		return nil, fmt.Errorf("%w: код не принят (%d)", ErrLogin, r9.status)
	}
	msAuthSSO := r9.setCookie

	// Шаг 10: возврат по redirectUrl.
	hdr = []string{hUA, hAccAll, hAccLang, hAccEnc, cookie(amp+".0.0.0", msAuthSSO), c.refIdmcLogin()}
	r10, err := c.do("GET", sess.RedirectURL, hdr, nil)
	if err != nil {
		return nil, err
	}
	if r10.status != 302 {
		return nil, fmt.Errorf("%w: возврат redirectUrl → %d", ErrLogin, r10.status)
	}

	// Шаг 11: переход в mc — 302 с cookie mc-sid.
	hdr = []string{hUA, hAccAll, hAccLang, hAccEnc, cookie(amp+".0.0.0", sess.MCSession, sess.MCArr), c.refIdmc()}
	r11, err := c.do("GET", r10.location, hdr, nil)
	if err != nil {
		return nil, err
	}
	if r11.status != 302 {
		return nil, fmt.Errorf("%w: переход в mc → %d", ErrLogin, r11.status)
	}
	mcSid := r11.setCookie

	// Шаг 12: завершение редиректа (200).
	hdr = []string{hUA, hAccAll, hAccLang, hAccEnc, cookie(amp + ".0.0.0")}
	if r, err := c.do("GET", r11.location, hdr, nil); err != nil {
		return nil, err
	} else if r.status != 200 {
		return nil, fmt.Errorf("%w: завершение входа → %d", ErrLogin, r.status)
	}

	// Шаг 13: список кабинетов владельца.
	hdr = append([]string{hUA, "Accept: */*", hAccLang, hAccEnc, c.refKaspi(), hCTJSON, c.originKaspi(), hKeepAlive, cookie(amp+".2.0.2", sess.MCSession, mcSid)}, sec4...)
	r13, err := c.do("GET", c.hosts.mc+"/s/m", hdr, nil)
	if err != nil {
		return nil, err
	}
	if r13.status != 200 {
		return nil, fmt.Errorf("%w: список кабинетов → %d", ErrLogin, r13.status)
	}
	var mr struct {
		Merchants []Merchant `json:"merchants"`
	}
	if err := json.Unmarshal(r13.body, &mr); err != nil {
		return nil, fmt.Errorf("%w: разбор кабинетов", ErrLogin)
	}

	// Несколько кабинетов — нужен выбор. Сохраняем состояние в sess и выходим.
	if len(mr.Merchants) != 1 {
		if len(mr.Merchants) == 0 {
			return nil, fmt.Errorf("%w: у владельца нет кабинетов", ErrLogin)
		}
		sess.MSAuthSSO = msAuthSSO
		sess.MCSid = mcSid
		sess.Merchants = mr.Merchants
		return nil, ErrSelectMerchant{Merchants: mr.Merchants}
	}

	return c.createEmployeeOnMerchant(sess, mr.Merchants[0].UID, mcSid, name, email, roles)
}

// CreateEmployeeForMerchant создаёт сотрудника в выбранном кабинете, когда после
// проверки кода их оказалось несколько. Порт verifyOTPwithMerchantID: код уже
// проверен, переиспользуются cookie из sess (в т.ч. MCSid).
func (c *Client) CreateEmployeeForMerchant(sess *OwnerSession, merchantID, name, email string, roles []string) (*EmployeeResult, error) {
	if sess.MCSid == "" {
		return nil, fmt.Errorf("%w: нет сессии для выбора кабинета", ErrLogin)
	}
	return c.createEmployeeOnMerchant(sess, merchantID, sess.MCSid, name, email, roles)
}

// createEmployeeOnMerchant — общий хвост обоих путей (шаги 14–16): проверка
// текущих аккаунтов, получение/выпуск токена API и добавление сотрудника.
func (c *Client) createEmployeeOnMerchant(sess *OwnerSession, merchantID, mcSid, name, email string, roles []string) (*EmployeeResult, error) {
	amp := sess.AmpCookie
	if len(roles) == 0 {
		roles = DefaultEmployeeRoles
	}

	// Шаг 14: список уже заведённых аккаунтов кабинета (проверка доступа).
	hdr := append([]string{hUA, "Accept: */*", hAccLang, hAccEnc, c.refKaspi(), hCTJSON, c.originKaspi(), hKeepAlive, cookie(amp+".88.0.88", sess.MCSession, mcSid)}, sec4...)
	if r, err := c.do("GET", c.hosts.mc+"/user-assignments/api/v1/mc/users/registered?m="+merchantID, hdr, nil); err != nil {
		return nil, err
	} else if r.status != 200 {
		return nil, fmt.Errorf("%w: доступ к кабинету → %d", ErrLogin, r.status)
	}

	// Шаг 15: токен официального API — сначала пробуем получить, затем выпустить.
	token := c.getAPIToken(sess, mcSid, merchantID)
	if token == "" {
		token = c.generateAPIToken(sess, mcSid, merchantID)
	}

	// Создание служебного сотрудника с почтой на нашем домене.
	addBody, _ := json.Marshal(map[string]any{
		"name":         name,
		"email":        email,
		"cityId":       nil,
		"roles":        roles,
		"pointName":    nil,
		"contactPhone": nil,
		"merchantUid":  merchantID,
	})
	hdr = append([]string{hUA, "Accept: */*", hAccLang, hAccEnc, c.refKaspi(), hCTJSON, c.originKaspi(), hKeepAlive, cookie(amp+".88.0.88", sess.MCSession, mcSid)}, sec4...)
	r, err := c.do("POST", c.hosts.mc+"/user-assignments/api/v1/mc/users/add-email", hdr, addBody)
	if err != nil {
		return nil, err
	}
	if r.status != 200 {
		return nil, fmt.Errorf("%w: создание сотрудника → %d", ErrLogin, r.status)
	}

	return &EmployeeResult{MerchantID: merchantID, APIToken: token}, nil
}

// getAPIToken читает уже выпущенный токен Shop API кабинета (GraphQL getTokenApi).
// Токена нет — "".
func (c *Client) getAPIToken(sess *OwnerSession, mcSid, merchantID string) string {
	body, _ := json.Marshal(map[string]any{
		"operationName": "getTokenApi",
		"variables":     map[string]any{"id": merchantID},
		"query":         "query getTokenApi($id: String!) {\n  merchant(id: $id) {\n    id\n    integration {\n      token\n      __typename\n    }\n    __typename\n  }\n}",
	})
	hdr := append([]string{hUA, "Accept: */*", hAccLang, hAccEnc, c.refKaspi(), hCTJSON, c.originKaspi(), hKeepAlive, cookie(sess.AmpCookie+".2.0.2", sess.MCSession, mcSid)}, sec4...)
	r, err := c.do("POST", c.hosts.mc+"/mc/facade/graphql?opName=getTokenApi", hdr, body)
	if err != nil || r.status != 200 {
		return ""
	}
	var resp struct {
		Data struct {
			Merchant struct {
				Integration struct {
					Token string `json:"token"`
				} `json:"integration"`
			} `json:"merchant"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.body, &resp); err != nil {
		return ""
	}
	return resp.Data.Merchant.Integration.Token
}

// generateAPIToken выпускает новый токен Shop API (GraphQL tokenGenerate).
func (c *Client) generateAPIToken(sess *OwnerSession, mcSid, merchantID string) string {
	body, _ := json.Marshal(map[string]any{
		"operationName": "tokenGenerate",
		"variables":     map[string]any{"merchantId": merchantID},
		"query":         "mutation tokenGenerate($merchantId: String!) {\n  tokenGenerate(merchantId: $merchantId)\n}",
	})
	hdr := append([]string{hUA, "Accept: */*", hAccLang, hAccEnc, c.refKaspi(), hCTJSON, c.originKaspi(), hKeepAlive, cookie(sess.AmpCookie+".3a.0.3a", sess.MCSession, mcSid)}, sec4...)
	r, err := c.do("POST", c.hosts.mc+"/mc/facade/graphql?opName=tokenGenerate", hdr, body)
	if err != nil || r.status != 200 {
		return ""
	}
	var resp struct {
		Data struct {
			TokenGenerate string `json:"tokenGenerate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.body, &resp); err != nil {
		return ""
	}
	return resp.Data.TokenGenerate
}
