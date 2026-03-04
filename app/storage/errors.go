package storage

import "fmt"

// StorageError 存储层错误，携带修复建议
type StorageError struct {
	Message  string
	HintText string
	Original error
}

func (e *StorageError) Error() string {
	if e.Original != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Original)
	}
	return e.Message
}

func (e *StorageError) Unwrap() error { return e.Original }
func (e *StorageError) Hint() string  { return e.HintText }
