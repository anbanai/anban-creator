//go:build unix

package main

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadProjectedTokenRejectsSpecialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProjectedToken(path); err == nil {
		t.Fatal("expected special file rejection")
	}
}
