package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func TestReleaseBuildsAgentRunnerAssets(t *testing.T) {
	body := readRepoFile(t, filepath.Join(repositoryRoot(t), ".github", "workflows", "release.yml"))
	for _, want := range []string{
		"-o bin/anban-linux-amd64 ./agent",
		"-o bin/anban-linux-arm64 ./agent",
		"-o bin/anban-darwin-amd64 ./agent",
		"-o bin/anban-darwin-arm64 ./agent",
		"-o bin/anban-windows-amd64.exe ./agent",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("release workflow missing %q", want)
		}
	}
}
