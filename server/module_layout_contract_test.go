package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func moduleLayoutRepoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve module layout test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), ".."))
}

func moduleLayoutRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestServerOwnsGoModuleLayout(t *testing.T) {
	root := moduleLayoutRepoRoot(t)
	for _, path := range []string{"app", "go.mod", "go.sum"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("repository root %s must not exist after Server module migration", path)
		}
	}
	for _, path := range []string{"server/app", "server/go.mod", "server/go.sum"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("required Server module path %s: %v", path, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(root, "server", "go.mod"))
	if err == nil && !strings.HasPrefix(string(data), "module github.com/anbanai/anban-creator/server\n") {
		t.Error("server/go.mod has unexpected module declaration")
	}

	legacyImport := "github.com/anbanai/anban-creator/" + "app/"
	err = filepath.WalkDir(filepath.Join(root, "server"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		if strings.Contains(moduleLayoutRead(t, path), legacyImport) {
			t.Errorf("%s still imports the old App package path", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestServerModuleToolingTargetsServerRoot(t *testing.T) {
	root := moduleLayoutRepoRoot(t)
	cases := []struct {
		path     string
		required []string
	}{
		{
			path: "Makefile",
			required: []string{
				"go -C server test -v ./...",
				"go -C server fmt ./...",
				"go -C server vet ./...",
				"go -C server run ./cmd/agent-pack check -plugin-root ../harness -catalog agentpack/catalog.generated.json",
				"go -C server build -o ../$(BINDIR)/$(BINARY) .",
			},
		},
		{
			path: ".github/workflows/ci.yml",
			required: []string{
				"cache-dependency-path: server/go.sum",
				"go -C server test -race ./...",
			},
		},
		{
			path: ".github/workflows/release.yml",
			required: []string{
				"cache-dependency-path: server/go.sum",
				"go -C server test -v ./...",
				"go -C server build -ldflags=\"-s -w\" -o ../bin/anban-creator-server-linux-amd64 .",
			},
		},
		{
			path: "deploy/docker/Dockerfile.server",
			required: []string{
				"WORKDIR /build/server",
				"COPY server/go.mod server/go.sum ./",
				"COPY server ./",
				`go build -ldflags="-s -w" -o /anban-creator-server .`,
			},
		},
	}
	for _, tc := range cases {
		body := moduleLayoutRead(t, filepath.Join(root, tc.path))
		for _, required := range tc.required {
			if !strings.Contains(body, required) {
				t.Errorf("%s missing %q", tc.path, required)
			}
		}
	}
}
