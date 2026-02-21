package wechat

import (
	"fmt"
	"regexp"
	"strconv"
)

var errCodeRegexp = regexp.MustCompile(`errcode=(-?\d+)`)

// WechatAPIError 微信 API 错误
type WechatAPIError struct {
	ErrCode  int
	UserMsg  string
	HintMsg  string
	Retryable bool
	Original error
}

func (e *WechatAPIError) Error() string {
	if e.HintMsg != "" {
		return fmt.Sprintf("%s（错误码 %d）\n提示：%s", e.UserMsg, e.ErrCode, e.HintMsg)
	}
	return fmt.Sprintf("%s（错误码 %d）", e.UserMsg, e.ErrCode)
}

func (e *WechatAPIError) Unwrap() error {
	return e.Original
}

func (e *WechatAPIError) Hint() string { return e.HintMsg }

// knownErrors 已知错误码映射表
var knownErrors = map[int]struct {
	userMsg   string
	hint      string
	retryable bool
}{
	40001: {"无效的凭证", "请检查 AppID 和 AppSecret 是否正确", false},
	40125: {"无效的 AppSecret", "请在微信公众平台重新确认 AppSecret", false},
	40164: {"IP 不在白名单", "请登录微信公众平台，将当前服务器 IP 加入白名单（开发->基本配置->IP白名单）", false},
	41001: {"缺少 access_token", "请检查配置是否正确", false},
	48001: {"API 功能未授权", "当前公众号未开通此接口权限", false},
	42001: {"access_token 已过期", "SDK 将自动刷新，请重试", true},
	45009: {"接口调用频率超限", "请稍后重试", true},
	-1:    {"系统繁忙", "请稍后重试", true},
}

// ParseWechatError 从错误中解析微信 API 错误码
// 如果不是微信 API 错误，返回 nil
func ParseWechatError(err error) *WechatAPIError {
	if err == nil {
		return nil
	}

	matches := errCodeRegexp.FindStringSubmatch(err.Error())
	if len(matches) < 2 {
		return nil
	}

	code, parseErr := strconv.Atoi(matches[1])
	if parseErr != nil {
		return nil
	}

	if info, ok := knownErrors[code]; ok {
		return &WechatAPIError{
			ErrCode:   code,
			UserMsg:   info.userMsg,
			HintMsg:   info.hint,
			Retryable: info.retryable,
			Original:  err,
		}
	}

	// 未知错误码，默认可重试（保守策略）
	return &WechatAPIError{
		ErrCode:   code,
		UserMsg:   fmt.Sprintf("微信 API 错误"),
		HintMsg:   "",
		Retryable: true,
		Original:  err,
	}
}

// IsRetryable 判断错误是否可重试
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if wErr, ok := err.(*WechatAPIError); ok {
		return wErr.Retryable
	}
	// 尝试解析
	wErr := ParseWechatError(err)
	if wErr != nil {
		return wErr.Retryable
	}
	// 非微信 API 错误（如网络错误），默认可重试
	return true
}
