//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package agentpack

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestGenerateRejectsDSHSkillNonRegularFile(t *testing.T) {
	root := writePackFixture(t, validDSHFixtureManifest())
	namedPipe := filepath.Join(root, "skills", "demo-skill", "invalid")
	if err := syscall.Mkfifo(namedPipe, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(root, filepath.Join(t.TempDir(), "generated")); err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("Generate error = %v, want non-regular file rejection", err)
	}
}
