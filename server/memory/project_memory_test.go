package memory

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/storage"
)

func TestProjectMemoryStageInitializesDefaultWhenArchiveMissing(t *testing.T) {
	store := newMemoryStore()
	dir := t.TempDir()
	mgr := NewProjectMemoryManager(store, config.MemoryConfig{
		Enabled:         true,
		OSSPrefix:       "claude-memory/projects",
		RuntimeDir:      ".claude/memory",
		MaxArchiveBytes: 262144,
	}, nil, zerolog.Nop())

	runtimeDir, err := mgr.Stage(context.Background(), "project-1", "task-1", dir)
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if runtimeDir != filepath.Join(dir, ".claude", "memory") {
		t.Fatalf("runtimeDir = %q", runtimeDir)
	}
	data, err := os.ReadFile(filepath.Join(runtimeDir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if !strings.Contains(string(data), "Project Memory") {
		t.Fatalf("default MEMORY.md = %q, want Project Memory heading", data)
	}
}

func TestProjectMemoryStageExtractsArchiveAndRejectsTraversal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries map[string]string
		wantErr string
	}{
		{
			name:    "safe archive",
			entries: map[string]string{"MEMORY.md": "# Existing\n", "topics/project-lessons.md": "- keep it short\n"},
		},
		{
			name:    "path traversal",
			entries: map[string]string{"../secret": "oops"},
			wantErr: "unsafe archive path",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryStore()
			store.objects["claude-memory/projects/project-1/current.tar.gz"] = mustTarGz(t, tc.entries)
			dir := t.TempDir()
			mgr := NewProjectMemoryManager(store, config.MemoryConfig{
				Enabled:         true,
				OSSPrefix:       "claude-memory/projects",
				RuntimeDir:      ".claude/memory",
				MaxArchiveBytes: 262144,
			}, nil, zerolog.Nop())

			runtimeDir, err := mgr.Stage(context.Background(), "project-1", "task-1", dir)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Stage() error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Stage() error = %v", err)
			}
			data, err := os.ReadFile(filepath.Join(runtimeDir, "topics", "project-lessons.md"))
			if err != nil {
				t.Fatalf("read topic: %v", err)
			}
			if !strings.Contains(string(data), "keep it short") {
				t.Fatalf("topic content = %q", data)
			}
		})
	}
}

func TestProjectMemoryStageRejectsOversizedArchive(t *testing.T) {
	store := newMemoryStore()
	store.objects["claude-memory/projects/project-1/current.tar.gz"] = mustTarGz(t, map[string]string{
		"MEMORY.md": strings.Repeat("x", 200),
	})
	dir := t.TempDir()
	mgr := NewProjectMemoryManager(store, config.MemoryConfig{
		Enabled:         true,
		OSSPrefix:       "claude-memory/projects",
		RuntimeDir:      ".claude/memory",
		MaxArchiveBytes: 10,
	}, nil, zerolog.Nop())

	if _, err := mgr.Stage(context.Background(), "project-1", "task-1", dir); err == nil {
		t.Fatal("Stage() error = nil, want oversized archive error")
	}
}

func TestProjectMemoryStageRejectsOversizedExtractedContent(t *testing.T) {
	store := newMemoryStore()
	store.objects["claude-memory/projects/project-1/current.tar.gz"] = mustTarGz(t, map[string]string{
		"MEMORY.md": strings.Repeat("x", 4096),
	})
	dir := t.TempDir()
	mgr := NewProjectMemoryManager(store, config.MemoryConfig{
		Enabled:         true,
		OSSPrefix:       "claude-memory/projects",
		RuntimeDir:      ".claude/memory",
		MaxArchiveBytes: 1024,
	}, nil, zerolog.Nop())

	_, err := mgr.Stage(context.Background(), "project-1", "task-1", dir)
	if err == nil || !strings.Contains(err.Error(), "extracted bytes exceed") {
		t.Fatalf("Stage() error = %v, want extracted bytes limit error", err)
	}
}

func TestProjectMemoryMergeWritesCurrentManifestAndVersion(t *testing.T) {
	store := newMemoryStore()
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, ".claude", "memory")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "MEMORY.md"), []byte("# Updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locker := &fakeMemoryLocker{locked: true}
	mgr := NewProjectMemoryManager(store, config.MemoryConfig{
		Enabled:         true,
		OSSPrefix:       "claude-memory/projects",
		RuntimeDir:      ".claude/memory",
		MaxArchiveBytes: 262144,
		LockTTL:         time.Minute,
	}, locker, zerolog.Nop())

	merged, err := mgr.Merge(context.Background(), "project-1", "task-1", dir)
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	if !merged {
		t.Fatal("Merge() merged = false, want true")
	}
	if _, ok := store.objects["claude-memory/projects/project-1/current.tar.gz"]; !ok {
		t.Fatal("current archive not uploaded")
	}
	if got := string(store.objects["claude-memory/projects/project-1/manifest.json"]); !strings.Contains(got, `"project_id":"project-1"`) {
		t.Fatalf("manifest = %s", got)
	}
	foundVersion := false
	for key := range store.objects {
		if strings.HasPrefix(key, "claude-memory/projects/project-1/versions/") && strings.HasSuffix(key, "-task-1.tar.gz") {
			foundVersion = true
		}
	}
	if !foundVersion {
		t.Fatal("version archive was not uploaded")
	}
	if !locker.released {
		t.Fatal("lock was not released")
	}
}

func TestProjectMemoryMergeSkipsOversizedFiles(t *testing.T) {
	store := newMemoryStore()
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, ".claude", "memory")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "MEMORY.md"), []byte("# Keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "large.log"), []byte(strings.Repeat("x", 2048)), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewProjectMemoryManager(store, config.MemoryConfig{
		Enabled:         true,
		OSSPrefix:       "claude-memory/projects",
		RuntimeDir:      ".claude/memory",
		MaxArchiveBytes: 1024,
		LockTTL:         time.Minute,
	}, nil, zerolog.Nop())

	merged, err := mgr.Merge(context.Background(), "project-1", "task-1", dir)
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	if !merged {
		t.Fatal("Merge() merged = false, want true")
	}
	names := tarGzNames(t, store.objects["claude-memory/projects/project-1/current.tar.gz"])
	if !containsName(names, "MEMORY.md") {
		t.Fatalf("archive names = %v, want MEMORY.md", names)
	}
	if containsName(names, "large.log") {
		t.Fatalf("archive names = %v, want large.log skipped", names)
	}
}

func TestProjectMemoryMergeSkipsWhenLockUnavailable(t *testing.T) {
	store := newMemoryStore()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	locker := &fakeMemoryLocker{locked: false}
	mgr := NewProjectMemoryManager(store, config.MemoryConfig{
		Enabled:         true,
		OSSPrefix:       "claude-memory/projects",
		RuntimeDir:      ".claude/memory",
		MaxArchiveBytes: 262144,
		LockTTL:         time.Minute,
	}, locker, zerolog.Nop())

	merged, err := mgr.Merge(context.Background(), "project-1", "task-1", dir)
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	if merged {
		t.Fatal("Merge() merged = true, want false")
	}
	if len(store.objects) != 0 {
		t.Fatalf("store objects = %v, want none", store.objects)
	}
}

type memoryStore struct {
	objects map[string][]byte
}

func newMemoryStore() *memoryStore {
	return &memoryStore{objects: map[string][]byte{}}
}

func (m *memoryStore) Name() string { return "oss" }

func (m *memoryStore) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	m.objects[key] = data
	return &storage.UploadResult{Key: key, URL: "mem://" + key, Size: int64(len(data)), MimeType: contentType}, nil
}

func (m *memoryStore) UploadFile(ctx context.Context, key, filePath, contentType string) (*storage.UploadResult, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return m.Upload(ctx, key, f, contentType)
}

func (m *memoryStore) UploadURL(context.Context, string, string, int) (string, error) { return "", nil }
func (m *memoryStore) GetURL(key string) string                                       { return "mem://" + key }
func (m *memoryStore) Read(_ context.Context, key string) ([]byte, error) {
	data, ok := m.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}
func (m *memoryStore) Delete(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}
func (m *memoryStore) DownloadURL(context.Context, string, int) (string, error) { return "", nil }
func (m *memoryStore) HasCustomDomain() bool                                    { return false }
func (m *memoryStore) IsOwnedURL(string) bool                                   { return false }

type fakeMemoryLocker struct {
	locked   bool
	released bool
}

func (f *fakeMemoryLocker) TryLock(context.Context, string, time.Duration) (func(), bool, error) {
	if !f.locked {
		return nil, false, nil
	}
	return func() { f.released = true }, true, nil
}

func mustTarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		data := []byte(body)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func tarGzNames(t *testing.T, data []byte) []string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
