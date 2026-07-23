package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type fakeTaskImageOperationsImage struct {
	uploadPath   string
	compressPath string
}

func (f *fakeTaskImageOperationsImage) UploadImage(_ context.Context, _, _, filePath string) (*UploadImageResult, error) {
	f.uploadPath = filePath
	return &UploadImageResult{URL: "https://cdn.example/image.png"}, nil
}

func (f *fakeTaskImageOperationsImage) CompressImage(filePath string, _ int) (string, bool, error) {
	f.compressPath = filePath
	return filePath + ".compressed", true, nil
}

type fakeTaskImageOperationsWriting struct {
	calls  int
	source string
	usage  srvconfig.TokenUsage
}

func (f *fakeTaskImageOperationsWriting) AnalyzeImageDetailed(_ context.Context, _, imageSource, _ string) (*LLMResult, error) {
	f.calls++
	f.source = imageSource
	return &LLMResult{Text: "analysis", Usage: f.usage}, nil
}

type fakeTaskImageOperationsCost struct {
	recorded *RecordProviderTokenCostRequest
}

func (f *fakeTaskImageOperationsCost) CatalogID() string { return "catalog" }
func (f *fakeTaskImageOperationsCost) RecordProviderTokenUsage(_ context.Context, req RecordProviderTokenCostRequest) (*model.BillingProviderCostEvent, error) {
	f.recorded = &req
	return &model.BillingProviderCostEvent{}, nil
}
func (f *fakeTaskImageOperationsCost) RecordMediaUnreconciled(context.Context, RecordMediaUnreconciledRequest) (*model.BillingProviderCostEvent, error) {
	return &model.BillingProviderCostEvent{}, nil
}

func TestTaskImageOperationsOwnPathResolutionAndAnalysisCost(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.Nop()
	userID := "task-image-operations-user"
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	taskID := "task-image-operations-task"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	imagePath := filepath.Join(workspace, taskID, "output", "image.png")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), 0o644); err != nil {
		t.Fatal(err)
	}
	tasks := NewTaskService(repo, nil, nil, nil, &logger, "", nil, workspace, nil, nil)
	images := &fakeTaskImageOperationsImage{}
	writing := &fakeTaskImageOperationsWriting{usage: srvconfig.TokenUsage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}}
	cost := &fakeTaskImageOperationsCost{}
	svc := NewTaskImageOperationsService(tasks, images, writing, cost, TaskImageOperationsConfig{
		UnderstandingProvider: "provider", UnderstandingModel: "model",
	}, &logger)

	if _, err := svc.Upload(ctx, UploadTaskImageRequest{UserID: userID, ProjectID: projectID, TaskID: taskID, FilePath: "output/image.png"}); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if images.uploadPath != imagePath {
		t.Fatalf("upload path = %q, want %q", images.uploadPath, imagePath)
	}
	if _, err := svc.Compress(ctx, CompressTaskImageRequest{UserID: userID, TaskID: taskID, FilePath: "output/image.png"}); err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if images.compressPath != imagePath {
		t.Fatalf("compress path = %q, want %q", images.compressPath, imagePath)
	}
	result, err := svc.Analyze(ctx, AnalyzeTaskImageRequest{UserID: userID, ProjectID: projectID, TaskID: taskID, FilePath: "output/image.png", Prompt: "inspect"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.Analysis != "analysis" || writing.calls != 1 || cost.recorded == nil {
		t.Fatalf("result=%#v writing_calls=%d cost=%#v", result, writing.calls, cost.recorded)
	}
	if cost.recorded.TaskID != taskID || cost.recorded.Provider != "provider" || cost.recorded.Model != "model" {
		t.Fatalf("provider cost request = %#v", cost.recorded)
	}
}

func TestTaskImageOperationsRejectForeignTaskBeforeDelegation(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.Nop()
	ownerID := "task-image-owner"
	projectID := createTestProject(t, repo, ownerID, model.PlatformArticle)
	taskID := "task-image-foreign"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: ownerID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	writing := &fakeTaskImageOperationsWriting{}
	svc := NewTaskImageOperationsService(NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil), nil, writing, nil, TaskImageOperationsConfig{}, &logger)
	_, err := svc.Analyze(ctx, AnalyzeTaskImageRequest{UserID: "foreign", ProjectID: projectID, TaskID: taskID, ImageURL: "https://example.com/image.png", Prompt: "inspect"})
	if !errors.Is(err, ErrTaskImageOperationOwnership) {
		t.Fatalf("Analyze error = %v, want ownership error", err)
	}
	if writing.calls != 0 {
		t.Fatalf("writing calls = %d, want 0", writing.calls)
	}
}
