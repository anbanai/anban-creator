package wework

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type MediaType = string

const (
	MediaTypeImage MediaType = "image"
	MediaTypeVoice MediaType = "voice"
	MediaTypeVideo MediaType = "video"
	MediaTypeFile  MediaType = "file"
)

// 上传临时素材的请求数据结构
type UploadTempMediaRequest struct {
	// 媒体文件类型，分别有图片（image）、语音（voice）、视频（video），普通文件(file)
	Type     MediaType `json:"type"`
	Media    []byte    `json:"media"`    // 媒体文件数据
	Filename string    `json:"filename"` // 媒体文件名
}

// 上传临时素材的响应数据结构
type UploadTempMediaResponse struct {
	CommonError
	Type string `json:"type"` // 媒体文件类型
	// 媒体文件上传后获取的唯一标识，3天内有效
	MediaID string `json:"media_id"` // 媒体文件上传后获取的唯一标识
	// 媒体文件上传时间戳
	// 秒级时间戳
	CreatedAt string `json:"created_at"`
}

// 上传临时素材
// doc: https://developer.work.weixin.qq.com/document/path/90253
func (cli *Wework) UploadTempMedia(req *UploadTempMediaRequest) (*UploadTempMediaResponse, error) {
	token, err := cli.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	queries := map[string]string{
		"access_token": token.AccessToken,
		"type":         req.Type,
	}

	url := "https://qyapi.weixin.qq.com/cgi-bin/media/upload"
	resp, err := cli.client.R().
		SetHeader("Content-Type", "multipart/form-data").
		SetFileReader("media", req.Filename, bytes.NewReader(req.Media)).
		SetQueryParams(queries).
		Post(url)
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("failed to request, status code: %d", resp.StatusCode())
	}

	response := new(UploadTempMediaResponse)
	if err := json.Unmarshal(resp.Bytes(), response); err != nil {
		return nil, sanitizeRequestError(err)
	}

	if err := response.GetResponseError(); err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
