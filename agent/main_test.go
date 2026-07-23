package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAgentPreparesRuntimeOutput(t *testing.T) {
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = runAgent(ctx, &Config{
		ServerURL: server.URL, APIKey: "key", TaskID: "task-1", TaskType: "article",
		Topic: "write", Workspace: workspace, MaxTurns: 1,
	}, io.Discard, io.Discard)

	info, err := os.Lstat(filepath.Join(workspace, "output"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o750 {
		t.Fatalf("runtime output = %#v, err=%v", info, err)
	}
}

func TestRunAgentRejectsUnsafeRuntimeOutput(t *testing.T) {
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
