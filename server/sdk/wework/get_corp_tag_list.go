package wework

import (
	"encoding/json"
	"fmt"
)

// GetCorpTagListRequest 是获取企业标签库的请求结构体
type GetCorpTagListRequest struct {
	TagID   []string `json:"tag_id,omitempty"`   // 要查询的标签id
	GroupID []string `json:"group_id,omitempty"` // 要查询的标签组id
}

// Tag 是标签的结构体
type Tag struct {
	ID         string `json:"id"`          // 标签id
	Name       string `json:"name"`        // 标签名称
	CreateTime int64  `json:"create_time"` // 标签创建时间
	Order      int    `json:"order"`       // 标签排序的次序值
	Deleted    bool   `json:"deleted"`     // 标签是否已经被删除
}

// TagGroup 是标签组的结构体
type TagGroup struct {
	GroupID    string `json:"group_id"`    // 标签组id
	GroupName  string `json:"group_name"`  // 标签组名称
	CreateTime int64  `json:"create_time"` // 标签组创建时间
	Order      int    `json:"order"`       // 标签组排序的次序值
	Deleted    bool   `json:"deleted"`     // 标签组是否已经被删除
	Tags       []Tag  `json:"tag"`         // 标签组内的标签列表
}

// GetCorpTagListResponse 是获取企业标签库的响应结构体
type GetCorpTagListResponse struct {
	CommonError
	TagGroups []TagGroup `json:"tag_group"` // 标签组列表
}

// GetCorpTagList 是在 Wework 结构体中添加的新方法，用于获取企业标签库
// doc: https://developer.work.weixin.qq.com/document/path/92117
func (w *Wework) GetCorpTagList(req *GetCorpTagListRequest) (*GetCorpTagListResponse, error) {
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

	url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/get_corp_tag_list"
	resp, err := w.client.R().
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

	response := new(GetCorpTagListResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
