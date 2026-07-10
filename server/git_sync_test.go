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

func TestPushManagedSubmodulesPushesDetachedHeadToSuperprojectBranch(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "plugin.git")
	seed := filepath.Join(root, "seed")
	superproject := filepath.Join(root, "superproject")

	mustMkdirAll(t, remote)
	runCommand(t, root, nil, "git", "init", "--bare", "--initial-branch=main", remote)
	initWorkingRepository(t, seed)
	writeFile(t, filepath.Join(seed, "plugin.txt"), "initial\n")
	runCommand(t, seed, nil, "git", "add", "plugin.txt")
	runCommand(t, seed, nil, "git", "commit", "-m", "initial")
	runCommand(t, seed, nil, "git", "remote", "add", "origin", remote)
	runCommand(t, seed, nil, "git", "push", "-u", "origin", "main")
	initialRemoteHead := strings.TrimSpace(runCommand(t, remote, nil, "git", "rev-parse", "refs/heads/main"))

	initWorkingRepository(t, superproject)
	writeFile(t, filepath.Join(superproject, "README.md"), "superproject\n")
	runCommand(t, superproject, nil, "git", "add", "README.md")
	runCommand(t, superproject, nil, "git", "commit", "-m", "initial superproject")
	runCommand(t, superproject, nil, "git", "-c", "protocol.file.allow=always", "submodule", "add", remote, "plugin")
	runCommand(t, superproject, nil, "git", "config", "-f", ".gitmodules", "submodule.plugin.syncPush", "true")
	runCommand(t, superproject, nil, "git", "add", ".gitmodules", "plugin")
	runCommand(t, superproject, nil, "git", "commit", "-m", "add plugin")

	plugin := filepath.Join(superproject, "plugin")
	configureGitIdentity(t, plugin)
	runCommand(t, plugin, nil, "git", "checkout", "--detach")
	writeFile(t, filepath.Join(plugin, "plugin.txt"), "detached commit\n")
	runCommand(t, plugin, nil, "git", "add", "plugin.txt")
	runCommand(t, plugin, nil, "git", "commit", "-m", "detached change")
	detachedHead := strings.TrimSpace(runCommand(t, plugin, nil, "git", "rev-parse", "HEAD"))
	if detachedHead == initialRemoteHead {
		t.Fatal("detached commit unexpectedly matches initial remote head")
	}
	runCommand(t, superproject, nil, "git", "add", "plugin")
	runCommand(t, superproject, nil, "git", "commit", "-m", "record detached plugin commit")

	script := repositoryPath(t, "scripts", "push-managed-submodules.sh")
	runCommand(t, superproject, nil, script, "main", "origin")

	remoteHead := strings.TrimSpace(runCommand(t, remote, nil, "git", "rev-parse", "refs/heads/main"))
	if remoteHead != detachedHead {
		t.Fatalf("remote main = %s, want detached submodule HEAD %s", remoteHead, detachedHead)
	}
}

func initWorkingRepository(t *testing.T, path string) {
	t.Helper()
	mustMkdirAll(t, path)
	runCommand(t, path, nil, "git", "init", "--initial-branch=main")
	configureGitIdentity(t, path)
}

func configureGitIdentity(t *testing.T, path string) {
	t.Helper()
	runCommand(t, path, nil, "git", "config", "user.name", "Anban Test")
	runCommand(t, path, nil, "git", "config", "user.email", "anban-test@example.com")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
