package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	serverbilling "github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

type taskImageResolverFake struct {
	resolved       *ResolvedImageModel
	calls          int
	userID         string
	imageModelKey  string
	imageType      string
	referenceCount int
}

func (f *taskImageResolverFake) ResolveImageModelForGeneration(_ context.Context, userID, imageModelKey, imageType string, referenceCount int) (*ResolvedImageModel, error) {
	f.calls++
	f.userID, f.imageModelKey, f.imageType, f.referenceCount = userID, imageModelKey, imageType, referenceCount
	return f.resolved, nil
}

type taskImageGeneratorFake struct {
	result       *ImageResult
	calls        int
	analyzeCalls int
	uploadCalls  int
	prompts      []string
}

type failingTaskImageDeleteStorage struct {
	storage.Provider
	err error
}

func (s *failingTaskImageDeleteStorage) Delete(context.Context, string) error { return s.err }

func (f *taskImageGeneratorFake) GenerateImage(_ context.Context, _, _, prompt, _, outputPath, _ string, _ []string, _, _ string, _ *ResolvedImageModel, _ *bool) (*ImageResult, error) {
	f.calls++
	f.prompts = append(f.prompts, prompt)
	copy := *f.result
	copy.FilePath = outputPath
	return &copy, nil
}

func (f *taskImageGeneratorFake) AnalyzeImage() { f.analyzeCalls++ }
func (f *taskImageGeneratorFake) UploadImage()  { f.uploadCalls++ }

type taskImageFixture struct {
	service     *TaskImageService
	db          *gorm.DB
	repo        repository.Repository
	resolver    *taskImageResolverFake
	generator   *taskImageGeneratorFake
	userID      string
	projectID   string
	taskID      string
	executionID string
}

func newTaskImageFixture(t *testing.T) *taskImageFixture {
	t.Helper()
	db := setupTaskTestDB(t)
	db.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	userID, projectID, taskID, executionID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	ctx := context.Background()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: uuid.NewString()[:8]}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
		Status: model.TaskStatusRunning, ImageModelKey: "preferred-image",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1, Target: "kubernetes",
		Status: model.TaskExecutionRunning, Started: true,
	}); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !won {
		t.Fatalf("set current execution: won=%v err=%v", won, err)
	}

	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := NewTaskService(repo, nil, store, &logger, "", nil, nil)
	bundle := &serverbilling.Bundle{Products: serverbilling.ProductCatalog{
		CatalogID: "retail-task-image-v1", Currency: "credits",
		SKUs: []serverbilling.SKUConfig{
			{ID: "image.cover.v1", Operation: "mcp.generate_image", Route: "image_generation.cover", ChargePolicy: "accepted_task_operation", PriceCredits: 500, Delivery: "persisted_image"},
			{ID: "image.content.v1", Operation: "mcp.generate_image", Route: "image_generation.content", ChargePolicy: "accepted_task_operation", PriceCredits: 500, Delivery: "persisted_image"},
		},
	}, Policy: serverbilling.PolicyCatalog{AcceptedTask: serverbilling.AcceptedTaskPolicy{
		ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true,
	}}}
	catalog := NewBillingCatalogService(repo, bundle, BillingCatalogOptions{})
	if _, err := catalog.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	taskSvc.SetBillingWalletService(NewBillingWalletService(repo, bundle, BillingWalletOptions{}))

	generatedPath := filepath.Join(t.TempDir(), "generated.png")
	if err := os.WriteFile(generatedPath, taskImageTinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := &taskImageResolverFake{resolved: &ResolvedImageModel{
		Provider: "openai", Model: "gpt-image-2", SelectionReason: "preferred",
		SupportsReference: true, MaxReferenceImages: 16,
	}}
	generator := &taskImageGeneratorFake{result: &ImageResult{
		LocalFilePath: generatedPath, OutputMIME: "image/png", Size: "3:4",
		Provider: "secret-provider", Model: "secret-model", RevisedPrompt: "secret prompt",
	}}
	return &taskImageFixture{
		service: NewTaskImageService(taskSvc, resolver, generator, catalog, &logger), db: db,
		repo: repo, resolver: resolver, generator: generator,
		userID: userID, projectID: projectID, taskID: taskID, executionID: executionID,
	}
}

func (f *taskImageFixture) request() GenerateTaskImageRequest {
	return GenerateTaskImageRequest{
		UserID: f.userID, ExecutionID: f.executionID, TaskID: f.taskID, ProjectID: f.projectID,
		Prompt: "draw a tea cover", ImageType: "cover", OutputPath: "output/cover.png", Size: "3:4",
	}
}

func TestGenerateTaskImagePersistsAndSettlesAtomically(t *testing.T) {
	f := newTaskImageFixture(t)
	asset, err := f.service.Generate(context.Background(), f.request())
	if err != nil {
		t.Fatal(err)
	}
	if asset.TaskFileID == "" || asset.FilePath != "output/cover.png" || asset.DownloadURL == "" ||
		asset.MimeType != "image/png" || asset.FileSize != int64(len(taskImageTinyPNG())) || asset.ContentHash != hashTaskFileContent(taskImageTinyPNG()) {
		t.Fatalf("asset = %#v", asset)
	}
	var files, settlements int64
	if err := f.db.Model(&model.TaskFile{}).Count(&files).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.BillingSettlementOutbox{}).Count(&settlements).Error; err != nil {
		t.Fatal(err)
	}
	if files != 1 || settlements != 1 {
		t.Fatalf("task files=%d settlements=%d, want 1/1", files, settlements)
	}
}

func TestGenerateTaskImageReplaysIdenticalSemanticRequest(t *testing.T) {
	f := newTaskImageFixture(t)
	first, err := f.service.Generate(context.Background(), f.request())
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Generate(context.Background(), f.request())
	if err != nil {
		t.Fatal(err)
	}
	if *first != *second || f.generator.calls != 1 {
		t.Fatalf("first=%#v second=%#v generator calls=%d", first, second, f.generator.calls)
	}
	var settlements int64
	if err := f.db.Model(&model.BillingSettlementOutbox{}).Count(&settlements).Error; err != nil || settlements != 1 {
		t.Fatalf("settlements=%d err=%v", settlements, err)
	}
}

func TestGenerateTaskImageChangedPromptCreatesNewOperation(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	originalBytes := append([]byte(nil), taskImageTinyPNG()...)
	original := f.request()
	first, err := f.service.Generate(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.generator.result.LocalFilePath, []byte("second-image"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := f.request()
	changed.Prompt = "draw a different tea cover"
	second, err := f.service.Generate(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := f.service.Generate(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if f.generator.calls != 2 {
		t.Fatalf("generator calls=%d, want 2", f.generator.calls)
	}
	if first.DownloadURL == second.DownloadURL {
		t.Fatalf("distinct paid operations share mutable delivery URL %q", first.DownloadURL)
	}
	if replayed.DownloadURL != first.DownloadURL {
		t.Fatalf("replayed download URL=%q, want original %q", replayed.DownloadURL, first.DownloadURL)
	}
	operationID, fingerprint, err := taskImageOperationIdentity(original)
	if err != nil {
		t.Fatal(err)
	}
	replayFile, _, err := f.service.tasks.FindExecutionTaskFileSettlement(
		ctx, f.taskID, f.executionID, operationID, fingerprint, "image", taskImageSettlementScope,
	)
	if err != nil {
		t.Fatal(err)
	}
	replayBytes, err := f.service.tasks.Storage().Read(ctx, replayFile.OSSKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(replayBytes) != string(originalBytes) {
		t.Fatalf("replayed bytes=%q, want original image bytes", replayBytes)
	}
	var settlements int64
	if err := f.db.Model(&model.BillingSettlementOutbox{}).Count(&settlements).Error; err != nil || settlements != 2 {
		t.Fatalf("settlements=%d err=%v", settlements, err)
	}
}

func TestTaskDeleteRemovesEveryImmutableImageOperationObject(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	if _, err := f.service.Generate(ctx, f.request()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.generator.result.LocalFilePath, []byte("second-image"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := f.request()
	changed.Prompt = "draw a different tea cover"
	if _, err := f.service.Generate(ctx, changed); err != nil {
		t.Fatal(err)
	}

	var settlements []model.BillingSettlementOutbox
	if err := f.db.Where("task_id = ?", f.taskID).Order("created_at ASC").Find(&settlements).Error; err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(settlements))
	for _, settlement := range settlements {
		var snapshot struct {
			OperationObject taskFileOperationObjectSnapshot `json:"operation_object"`
		}
		if err := json.Unmarshal(settlement.ResultSnapshot, &snapshot); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, snapshot.OperationObject.OSSKey)
	}
	if len(keys) != 2 || keys[0] == "" || keys[1] == "" || keys[0] == keys[1] {
		t.Fatalf("operation object keys = %v, want two unique keys", keys)
	}
	collectedKey := "uploads/users/" + f.userID + "/tasks/" + f.taskID + "/collected.md"
	cleanupKey := "uploads/users/" + f.userID + "/tasks/" + f.taskID + "/superseded.md"
	if _, err := f.service.tasks.Storage().Upload(ctx, collectedKey, strings.NewReader("collected"), "text/markdown"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.tasks.Storage().Upload(ctx, cleanupKey, strings.NewReader("superseded"), "text/markdown"); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: uuid.NewString(), TaskID: f.taskID, ExecutionID: "collected-execution",
		State: model.TaskFileStateCollected, Role: model.FileRoleMarkdown,
		FilePath: "output/collected.md", FileName: "collected.md", MimeType: "text/markdown",
		FileSize: 9, ContentHash: strings.Repeat("a", 64), OSSKey: collectedKey,
		CleanupOSSKey: cleanupKey, StorageProvider: "local",
	}); err != nil {
		t.Fatal(err)
	}
	keys = append(keys, collectedKey, cleanupKey)
	if err := f.repo.Tasks().UpdateStatus(ctx, f.taskID, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if won, err := f.repo.TaskExecutions().Transition(ctx, f.executionID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionSucceeded,
		model.ExecutionTransition{
			FinalizationStatus: model.TaskExecutionFinalizationDone,
			CleanupStatus:      model.TaskExecutionCleanupDone,
		}); err != nil || !won {
		t.Fatalf("complete execution cleanup: won=%v err=%v", won, err)
	}
	if err := f.service.tasks.Delete(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if _, err := f.service.tasks.Storage().Read(ctx, key); err == nil {
			t.Errorf("immutable operation object %q still exists after task deletion", key)
		}
	}
}

func TestTaskDeleteRetainsReferencesWhenOperationObjectDeletionFails(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	if _, err := f.service.Generate(ctx, f.request()); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Tasks().UpdateStatus(ctx, f.taskID, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	f.service.tasks.store = &failingTaskImageDeleteStorage{
		Provider: f.service.tasks.store,
		err:      errors.New("storage unavailable"),
	}
	if err := f.service.tasks.Delete(ctx, f.taskID); err == nil {
		t.Fatal("task deletion succeeded despite operation object cleanup failure")
	}
	if _, err := f.repo.Tasks().FindByID(ctx, f.taskID); err != nil {
		t.Fatalf("task reference was removed after cleanup failure: %v", err)
	}
	var files int64
	if err := f.db.Model(&model.TaskFile{}).Where("task_id = ?", f.taskID).Count(&files).Error; err != nil || files != 1 {
		t.Fatalf("task files after cleanup failure = %d, %v; want one retained row", files, err)
	}
}

func TestTaskDeleteRejectsUnknownTaskFileStorageProvider(t *testing.T) {
	for _, provider := range []string{"", "other"} {
		t.Run("provider="+provider, func(t *testing.T) {
			f := newTaskImageFixture(t)
			ctx := context.Background()
			if _, err := f.service.Generate(ctx, f.request()); err != nil {
				t.Fatal(err)
			}
			if err := f.repo.Tasks().UpdateStatus(ctx, f.taskID, model.TaskStatusCompleted); err != nil {
				t.Fatal(err)
			}
			if err := f.db.Model(&model.TaskFile{}).Where("task_id = ?", f.taskID).Update("storage_provider", provider).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.service.tasks.Delete(ctx, f.taskID); err == nil {
				t.Fatal("task deletion accepted an unknown task-file storage provider")
			}
			if _, err := f.repo.Tasks().FindByID(ctx, f.taskID); err != nil {
				t.Fatalf("task reference was removed after provider mismatch: %v", err)
			}
			var files int64
			if err := f.db.Model(&model.TaskFile{}).Where("task_id = ?", f.taskID).Count(&files).Error; err != nil || files != 1 {
				t.Fatalf("task files after provider mismatch = %d, %v; want one retained row", files, err)
			}
		})
	}
}

func TestGenerateTaskImageDoesNotAnalyzeOrUpload(t *testing.T) {
	f := newTaskImageFixture(t)
	if _, err := f.service.Generate(context.Background(), f.request()); err != nil {
		t.Fatal(err)
	}
	if f.generator.analyzeCalls != 0 || f.generator.uploadCalls != 0 {
		t.Fatalf("analyze/upload calls=%d/%d", f.generator.analyzeCalls, f.generator.uploadCalls)
	}
}

func TestTaskImageAssetDoesNotExposeProviderMetadata(t *testing.T) {
	raw, err := json.Marshal(TaskImageAsset{
		TaskFileID: "file-1", FilePath: "output/cover.png", DownloadURL: "/files/cover.png",
		MimeType: "image/png", FileSize: 123, ContentHash: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"provider", "model", "selection_reason", "response_type", "revised_prompt", "billing"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("asset exposes %q: %s", forbidden, raw)
		}
	}
}

func taskImageTinyPNG() []byte {
	data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	return data
}

func hashTaskFileContent(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
