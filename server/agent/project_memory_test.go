package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	projectmemory "github.com/anbanai/anban-creator/server/memory"
)

type testProjectMemoryStore struct {
	ensureCalls  []string
	requireCalls []string
	err          error
}

func (s *testProjectMemoryStore) EnsureProject(_ context.Context, projectID string) error {
	s.ensureCalls = append(s.ensureCalls, projectID)
	return s.err
}

func (s *testProjectMemoryStore) RequireProject(_ context.Context, projectID string) error {
	s.requireCalls = append(s.requireCalls, projectID)
	return s.err
}

func TestPrepareProjectMemoryCreatesOnlyInitialExecution(t *testing.T) {
	store := &testProjectMemoryStore{}
	if err := prepareProjectMemory(context.Background(), store, "project-1", false); err != nil {
		t.Fatal(err)
	}
	if err := prepareProjectMemory(context.Background(), store, "project-1", true); err != nil {
		t.Fatal(err)
	}
	if len(store.ensureCalls) != 1 || len(store.requireCalls) != 1 {
		t.Fatalf("ensure=%v require=%v", store.ensureCalls, store.requireCalls)
	}
}

func TestPrepareProjectMemoryPersistsAcrossExecutionsAndFailsClosedOnMissingResume(t *testing.T) {
	root := t.TempDir()
	store, err := projectmemory.NewFilesystemStore(root, projectmemory.Limits{
		MaxProjectBytes: 1024, MaxFiles: 8, MaxDepth: 4, MaxFileBytes: 512, MaxPreviewBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	projectID := uuid.NewString()
	if err := prepareProjectMemory(context.Background(), store, projectID, false); err != nil {
		t.Fatal(err)
	}
	memoryPath := filepath.Join(root, "projects", projectID, "MEMORY.md")
	if err := os.WriteFile(memoryPath, []byte("survives an interrupted task"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareProjectMemory(context.Background(), store, projectID, true); err != nil {
		t.Fatalf("resume existing project memory: %v", err)
	}
	view, err := store.ReadProject(context.Background(), projectID)
	if err != nil || len(view.Files) != 1 || view.Files[0].Content != "survives an interrupted task" {
		t.Fatalf("persisted view = %#v, err=%v", view, err)
	}
	if err := prepareProjectMemory(context.Background(), store, uuid.NewString(), true); err == nil || !IsPermanentDispatchError(err) {
		t.Fatalf("missing resume error = %v, want permanent storage error", err)
	}
}
