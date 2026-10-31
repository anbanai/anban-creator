package feishubot

import (
	"errors"
	"net/url"
	"strings"
)

type sanitizedRequestError struct {
	cause   error
	message string
}

func (e sanitizedRequestError) Error() string { return e.message }
func (e sanitizedRequestError) Unwrap() error { return e.cause }

func sanitizeRequestError(err error, webhookID string) error {
	if err == nil || webhookID == "" {
		return err
	}
	var safe sanitizedRequestError
	if errors.As(err, &safe) {
		return err
	}
	redacted := strings.NewReplacer(webhookID, "[REDACTED]", url.PathEscape(webhookID), "[REDACTED]").Replace(err.Error())
	if redacted == err.Error() {
		return err
	}
	return sanitizedRequestError{cause: err, message: redacted}
}

// Error 飞书API错误
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
	// CodeInvalidParam 参数错误
	CodeInvalidParam = 9499
	// CodeFrequencyLimit 频率限制
	CodeFrequencyLimit = 11232
	// CodeInvalidWebhook 无效的webhook地址
	CodeInvalidWebhook = 19001
	// CodeInvalidSignature 签名校验失败
	CodeInvalidSignature = 19002
	// CodeInvalidIP IP校验失败
	CodeInvalidIP = 19003
	// CodeInvalidKeyword 关键词校验失败
	CodeInvalidKeyword = 19004
	// CodeInvalidBot 机器人不存在或已被禁用
	CodeInvalidBot = 19005
)

// 错误信息
var errorMessages = map[int]string{
	CodeSuccess:          "success",
	CodeInvalidParam:     "参数错误",
	CodeFrequencyLimit:   "频率限制",
	CodeInvalidWebhook:   "无效的webhook地址",
	CodeInvalidSignature: "签名校验失败",
	CodeInvalidIP:        "IP校验失败",
	CodeInvalidKeyword:   "关键词校验失败",
	CodeInvalidBot:       "机器人不存在或已被禁用",
}

// GetErrorMessage 获取错误信息
func GetErrorMessage(code int) string {
	if msg, ok := errorMessages[code]; ok {
		return msg
	}
	return "未知错误"
}
