package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractArticleDraftFromWorkspaceSkipsDockerRuntimeHome(t *testing.T) {
	root := t.TempDir()
	runtimeHTML := filepath.Join(root, ".anban-runtime-home", "cached.html")
	if err := os.MkdirAll(filepath.Dir(runtimeHTML), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimeHTML, []byte("<h1>private runtime state</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := extractArticleDraftFromWorkspace(root)
	if err == nil || !strings.Contains(err.Error(), "no draft.json or HTML file") {
		t.Fatalf("extractArticleDraftFromWorkspace error = %v, want no draft error", err)
	}
}
