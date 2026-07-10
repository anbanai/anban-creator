package main

import (
	"fmt"
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
	return runCommandInput(t, dir, env, "", name, args...)
}

func runCommandInput(t *testing.T, dir string, env []string, input, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %s in %s: %v\n%s", name, strings.Join(args, " "), dir, err, output)
	}
	return string(output)
}

func runCommandError(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	return runCommandErrorInput(t, dir, env, "", name, args...)
}

func runCommandErrorInput(t *testing.T, dir string, env []string, input, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("run %s %s in %s succeeded, want failure\n%s", name, strings.Join(args, " "), dir, output)
	}
	return string(output)
}

func TestPrePushHookDelegatesWithDestinationCommitAndRecursionGuard(t *testing.T) {
	hook := repositoryPath(t, ".githooks", "pre-push")
	info, err := os.Stat(hook)
	if err != nil {
		t.Fatalf("stat pre-push hook: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("pre-push hook mode = %v, want executable", info.Mode().Perm())
	}

	repo := filepath.Join(t.TempDir(), "repo")
	initWorkingRepository(t, repo)
	writeFile(t, filepath.Join(repo, "README.md"), "hook test\n")
	runCommand(t, repo, nil, "git", "add", "README.md")
	runCommand(t, repo, nil, "git", "commit", "-m", "initial")

	fakeScript := filepath.Join(repo, "scripts", "push-managed-submodules.sh")
	mustMkdirAll(t, filepath.Dir(fakeScript))
	writeExecutable(t, fakeScript, `#!/bin/sh
printf '%s|%s|%s|%s\n' "$#" "$1" "$2" "$ANBAN_SUBMODULE_PUSH_ACTIVE" > "$ANBAN_PUSH_LOG"
`)
	logPath := filepath.Join(repo, "push.log")
	head := strings.TrimSpace(runCommand(t, repo, nil, "git", "rev-parse", "HEAD"))
	input := prePushLine("refs/heads/main", head, "refs/heads/release")
	runCommandInput(t, repo, []string{"ANBAN_PUSH_LOG=" + logPath}, input, hook, "upstream", "/tmp/remote.git")

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read hook delegation log: %v", err)
	}
	if actual, expected := strings.TrimSpace(string(logData)), "2|release|"+head+"|1"; actual != expected {
		t.Fatalf("hook delegation = %q, want %q", actual, expected)
	}

	if err := os.Remove(logPath); err != nil {
		t.Fatalf("remove hook delegation log: %v", err)
	}
	runCommandInput(t, repo, []string{
		"ANBAN_PUSH_LOG=" + logPath,
		"ANBAN_SUBMODULE_PUSH_ACTIVE=1",
	}, input, hook, "upstream", "/tmp/remote.git")
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("recursive hook invocation created log; stat error = %v", err)
	}
}

func TestPrePushHookSkipsRefsOutsideCurrentBranch(t *testing.T) {
	hook := repositoryPath(t, ".githooks", "pre-push")
	repo := filepath.Join(t.TempDir(), "repo")
	initWorkingRepository(t, repo)
	writeFile(t, filepath.Join(repo, "README.md"), "hook test\n")
	runCommand(t, repo, nil, "git", "add", "README.md")
	runCommand(t, repo, nil, "git", "commit", "-m", "initial")
	head := strings.TrimSpace(runCommand(t, repo, nil, "git", "rev-parse", "HEAD"))

	fakeScript := filepath.Join(repo, "scripts", "push-managed-submodules.sh")
	mustMkdirAll(t, filepath.Dir(fakeScript))
	writeExecutable(t, fakeScript, `#!/bin/sh
printf 'unexpected delegation\n' > "$ANBAN_PUSH_LOG"
`)
	logPath := filepath.Join(repo, "push.log")
	input := prePushLine("refs/tags/v1.0.0", head, "refs/tags/v1.0.0") +
		prePushLine("refs/heads/other", head, "refs/heads/other")
	runCommandInput(t, repo, []string{"ANBAN_PUSH_LOG=" + logPath}, input, hook, "origin", "/tmp/remote.git")

	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("non-current ref push delegated to submodule script; stat error = %v", err)
	}
}

func TestPrePushHookSkipsTagOnlyPushFromDetachedHead(t *testing.T) {
	hook := repositoryPath(t, ".githooks", "pre-push")
	repo := filepath.Join(t.TempDir(), "repo")
	initWorkingRepository(t, repo)
	writeFile(t, filepath.Join(repo, "README.md"), "hook test\n")
	runCommand(t, repo, nil, "git", "add", "README.md")
	runCommand(t, repo, nil, "git", "commit", "-m", "initial")
	head := strings.TrimSpace(runCommand(t, repo, nil, "git", "rev-parse", "HEAD"))
	runCommand(t, repo, nil, "git", "checkout", "--detach")

	input := prePushLine("refs/tags/v1.0.0", head, "refs/tags/v1.0.0")
	runCommandInput(t, repo, nil, input, hook, "origin", "/tmp/remote.git")
}

func TestPrePushHookRejectsDetachedSuperproject(t *testing.T) {
	hook := repositoryPath(t, ".githooks", "pre-push")
	repo := filepath.Join(t.TempDir(), "repo")
	initWorkingRepository(t, repo)
	writeFile(t, filepath.Join(repo, "README.md"), "hook test\n")
	runCommand(t, repo, nil, "git", "add", "README.md")
	runCommand(t, repo, nil, "git", "commit", "-m", "initial")
	runCommand(t, repo, nil, "git", "checkout", "--detach")

	head := strings.TrimSpace(runCommand(t, repo, nil, "git", "rev-parse", "HEAD"))
	input := prePushLine("HEAD", head, "refs/heads/main")
	output := runCommandErrorInput(t, repo, nil, input, hook, "origin", "/tmp/remote.git")
	if !strings.Contains(output, "superproject HEAD is detached") {
		t.Fatalf("error output = %q, want detached HEAD hint", output)
	}
}

func prePushLine(localRef, localOID, remoteRef string) string {
	return fmt.Sprintf("%s %s %s %s\n", localRef, localOID, remoteRef, strings.Repeat("0", 40))
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
	runCommand(t, fixture.superproject, nil, script, "main")

	remoteHead := strings.TrimSpace(runCommand(t, fixture.remote, nil, "git", "rev-parse", "refs/heads/main"))
	if remoteHead != fixture.detachedHead {
		t.Fatalf("remote main = %s, want detached submodule HEAD %s", remoteHead, fixture.detachedHead)
	}
}

func TestPushManagedSubmodulesSkipsUnmarkedSubmodule(t *testing.T) {
	fixture := newGitSyncFixture(t, false, true)

	script := repositoryPath(t, "scripts", "push-managed-submodules.sh")
	runCommand(t, fixture.superproject, nil, script, "main")

	remoteHead := strings.TrimSpace(runCommand(t, fixture.remote, nil, "git", "rev-parse", "refs/heads/main"))
	if remoteHead != fixture.initialRemoteHead {
		t.Fatalf("unmarked submodule remote main = %s, want unchanged %s", remoteHead, fixture.initialRemoteHead)
	}
}

func TestPushManagedSubmodulesRejectsDirtySubmodule(t *testing.T) {
	fixture := newGitSyncFixture(t, true, true)
	writeFile(t, filepath.Join(fixture.plugin, "uncommitted.txt"), "not committed\n")

	script := repositoryPath(t, "scripts", "push-managed-submodules.sh")
	output := runCommandError(t, fixture.superproject, nil, script, "main")
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
	output := runCommandError(t, fixture.superproject, nil, script, "main")
	if !strings.Contains(output, "does not match the superproject gitlink") {
		t.Fatalf("error output = %q, want stale gitlink hint", output)
	}

	remoteHead := strings.TrimSpace(runCommand(t, fixture.remote, nil, "git", "rev-parse", "refs/heads/main"))
	if remoteHead != fixture.initialRemoteHead {
		t.Fatalf("stale-gitlink submodule remote main = %s, want unchanged %s", remoteHead, fixture.initialRemoteHead)
	}
}

func TestPushManagedSubmodulesUsesConfiguredRemote(t *testing.T) {
	fixture := newGitSyncFixture(t, true, true)
	runCommand(t, fixture.plugin, nil, "git", "remote", "rename", "origin", "plugin-origin")
	runCommand(t, fixture.superproject, nil, "git", "config", "-f", ".gitmodules", "submodule.plugin.syncRemote", "plugin-origin")
	runCommand(t, fixture.superproject, nil, "git", "add", ".gitmodules")
	runCommand(t, fixture.superproject, nil, "git", "commit", "-m", "configure plugin push remote")

	script := repositoryPath(t, "scripts", "push-managed-submodules.sh")
	runCommand(t, fixture.superproject, nil, script, "release")

	remoteHead := strings.TrimSpace(runCommand(t, fixture.remote, nil, "git", "rev-parse", "refs/heads/release"))
	if remoteHead != fixture.detachedHead {
		t.Fatalf("remote release = %s, want detached submodule HEAD %s", remoteHead, fixture.detachedHead)
	}
}

func TestPushManagedSubmodulesValidatesPushedCommitGitlink(t *testing.T) {
	fixture := newGitSyncFixture(t, true, true)
	writeFile(t, filepath.Join(fixture.plugin, "plugin.txt"), "newer detached commit\n")
	runCommand(t, fixture.plugin, nil, "git", "add", "plugin.txt")
	runCommand(t, fixture.plugin, nil, "git", "commit", "-m", "newer detached change")
	runCommand(t, fixture.superproject, nil, "git", "add", "plugin")

	script := repositoryPath(t, "scripts", "push-managed-submodules.sh")
	output := runCommandError(t, fixture.superproject, nil, script, "main")
	if !strings.Contains(output, "does not match the superproject gitlink in the pushed commit") {
		t.Fatalf("error output = %q, want pushed-commit gitlink hint", output)
	}

	remoteHead := strings.TrimSpace(runCommand(t, fixture.remote, nil, "git", "rev-parse", "refs/heads/main"))
	if remoteHead != fixture.initialRemoteHead {
		t.Fatalf("mismatched submodule remote main = %s, want unchanged %s", remoteHead, fixture.initialRemoteHead)
	}
}

func TestPushManagedSubmodulesValidatesAllBeforePushing(t *testing.T) {
	fixture := newGitSyncFixture(t, true, true)
	runCommand(t, fixture.superproject, nil, "git", "-c", "protocol.file.allow=always", "submodule", "add", fixture.remote, "broken")
	runCommand(t, fixture.superproject, nil, "git", "config", "-f", ".gitmodules", "submodule.broken.syncPush", "true")
	runCommand(t, fixture.superproject, nil, "git", "add", ".gitmodules", "broken")
	runCommand(t, fixture.superproject, nil, "git", "commit", "-m", "add second managed plugin")

	broken := filepath.Join(fixture.superproject, "broken")
	configureGitIdentity(t, broken)
	runCommand(t, broken, nil, "git", "checkout", "--detach")
	writeFile(t, filepath.Join(broken, "broken.txt"), "unrecorded commit\n")
	runCommand(t, broken, nil, "git", "add", "broken.txt")
	runCommand(t, broken, nil, "git", "commit", "-m", "unrecorded broken change")

	script := repositoryPath(t, "scripts", "push-managed-submodules.sh")
	output := runCommandError(t, fixture.superproject, nil, script, "main")
	if !strings.Contains(output, "does not match the superproject gitlink in the pushed commit") {
		t.Fatalf("error output = %q, want pushed-commit gitlink hint", output)
	}

	remoteHead := strings.TrimSpace(runCommand(t, fixture.remote, nil, "git", "rev-parse", "refs/heads/main"))
	if remoteHead != fixture.initialRemoteHead {
		t.Fatalf("remote main changed before all submodules validated: got %s, want %s", remoteHead, fixture.initialRemoteHead)
	}
}

func TestPushManagedSubmodulesRejectsMalformedManagedMetadata(t *testing.T) {
	fixture := newGitSyncFixture(t, true, true)
	runCommand(t, fixture.superproject, nil, "git", "config", "-f", ".gitmodules", "--unset", "submodule.plugin.path")
	runCommand(t, fixture.superproject, nil, "git", "add", ".gitmodules")
	runCommand(t, fixture.superproject, nil, "git", "commit", "-m", "remove managed plugin path")

	script := repositoryPath(t, "scripts", "push-managed-submodules.sh")
	output := runCommandError(t, fixture.superproject, nil, script, "main")
	if !strings.Contains(output, "has no path in the pushed .gitmodules") {
		t.Fatalf("error output = %q, want missing path hint", output)
	}

	remoteHead := strings.TrimSpace(runCommand(t, fixture.remote, nil, "git", "rev-parse", "refs/heads/main"))
	if remoteHead != fixture.initialRemoteHead {
		t.Fatalf("malformed metadata remote main = %s, want unchanged %s", remoteHead, fixture.initialRemoteHead)
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

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
