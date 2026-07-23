package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

type renderedImageLifecycleStorage struct {
	*fakeTaskStorage
	uploadCalls    int
	deleteFailures int
}

func (s *renderedImageLifecycleStorage) Upload(ctx context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	s.uploadCalls++
	return s.fakeTaskStorage.Upload(ctx, key, reader, contentType)
}

func (s *renderedImageLifecycleStorage) Delete(_ context.Context, key string) error {
	s.deletedKeys = append(s.deletedKeys, key)
	if s.deleteFailures > 0 {
		s.deleteFailures--
		return errors.New("injected superseded object delete failure")
	}
	delete(s.files, key)
	return nil
}

func TestTaskServiceRegisterRenderedImageOwnsValidationAndRegistration(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &fakeTaskStorage{name: "oss"}
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID,
		Type: model.PlatformArticle, Status: model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID,
		Name: "cover.png", Role: model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(png),
	})
	if err != nil {
		t.Fatalf("RegisterRenderedImage: %v", err)
	}
	if result.TaskFileID == "" || result.Name != "cover.png" || result.Role != model.FileRoleCover || result.MimeType != "image/png" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: uuid.NewString(), ProjectID: projectID, TaskID: task.ID,
		Name: "foreign.png", ImageBase64: base64.StdEncoding.EncodeToString(png),
	}); err == nil {
		t.Fatal("RegisterRenderedImage accepted a foreign user")
	}
}

func TestRegisterRenderedImagePersistsRequestedRoleInInitialWrite(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &fakeTaskStorage{name: "oss"}
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	writes := 0
	roles := []string{}
	if err := db.Callback().Create().Before("gorm:create").Register("observe_rendered_image_role", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "task_files" {
			return
		}
		writes++
		if file, ok := tx.Statement.Dest.(*model.TaskFile); ok {
			roles = append(roles, file.Role)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleOther,
		ImageBase64: base64.StdEncoding.EncodeToString(mustDecodeRenderedPNG(t)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if writes != 1 || len(roles) != 1 || roles[0] != model.FileRoleOther {
		t.Fatalf("task-file writes=%d roles=%v, want one initial write with requested role", writes, roles)
	}
}

func TestRegisterRenderedImageUpdatesRoleWithoutReuploadingIdenticalContent(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &renderedImageLifecycleStorage{fakeTaskStorage: &fakeTaskStorage{name: "oss"}}
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	payload := base64.StdEncoding.EncodeToString(mustDecodeRenderedPNG(t))
	if _, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover, ImageBase64: payload,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleOther, ImageBase64: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Role != model.FileRoleOther {
		t.Fatalf("result role = %q, want %q", result.Role, model.FileRoleOther)
	}
	persisted, err := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
	if err != nil || persisted == nil {
		t.Fatalf("find persisted rendered image: %#v, %v", persisted, err)
	}
	if persisted.Role != model.FileRoleOther {
		t.Fatalf("persisted role = %q, want %q", persisted.Role, model.FileRoleOther)
	}
	if store.uploadCalls != 1 {
		t.Fatalf("upload calls = %d, want 1 for role-only replacement", store.uploadCalls)
	}
}

func TestRegisterRenderedImageDeletesUploadedObjectWhenPersistenceFails(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &fakeTaskStorage{name: "oss"}
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	persistFailure := errors.New("injected task-file persistence failure")
	if err := db.Callback().Create().Before("gorm:create").Register("fail_rendered_image_persist", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "task_files" {
			tx.AddError(persistFailure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "image.png", Role: model.FileRoleImage,
		ImageBase64: base64.StdEncoding.EncodeToString(mustDecodeRenderedPNG(t)),
	})
	if !errors.Is(err, persistFailure) {
		t.Fatalf("RegisterRenderedImage error = %v, want injected persistence failure", err)
	}
	if len(store.deletedKeys) != 1 || len(store.files) != 0 {
		t.Fatalf("deleted=%v remaining=%v, want uploaded object cleanup", store.deletedKeys, store.files)
	}
}

func TestRegisterRenderedImageFailedReplacementPreservesExistingObject(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &fakeTaskStorage{name: "oss"}
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	originalBytes := mustDecodeRenderedPNG(t)
	if _, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(originalBytes),
	}); err != nil {
		t.Fatalf("initial RegisterRenderedImage: %v", err)
	}
	original, err := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
	if err != nil || original == nil {
		t.Fatalf("find original task file: %#v, %v", original, err)
	}

	persistFailure := errors.New("injected replacement persistence failure")
	if err := db.Callback().Create().Before("gorm:create").Register("fail_rendered_image_replacement", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "task_files" {
			tx.AddError(persistFailure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	replacementBytes := append(append([]byte(nil), originalBytes...), 0)
	_, err = svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(replacementBytes),
	})
	if !errors.Is(err, persistFailure) {
		t.Fatalf("replacement error = %v, want injected persistence failure", err)
	}
	current, findErr := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
	if findErr != nil || current == nil || current.ID != original.ID || current.OSSKey != original.OSSKey {
		t.Fatalf("current task file = %#v, %v; want original %#v", current, findErr, original)
	}
	stored, ok := store.files[original.OSSKey]
	if !ok || !bytes.Equal(stored, originalBytes) {
		t.Fatalf("existing object %q was changed or deleted after failed replacement", original.OSSKey)
	}
}

func TestRegisterRenderedImageSuccessfulReplacementDeletesSupersededObject(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &fakeTaskStorage{name: "oss"}
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	originalBytes := mustDecodeRenderedPNG(t)
	if _, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(originalBytes),
	}); err != nil {
		t.Fatal(err)
	}
	original, err := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
	if err != nil || original == nil {
		t.Fatalf("find original: %#v, %v", original, err)
	}
	replacementBytes := append(append([]byte(nil), originalBytes...), 0)
	if _, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(replacementBytes),
	}); err != nil {
		t.Fatalf("replace rendered image: %v", err)
	}
	current, err := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
	if err != nil || current == nil || current.OSSKey == original.OSSKey {
		t.Fatalf("current task file = %#v, %v; want new object key", current, err)
	}
	if _, ok := store.files[original.OSSKey]; ok {
		t.Fatalf("superseded object %q remains after successful replacement", original.OSSKey)
	}
	if stored, ok := store.files[current.OSSKey]; !ok || !bytes.Equal(stored, replacementBytes) {
		t.Fatalf("current object %q is missing or incorrect", current.OSSKey)
	}
}

func TestRegisterRenderedImageDeleteFailureLeavesRecoverableCleanupIntent(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &renderedImageLifecycleStorage{
		fakeTaskStorage: &fakeTaskStorage{name: "oss"},
		deleteFailures:  1,
	}
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	originalBytes := mustDecodeRenderedPNG(t)
	if _, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(originalBytes),
	}); err != nil {
		t.Fatal(err)
	}
	original, err := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
	if err != nil || original == nil {
		t.Fatalf("find original: %#v, %v", original, err)
	}
	replacementBytes := append(append([]byte(nil), originalBytes...), 0)
	if _, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(replacementBytes),
	}); err != nil {
		t.Fatalf("replacement registration failed with cleanup error: %v", err)
	}
	current, err := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
	if err != nil || current == nil || current.OSSKey == original.OSSKey {
		t.Fatalf("current registration = %#v, %v; want replacement", current, err)
	}
	if _, ok := store.files[original.OSSKey]; !ok {
		t.Fatalf("failed delete unexpectedly removed superseded object %q", original.OSSKey)
	}
	var cleanupKey string
	if err := db.Model(&model.TaskFile{}).Select("cleanup_oss_key").Where("id = ?", current.ID).Scan(&cleanupKey).Error; err != nil {
		t.Fatalf("load durable cleanup intent: %v", err)
	}
	if cleanupKey != original.OSSKey {
		t.Fatalf("cleanup intent = %q, want %q", cleanupKey, original.OSSKey)
	}
	runner, ok := any(svc).(interface {
		CleanupSupersededTaskFileObjects(context.Context, int) (int, error)
	})
	if !ok {
		t.Fatal("TaskService does not expose superseded task-file cleanup recovery")
	}
	cleaned, err := runner.CleanupSupersededTaskFileObjects(ctx, 10)
	if err != nil || cleaned != 1 {
		t.Fatalf("cleanup retry = %d, %v; want 1, nil", cleaned, err)
	}
	if _, ok := store.files[original.OSSKey]; ok {
		t.Fatalf("superseded object %q remains after cleanup retry", original.OSSKey)
	}
	cleanupKey = "not-cleared"
	if err := db.Model(&model.TaskFile{}).Select("cleanup_oss_key").Where("id = ?", current.ID).Scan(&cleanupKey).Error; err != nil {
		t.Fatalf("reload cleanup intent: %v", err)
	}
	if cleanupKey != "" {
		t.Fatalf("cleanup intent remains after successful retry: %q", cleanupKey)
	}
}

func mustDecodeRenderedPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRenderedTaskImageInputValidation(t *testing.T) {
	textPayload := base64.StdEncoding.EncodeToString([]byte("this is text, not an image"))
	data, mimeType, err := loadRenderedTaskImage(RegisterRenderedImageRequest{ImageBase64: textPayload})
	if err != nil {
		t.Fatalf("load text payload: %v", err)
	}
	if err := validateRenderedTaskImage("not-image.png", mimeType); err == nil || !strings.Contains(err.Error(), "unsupported image MIME") {
		t.Fatalf("invalid MIME error = %v, data size = %d", err, len(data))
	}

	tooLarge := make([]byte, maxRenderedTaskImageBytes+1)
	if _, _, err := loadRenderedTaskImage(RegisterRenderedImageRequest{
		ImageBase64: base64.StdEncoding.EncodeToString(tooLarge),
	}); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversize error = %v", err)
	}
	if _, _, err := loadRenderedTaskImage(RegisterRenderedImageRequest{FilePath: "../secret.png"}); err == nil || !strings.Contains(err.Error(), "absolute server-local path") {
		t.Fatalf("unsafe path error = %v", err)
	}
}
