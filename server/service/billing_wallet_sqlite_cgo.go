//go:build cgo

package service

import (
	"errors"

	"github.com/mattn/go-sqlite3"
)

func isRetryableSQLiteError(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked)
}
