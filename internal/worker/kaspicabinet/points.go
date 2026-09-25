package kaspicabinet

import (
	"encoding/json"
	"fmt"
)

// Point — точка продавца (склад/пункт выдачи) из кабинета.
type Point struct {
	Code    string // код точки (без префикса merchant)
	City    string
	Enabled bool
	Address string
}

// Points возвращает точки продавца через GraphQL getPointList.
func (c *Client) Points(sess *Session) ([]Point, error) {
	query := "query getPointList($merchantUid: String!) {\n  merchant(id: $merchantUid) {\n    id\n    points {\n      id\n      enabled\n      city { name __typename }\n      address { streetName streetNumber building phone name __typename }\n      type\n      __typename\n    }\n    __typename\n  }\n}\n"
	body, _ := json.Marshal(map[string]any{
		"operationName": "getPointList",
		"variables":     map[string]any{"merchantUid": sess.MerchantID},
		"query":         query,
	})

	hdr := append([]string{
		hUA, "Accept: */*", hAccLang, hAccEnc, c.refKaspi(), hCTJSON, c.originKaspi(), hKeepAlive,
		cookie(sess.AmpCookie+".1c.0.1c", sess.MCSession, sess.MCSid),
	}, sec4...)

	resp, err := c.do("POST", c.hosts.mc+"/mc/facade/graphql?opName=getPointList", hdr, body)
	if err != nil {
		return nil, err
	}
	if resp.status == 401 {
		return nil, ErrSessionExpired
	}
	if resp.status != 200 {
		return nil, fmt.Errorf("kaspicabinet: getPointList → %d", resp.status)
	}

	var parsed struct {
		Data struct {
			Merchant struct {
				Points []struct {
					ID      string `json:"id"`
					Enabled bool   `json:"enabled"`
					City    struct {
						Name string `json:"name"`
					} `json:"city"`
					Address struct {
						StreetName   string `json:"streetName"`
						StreetNumber string `json:"streetNumber"`
						Building     string `json:"building"`
						Name         string `json:"name"`
					} `json:"address"`
				} `json:"points"`
			} `json:"merchant"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.body, &parsed); err != nil {
		return nil, fmt.Errorf("%w: getPointList", ErrFormatChanged)
	}

	out := make([]Point, 0, len(parsed.Data.Merchant.Points))
	for _, p := range parsed.Data.Merchant.Points {
		out = append(out, Point{
			Code:    storeCodeOf(p.ID),
			City:    p.City.Name,
			Enabled: p.Enabled,
			Address: joinAddress(p.Address.StreetName, p.Address.StreetNumber, p.Address.Building, p.Address.Name),
		})
	}
	return out, nil
}

func joinAddress(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += p
	}
	return out
}
