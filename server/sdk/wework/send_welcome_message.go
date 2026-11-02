package wework

import (
	"encoding/json"
	"fmt"
)

// 请求的数据结构
type SendWelcomeMessageRequest struct {
	WelcomeCode string `json:"welcome_code"`
	Text        *Text  `json:"text,omitempty"`
	// 附件，最多可添加9个附件
	Attachments []Attachment `json:"attachments,omitempty"`
}

type Text struct {
	// 消息文本内容,最长为4000字节
	Content string `json:"content"`
}

type Attachment struct {
	MsgType     string       `json:"msgtype"`
	Image       *Media       `json:"image,omitempty"`
	Link        *Link        `json:"link,omitempty"`
	MiniProgram *MiniProgram `json:"miniprogram,omitempty"`
	Video       *Media       `json:"video,omitempty"`
	File        *Media       `json:"file,omitempty"`
}

type Media struct {
	MediaID string `json:"media_id,omitempty"`
	PicURL  string `json:"pic_url,omitempty"`
}

type Link struct {
	Title  string `json:"title"`
	PicURL string `json:"picurl,omitempty"`
	Desc   string `json:"desc,omitempty"`
	URL    string `json:"url"`
}

type MiniProgram struct {
	Title      string `json:"title"`
	PicMediaID string `json:"pic_media_id"`
	AppID      string `json:"appid"`
	Page       string `json:"page"`
}

// 响应的数据结构
type SendWelcomeMessageResponse struct {
	CommonError
}

// 发送欢迎消息
// doc: https://developer.work.weixin.qq.com/document/path/92137
func (cli *Wework) SendWelcomeMessage(req *SendWelcomeMessageRequest) (*SendWelcomeMessageResponse, error) {
	token, err := cli.AccessToken()
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

	url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/send_welcome_msg"
	resp, err := cli.client.R().
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

	response := new(SendWelcomeMessageResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
