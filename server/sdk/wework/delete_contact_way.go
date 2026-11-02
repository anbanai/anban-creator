package wework

import (
	"encoding/json"
	"fmt"
)

// DeleteContactWayRequest 是删除企业已配置的「联系我」方式的请求参数
type DeleteContactWayRequest struct {
	ConfigID string `json:"config_id"` // 企业联系方式的配置id
}

// DeleteContactWayResponse 是删除企业已配置的「联系我」方式的返回结果
type DeleteContactWayResponse struct {
	CommonError
}

// DeleteContactWay 删除企业已配置的「联系我」方式
func (c *Wework) DeleteContactWay(req *DeleteContactWayRequest) (*DeleteContactWayResponse, error) {
	token, err := c.AccessToken()
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

	url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/del_contact_way"
	resp, err := c.client.R().
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

	response := new(DeleteContactWayResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
