package wework

import (
	"encoding/json"
	"fmt"
)

// GetAgentJsapiTicketResponse 表示获取访问令牌API的响应结构。
type GetAgentJsapiTicketResponse struct {
	CommonError
	Ticket    string `json:"ticket"`     // 访问令牌
	ExpiresIn int    `json:"expires_in"` // 令牌有效期（秒）
}

// GetAgentJsapiTicket 获取应用的jsapi_ticket
// doc: https://developer.work.weixin.qq.com/document/10029#%E8%8E%B7%E5%8F%96%E5%BA%94%E7%94%A8%E7%9A%84jsapi_ticket
func (sdk *Wework) GetAgentJsapiTicket() (*GetAgentJsapiTicketResponse, error) {
	token, err := sdk.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	// 发送请求获取访问令牌
	url := `https://qyapi.weixin.qq.com/cgi-bin/ticket/get`

	queries := map[string]string{
		"type":         "agent_config",
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

	response := new(GetAgentJsapiTicketResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
