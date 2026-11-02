package wework

import (
	"encoding/json"
	"fmt"
)

// Conclusion 是结束语的定义
type SetConclusionRequest struct {
	Text struct {
		Content string `json:"content"` // 消息文本内容,最长为4000字节
	} `json:"text"`
	Image struct {
		MediaID string `json:"media_id"` // 图片的media_id
		PicURL  string `json:"pic_url"`  // 图片的url
	} `json:"image"`
	Link struct {
		Title  string `json:"title"`  // 图文消息标题，最长为128字节
		PicURL string `json:"picurl"` // 图文消息封面的url
		Desc   string `json:"desc"`   // 图文消息的描述，最长为512字节
		URL    string `json:"url"`    // 图文消息的链接
	} `json:"link"`
	Miniprogram struct {
		Title      string `json:"title"`        // 小程序消息标题，最长为64字节
		PicMediaID string `json:"pic_media_id"` // 小程序消息封面的mediaid，封面图建议尺寸为520*416
		AppID      string `json:"appid"`        // 小程序appid，必须是关联到企业的小程序应用
		Page       string `json:"page"`         // 小程序page路径
	} `json:"miniprogram"`
}

type SetConclusionResponse struct {
	CommonError
}

// SetConclusion 设置结束语
func (c *Wework) SetConclusion(req *SetConclusionRequest) (*SetConclusionResponse, error) {

	token, err := c.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}
	url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/set_conclusion"
	resp, err := c.client.R().
		SetHeader("Content-Type", "application/json").
		SetQueryParam("access_token", token.AccessToken).
		SetBody(req).
		Post(url)

	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	result := new(SetConclusionResponse)
	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("failed to request, status code: %d", resp.StatusCode())
	}
	if err = json.Unmarshal(resp.Bytes(), result); err != nil {
		return nil, sanitizeRequestError(err)
	}
	if err := result.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return result, nil
}
