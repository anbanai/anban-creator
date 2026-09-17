package wechat

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var ErrDownloadExceedsMaxSize = errors.New("download exceeds maximum size")
var ErrUnsafeDownloadURL = errors.New("unsafe download URL")

var errCodeRegexp = regexp.MustCompile(`errcode=(-?\d+)`)

// WechatAPIError 微信 API 错误
type WechatAPIError struct {
	ErrCode   int
	UserMsg   string
	HintMsg   string
	Retryable bool
	Original  error
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

// DownloadError carries diagnostics for server-side file downloads.
type DownloadError struct {
	URL           string
	StatusCode    int
	ContentType   string
	ContentLength string
	Server        string
	CFRay         string
	Location      string
	BodyPreview   string
	Elapsed       time.Duration
	Original      error
}

func (e *DownloadError) Error() string {
	if e == nil {
		return "download failed"
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("download failed with status: %d", e.StatusCode)
	}
	if e.Original != nil {
		return fmt.Sprintf("download file: %v", e.Original)
	}
	return "download failed"
}

func (e *DownloadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Original
}

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
	48001: {"API 功能未授权", "仅认证后的服务号支持此接口，订阅号和未认证服务号无此权限", false},
	42001: {"access_token 已过期", "SDK 将自动刷新，请重试", true},
	45009: {"接口调用频率超限", "请稍后重试", true},
	40007: {"无效的 media_id", "thumb_media_id 必须是微信素材管理接口返回的永久素材 ID，不能使用 URL 或文件路径", false},
	40009: {"无效的图片媒体 ID", "图片 media_id 不存在或已过期，请重新上传", false},
	41006: {"缺少 media_id", "请检查请求中是否包含必需的 media_id", false},
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

	// 未知错误码默认不可重试，避免永久性错误导致无效重试
	return &WechatAPIError{
		ErrCode:   code,
		UserMsg:   "微信 API 错误",
		HintMsg:   "",
		Retryable: false,
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
