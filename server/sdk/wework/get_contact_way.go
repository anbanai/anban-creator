package wework

import (
	"encoding/json"
	"fmt"
)

type GetContactWayRequest struct {
	ConfigID string `json:"config_id"`
}

type ContactWay struct {
	ConfigID      string   `json:"config_id"`
	Type          int      `json:"type"`
	Scene         int      `json:"scene"`
	Style         int      `json:"style"`
	Remark        string   `json:"remark"`
	SkipVerify    bool     `json:"skip_verify"`
	State         string   `json:"state"`
	QRCode        string   `json:"qr_code"`
	User          []string `json:"user"`
	Party         []int    `json:"party"`
	IsTemp        bool     `json:"is_temp"`
	ExpiresIn     int      `json:"expires_in"`
	ChatExpiresIn int      `json:"chat_expires_in"`
	Unionid       string   `json:"unionid"`
	Conclusions   string   `json:"conclusions"`
}

type GetContactWayResponse struct {
	CommonError
	ContactWay ContactWay `json:"contact_way"`
}

func (w *Wework) GetContactWay(req *GetContactWayRequest) (*GetContactWayResponse, error) {
	token, err := w.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	raw, err := json.Marshal(req)
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	queries := map[string]string{
		"access_token": token.AccessToken,
	}

	url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/get_contact_way"
	resp, err := w.client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(raw).
		SetQueryParams(queries).
		Post(url)
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("failed to request, status code: %d", resp.StatusCode())
	}

	response := new(GetContactWayResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
