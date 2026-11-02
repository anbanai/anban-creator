package wework // 获取临时素材

import (
	"encoding/json"
	"fmt"
)

type GetExternalcontactFollowUserListRequest struct {
}

type GetExternalcontactFollowUserListResponse struct {
	CommonError
	// 应用仅可获取可见范围内的成员。
	// 配置了客户联系功能的成员userid列表
	FollowUser []string `json:"follow_user"`
}

// 获取配置了客户联系功能的成员列表
// doc: https://developer.work.weixin.qq.com/document/path/92576
func (cli *Wework) GetExternalcontactFollowUserList(req *GetExternalcontactFollowUserListRequest) (*GetExternalcontactFollowUserListResponse, error) {
	token, err := cli.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	queries := map[string]string{
		"access_token": token.AccessToken,
	}

	url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/get_follow_user_list"
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

	response := new(GetExternalcontactFollowUserListResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
