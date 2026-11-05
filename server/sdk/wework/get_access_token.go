package wework

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// GetAccessTokenResponse 表示获取访问令牌API的响应结构。
type GetAccessTokenResponse struct {
	CommonError
	AccessToken string `json:"access_token"` // 访问令牌
	ExpiresIn   int    `json:"expires_in"`   // 令牌有效期（秒）
}

// GetAccessToken 从企业微信获取访问令牌。
// doc: https://developer.work.weixin.qq.com/document/10013#%E7%AC%AC%E4%B8%89%E6%AD%A5%EF%BC%9A%E8%8E%B7%E5%8F%96access_token
func (sdk *Wework) GetAccessToken() (*GetAccessTokenResponse, error) {
	return sdk.GetAccessTokenContext(sdk.requestContext())
}

// GetAccessTokenContext 从企业微信获取访问令牌并遵循调用方取消信号。
func (sdk *Wework) GetAccessTokenContext(ctx context.Context) (*GetAccessTokenResponse, error) {
	if ctx == nil {
		ctx = sdk.requestContext()
	}
	query := url.Values{}
	query.Set("corpid", sdk.cfg.CropID)
	query.Set("corpsecret", sdk.cfg.Secret)
	endpoint := "https://qyapi.weixin.qq.com/cgi-bin/gettoken?" + query.Encode()
	resp, err := sdk.client.R().SetContext(ctx).Get(endpoint)
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("failed to request, status code: %d", resp.StatusCode())
	}

	response := new(GetAccessTokenResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, err
	}
	if err := response.GetResponseError(); err != nil {
		return nil, err
	}
	return response, nil
}
