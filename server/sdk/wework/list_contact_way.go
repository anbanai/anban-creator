package wework

import (
	"encoding/json"
	"fmt"
)

// ListContactWayRequest 是获取企业已配置的「联系我」列表的请求参数
type ListContactWayRequest struct {
	StartTime int64  `json:"start_time,omitempty"` // 「联系我」创建起始时间戳, 默认为90天前
	EndTime   int64  `json:"end_time,omitempty"`   // 「联系我」创建结束时间戳, 默认为当前时间
	Cursor    string `json:"cursor,omitempty"`     // 分页查询使用的游标，为上次请求返回的 next_cursor
	Limit     int    `json:"limit,omitempty"`      // 每次查询的分页大小，默认为100条，最多支持1000条
}

// ListContactWayResponse 是获取企业已配置的「联系我」列表的返回结果
type ListContactWayResponse struct {
	CommonError
	ContactWay []struct {
		ConfigID string `json:"config_id"` // 联系方式的配置id
	} `json:"contact_way"`
	NextCursor string `json:"next_cursor"` // 分页参数，用于查询下一个分页的数据，为空时表示没有更多的分页
}

// ListContactWay 获取企业已配置的「联系我」列表
func (c *Wework) ListContactWay(req *ListContactWayRequest) (*ListContactWayResponse, error) {
	token, err := c.AccessToken()
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

	url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/list_contact_way"
	resp, err := c.client.R().
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

	response := new(ListContactWayResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
