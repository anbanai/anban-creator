package wework

import (
	"encoding/json"
	"fmt"
)

// 请求参数
type AddContactWayRequest struct {
	// type	是	联系方式类型,1-单人, 2-多人
	Type int `json:"type"`
	// scene	是	场景，1-在小程序中联系，2-通过二维码联系
	Scene int `json:"scene"`
	// style	否	在小程序中联系时使用的控件样式，详见附表
	Style int `json:"style,omitempty"`
	// remark	否	联系方式的备注信息，用于助记，不超过30个字符
	Remark string `json:"remark,omitempty"`
	// skip_verify	否	外部客户添加时是否无需验证，默认为true
	SkipVerify bool `json:"skip_verify,omitempty"`
	// state	否	企业自定义的state参数，用于区分不同的添加渠道，在调用“获取外部联系人详情”时会返回该参数值，不超过30个字符
	State string `json:"state,omitempty"`
	// user	否	使用该联系方式的用户userID列表，在type为1时为必填，且只能有一个
	User []string `json:"user,omitempty"`
	// party	否	使用该联系方式的部门id列表，只在type为2时有效
	Party []int `json:"party,omitempty"`
	// is_temp	否	是否临时会话模式，true表示使用临时会话模式，默认为false
	IsTemp bool `json:"is_temp,omitempty"`
	// expires_in	否	临时会话二维码有效期，以秒为单位。该参数仅在is_temp为true时有效，默认7天，最多为14天
	ExpiresIn int `json:"expires_in,omitempty"`
	// chat_expires_in	否	临时会话有效期，以秒为单位。该参数仅在is_temp为true时有效，默认为添加好友后24小时，最多为14天
	ChatExpiresIn int `json:"chat_expires_in,omitempty"`
	// unionid	否	可进行临时会话的客户unionid，该参数仅在is_temp为true时有效，如不指定则不进行限制
	Unionid string `json:"unionid,omitempty"`
	// is_exclusive	否	是否开启同一外部企业客户只能添加同一个员工，默认为否，开启后，同一个企业的客户会优先添加到同一个跟进人
	IsExclusive bool `json:"is_exclusive,omitempty"`
	// conclusions	否	结束语，会话结束时自动发送给客户，可参考“结束语定义”，仅在is_temp为true时有效
	// Conclusions any `json:"conclusions,omitempty"`
}

type AddContactWayResponse struct {
	CommonError
	// config_id	新增联系方式的配置id
	ConfigID string `json:"config_id"`
	// qr_code	联系我二维码链接，仅在scene为2时返回
	QRCode string `json:"qr_code"`
}

// 客户联系「联系我」管理: 添加联系方式
// doc: https://developer.work.weixin.qq.com/document/path/92228
func (w *Wework) AddContactWay(req *AddContactWayRequest) (*AddContactWayResponse, error) {
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

	url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/add_contact_way"
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

	response := new(AddContactWayResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
