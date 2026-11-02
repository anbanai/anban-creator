package wework

import (
	"encoding/json"
	"fmt"
)

// UpdateContactWayRequest 是更新企业已配置的「联系我」方式的请求参数
type UpdateContactWayRequest struct {
	ConfigID      string   `json:"config_id"`       // 企业联系方式的配置id
	Remark        string   `json:"remark"`          // 联系方式的备注信息，不超过30个字符，将覆盖之前的备注
	SkipVerify    bool     `json:"skip_verify"`     // 外部客户添加时是否无需验证
	Style         int      `json:"style"`           // 样式，只针对“在小程序中联系”的配置生效
	State         string   `json:"state"`           // 企业自定义的state参数，用于区分不同的添加渠道，在调用“获取外部联系人详情”时会返回该参数值
	User          []string `json:"user"`            // 使用该联系方式的用户列表，将覆盖原有用户列表
	Party         []int    `json:"party"`           // 使用该联系方式的部门列表，将覆盖原有部门列表，只在配置的type为2时有效
	ExpiresIn     int      `json:"expires_in"`      // 临时会话二维码有效期，以秒为单位，该参数仅在临时会话模式下有效
	ChatExpiresIn int      `json:"chat_expires_in"` // 临时会话有效期，以秒为单位，该参数仅在临时会话模式下有效
	Unionid       string   `json:"unionid"`         // 可进行临时会话的客户unionid，该参数仅在临时会话模式有效，如不指定则不进行限制
	// Conclusions   any      `json:"conclusions,omitempty"` // 结束语，会话结束时自动发送给客户，可参考“结束语定义”，仅临时会话模式（is_temp为true）可设置
}

// UpdateContactWayResponse 是更新企业已配置的「联系我」方式的返回结果
type UpdateContactWayResponse struct {
	CommonError
}

// UpdateContactWay 更新企业已配置的「联系我」方式
func (c *Wework) UpdateContactWay(req *UpdateContactWayRequest) (*UpdateContactWayResponse, error) {
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

	url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/update_contact_way"
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

	response := new(UpdateContactWayResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
