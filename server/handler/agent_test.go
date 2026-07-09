package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

// setupAgentClaimApp wires an AgentHandler (real API-key auth via APIKeyService)
// over a fresh sqlite DB seeded with a user + project, and returns the app, the
// repo handle (so callers can seed tasks against the same DB), the user's raw
// API key, and the user/project IDs.
func setupAgentClaimApp(t *testing.T) (app *fiber.App, repo repository.Repository, rawKey, userID, projectID string) {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo = repository.New(db)
	ctx := context.Background()

	userID = uuid.New().String()
	projectID = uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "claim@example.com", Password: "x", InviteCode: "ic"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "P", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	taskSvc.SetExecutorDefaults("claude-test", nil)
	apiKeySvc := service.NewAPIKeyService(repo, &logger)
	_, rawKey, err := apiKeySvc.Create(ctx, userID, "test")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	h := NewAgentHandler(taskSvc, apiKeySvc, nil, "", &logger)
	app = fiber.New()
	app.Post("/agent/claim", h.AuthMiddleware, h.Claim)
	return app, repo, rawKey, userID, projectID
}

// seedClaimableLocalTask persists a pending local-target task and returns its ID.
func seedClaimableLocalTask(t *testing.T, repo repository.Repository, userID, projectID string) string {
	t.Helper()
	task := &model.Task{
		ID:                 uuid.New().String(),
		UserID:             userID,
		ProjectID:          projectID,
		Type:               model.PlatformSeednote,
		Status:             model.TaskStatusPending,
		Prompt:             "测试选题",
		ExecutionTarget:    model.ExecutionTargetLocal,
		LocalClaimDeadline: ptrTime(time.Now().Add(service.LocalClaimWindow)),
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create seed task: %v", err)
	}
	return task.ID
}

func ptrTime(t time.Time) *time.Time { return &t }

type fakeAgentArtifactStorage struct {
	uploadKey         string
	uploadContentType string
	stats             map[string]*storage.ObjectInfo
}

func (f *fakeAgentArtifactStorage) Name() string { return "oss" }
func (f *fakeAgentArtifactStorage) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, nil
}
func (f *fakeAgentArtifactStorage) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, nil
}
func (f *fakeAgentArtifactStorage) UploadURL(_ context.Context, key string, contentType string, _ int) (string, error) {
	f.uploadKey = key
	f.uploadContentType = contentType
	return "https://upload.example.com/" + key, nil
}
func (f *fakeAgentArtifactStorage) GetURL(key string) string { return "https://cdn.example.com/" + key }
func (f *fakeAgentArtifactStorage) Read(context.Context, string) ([]byte, error) {
	return nil, os.ErrNotExist
}
func (f *fakeAgentArtifactStorage) Delete(context.Context, string) error { return nil }
func (f *fakeAgentArtifactStorage) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	return "https://download.example.com/" + key, nil
}
func (f *fakeAgentArtifactStorage) HasCustomDomain() bool { return true }
func (f *fakeAgentArtifactStorage) IsOwnedURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "https://cdn.example.com/")
}
func (f *fakeAgentArtifactStorage) StatObject(_ context.Context, key string) (*storage.ObjectInfo, error) {
	if f.stats == nil || f.stats[key] == nil {
		return nil, os.ErrNotExist
	}
	cp := *f.stats[key]
	return &cp, nil
}

func setupAgentArtifactApp(t *testing.T) (*fiber.App, repository.Repository, *model.Task, *fakeAgentArtifactStorage, string, string) {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	projectID := uuid.New().String()
	for _, user := range []*model.User{
		{ID: userID, Email: "artifact@example.com", Password: "x", InviteCode: "artifact"},
		{ID: otherUserID, Email: "other-artifact@example.com", Password: "x", InviteCode: "other-artifact"},
	} {
		if err := repo.Users().Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "P", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	store := &fakeAgentArtifactStorage{}
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, nil, &logger, "", nil, "", nil, nil)
	apiKeySvc := service.NewAPIKeyService(repo, &logger)
	_, rawKey, err := apiKeySvc.Create(ctx, userID, "artifact")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	_, otherRawKey, err := apiKeySvc.Create(ctx, otherUserID, "artifact-other")
	if err != nil {
		t.Fatalf("create other api key: %v", err)
	}
	h := NewAgentHandler(taskSvc, apiKeySvc, store, "", &logger)
	h.SetDirectUploadConfig(service.DirectUploadConfig{
		Storage: config.StorageConfig{
			Provider:       "oss",
			Endpoint:       "oss-cn-hangzhou.aliyuncs.com",
			BucketName:     "anban-test",
			Region:         "oss-cn-hangzhou",
			STSRoleArn:     "acs:ram::1:role/upload",
			STSSessionName: "agent-artifact-upload",
		},
		CredentialIssuer: service.StaticUploadCredentialIssuer(func(context.Context, service.UploadCredentialRequest) (*service.UploadCredential, error) {
			return &service.UploadCredential{
				AccessKeyID:     "sts-ak",
				AccessKeySecret: "sts-secret",
				SecurityToken:   "sts-token",
				ExpiresAt:       time.Now().Add(15 * time.Minute),
			}, nil
		}),
	})

	app := fiber.New()
	app.Post("/agent/artifacts/prepare", h.AuthMiddleware, h.PrepareArtifactUpload)
	app.Post("/agent/artifacts/manifest", h.AuthMiddleware, h.ReportArtifactManifest)
	return app, repo, task, store, rawKey, otherRawKey
}

func TestAgentArtifactPrepareAndManifest(t *testing.T) {
	app, repo, task, store, rawKey, _ := setupAgentArtifactApp(t)

	prepareBody := `{"task_id":"` + task.ID + `","relative_path":"output/article.md","filename":"article.md","content_type":"text/markdown","size":123}`
	req := httptest.NewRequest("POST", "/agent/artifacts/prepare", strings.NewReader(prepareBody))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("prepare request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("prepare status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	var env struct {
		Data struct {
			Key                string `json:"key"`
			STSAccessKeyID     string `json:"sts_access_key_id"`
			STSSecurityToken   string `json:"sts_security_token"`
			STSAccessKeySecret string `json:"sts_access_key_secret"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}
	wantKey := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/artifacts/output/article.md"
	if env.Data.Key != wantKey || store.uploadKey != wantKey || store.uploadContentType != "text/markdown" {
		t.Fatalf("prepared key/store = %q/%q/%q, want %q", env.Data.Key, store.uploadKey, store.uploadContentType, wantKey)
	}
	if env.Data.STSAccessKeyID != "sts-ak" || env.Data.STSSecurityToken != "sts-token" || env.Data.STSAccessKeySecret == "" {
		t.Fatalf("missing sts credentials: %#v", env.Data)
	}

	body := []byte("# title\n\nbody")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	store.stats = map[string]*storage.ObjectInfo{
		wantKey: {Key: wantKey, Size: int64(len(body)), ContentType: "text/markdown", ETag: "etag"},
	}
	manifestBody := `{"task_id":"` + task.ID + `","files":[{"relative_path":"output/article.md","object_key":"` + wantKey + `","content_type":"text/markdown","size":` + "13" + `,"sha256":"` + hash + `","etag":"etag"}]}`
	req = httptest.NewRequest("POST", "/agent/artifacts/manifest", strings.NewReader(manifestBody))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("manifest request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("manifest status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	files, err := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("find task files: %v", err)
	}
	if len(files) != 1 || files[0].OSSKey != wantKey {
		t.Fatalf("task files = %#v, want one file with key %q", files, wantKey)
	}
}

func TestAgentArtifactPrepareRejectsCrossUser(t *testing.T) {
	app, _, task, _, _, otherRawKey := setupAgentArtifactApp(t)

	body := `{"task_id":"` + task.ID + `","relative_path":"output/article.md","filename":"article.md","content_type":"text/markdown","size":123}`
	req := httptest.NewRequest("POST", "/agent/artifacts/prepare", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+otherRawKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 403; body=%s", resp.StatusCode, body)
	}
}

// TestAgentClaim_RequiresAuth confirms a missing token is rejected.
func TestAgentClaim_RequiresAuth(t *testing.T) {
	app, _, _, _, _ := setupAgentClaimApp(t)
	req := httptest.NewRequest("POST", "/agent/claim", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestAgentClaim_RejectsInvalidToken confirms an unknown token is rejected.
func TestAgentClaim_RejectsInvalidToken(t *testing.T) {
	app, _, _, _, _ := setupAgentClaimApp(t)
	req := httptest.NewRequest("POST", "/agent/claim", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-key")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestAgentClaim_ReturnsConfigThenNoContent confirms a valid claim returns the
// full task config, and a follow-up claim returns 204 (nothing left).
func TestAgentClaim_ReturnsConfigThenNoContent(t *testing.T) {
	app, repo, rawKey, userID, projectID := setupAgentClaimApp(t)
	taskID := seedClaimableLocalTask(t, repo, userID, projectID)

	// First claim: should return 200 + config carrying the task id.
	req := httptest.NewRequest("POST", "/agent/claim", strings.NewReader(`{"executor_info":{"hostname":"mbp"}}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), taskID) {
		t.Fatalf("response body does not contain task id %q: %s", taskID, body)
	}
	if !strings.Contains(string(body), `"agent_flag"`) {
		t.Fatalf("response body missing agent_flag field: %s", body)
	}

	// Second claim: nothing claimable → 204 No Content.
	req2 := httptest.NewRequest("POST", "/agent/claim", nil)
	req2.Header.Set("Authorization", "Bearer "+rawKey)
	resp2, err := app.Test(req2)
	if err != nil {
		t.Fatalf("second claim failed: %v", err)
	}
	if resp2.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp2.StatusCode)
	}
}

// TestAgentClaim_NoContentWhenEmpty confirms an unauthenticated-of-tasks claim
// returns 204 even with a valid token.
func TestAgentClaim_NoContentWhenEmpty(t *testing.T) {
	app, _, rawKey, _, _ := setupAgentClaimApp(t)
	// No task seeded.
	req := httptest.NewRequest("POST", "/agent/claim", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}
