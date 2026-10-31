package wework // 获取临时素材

import (
	"encoding/json"
	"fmt"
)

type GetTempMediaRequest struct {
	MediaID string `json:"media_id"`
}

type GetTempMediaResponse struct {
	CommonError
}

// 获取临时素材
// doc: https://developer.work.weixin.qq.com/document/path/90254
func (cli *Wework) GetTempMedia(req *GetTempMediaRequest) (*GetTempMediaResponse, error) {
	token, err := cli.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	queries := map[string]string{
		"access_token": token.AccessToken,
		"media_id":     req.MediaID,
	}

	url := "https://qyapi.weixin.qq.com/cgi-bin/media/get"
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

	response := new(GetTempMediaResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
