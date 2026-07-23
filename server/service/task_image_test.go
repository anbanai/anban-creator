package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	taskSvc := NewTaskService(repo, nil, nil, store, &logger, "", nil, "", nil, nil)
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
	if asset.Name != "cover.png" || asset.Role != model.FileRoleCover || asset.FilePath != "output/cover.png" || asset.DownloadURL == "" {
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
	if _, err := f.service.Generate(context.Background(), f.request()); err != nil {
		t.Fatal(err)
	}
	changed := f.request()
	changed.Prompt = "draw a different tea cover"
	if _, err := f.service.Generate(context.Background(), changed); err != nil {
		t.Fatal(err)
	}
	if f.generator.calls != 2 {
		t.Fatalf("generator calls=%d, want 2", f.generator.calls)
	}
	var settlements int64
	if err := f.db.Model(&model.BillingSettlementOutbox{}).Count(&settlements).Error; err != nil || settlements != 2 {
		t.Fatalf("settlements=%d err=%v", settlements, err)
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
	raw, err := json.Marshal(TaskImageAsset{Name: "cover.png", Role: "cover", DownloadURL: "/files/cover.png", FilePath: "output/cover.png"})
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
