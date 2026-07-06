package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type fakeVideoProductionStore struct {
	files map[string][]byte
}

func (f *fakeVideoProductionStore) Name() string { return "fake" }

func (f *fakeVideoProductionStore) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[key] = append([]byte(nil), data...)
	return &storage.UploadResult{Key: key, URL: f.GetURL(key), Size: int64(len(data)), MimeType: contentType}, nil
}
func (f *fakeVideoProductionStore) UploadFile(ctx context.Context, key string, filePath string, contentType string) (*storage.UploadResult, error) {
	return &storage.UploadResult{Key: key, URL: f.GetURL(key), MimeType: contentType}, nil
}
func (f *fakeVideoProductionStore) UploadURL(context.Context, string, string, int) (string, error) {
	return "", nil
}
func (f *fakeVideoProductionStore) GetURL(key string) string { return "https://cdn.example.com/" + key }
func (f *fakeVideoProductionStore) Read(_ context.Context, key string) ([]byte, error) {
	return append([]byte(nil), f.files[key]...), nil
}
func (f *fakeVideoProductionStore) Delete(context.Context, string) error { return nil }
func (f *fakeVideoProductionStore) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	return "https://download.example.com/" + key, nil
}
func (f *fakeVideoProductionStore) HasCustomDomain() bool  { return true }
func (f *fakeVideoProductionStore) IsOwnedURL(string) bool { return false }

func TestTaskVideoProductionAggregatesProductionArtifacts(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	taskID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "video-production@example.com", Password: "hashed", InviteCode: "videoproduction"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformVideo, Name: "Video", Status: model.ProjectStatusActive}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformVideo,
		Status:    model.TaskStatusCompleted,
		Prompt:    "做一条产品视频",
	}
	task.SetVideoConfig(model.VideoTaskConfig{ScenarioKey: "live_selling", ProductionMode: "guided"})
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	store := &fakeVideoProductionStore{files: map[string][]byte{
		"tasks/" + taskID + "/creative-brief.md":      []byte("# Creative Brief\n产品可信感"),
		"tasks/" + taskID + "/project-state.json":     []byte(`{"project_id":"` + projectID + `","current_clip_id":"clip-01"}`),
		"tasks/" + taskID + "/quality-review.md":      []byte("# Quality Review\nverdict: Keep"),
		"tasks/" + taskID + "/delivery-manifest.json": []byte(`{"final_video_task_file":"file-final"}`),
	}}
	files := []*model.TaskFile{
		{ID: uuid.New().String(), TaskID: taskID, Role: model.FileRoleOther, FileName: "creative-brief.md", FilePath: "creative-brief.md", MimeType: "text/markdown", OSSKey: "tasks/" + taskID + "/creative-brief.md", StorageProvider: "oss"},
		{ID: uuid.New().String(), TaskID: taskID, Role: model.FileRoleOther, FileName: "project-state.json", FilePath: "project-state.json", MimeType: "application/json", OSSKey: "tasks/" + taskID + "/project-state.json", StorageProvider: "oss"},
		{ID: uuid.New().String(), TaskID: taskID, Role: model.FileRoleReview, FileName: "quality-review.md", FilePath: "quality-review.md", MimeType: "text/markdown", OSSKey: "tasks/" + taskID + "/quality-review.md", StorageProvider: "oss"},
		{ID: uuid.New().String(), TaskID: taskID, Role: model.FileRoleOther, FileName: "delivery-manifest.json", FilePath: "delivery-manifest.json", MimeType: "application/json", OSSKey: "tasks/" + taskID + "/delivery-manifest.json", StorageProvider: "oss"},
	}
	for _, file := range files {
		if err := repo.TaskFiles().Create(ctx, file); err != nil {
			t.Fatalf("create task file %s: %v", file.FileName, err)
		}
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	app := fiber.New()
	app.Get("/tasks/:id/video-production", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.GetVideoProduction(c)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/tasks/"+taskID+"/video-production", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data struct {
			TaskID    string `json:"task_id"`
			Scenario  string `json:"scenario_key"`
			Mode      string `json:"production_mode"`
			Artifacts map[string]struct {
				Status     string         `json:"status"`
				Content    string         `json:"content"`
				ParsedJSON map[string]any `json:"parsed_json,omitempty"`
			} `json:"artifacts"`
			RetakeActions []string `json:"retake_actions"`
			NextActions   []string `json:"next_actions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.TaskID != taskID || body.Data.Scenario != "live_selling" || body.Data.Mode != "guided" {
		t.Fatalf("production identity = %+v", body.Data)
	}
	if body.Data.Artifacts["creative-brief.md"].Status != "available" || body.Data.Artifacts["creative-brief.md"].Content == "" {
		t.Fatalf("creative brief artifact = %+v", body.Data.Artifacts["creative-brief.md"])
	}
	if body.Data.Artifacts["project-state.json"].ParsedJSON["current_clip_id"] != "clip-01" {
		t.Fatalf("project state artifact = %+v", body.Data.Artifacts["project-state.json"])
	}
	if body.Data.Artifacts["take-log.md"].Status != "missing" {
		t.Fatalf("take log artifact = %+v, want missing placeholder", body.Data.Artifacts["take-log.md"])
	}
	if !containsString(body.Data.RetakeActions, "re_roll") || !containsString(body.Data.NextActions, "continue_editing") {
		t.Fatalf("actions = retake:%+v next:%+v", body.Data.RetakeActions, body.Data.NextActions)
	}
}
