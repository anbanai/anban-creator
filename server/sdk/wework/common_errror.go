package wework

import (
	"fmt"
)

// CommonError 微信返回错误信息
type CommonError struct {
	ErrCode int    `json:"errcode"` // 错误码，0表示成功，其他值表示失败
	ErrMsg  string `json:"errmsg"`  // 错误信息，成功时为空字符串
}

// GetResponseError 获取微信服务器错返回误信息
func (err CommonError) GetResponseError() error {
	if err.ErrCode != 0 {
		return fmt.Errorf("wework server error: code[%d] msg[%s]", err.ErrCode, err.ErrMsg)
	}

	return nil
}
