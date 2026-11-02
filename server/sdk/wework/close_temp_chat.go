package wework

import (
	"encoding/json"
	"fmt"
)

// CloseTempChatRequest 是结束临时会话的请求参数
type CloseTempChatRequest struct {
	UserID         string `json:"userid"`          // 企业成员的userid
	ExternalUserID string `json:"external_userid"` // 客户的外部联系人userid
}

// CloseTempChatResponse 是结束临时会话的返回结果
type CloseTempChatResponse struct {
	ErrCode int    `json:"errcode"` // 返回码
	ErrMsg  string `json:"errmsg"`  // 对返回码的文本描述内容
}

// CloseTempChat 结束指定的企业成员和客户之间的临时会话
func (c *Wework) CloseTempChat(req *CloseTempChatRequest) (*CloseTempChatResponse, error) {

	token, err := c.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}
	url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/close_temp_chat"

	resp, err := c.client.R().
		SetHeader("Content-Type", "application/json").
		SetQueryParam("access_token", token.AccessToken).
		SetBody(req).
		Post(url)

	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	var result CloseTempChatResponse
	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("failed to request, status code: %d", resp.StatusCode())
	}
	if err = json.Unmarshal(resp.Bytes(), &result); err != nil {
		return nil, sanitizeRequestError(err)
	}
	if err := (CommonError{ErrCode: result.ErrCode, ErrMsg: result.ErrMsg}).GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return &result, nil
}
