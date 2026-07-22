//go:build unix

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestMaterializeBootstrapDownloadFailureLeavesWorkspaceUnchanged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/two") {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("one"))
	}))
	defer server.Close()
	root := t.TempDir()
	client := bootstrapLoopbackTestDownloadClient()
	files := []BootstrapFile{
		{Path: "input/one.txt", DownloadURL: server.URL + "/one", Mode: 0o644},
		{Path: "input/two.txt", DownloadURL: server.URL + "/two", Mode: 0o644},
	}
	if err := materializeBootstrap(context.Background(), root, files, client); err == nil {
		t.Fatal("expected download failure")
	}
	assertBootstrapWorkspaceEmpty(t, root)
}

func TestMaterializeBootstrapCommitFailureRollsBackCreatedFiles(t *testing.T) {
	root := t.TempDir()
	bootstrapCommitHook = func(rel string) error {
		if rel == "input/two.txt" {
			return errors.New("forced commit failure")
		}
		return nil
	}
	t.Cleanup(func() { bootstrapCommitHook = nil })
	files := []BootstrapFile{
		{Path: "input/one.txt", Text: "one", Mode: 0o644},
		{Path: "input/two.txt", Text: "two", Mode: 0o644},
	}
	if err := materializeBootstrap(context.Background(), root, files, nil); err == nil {
		t.Fatal("expected commit failure")
	}
	assertBootstrapWorkspaceEmpty(t, root)
}

func TestMaterializeBootstrapParentSwapCannotEscapePinnedDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	parent := filepath.Join(root, "safe")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	pinned := filepath.Join(root, "pinned")
	bootstrapCommitHook = func(rel string) error {
		if rel != "safe/file.txt" {
			return nil
		}
		if err := os.Rename(parent, pinned); err != nil {
			return err
		}
		return os.Symlink(outside, parent)
	}
	t.Cleanup(func() { bootstrapCommitHook = nil })
	err := materializeBootstrap(context.Background(), root, []BootstrapFile{{Path: "safe/file.txt", Text: "safe", Mode: 0o644}}, nil)
	if _, statErr := os.Stat(filepath.Join(outside, "file.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("publication escaped workspace: %v", statErr)
	}
	if err == nil {
		got, readErr := os.ReadFile(filepath.Join(pinned, "file.txt"))
		if readErr != nil || string(got) != "safe" {
			t.Fatalf("pinned publication=%q err=%v", got, readErr)
		}
	}
}

func TestMaterializeBootstrapPostLinkFsyncFailureRollsBackLink(t *testing.T) {
	root := t.TempDir()
	failNext := false
	previousFsync := bootstrapFsync
	bootstrapFsync = func(fd int) error {
		if failNext {
			failNext = false
			return errors.New("forced fsync failure")
		}
		return previousFsync(fd)
	}
	bootstrapCommitHook = func(string) error { failNext = true; return nil }
	t.Cleanup(func() { bootstrapFsync = previousFsync; bootstrapCommitHook = nil })
	err := materializeBootstrap(context.Background(), root, []BootstrapFile{{Path: "output.txt", Text: "x", Mode: 0o644}}, nil)
	if err == nil {
		t.Fatal("expected fsync failure")
	}
	if _, statErr := os.Stat(filepath.Join(root, "output.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("link survived rollback: %v", statErr)
	}
}

func TestMaterializeBootstrapDupFailureAfterMkdirRollsBackDirectory(t *testing.T) {
	root := t.TempDir()
	previousDup := bootstrapRollbackDup
	bootstrapRollbackDup = func(int) (int, error) { return -1, errors.New("forced dup failure") }
	t.Cleanup(func() { bootstrapRollbackDup = previousDup })

	err := materializeBootstrap(context.Background(), root, []BootstrapFile{{Path: "new/dir/output.txt", Text: "x", Mode: 0o644}}, nil)
	if err == nil || !strings.Contains(err.Error(), "forced dup failure") {
		t.Fatalf("materializeBootstrap error = %v, want dup failure", err)
	}
	assertBootstrapWorkspaceEmpty(t, root)
}

func TestMaterializeBootstrapReplayPreservesMontageAndClaudeRuntimeState(t *testing.T) {
	root := t.TempDir()
	checkpointPath := filepath.Join(root, "montage", "projects", "task-1", "checkpoint_assets.json")
	sessionPath := filepath.Join(root, ".anban-runtime-home", ".claude", "projects", "session.jsonl")
	for path, body := range map[string]string{
		checkpointPath: `{"checkpoint":"assets"}`,
		sessionPath:    `{"session":"claude"}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	checkpointBefore, _ := os.ReadFile(checkpointPath)
	sessionBefore, _ := os.ReadFile(sessionPath)
	base := BootstrapFile{Path: ".anban-creator/task.json", Text: `{"task":"task-1"}`, Mode: 0o644}
	if err := materializeBootstrap(context.Background(), root, []BootstrapFile{base}, nil); err != nil {
		t.Fatal(err)
	}
	resume := BootstrapFile{Path: ".anban-creator/resume/execution-2/context.md", Text: "continue", Mode: 0o644}
	if err := materializeBootstrap(context.Background(), root, []BootstrapFile{base, resume}, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(checkpointPath); err != nil || string(got) != string(checkpointBefore) {
		t.Fatalf("checkpoint changed across bootstrap replay: %q err=%v", got, err)
	}
	if got, err := os.ReadFile(sessionPath); err != nil || string(got) != string(sessionBefore) {
		t.Fatalf("Claude session changed across bootstrap replay: %q err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(resume.Path))); err != nil || string(got) != resume.Text {
		t.Fatalf("resume context = %q err=%v", got, err)
	}
}

func assertBootstrapWorkspaceEmpty(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("workspace contains partial bootstrap state: %v", entries)
	}
}
