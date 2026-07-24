package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
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

type concurrentRenderedImageStorage struct {
	*fakeTaskStorage
	mu             sync.Mutex
	uploadCalls    int
	uploadedKeys   []string
	blockFromCall  int
	blockCallCount int
	arrived        chan struct{}
	release        chan struct{}
	deleteFailures int
	blockDeleteKey string
	deleteArrived  chan struct{}
	deleteRelease  chan struct{}
	deleteOnce     sync.Once
}

func (s *concurrentRenderedImageStorage) Upload(ctx context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	s.mu.Lock()
	s.uploadCalls++
	call := s.uploadCalls
	s.uploadedKeys = append(s.uploadedKeys, key)
	shouldBlock := s.blockFromCall > 0 && call >= s.blockFromCall && call < s.blockFromCall+s.blockCallCount
	s.mu.Unlock()
	if shouldBlock {
		s.arrived <- struct{}{}
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeTaskStorage.Upload(ctx, key, reader, contentType)
}

func (s *concurrentRenderedImageStorage) Delete(_ context.Context, key string) error {
	if key == s.blockDeleteKey && s.deleteArrived != nil && s.deleteRelease != nil {
		s.deleteOnce.Do(func() {
			s.deleteArrived <- struct{}{}
			<-s.deleteRelease
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedKeys = append(s.deletedKeys, key)
	if s.deleteFailures > 0 {
		s.deleteFailures--
		return errors.New("injected superseded object delete failure")
	}
	delete(s.files, key)
	return nil
}

func (s *concurrentRenderedImageStorage) snapshot() (map[string][]byte, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	files := make(map[string][]byte, len(s.files))
	for key, data := range s.files {
		files[key] = append([]byte(nil), data...)
	}
	return files, append([]string(nil), s.uploadedKeys...)
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
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
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
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
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
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
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
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
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
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
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
	if err := db.Callback().Update().Before("gorm:update").Register("fail_rendered_image_replacement", func(tx *gorm.DB) {
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
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
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
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
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

func TestRegisterRenderedImageConcurrentReplacementsDoNotLeakCASLoser(t *testing.T) {
	db := setupTaskTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &concurrentRenderedImageStorage{fakeTaskStorage: &fakeTaskStorage{name: "oss"}}
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	base := mustDecodeRenderedPNG(t)
	if _, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(base),
	}); err != nil {
		t.Fatal(err)
	}
	store.blockFromCall, store.blockCallCount = 2, 2
	store.arrived, store.release = make(chan struct{}, 2), make(chan struct{})

	errCh := make(chan error, 2)
	for _, suffix := range []byte{1, 2} {
		payload := append(append([]byte(nil), base...), suffix)
		go func() {
			_, registerErr := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
				UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
				ImageBase64: base64.StdEncoding.EncodeToString(payload),
			})
			errCh <- registerErr
		}()
	}
	<-store.arrived
	<-store.arrived
	close(store.release)
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent replacement: %v", err)
		}
	}
	current, err := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
	if err != nil || current == nil {
		t.Fatalf("find current replacement: %#v, %v", current, err)
	}
	files, uploaded := store.snapshot()
	for _, key := range uploaded[1:] {
		if key == current.OSSKey || key == current.CleanupOSSKey {
			continue
		}
		if _, leaked := files[key]; leaked {
			t.Fatalf("noncurrent uploaded object %q is neither deleted nor durably queued; current=%#v uploaded=%v", key, current, uploaded)
		}
	}
}

func TestUnpublishedRenderedImageCleanupCannotDeleteRepublishedContent(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &concurrentRenderedImageStorage{fakeTaskStorage: &fakeTaskStorage{name: "oss"}}
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	base := mustDecodeRenderedPNG(t)
	register := func(payload []byte) *model.TaskFile {
		t.Helper()
		_, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
			UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
			ImageBase64: base64.StdEncoding.EncodeToString(payload),
		})
		if err != nil {
			t.Fatal(err)
		}
		file, err := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
		if err != nil || file == nil {
			t.Fatalf("find current rendered image: %#v, %v", file, err)
		}
		return file
	}
	register(base)
	republishedPayload := append(append([]byte(nil), base...), 1)
	old := register(republishedPayload)
	current := register(append(append([]byte(nil), base...), 2))

	store.mu.Lock()
	store.files[old.OSSKey] = append([]byte(nil), republishedPayload...)
	store.mu.Unlock()
	store.blockDeleteKey = old.OSSKey
	store.deleteArrived = make(chan struct{}, 1)
	store.deleteRelease = make(chan struct{})
	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- svc.cleanupUnpublishedTaskFileObject(ctx, current, old.OSSKey) }()
	<-store.deleteArrived

	republished := register(republishedPayload)
	close(store.deleteRelease)
	if err := <-cleanupDone; err != nil {
		t.Fatalf("cleanup unpublished object: %v", err)
	}
	files, _ := store.snapshot()
	if _, ok := files[republished.OSSKey]; !ok {
		t.Fatalf("cleanup deleted republished current object %q (old key %q)", republished.OSSKey, old.OSSKey)
	}
}

func TestQueuedRenderedImageCleanupCannotDeleteRepublishedContent(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &concurrentRenderedImageStorage{fakeTaskStorage: &fakeTaskStorage{name: "oss"}}
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	base := mustDecodeRenderedPNG(t)
	register := func(payload []byte) *model.TaskFile {
		t.Helper()
		_, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
			UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
			ImageBase64: base64.StdEncoding.EncodeToString(payload),
		})
		if err != nil {
			t.Fatal(err)
		}
		file, err := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
		if err != nil || file == nil {
			t.Fatalf("find current rendered image: %#v, %v", file, err)
		}
		return file
	}
	register(base)
	republishedPayload := append(append([]byte(nil), base...), 3)
	old := register(republishedPayload)
	current := register(append(append([]byte(nil), base...), 4))
	store.mu.Lock()
	store.files[old.OSSKey] = append([]byte(nil), republishedPayload...)
	store.mu.Unlock()
	if err := repo.TaskFiles().QueueObjectCleanup(ctx, &model.TaskFileObjectCleanup{
		TaskFileID: current.ID, StorageProvider: store.Name(), OSSKey: old.OSSKey,
	}); err != nil {
		t.Fatal(err)
	}
	store.blockDeleteKey = old.OSSKey
	store.deleteArrived = make(chan struct{}, 1)
	store.deleteRelease = make(chan struct{})
	cleanupDone := make(chan error, 1)
	go func() {
		_, err := svc.CleanupSupersededTaskFileObjects(ctx, 10)
		cleanupDone <- err
	}()
	<-store.deleteArrived

	republished := register(republishedPayload)
	close(store.deleteRelease)
	if err := <-cleanupDone; err != nil {
		t.Fatalf("queued cleanup: %v", err)
	}
	files, _ := store.snapshot()
	if _, ok := files[republished.OSSKey]; !ok {
		t.Fatalf("queued cleanup deleted republished current object %q (old key %q)", republished.OSSKey, old.OSSKey)
	}
}

func TestTaskFileCleanupAttemptsQueuedObjectsWhenLegacyBatchIsFull(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &renderedImageLifecycleStorage{
		fakeTaskStorage: &fakeTaskStorage{name: "oss", files: map[string][]byte{}},
		deleteFailures:  2,
	}
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	ctx := context.Background()
	var owner *model.TaskFile
	for index := range 2 {
		file := &model.TaskFile{
			TaskID: uuid.NewString(), State: model.TaskFileStatePublished, Role: model.FileRoleImage,
			FilePath: fmt.Sprintf("legacy-%d.png", index), FileName: fmt.Sprintf("legacy-%d.png", index),
			OSSKey: fmt.Sprintf("current-%d", index), CleanupOSSKey: fmt.Sprintf("legacy-%d", index), StorageProvider: store.Name(),
		}
		if err := repo.TaskFiles().Create(ctx, file); err != nil {
			t.Fatal(err)
		}
		store.files[file.CleanupOSSKey] = []byte("legacy")
		if owner == nil {
			owner = file
		}
	}
	queuedKey := "queued-object"
	store.files[queuedKey] = []byte("queued")
	if err := repo.TaskFiles().QueueObjectCleanup(ctx, &model.TaskFileObjectCleanup{
		TaskFileID: owner.ID, StorageProvider: store.Name(), OSSKey: queuedKey,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := svc.CleanupSupersededTaskFileObjects(ctx, 2)
	if err == nil {
		t.Fatal("cleanup unexpectedly hid injected legacy delete failures")
	}
	if _, ok := store.files[queuedKey]; ok {
		t.Fatalf("queued object %q was starved by a full legacy cleanup batch", queuedKey)
	}
}

func TestRegisterRenderedImageRoleUpdateRacingReplacementPreservesCurrentIdentity(t *testing.T) {
	for _, replacementFirst := range []bool{false, true} {
		name := "role_first"
		if replacementFirst {
			name = "replacement_first"
		}
		t.Run(name, func(t *testing.T) {
			db := setupTaskTestDB(t)
			repo := repository.New(db)
			logger := zerolog.New(io.Discard)
			store := &concurrentRenderedImageStorage{fakeTaskStorage: &fakeTaskStorage{name: "oss"}, deleteFailures: 1}
			svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
			ctx := context.Background()
			userID := uuid.NewString()
			projectID := createTestProject(t, repo, userID, model.PlatformArticle)
			task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
			if err := repo.Tasks().Create(ctx, task); err != nil {
				t.Fatal(err)
			}
			base := mustDecodeRenderedPNG(t)
			if _, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
				UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
				ImageBase64: base64.StdEncoding.EncodeToString(base),
			}); err != nil {
				t.Fatal(err)
			}
			original, err := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
			if err != nil || original == nil {
				t.Fatalf("find original: %#v, %v", original, err)
			}
			replacement := append(append([]byte(nil), base...), 9)
			roleDone, replaceDone := make(chan error, 1), make(chan error, 1)

			if replacementFirst {
				type queryBarrierKey struct{}
				arrived, release := make(chan struct{}, 1), make(chan struct{})
				var blockOnce sync.Once
				if err := db.Callback().Query().After("gorm:query").Register("block_stale_role_after_read", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "task_files" && tx.Statement.Context.Value(queryBarrierKey{}) != nil {
						blockOnce.Do(func() {
							arrived <- struct{}{}
							<-release
						})
					}
				}); err != nil {
					t.Fatal(err)
				}
				roleCtx := context.WithValue(ctx, queryBarrierKey{}, true)
				go func() {
					_, roleErr := svc.RegisterRenderedImage(roleCtx, RegisterRenderedImageRequest{
						UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleOther,
						ImageBase64: base64.StdEncoding.EncodeToString(base),
					})
					roleDone <- roleErr
				}()
				<-arrived
				_, err = svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
					UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
					ImageBase64: base64.StdEncoding.EncodeToString(replacement),
				})
				if err != nil {
					t.Fatalf("replacement: %v", err)
				}
				close(release)
				if err := <-roleDone; err != nil {
					t.Fatalf("role update: %v", err)
				}
			} else {
				store.blockFromCall, store.blockCallCount = 2, 1
				store.arrived, store.release = make(chan struct{}, 1), make(chan struct{})
				go func() {
					_, replaceErr := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
						UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleCover,
						ImageBase64: base64.StdEncoding.EncodeToString(replacement),
					})
					replaceDone <- replaceErr
				}()
				<-store.arrived
				if _, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
					UserID: userID, ProjectID: projectID, TaskID: task.ID, Name: "cover.png", Role: model.FileRoleOther,
					ImageBase64: base64.StdEncoding.EncodeToString(base),
				}); err != nil {
					t.Fatalf("role update: %v", err)
				}
				close(store.release)
				if err := <-replaceDone; err != nil {
					t.Fatalf("replacement: %v", err)
				}
			}
			current, err := repo.TaskFiles().FindExisting(ctx, task.ID, "cover.png")
			if err != nil || current == nil {
				t.Fatalf("find current: %#v, %v", current, err)
			}
			if current.OSSKey == original.OSSKey || current.ContentHash == original.ContentHash {
				t.Fatalf("stale role update restored original identity: current=%#v original=%#v", current, original)
			}
			if current.CleanupOSSKey != original.OSSKey {
				t.Fatalf("cleanup intent = %q, want original key %q", current.CleanupOSSKey, original.OSSKey)
			}
		})
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
