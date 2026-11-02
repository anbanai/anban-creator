package wework

import (
	"encoding/json"
	"fmt"
)

// UpdateExternalContactRemarkRequest 更新备注信息请求结构体
type UpdateExternalContactRemarkRequest struct {
	UserID           string   `json:"userid"`                       // 企业成员的userid
	ExternalUserID   string   `json:"external_userid"`              // 外部联系人userid
	Remark           string   `json:"remark,omitempty"`             // 此用户对外部联系人的备注，最多20个字符
	Description      string   `json:"description,omitempty"`        // 此用户对外部联系人的描述，最多150个字符
	RemarkCompany    string   `json:"remark_company,omitempty"`     // 此用户对外部联系人备注的所属公司名称，最多20个字符
	RemarkMobiles    []string `json:"remark_mobiles,omitempty"`     // 此用户对外部联系人备注的手机号
	RemarkPicMediaID string   `json:"remark_pic_mediaid,omitempty"` // 备注图片的mediaid
}

// UpdateExternalContactRemarkResponse 更新备注信息响应结构体
type UpdateExternalContactRemarkResponse struct {
	CommonError
}

// UpdateExternalContactRemark 更新备注信息
func (w *Wework) UpdateExternalContactRemark(req *UpdateExternalContactRemarkRequest) (*UpdateExternalContactRemarkResponse, error) {

	token, err := w.AccessToken()
	if err != nil {
		return nil, err
	}

	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	queries := map[string]string{
		"access_token": token.AccessToken,
	}

	mutex := w.userLockFor(req.ExternalUserID)
	mutex.Lock()
	defer mutex.Unlock()

	response := new(UpdateExternalContactRemarkResponse)

	err = w.locking(func() error {
		url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/remark"
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
		return nil, err
	}

	return response, nil
}
