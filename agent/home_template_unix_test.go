//go:build unix

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestMaterializeHomeTemplateRejectsSpecialFile(t *testing.T) {
	templateRoot := canonicalTempDir(t)
	if err := syscall.Mkfifo(filepath.Join(templateRoot, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := materializeHomeTemplate(templateRoot, canonicalTempDir(t)); err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("materializeHomeTemplate error = %v, want special-file rejection", err)
	}
}
