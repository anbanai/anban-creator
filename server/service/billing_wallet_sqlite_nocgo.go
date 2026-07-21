//go:build !cgo

package service

func isRetryableSQLiteError(error) bool {
	return false
}
