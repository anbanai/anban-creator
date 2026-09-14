package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type fakeNativeVideoUnderstandingClient struct {
	videoURL string
	prompt   string
	usage    srvconfig.TokenUsage
	err      error
}

func (*fakeNativeVideoUnderstandingClient) Complete(context.Context, string, string) (string, error) {
	return "", errors.New("text fallback must not be called")
}

func (*fakeNativeVideoUnderstandingClient) CompleteWithImage(context.Context, string, string, string) (string, error) {
	return "", errors.New("image fallback must not be called")
}

func (f *fakeNativeVideoUnderstandingClient) CompleteWithVideoURLResult(_ context.Context, _, prompt, videoURL string) (*LLMResult, error) {
	f.videoURL = videoURL
	f.prompt = prompt
	if f.err != nil {
		return nil, f.err
	}
	return &LLMResult{Text: "  完整视频分析  ", Usage: f.usage}, nil
}

type fakeNonNativeVideoUnderstandingClient struct{}

func (*fakeNonNativeVideoUnderstandingClient) Complete(context.Context, string, string) (string, error) {
	return "", errors.New("text fallback must not be called")
}

func (*fakeNonNativeVideoUnderstandingClient) CompleteWithImage(context.Context, string, string, string) (string, error) {
	return "", errors.New("image fallback must not be called")
}

type taskVideoOperationsFixture struct {
	repo         repository.Repository
	store        *fakeTaskStorage
	nativeClient *fakeNativeVideoUnderstandingClient
	cost         *fakeTaskImageOperationsCost
	userID       string
	projectID    string
	taskID       string
	videoFileID  string
	videoFileKey string
}

func setupTaskVideoOperations(t *testing.T) (*TaskVideoOperationsService, *taskVideoOperationsFixture) {
	t.Helper()
	repo := repository.New(setupTaskTestDB(t))
	ctx := context.Background()
	logger := zerolog.Nop()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := uuid.NewString()
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: taskID, UserID: userID, ProjectID: projectID,
		Type: model.PlatformSeednote, Status: model.TaskStatusCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	videoFileID := uuid.NewString()
	videoFileKey := "tasks/" + taskID + "/output/video.mp4"
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: videoFileID, TaskID: taskID, ExecutionID: "execution-1",
		State: model.TaskFileStateDelivered, Role: model.FileRoleOther,
		FilePath: "output/video.mp4", FileName: "video.mp4", MimeType: "video/mp4",
		OSSKey: videoFileKey, StorageProvider: "oss",
	}); err != nil {
		t.Fatal(err)
	}
	store := &fakeTaskStorage{}
	nativeClient := &fakeNativeVideoUnderstandingClient{usage: srvconfig.TokenUsage{InputTokens: 4, OutputTokens: 3, TotalTokens: 7}}
	cost := &fakeTaskImageOperationsCost{}
	svc := NewTaskVideoOperationsService(repo, store, nativeClient, cost, TaskVideoOperationsConfig{
		UnderstandingProvider: "video-provider", UnderstandingModel: "video-model",
	}, &logger)
	return svc, &taskVideoOperationsFixture{
		repo: repo, store: store, nativeClient: nativeClient, cost: cost,
		userID: userID, projectID: projectID, taskID: taskID,
		videoFileID: videoFileID, videoFileKey: videoFileKey,
	}
}

func TestTaskVideoOperationsAnalyzeTaskFileWithNativeVideoRoute(t *testing.T) {
	svc, fixture := setupTaskVideoOperations(t)
	result, err := svc.Analyze(context.Background(), AnalyzeTaskVideoRequest{
		UserID: fixture.userID, ProjectID: fixture.projectID, TaskID: fixture.taskID,
		TaskFileID: fixture.videoFileID, Prompt: "总结镜头和动作",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Analysis != "完整视频分析" {
		t.Fatalf("analysis=%q", result.Analysis)
	}
	if !strings.Contains(fixture.nativeClient.videoURL, fixture.videoFileKey) {
		t.Fatalf("video URL=%q, want persisted key %q", fixture.nativeClient.videoURL, fixture.videoFileKey)
	}
	if fixture.cost.recorded == nil || !strings.Contains(fixture.cost.recorded.ProviderRequestID, model.OperationVideoUnderstanding) {
		t.Fatalf("provider cost=%#v", fixture.cost.recorded)
	}
}

func TestTaskVideoOperationsRejectsInvalidRequestsBeforeNativeCall(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AnalyzeTaskVideoRequest, *taskVideoOperationsFixture)
		want   string
	}{
		{name: "missing project", mutate: func(r *AnalyzeTaskVideoRequest, _ *taskVideoOperationsFixture) { r.ProjectID = "" }, want: "project_id is required"},
		{name: "missing prompt", mutate: func(r *AnalyzeTaskVideoRequest, _ *taskVideoOperationsFixture) { r.Prompt = "" }, want: "prompt is required"},
		{name: "missing source", mutate: func(r *AnalyzeTaskVideoRequest, _ *taskVideoOperationsFixture) { r.TaskFileID = "" }, want: "exactly one"},
		{name: "both sources", mutate: func(r *AnalyzeTaskVideoRequest, _ *taskVideoOperationsFixture) {
			r.VideoURL = "https://example.com/video.mp4"
		}, want: "exactly one"},
		{name: "non https", mutate: func(r *AnalyzeTaskVideoRequest, _ *taskVideoOperationsFixture) {
			r.TaskFileID = ""
			r.VideoURL = "http://example.com/video.mp4"
		}, want: "HTTPS"},
		{name: "userinfo", mutate: func(r *AnalyzeTaskVideoRequest, _ *taskVideoOperationsFixture) {
			r.TaskFileID = ""
			r.VideoURL = "https://user:pass@example.com/video.mp4"
		}, want: "userinfo"},
		{name: "task mismatch", mutate: func(r *AnalyzeTaskVideoRequest, _ *taskVideoOperationsFixture) { r.TaskID = "different-task" }, want: "task_file_id does not belong to task_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, fixture := setupTaskVideoOperations(t)
			req := AnalyzeTaskVideoRequest{
				UserID: fixture.userID, ProjectID: fixture.projectID, TaskID: fixture.taskID,
				TaskFileID: fixture.videoFileID, Prompt: "分析视频",
			}
			tt.mutate(&req, fixture)
			_, err := svc.Analyze(context.Background(), req)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
			if fixture.nativeClient.videoURL != "" {
				t.Fatalf("native client called with %q", fixture.nativeClient.videoURL)
			}
		})
	}
}

func TestTaskVideoOperationsRejectsForeignTaskFileAndNonVideoMIME(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*model.Task, *model.TaskFile)
		want   string
	}{
		{name: "foreign owner", mutate: func(task *model.Task, _ *model.TaskFile) { task.UserID = uuid.NewString() }, want: "does not belong to user"},
		{name: "project mismatch", mutate: func(task *model.Task, _ *model.TaskFile) { task.ProjectID = uuid.NewString() }, want: "requested project"},
		{name: "non video mime", mutate: func(_ *model.Task, file *model.TaskFile) { file.MimeType = "image/png" }, want: "not a video"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, fixture := setupTaskVideoOperations(t)
			task, err := fixture.repo.Tasks().FindByID(context.Background(), fixture.taskID)
			if err != nil {
				t.Fatal(err)
			}
			file, err := fixture.repo.TaskFiles().FindByID(context.Background(), fixture.videoFileID)
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(task, file)
			if err := fixture.repo.Tasks().Update(context.Background(), task); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.repo.TaskFiles().Upsert(context.Background(), file); err != nil {
				t.Fatal(err)
			}
			_, err = svc.Analyze(context.Background(), AnalyzeTaskVideoRequest{
				UserID: fixture.userID, ProjectID: fixture.projectID,
				TaskFileID: fixture.videoFileID, Prompt: "分析视频",
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestTaskVideoOperationsRequiresConfiguredNativeVideoRoute(t *testing.T) {
	svc, fixture := setupTaskVideoOperations(t)
	svc.client = nil
	_, err := svc.Analyze(context.Background(), AnalyzeTaskVideoRequest{
		UserID: fixture.userID, ProjectID: fixture.projectID,
		VideoURL: "https://example.com/video.mp4", Prompt: "分析视频",
	})
	if !errors.Is(err, ErrVideoUnderstandingUnavailable) {
		t.Fatalf("error=%v", err)
	}

	svc.client = &fakeNonNativeVideoUnderstandingClient{}
	_, err = svc.Analyze(context.Background(), AnalyzeTaskVideoRequest{
		UserID: fixture.userID, ProjectID: fixture.projectID,
		VideoURL: "https://example.com/video.mp4", Prompt: "分析视频",
	})
	if !errors.Is(err, ErrVideoUnderstandingNativeUnsupported) {
		t.Fatalf("error=%v", err)
	}
}
