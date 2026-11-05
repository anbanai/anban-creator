package wework // 获取临时素材

import (
	"encoding/json"
	"fmt"
)

type ExternalcontactListRequest struct {
	Userid string `json:"userid"`
}

type ExternalcontactListResponse struct {
	CommonError
	// 外部联系人的userid列表
	ExternalUserid []string `json:"external_userid"`
}

// 获取客户列表
// doc: https://developer.work.weixin.qq.com/document/path/92264
func (cli *Wework) ExternalcontactList(req *ExternalcontactListRequest) (*ExternalcontactListResponse, error) {
	token, err := cli.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	queries := map[string]string{
		"access_token": token.AccessToken,
		"userid":       req.Userid,
	}

	url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/list"
	// 使用 resty 发起 GET 请求
	resp, err := cli.client.R().
		SetHeader("Content-Type", "application/json").
		SetQueryParams(queries).
		Get(url)
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("failed to request, status code: %d", resp.StatusCode())
	}

	response := new(ExternalcontactListResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
