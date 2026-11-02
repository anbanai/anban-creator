package wework

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// RefreshAccessTokenResponse 表示获取访问令牌API的响应结构。
type RefreshAccessTokenResponse struct {
	CommonError
	AccessToken string `json:"access_token"` // 令牌
	ExpiresIn   int    `json:"expires_in"`   // 令牌有效期（秒）
}

// RefreshAccessToken 刷新访问令牌。
// doc: https://developer.work.weixin.qq.com/document/10013#%E7%AC%AC%E4%B8%89%E6%AD%A5%EF%BC%9A%E8%8E%B7%E5%8F%96access_token
func (sdk *Wework) RefreshAccessToken() (string, error) {
	return sdk.RefreshAccessTokenContext(sdk.requestContext())
}

// RefreshAccessTokenContext 刷新访问令牌并遵循调用方取消信号。
func (sdk *Wework) RefreshAccessTokenContext(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = sdk.requestContext()
	}
	sdk.cache.tokenLocker.Lock()
	defer sdk.cache.tokenLocker.Unlock()
	if sdk.cache.accessToken == nil {
		return "", fmt.Errorf("access token is empty")
	}

	query := url.Values{}
	query.Set("corpid", sdk.cfg.CropID)
	query.Set("grant_type", "refresh_token")
	query.Set("refresh_token", sdk.cache.accessToken.AccessToken)
	endpoint := "https://qyapi.weixin.qq.com/cgi-bin/gettoken?" + query.Encode()
	resp, err := sdk.client.R().SetContext(ctx).Get(endpoint)
	if err != nil {
		return "", sanitizeRequestError(err)
	}

	response := new(RefreshAccessTokenResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return "", err
	}
	if err := response.GetResponseError(); err != nil {
		return "", err
	}

	sdk.cache.accessToken = &AccessToken{
		AccessToken: response.AccessToken,
		Expiration:  time.Now().Add(time.Duration(response.ExpiresIn) * time.Second),
	}
	return sdk.cache.accessToken.AccessToken, nil
}
