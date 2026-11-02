package wework

import (
	"encoding/json"
	"fmt"
)

// GetJsapiTicketResponse 表示获取访问令牌API的响应结构。
type GetJsapiTicketRequest struct {
	Type int `json:"type"` // 企业微信应用的类型，1表示企业内部开发，2表示第三方服务商
}

// GetJsapiTicketResponse 表示获取访问令牌API的响应结构。
type GetJsapiTicketResponse struct {
	CommonError
	Ticket    string `json:"ticket"`     // 访问令牌
	ExpiresIn int    `json:"expires_in"` // 令牌有效期（秒）
}

// GetJsapiTicket 从企业微信获取访问令牌。
// doc: https://developer.work.weixin.qq.com/document/10013#%E7%AC%AC%E4%B8%89%E6%AD%A5%EF%BC%9A%E8%8E%B7%E5%8F%96access_token
func (sdk *Wework) GetJsapiTicket() (*GetJsapiTicketResponse, error) {
	token, err := sdk.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	// 发送请求获取访问令牌
	url := `https://qyapi.weixin.qq.com/cgi-bin/get_jsapi_ticket`

	queries := map[string]string{
		"access_token": token.AccessToken,
	}

	resp, err := sdk.client.R().
		SetHeader("Content-Type", "application/json").
		SetQueryParams(queries).
		Get(url)
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("failed to request, status code: %d", resp.StatusCode())
	}

	response := new(GetJsapiTicketResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
