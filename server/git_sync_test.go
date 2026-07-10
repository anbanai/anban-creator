package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupGitSyncConfiguresRepositoryLocalBehavior(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	mustMkdirAll(t, repo)
	runCommand(t, repo, nil, "git", "init", "--initial-branch=main")

	script := repositoryPath(t, "scripts", "setup-git-sync.sh")
	runCommand(t, repo, nil, script)

	want := map[string]string{
		"core.hooksPath":          ".githooks",
		"submodule.recurse":       "true",
		"fetch.recurseSubmodules": "on-demand",
		"push.recurseSubmodules":  "no",
		"status.submoduleSummary": "true",
		"diff.submodule":          "log",
	}
	for key, expected := range want {
		t.Run(key, func(t *testing.T) {
			actual := strings.TrimSpace(runCommand(t, repo, nil, "git", "config", "--local", "--get", key))
			if actual != expected {
				t.Fatalf("git config %s = %q, want %q", key, actual, expected)
			}
		})
	}
}

func repositoryPath(t *testing.T, elements ...string) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return filepath.Join(append([]string{root}, elements...)...)
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("create directory %s: %v", path, err)
	}
}

func runCommand(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %s in %s: %v\n%s", name, strings.Join(args, " "), dir, err, output)
	}
	return string(output)
}
