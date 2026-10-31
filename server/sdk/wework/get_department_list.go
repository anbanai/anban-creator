package wework

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// 部门信息
type Department struct {
	ID       int `json:"id"`
	ParentID int `json:"parentid"`
	Order    int `json:"order"`
}

// 获取部门列表的响应
type GetDepartmentListResponse struct {
	CommonError
	DepartmentID []Department `json:"department_id"`
}

// 获取部门列表
// https://qyapi.weixin.qq.com/cgi-bin/department/simplelist?access_token=ACCESS_TOKEN&id=ID
func (w *Wework) GetDepartmentList(id *int) (*GetDepartmentListResponse, error) {
	token, err := w.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}
	url := "https://qyapi.weixin.qq.com/cgi-bin/department/simplelist"
	request := w.client.R().
		SetHeader("Content-Type", "application/json").
		SetQueryParam("access_token", token.AccessToken)
	if id != nil {
		request.SetQueryParam("id", strconv.Itoa(*id))
	}

	resp, err := request.Get(url)
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("http request error with status %v", resp.StatusCode())
	}

	response := new(GetDepartmentListResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
