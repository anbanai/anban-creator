package main

import "errors"

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
