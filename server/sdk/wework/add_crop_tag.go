package wework

import (
	"encoding/json"
	"fmt"
)

// AddCropTagRequest 是标记和移除标签的请求结构体
type AddCropTagRequest struct {
	GroupID   string       `json:"group_id,omitempty"`   // group_id 标签组id，非必填
	GroupName string       `json:"group_name,omitempty"` // group_name 标签组名称，非必填，最长为30个字符
	Order     int          `json:"order,omitempty"`      // order 标签组次序值，非必填，有效的值范围是[0, 2^32)
	Tags      []AddCropTag `json:"tag,omitempty"`        // tag 标签列表
	AgentID   int          `json:"agentid,omitempty"`    // agentid 授权方安装的应用agentid，非必填，仅旧的第三方多应用套件需要填此参数
}

// Tag 标签结构体
type AddCropTag struct {
	Name  string `json:"name"`  // name 标签名称，必填，最长为30个字符
	Order int    `json:"order"` // order 标签次序值，非必填，有效的值范围是[0, 2^32)
}

// AddCropTagResponse 是标记和移除标签的响应结构体
type AddCropTagResponse struct {
	CommonError
	TagGroup struct {
		GroupID    string `json:"group_id"`    // 标签组id
		GroupName  string `json:"group_name"`  // 标签组名称
		CreateTime int64  `json:"create_time"` // 标签组创建时间
		Order      uint32 `json:"order"`       // 标签组次序值
		Tags       []struct {
			ID         string `json:"id"`          // 新建标签id
			Name       string `json:"name"`        // 新建标签名称
			CreateTime int64  `json:"create_time"` // 标签创建时间
			Order      uint32 `json:"order"`       // 标签次序值
		} `json:"tag"` // 标签组内的标签列表
	} `json:"tag_group"` // 标签组详细信息
}

// AddCropTag 添加企业客户标签
// Doc: https://developer.work.weixin.qq.com/document/path/92117
// 企业可通过此接口向客户标签库中添加新的标签组和标签，每个企业最多可配置10000个企业标签。
func (w *Wework) AddCropTag(req *AddCropTagRequest) (*AddCropTagResponse, error) {

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

	response := new(AddCropTagResponse)
	err = w.locking(func() error {

		url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/add_corp_tag"
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

		return nil
	})
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
