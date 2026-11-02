package wework

import (
	"encoding/json"
	"fmt"
)

// ExternalContactMarkTagRequest 是标记和移除标签的请求结构体
type ExternalContactMarkTagRequest struct {
	UserID         string   `json:"userid"`               // 添加外部联系人的userid
	ExternalUserID string   `json:"external_userid"`      // 外部联系人userid
	AddTag         []string `json:"add_tag,omitempty"`    // 要标记的标签列表
	RemoveTag      []string `json:"remove_tag,omitempty"` // 要移除的标签列表
}

// ExternalContactMarkTagResponse 是标记和移除标签的响应结构体
type ExternalContactMarkTagResponse struct {
	CommonError
}

// ExternalContactMarkTag 是在 Wework 结构体中添加的新方法，用于标记和移除标签
// Doc: https://developer.work.weixin.qq.com/document/path/92118
func (w *Wework) ExternalContactMarkTag(req *ExternalContactMarkTagRequest) (*ExternalContactMarkTagResponse, error) {

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

	mutex := w.userLockFor(req.ExternalUserID)
	mutex.Lock()
	defer mutex.Unlock()

	response := new(ExternalContactMarkTagResponse)
	err = w.locking(func() error {

		url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/mark_tag"
		resp, err := w.client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(raw).
			SetQueryParams(queries).
			Post(url)
		if err != nil {
			return err
		}

		if resp.IsStatusFailure() {
			return fmt.Errorf("failed to request, status code: %d", resp.StatusCode())
		}

		if err := json.Unmarshal(resp.Bytes(), response); err != nil {
			return err
		}

		if err := response.GetResponseError(); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
