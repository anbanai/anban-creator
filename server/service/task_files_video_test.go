package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestUploadMissingTaskFilesVideoGenerationUsesDeliveryAllowlist(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	store := &fakeAudioASRStorage{name: "oss"}
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, nil, &logger, "", nil, "", nil, nil)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformVideoCreator)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformVideoCreator,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	workDir := t.TempDir()
	writeWorkspaceFile(t, workDir, "output/script.md", "script")
	writeWorkspaceFile(t, workDir, "output/generation-plan.json", `{"ok":true}`)
	writeWorkspaceFile(t, workDir, "output/final.mp4", "fake-video")
	writeWorkspaceFile(t, workDir, "output/package.json", `{"private":true}`)
	writeWorkspaceFile(t, workDir, "output/src/index.ts", "export {}")
	writeWorkspaceFile(t, workDir, "output/node_modules/pkg/index.js", "module.exports = {}")
	writeWorkspaceFile(t, workDir, "output/.git/HEAD", "ref: refs/heads/main")

	if err := svc.uploadMissingTaskFiles(ctx, task.ID, userID, workDir); err != nil {
		t.Fatalf("uploadMissingTaskFiles: %v", err)
	}
	files, err := repo.TaskFiles().FindByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find files: %v", err)
	}
	var paths []string
	for _, file := range files {
		paths = append(paths, file.FilePath)
	}
	sort.Strings(paths)
	want := []string{"output/final.mp4", "output/generation-plan.json", "output/script.md"}
	if len(paths) != len(want) {
		t.Fatalf("uploaded paths = %#v, want %#v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("uploaded paths = %#v, want %#v", paths, want)
		}
	}
}

func TestShouldCollectTaskFileVideoEditingRestrictsVideoDeliverables(t *testing.T) {
	task := &model.Task{Type: model.PlatformVideoEditor}

	tests := []struct {
		path string
		want bool
	}{
		{path: "output/final.mp4", want: true},
		{path: "output/preview.mp4", want: true},
		{path: "output/edit/edl.json", want: true},
		{path: "output/clips/intermediate.mp4", want: false},
		{path: "output/render/raw.webm", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := ShouldCollectTaskFile(task, tt.path); got != tt.want {
				t.Fatalf("ShouldCollectTaskFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestUploadMissingTaskFilesRefreshesExistingPathWhenContentChanges(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	store := &fakeAudioASRStorage{name: "oss"}
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, nil, &logger, "", nil, "", nil, nil)
	ctx := context.Background()
	userID := uuid.New().String()
	task := &model.Task{
		ID:     uuid.New().String(),
		UserID: userID,
		Type:   model.PlatformVideoCreator,
		Status: model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	oldHash := sha256.Sum256([]byte("old script"))
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID:              uuid.New().String(),
		TaskID:          task.ID,
		Role:            model.FileRoleMarkdown,
		FileName:        "script.md",
		FilePath:        "output/script.md",
		MimeType:        "text/markdown",
		FileSize:        int64(len("old script")),
		ContentHash:     hex.EncodeToString(oldHash[:]),
		OSSKey:          buildTaskStorageKey(userID, task.ID, "output/script.md"),
		StorageProvider: "oss",
	}); err != nil {
		t.Fatalf("create existing task file: %v", err)
	}

	workDir := t.TempDir()
	writeWorkspaceFile(t, workDir, "output/script.md", "new script")
	if err := svc.uploadMissingTaskFiles(ctx, task.ID, userID, workDir); err != nil {
		t.Fatalf("uploadMissingTaskFiles: %v", err)
	}

	files, err := repo.TaskFiles().FindByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("file count = %d, want 1", len(files))
	}
	newHash := sha256.Sum256([]byte("new script"))
	if files[0].ContentHash != hex.EncodeToString(newHash[:]) {
		t.Fatalf("content hash = %q, want refreshed hash", files[0].ContentHash)
	}
	if files[0].FileSize != int64(len("new script")) {
		t.Fatalf("file size = %d, want %d", files[0].FileSize, len("new script"))
	}
	if got := string(store.files[buildTaskStorageKey(userID, task.ID, "output/script.md")]); got != "new script" {
		t.Fatalf("stored content = %q, want new script", got)
	}
}

func writeWorkspaceFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}
