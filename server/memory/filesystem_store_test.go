package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func testLimits() Limits {
	return Limits{
		MaxProjectBytes: 32,
		MaxFiles:        3,
		MaxDepth:        2,
		MaxFileBytes:    8,
		MaxPreviewBytes: 16,
	}
}

func TestFilesystemStoreEnsuresAndRequiresCanonicalProjectDirectory(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	projectID := uuid.NewString()

	if err := store.RequireProject(context.Background(), projectID); !errors.Is(err, ErrProjectMemoryMissing) {
		t.Fatalf("RequireProject() error = %v, want ErrProjectMemoryMissing", err)
	}
	if err := store.EnsureProject(context.Background(), projectID); err != nil {
		t.Fatalf("EnsureProject() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "projects", projectID))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("project memory mode = %v, want directory 0700", info.Mode())
	}
	if err := store.RequireProject(context.Background(), projectID); err != nil {
		t.Fatalf("RequireProject() after ensure = %v", err)
	}
	if err := store.EnsureProject(context.Background(), "../other"); !errors.Is(err, ErrInvalidProjectID) {
		t.Fatalf("unsafe project ID error = %v, want ErrInvalidProjectID", err)
	}
}

func TestFilesystemStoreRejectsProjectOverQuota(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	projectID := uuid.NewString()
	if err := store.EnsureProject(context.Background(), projectID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", projectID, "large.md"), []byte(strings.Repeat("x", 33)), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.RequireProject(context.Background(), projectID); !errors.Is(err, ErrProjectMemoryQuotaExceeded) {
		t.Fatalf("RequireProject() error = %v, want ErrProjectMemoryQuotaExceeded", err)
	}
}

func TestFilesystemStoreReadsBoundedMarkdownInStableOrder(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	projectID := uuid.NewString()
	projectDir := filepath.Join(root, "projects", projectID)
	if err := os.MkdirAll(filepath.Join(projectDir, "topics"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"z.md":              "z",
		"MEMORY.md":         "memory-index",
		"topics/detail.md":  "detail",
		"ignored.txt":       "secret",
		"topics/invalid.md": string([]byte{0xff, 0xfe}),
	} {
		path := filepath.Join(projectDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "outside.md"), filepath.Join(projectDir, "linked.md")); err != nil {
		t.Fatal(err)
	}

	view, err := store.ReadProject(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != StatusReady || len(view.Files) != 3 {
		t.Fatalf("view = %#v, want ready with three Markdown files", view)
	}
	if got := []string{view.Files[0].Path, view.Files[1].Path, view.Files[2].Path}; strings.Join(got, ",") != "MEMORY.md,topics/detail.md,z.md" {
		t.Fatalf("paths = %v", got)
	}
	if view.Files[0].Content != "memory-i" || !view.Files[0].Truncated || !view.Partial {
		t.Fatalf("bounded MEMORY.md = %#v, partial=%v", view.Files[0], view.Partial)
	}
}

func TestFilesystemStoreReturnsEmptyAndDeletesOnlyRequestedProject(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	first, second := uuid.NewString(), uuid.NewString()
	if err := store.EnsureProject(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureProject(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	view, err := store.ReadProject(context.Background(), first)
	if err != nil || view.Status != StatusEmpty || len(view.Files) != 0 {
		t.Fatalf("empty view = %#v, err=%v", view, err)
	}

	if err := store.DeleteProject(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", first)); !os.IsNotExist(err) {
		t.Fatalf("deleted project stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", second)); err != nil {
		t.Fatalf("other project was affected: %v", err)
	}
	if err := store.DeleteProject(context.Background(), first); err != nil {
		t.Fatalf("idempotent delete = %v", err)
	}
}

func TestFilesystemStoreDoesNotTurnTruncatedInvalidUTF8IntoContent(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	projectID := uuid.NewString()
	if err := store.EnsureProject(context.Background(), projectID); err != nil {
		t.Fatal(err)
	}
	invalid := append([]byte{0xff}, []byte(strings.Repeat("x", 20))...)
	if err := os.WriteFile(filepath.Join(root, "projects", projectID, "invalid.md"), invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := store.ReadProject(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != StatusEmpty || len(view.Files) != 0 || !view.Partial {
		t.Fatalf("invalid UTF-8 view = %#v", view)
	}
}

func TestFilesystemStoreRejectsSymlinkedProjectsRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "projects")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesystemStore(root, testLimits()); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("NewFilesystemStore() error = %v, want symlink rejection", err)
	}
}
