package main

import (
	"errors"
	"fmt"
)

// Hinter 可提供修复建议的错误接口
type Hinter interface {
	Hint() string
}

// AppError 通用应用错误，携带修复建议
type AppError struct {
	Code     string
	Message  string
	HintText string
	Original error
}

func (e *AppError) Error() string {
	if e.Original != nil {
		return e.Message + ": " + e.Original.Error()
	}
	return e.Message
}

func (e *AppError) Unwrap() error { return e.Original }
func (e *AppError) Hint() string  { return e.HintText }

// hintFrom 从错误链中提取修复建议
func hintFrom(err error) string {
	var h Hinter
	if errors.As(err, &h) {
		return h.Hint()
	}
	return ""
}

// DraftError 草稿错误
type DraftError struct {
	Message string
	HintMsg string
}

func (e *DraftError) Error() string {
	msg := fmt.Sprintf("草稿错误: %s", e.Message)
	if e.HintMsg != "" {
		msg += fmt.Sprintf("\n💡 提示:\n   %s", e.HintMsg)
	}
	return msg
}

func (e *DraftError) Hint() string { return e.HintMsg }
