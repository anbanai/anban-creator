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

func runCommandError(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("run %s %s in %s succeeded, want failure\n%s", name, strings.Join(args, " "), dir, output)
	}
	return string(output)
}

type gitSyncFixture struct {
	remote            string
	superproject      string
	plugin            string
	initialRemoteHead string
	detachedHead      string
}

func TestPushManagedSubmodulesPushesDetachedHeadToSuperprojectBranch(t *testing.T) {
	fixture := newGitSyncFixture(t, true, true)

	script := repositoryPath(t, "scripts", "push-managed-submodules.sh")
	runCommand(t, fixture.superproject, nil, script, "main", "origin")

	remoteHead := strings.TrimSpace(runCommand(t, fixture.remote, nil, "git", "rev-parse", "refs/heads/main"))
	if remoteHead != fixture.detachedHead {
		t.Fatalf("remote main = %s, want detached submodule HEAD %s", remoteHead, fixture.detachedHead)
	}
}

func TestPushManagedSubmodulesSkipsUnmarkedSubmodule(t *testing.T) {
	fixture := newGitSyncFixture(t, false, true)

	script := repositoryPath(t, "scripts", "push-managed-submodules.sh")
	runCommand(t, fixture.superproject, nil, script, "main", "origin")

	remoteHead := strings.TrimSpace(runCommand(t, fixture.remote, nil, "git", "rev-parse", "refs/heads/main"))
	if remoteHead != fixture.initialRemoteHead {
		t.Fatalf("unmarked submodule remote main = %s, want unchanged %s", remoteHead, fixture.initialRemoteHead)
	}
}

func TestPushManagedSubmodulesRejectsDirtySubmodule(t *testing.T) {
	fixture := newGitSyncFixture(t, true, true)
	writeFile(t, filepath.Join(fixture.plugin, "uncommitted.txt"), "not committed\n")

	script := repositoryPath(t, "scripts", "push-managed-submodules.sh")
	output := runCommandError(t, fixture.superproject, nil, script, "main", "origin")
	if !strings.Contains(output, "uncommitted changes") {
		t.Fatalf("error output = %q, want uncommitted changes hint", output)
	}

	remoteHead := strings.TrimSpace(runCommand(t, fixture.remote, nil, "git", "rev-parse", "refs/heads/main"))
	if remoteHead != fixture.initialRemoteHead {
		t.Fatalf("dirty submodule remote main = %s, want unchanged %s", remoteHead, fixture.initialRemoteHead)
	}
}

func TestPushManagedSubmodulesRejectsStaleGitlink(t *testing.T) {
	fixture := newGitSyncFixture(t, true, false)

	script := repositoryPath(t, "scripts", "push-managed-submodules.sh")
	output := runCommandError(t, fixture.superproject, nil, script, "main", "origin")
	if !strings.Contains(output, "does not match the superproject gitlink") {
		t.Fatalf("error output = %q, want stale gitlink hint", output)
	}

	remoteHead := strings.TrimSpace(runCommand(t, fixture.remote, nil, "git", "rev-parse", "refs/heads/main"))
	if remoteHead != fixture.initialRemoteHead {
		t.Fatalf("stale-gitlink submodule remote main = %s, want unchanged %s", remoteHead, fixture.initialRemoteHead)
	}
}

func newGitSyncFixture(t *testing.T, marked, recordDetachedHead bool) gitSyncFixture {
	t.Helper()
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
	if marked {
		runCommand(t, superproject, nil, "git", "config", "-f", ".gitmodules", "submodule.plugin.syncPush", "true")
	}
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
	if recordDetachedHead {
		runCommand(t, superproject, nil, "git", "add", "plugin")
		runCommand(t, superproject, nil, "git", "commit", "-m", "record detached plugin commit")
	}

	return gitSyncFixture{
		remote:            remote,
		superproject:      superproject,
		plugin:            plugin,
		initialRemoteHead: initialRemoteHead,
		detachedHead:      detachedHead,
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
