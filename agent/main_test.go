package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
)

func TestRunAgentPreparesDesktopWorkspaceOutput(t *testing.T) {
	t.Setenv(homeTemplateEnv, "")
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := &Config{
		ServerURL: server.URL, APIKey: "key", TaskID: "task-1", TaskType: "article",
		Topic: "write", Workspace: workspace, MaxTurns: 1,
	}
	_ = runAgent(ctx, cfg, io.Discard, io.Discard)
	info, err := os.Lstat(filepath.Join(workspace, "output"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o750 {
		t.Fatalf("desktop output = %#v, err=%v", info, err)
	}
}

func TestRunAgentRejectsUnsafeWorkspaceOutput(t *testing.T) {
	t.Setenv(homeTemplateEnv, "")
	for _, setup := range []struct {
		name string
		run  func(*testing.T, string)
	}{
		{name: "symlink", run: func(t *testing.T, path string) {
			if err := os.Symlink(t.TempDir(), path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "regular file", run: func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("unsafe"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(setup.name, func(t *testing.T) {
			workspace := t.TempDir()
			setup.run(t, filepath.Join(workspace, "output"))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := runAgent(ctx, &Config{
				ServerURL: "http://127.0.0.1:1", APIKey: "key", TaskID: "task-1", TaskType: "article",
				Topic: "write", Workspace: workspace, MaxTurns: 1,
			}, io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "real directory") {
				t.Fatalf("unsafe output error = %v", err)
			}
		})
	}
}

func TestPrepareRuntimeWorkspaceCreatesLiveSlicerOutputTree(t *testing.T) {
	workspace := t.TempDir()
	if err := prepareRuntimeWorkspace(workspace, model.TaskTypeLiveSlicer, agentpack.AdapterStandard); err != nil {
		t.Fatalf("prepareRuntimeWorkspace: %v", err)
	}

	for _, relative := range []string{"output", "output/exports", "output/exports/.parts"} {
		assertRuntimeDirectory(t, filepath.Join(workspace, filepath.FromSlash(relative)))
	}
}

func TestPrepareRuntimeWorkspaceReusesLiveSlicerOutputTree(t *testing.T) {
	workspace := t.TempDir()
	for _, relative := range []string{"output", "output/exports", "output/exports/.parts"} {
		path := filepath.Join(workspace, filepath.FromSlash(relative))
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", relative, err)
		}
	}

	for range 2 {
		if err := prepareRuntimeWorkspace(workspace, " "+model.TaskTypeLiveSlicer+" ", agentpack.AdapterStandard); err != nil {
			t.Fatalf("prepareRuntimeWorkspace: %v", err)
		}
	}
	for _, relative := range []string{"output", "output/exports", "output/exports/.parts"} {
		assertRuntimeDirectory(t, filepath.Join(workspace, filepath.FromSlash(relative)))
	}
}

func TestPrepareRuntimeWorkspaceRejectsUnsafeLiveSlicerOutputTree(t *testing.T) {
	tests := []struct {
		name     string
		relative string
		setup    func(*testing.T, string)
	}{
		{name: "exports symlink", relative: "output/exports", setup: symlinkRuntimePath},
		{name: "exports regular file", relative: "output/exports", setup: fileRuntimePath},
		{name: "parts symlink", relative: "output/exports/.parts", setup: symlinkRuntimePath},
		{name: "parts regular file", relative: "output/exports/.parts", setup: fileRuntimePath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			path := filepath.Join(workspace, filepath.FromSlash(tt.relative))
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			tt.setup(t, path)

			err := prepareRuntimeWorkspace(workspace, model.TaskTypeLiveSlicer, agentpack.AdapterStandard)
			if err == nil || !strings.Contains(err.Error(), "real directory") {
				t.Fatalf("prepareRuntimeWorkspace error = %v, want real-directory rejection", err)
			}
		})
	}
}

func TestPrepareRuntimeWorkspaceLeavesOtherProfilesAtOutputRoot(t *testing.T) {
	workspace := t.TempDir()
	if err := prepareRuntimeWorkspace(workspace, "article", agentpack.AdapterStandard); err != nil {
		t.Fatalf("prepareRuntimeWorkspace: %v", err)
	}
	assertRuntimeDirectory(t, filepath.Join(workspace, "output"))
	if _, err := os.Lstat(filepath.Join(workspace, "output", "exports")); !os.IsNotExist(err) {
		t.Fatalf("article exports path error = %v, want not exist", err)
	}
}

func assertRuntimeDirectory(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o750 {
		t.Fatalf("runtime directory %s = %#v, err=%v", path, info, err)
	}
}

func symlinkRuntimePath(t *testing.T, path string) {
	t.Helper()
	if err := os.Symlink(t.TempDir(), path); err != nil {
		t.Fatal(err)
	}
}

func fileRuntimePath(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("unsafe"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProcessExitCodeReservesTwoForCompletionReportFailure(t *testing.T) {
	if got := processExitCode(&completionReportError{err: errors.New("server unavailable")}); got != 2 {
		t.Fatalf("completion exit code = %d, want 2", got)
	}
	if got := processExitCode(errors.New("runner failed")); got != 1 {
		t.Fatalf("ordinary exit code = %d, want 1", got)
	}
}
