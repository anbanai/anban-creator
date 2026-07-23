package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	err    error
}

func (f *fakeTaskImageOperationsWriting) AnalyzeImageDetailed(_ context.Context, _, imageSource, _ string) (*LLMResult, error) {
	f.calls++
	f.source = imageSource
	if f.err != nil {
		return nil, f.err
	}
	return &LLMResult{Text: "analysis", Usage: f.usage}, nil
}

type fakeTaskImageOperationsCost struct {
	recorded     *RecordProviderTokenCostRequest
	unreconciled *RecordMediaUnreconciledRequest
	recordErr    error
}

func (f *fakeTaskImageOperationsCost) CatalogID() string { return "catalog" }
func (f *fakeTaskImageOperationsCost) RecordProviderTokenUsage(_ context.Context, req RecordProviderTokenCostRequest) (*model.BillingProviderCostEvent, error) {
	f.recorded = &req
	return &model.BillingProviderCostEvent{}, f.recordErr
}
func (f *fakeTaskImageOperationsCost) RecordMediaUnreconciled(_ context.Context, req RecordMediaUnreconciledRequest) (*model.BillingProviderCostEvent, error) {
	f.unreconciled = &req
	return &model.BillingProviderCostEvent{}, f.recordErr
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

func TestTaskImageOperationsRejectsProjectMismatchBeforeDelegation(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.Nop()
	userID := "task-image-project-owner"
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	taskID := "task-image-project-mismatch"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	writing := &fakeTaskImageOperationsWriting{}
	svc := NewTaskImageOperationsService(NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil), nil, writing, nil, TaskImageOperationsConfig{}, &logger)
	_, err := svc.Analyze(ctx, AnalyzeTaskImageRequest{UserID: userID, ProjectID: "wrong-project", TaskID: taskID, ImageURL: "https://example.com/image.png", Prompt: "inspect"})
	if !errors.Is(err, ErrTaskImageOperationProjectMismatch) || writing.calls != 0 {
		t.Fatalf("Analyze = %v, writing calls=%d; want project mismatch before delegation", err, writing.calls)
	}
}

func TestTaskImageOperationsRejectsInvalidLocalAndRemoteImageSources(t *testing.T) {
	logger := zerolog.Nop()
	localText := filepath.Join(t.TempDir(), "not-image.txt")
	if err := os.WriteFile(localText, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	localLarge := filepath.Join(t.TempDir(), "large.png")
	if err := os.WriteFile(localLarge, make([]byte, maxAnalyzedTaskImageBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		filePath   string
		imageURL   string
		downloaded []byte
		want       string
	}{
		{name: "local non-image", filePath: localText, want: "file is not an image"},
		{name: "local oversized", filePath: localLarge, want: "too large"},
		{name: "remote non-image", imageURL: "https://example.com/text", downloaded: []byte("plain text"), want: "downloaded file is not an image"},
		{name: "remote oversized", imageURL: "https://example.com/large.png", downloaded: make([]byte, maxAnalyzedTaskImageBytes+1), want: "exceeds max size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &fakeTaskImageOperationsWriting{}
			svc := NewTaskImageOperationsService(nil, nil, writing, nil, TaskImageOperationsConfig{}, &logger)
			svc.downloadAnalysisImage = func(context.Context, string, int64) ([]byte, error) {
				if int64(len(tt.downloaded)) > maxAnalyzedTaskImageBytes {
					return nil, errors.New("external image exceeds max size")
				}
				return tt.downloaded, nil
			}
			_, err := svc.Analyze(context.Background(), AnalyzeTaskImageRequest{ImageURL: tt.imageURL, FilePath: tt.filePath, ProjectID: "project", Prompt: "inspect"})
			if err == nil || !strings.Contains(err.Error(), tt.want) || writing.calls != 0 {
				t.Fatalf("Analyze = %v, calls=%d; want %q before delegation", err, writing.calls, tt.want)
			}
		})
	}
}

func TestTaskImageAnalysisSSRFAndRedirectPolicy(t *testing.T) {
	if _, err := publicTaskAnalysisDialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", "443")); err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("private address dial error = %v, want non-public rejection", err)
	}
	for _, tt := range []struct {
		name string
		req  *http.Request
		via  []*http.Request
		want string
	}{
		{name: "downgrade", req: &http.Request{URL: &url.URL{Scheme: "http", Host: "example.com"}}, want: "must use https"},
		{name: "too many", req: &http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}}, via: make([]*http.Request, 3), want: "too many redirects"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateTaskAnalysisRedirect(tt.req, tt.via); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("redirect validation = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestTaskImageAnalysisCostEvidenceLifecycle(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, png, 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name             string
		writing          *fakeTaskImageOperationsWriting
		costErr          error
		wantAnalyzeError bool
		wantUnreconciled bool
		wantCacheRead    int64
	}{
		{name: "analysis failure is unreconciled", writing: &fakeTaskImageOperationsWriting{err: errors.New("vision failed")}, wantAnalyzeError: true, wantUnreconciled: true},
		{name: "zero usage is unreconciled", writing: &fakeTaskImageOperationsWriting{}, wantUnreconciled: true},
		{name: "cached token fallback", writing: &fakeTaskImageOperationsWriting{usage: srvconfig.TokenUsage{InputTokens: 9, CachedInputTokens: 4, OutputTokens: 1, TotalTokens: 10}}, wantCacheRead: 4},
		{name: "explicit cache read wins", writing: &fakeTaskImageOperationsWriting{usage: srvconfig.TokenUsage{InputTokens: 9, CachedInputTokens: 4, CacheReadInputTokens: 6, CacheCreationInputTokens: 2, OutputTokens: 1, TotalTokens: 10}}, wantCacheRead: 6},
		{name: "cost write failure preserves result", writing: &fakeTaskImageOperationsWriting{usage: srvconfig.TokenUsage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}}, costErr: errors.New("cost write failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()
			cost := &fakeTaskImageOperationsCost{recordErr: tt.costErr}
			svc := NewTaskImageOperationsService(nil, nil, tt.writing, cost, TaskImageOperationsConfig{UnderstandingProvider: "provider", UnderstandingModel: "model"}, &logger)
			result, err := svc.Analyze(context.Background(), AnalyzeTaskImageRequest{ProjectID: "project", FilePath: path, Prompt: "inspect"})
			if tt.wantAnalyzeError {
				if err == nil || result != nil {
					t.Fatalf("Analyze = %#v, %v; want analysis error", result, err)
				}
			} else if err != nil || result == nil || result.Analysis != "analysis" {
				t.Fatalf("Analyze = %#v, %v; want successful result", result, err)
			}
			if tt.wantUnreconciled {
				if cost.unreconciled == nil || cost.unreconciled.ReasonCode != model.BillingExecutionCostReasonMissingProviderUsage {
					t.Fatalf("unreconciled evidence = %#v", cost.unreconciled)
				}
				return
			}
			if cost.recorded == nil || cost.recorded.Usage.CacheRead != tt.wantCacheRead {
				t.Fatalf("token evidence = %#v, want cache read %d", cost.recorded, tt.wantCacheRead)
			}
		})
	}
}
