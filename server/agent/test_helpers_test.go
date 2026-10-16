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
		"node-version: \"24.22.0\"",
		"npm ci",
		"npm run typecheck",
		"npm run test",
		"npm run build",
		"anban-runtime-claude-${{ steps.version.outputs.VERSION }}.tar.gz",
		"-C runtime-claude package.json package-lock.json dist",
		"anban-runtime-dsh-${{ steps.version.outputs.VERSION }}.tar.gz",
		"npm run patch:dependencies",
		"-C runtime-dsh package.json package-lock.json scripts vendor dist",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("release workflow missing %q", want)
		}
	}
}
