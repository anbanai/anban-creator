package dingtalkbot

import (
	"errors"
	"regexp"
)

var credentialQueryParameter = regexp.MustCompile(`(?i)([?&](?:access_token|sign)=)[^&\s"'<>]+`)

type sanitizedRequestError struct {
	cause   error
	message string
}

func (e sanitizedRequestError) Error() string { return e.message }
func (e sanitizedRequestError) Unwrap() error { return e.cause }

func sanitizeRequestError(err error) error {
	if err == nil {
		return nil
	}
	var safe sanitizedRequestError
	if errors.As(err, &safe) {
		return err
	}
	redacted := credentialQueryParameter.ReplaceAllString(err.Error(), `${1}[REDACTED]`)
	if redacted == err.Error() {
		return err
	}
	return sanitizedRequestError{cause: err, message: redacted}
}

// Error 钉钉API错误
type Error struct {
	// 错误码，0表示成功
	Code int `json:"code"`
	// 错误描述
	Msg string `json:"msg"`
}

// IsSuccess 判断是否成功
func (e *Error) IsSuccess() bool {
	return e.Code == 0
}

// Error 实现error接口
func (e *Error) Error() string {
	return e.Msg
}

// 错误码常量
const (
	// CodeSuccess 成功
	CodeSuccess = 0
	// CodeInvalidToken 无效的access_token
	CodeInvalidToken = 40014
	// CodeInvalidWebhook 无效的webhook地址
	CodeInvalidWebhook = 310000
	// CodeInvalidIP IP地址不在白名单中
	CodeInvalidIP = 310001
	// CodeInvalidKeyword 消息内容中不包含关键词
	CodeInvalidKeyword = 310002
	// CodeFrequencyLimit 发送消息频率超限
	CodeFrequencyLimit = 310003
	// CodeInvalidSignature 签名不合法
	CodeInvalidSignature = 310004
)

// 错误信息
var errorMessages = map[int]string{
	CodeSuccess:          "success",
	CodeInvalidToken:     "无效的access_token",
	CodeInvalidWebhook:   "无效的webhook地址",
	CodeInvalidIP:        "IP地址不在白名单中",
	CodeInvalidKeyword:   "消息内容中不包含关键词",
	CodeFrequencyLimit:   "发送消息频率超限",
	CodeInvalidSignature: "签名不合法",
}

// GetErrorMessage 获取错误信息
func GetErrorMessage(code int) string {
	if msg, ok := errorMessages[code]; ok {
		return msg
	}
	return "未知错误"
}
