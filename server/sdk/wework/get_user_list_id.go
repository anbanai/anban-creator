package wework

import (
	"encoding/json"
	"fmt"
)

// GetUserListIDRequest 是获取企业成员的userid与对应的部门ID列表的请求参数
type GetUserListIDRequest struct {
	Cursor string `json:"cursor,omitempty"` // 用于分页查询的游标，字符串类型，由上一次调用返回，首次调用不填
	Limit  int    `json:"limit,omitempty"`  // 分页，预期请求的数据量，取值范围 1 ~ 10000
}

// DeptUser 是用户-部门关系列表
type DeptUser struct {
	UserID     string `json:"userid"`     // 用户userid，当用户在多个部门下时会有多条记录
	Department int    `json:"department"` // 用户所属部门
}

// GetUserListIDResponse 是获取企业成员的userid与对应的部门ID列表的响应结果
type GetUserListIDResponse struct {
	CommonError
	NextCursor string     `json:"next_cursor"` // 分页游标，下次请求时填写以获取之后分页的记录。如果该字段返回空则表示已没有更多数据
	DeptUser   []DeptUser `json:"dept_user"`   // 用户-部门关系列表
}

// GetUserListID 获取企业成员的userid与对应的部门ID列表
// doc: https://developer.work.weixin.qq.com/document/40856
func (w *Wework) GetUserListID(req *GetUserListIDRequest) (*GetUserListIDResponse, error) {

	token, err := w.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	raw, err := json.Marshal(req)
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	url := "https://qyapi.weixin.qq.com/cgi-bin/user/list_id"
	resp, err := w.client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(raw).
		SetQueryParam("access_token", token.AccessToken).
		Post(url)
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("failed to request, status code: %d", resp.StatusCode())
	}

	response := new(GetUserListIDResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
