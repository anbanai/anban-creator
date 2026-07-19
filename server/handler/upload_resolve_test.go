package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type resolveDownloadCall struct {
	key string
	ttl int
}

type resolveDownloadStore struct {
	calls       []resolveDownloadCall
	downloadErr error
}

func (s *resolveDownloadStore) Name() string { return "test" }
func (s *resolveDownloadStore) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}
func (s *resolveDownloadStore) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}
func (s *resolveDownloadStore) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errors.New("not implemented")
}
func (s *resolveDownloadStore) GetURL(key string) string { return "/api/v1/files/" + key }
func (s *resolveDownloadStore) Read(context.Context, string) ([]byte, error) {
	return nil, errors.New("not implemented")
}
func (s *resolveDownloadStore) Delete(context.Context, string) error { return nil }
func (s *resolveDownloadStore) DownloadURL(_ context.Context, key string, ttl int) (string, error) {
	s.calls = append(s.calls, resolveDownloadCall{key: key, ttl: ttl})
	if s.downloadErr != nil {
		return "", s.downloadErr
	}
	return "https://signed.example.com/" + key + "?token=fresh", nil
}
func (s *resolveDownloadStore) HasCustomDomain() bool { return false }
func (s *resolveDownloadStore) IsOwnedURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "/api/v1/files/") || strings.HasPrefix(rawURL, "https://owned.example.com/")
}

func newResolveUploadHandler(t *testing.T, repo repository.Repository, store *resolveDownloadStore, userID string) (*fiber.App, *UploadHandler) {
	t.Helper()
	logger := zerolog.New(io.Discard)
	h := NewUploadHandler(store, repo, service.DirectUploadConfig{}, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	app.Post("/uploads/resolve-download-url", h.ResolveDownloadURL)
	return app, h
}

func resolveDownloadRequest(t *testing.T, app *fiber.App, payload string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/uploads/resolve-download-url", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp, body
}

func createUploadSession(t *testing.T, repo repository.Repository, session *model.UploadSession, asset *model.Asset) {
	t.Helper()
	if err := repo.UploadSessions().Create(t.Context(), session); err != nil {
		t.Fatalf("create upload session: %v", err)
	}
	if asset != nil {
		if err := repo.Assets().Create(t.Context(), asset); err != nil {
			t.Fatalf("create asset: %v", err)
		}
	}
}

func TestResolveAttachmentDownloadURLByUploadID(t *testing.T) {
	repo := repository.New(setupTaskHandlerTestDB(t))
	now := time.Now()
	userID := uuid.NewString()
	otherUserID := uuid.NewString()
	stagingKey := "uploads/pending/" + userID + "/staging/input.png"
	finalSourceKey := "uploads/pending/" + userID + "/final/input.png"
	finalKey := "assets/users/" + userID + "/final/input.png"
	createUploadSession(t, repo, &model.UploadSession{ID: "staging", UserID: userID, Purpose: service.DirectUploadPurposeAIEntryAttachment, StagingKey: stagingKey, FileName: "input.png", Status: model.UploadSessionPending, ExpiresAt: now.Add(time.Hour)}, nil)
	createUploadSession(t, repo, &model.UploadSession{ID: "final", UserID: userID, Purpose: service.DirectUploadPurposeAIEntryAttachment, StagingKey: finalSourceKey, FileName: "input.png", Status: model.UploadSessionFinalized, FinalizationETag: "etag-final", AssetID: "final", ExpiresAt: now.Add(time.Hour)}, &model.Asset{ID: "final", UserID: userID, Purpose: service.DirectUploadPurposeAIEntryAttachment, StorageKey: finalKey, FileName: "input.png", ETag: "etag-final"})
	createUploadSession(t, repo, &model.UploadSession{ID: "other", UserID: otherUserID, Purpose: service.DirectUploadPurposeAIEntryAttachment, StagingKey: "uploads/pending/other/input.png", FileName: "input.png", Status: model.UploadSessionPending, ExpiresAt: now.Add(time.Hour)}, nil)
	createUploadSession(t, repo, &model.UploadSession{ID: "expired", UserID: userID, Purpose: service.DirectUploadPurposeAIEntryAttachment, StagingKey: "uploads/pending/expired/input.png", FileName: "input.png", Status: model.UploadSessionPending, ExpiresAt: now.Add(-time.Minute)}, nil)
	assertFinalizedAsset(t, repo, "final", finalKey)

	store := &resolveDownloadStore{}
	app, _ := newResolveUploadHandler(t, repo, store, userID)
	tests := []struct {
		name       string
		payload    string
		wantStatus int
		wantKey    string
	}{
		{name: "staging source preview", payload: fmt.Sprintf(`{"upload_id":"staging","key":%q}`, stagingKey), wantStatus: fiber.StatusOK, wantKey: stagingKey},
		{name: "finalized immutable key", payload: fmt.Sprintf(`{"upload_id":"final","key":%q}`, finalKey), wantStatus: fiber.StatusOK, wantKey: finalKey},
		{name: "finalized source rejected", payload: fmt.Sprintf(`{"upload_id":"final","key":%q}`, finalSourceKey), wantStatus: fiber.StatusForbidden},
		{name: "cross user rejected", payload: `{"upload_id":"other","key":"uploads/pending/other/input.png"}`, wantStatus: fiber.StatusForbidden},
		{name: "key mismatch rejected", payload: `{"upload_id":"staging","key":"uploads/pending/attacker.png"}`, wantStatus: fiber.StatusForbidden},
		{name: "expired upload session rejected", payload: `{"upload_id":"expired","key":"uploads/pending/expired/input.png"}`, wantStatus: fiber.StatusForbidden},
		{name: "unknown upload session", payload: `{"upload_id":"missing","key":"uploads/pending/missing.png"}`, wantStatus: fiber.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(store.calls)
			resp, body := resolveDownloadRequest(t, app, tt.payload)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d body=%v", resp.StatusCode, tt.wantStatus, body)
			}
			if tt.wantKey == "" {
				if len(store.calls) != before {
					t.Fatalf("DownloadURL called for rejected request: %+v", store.calls[before:])
				}
				return
			}
			if len(store.calls) != before+1 || store.calls[before] != (resolveDownloadCall{key: tt.wantKey, ttl: service.DefaultSignedURLTTL}) {
				t.Fatalf("DownloadURL calls = %+v", store.calls[before:])
			}
			data := body["data"].(map[string]any)
			if data["url"] == "" || data["expires_at"] == "" {
				t.Fatalf("response data = %#v", data)
			}
			expiresAt, err := time.Parse(time.RFC3339Nano, data["expires_at"].(string))
			if err != nil || time.Until(expiresAt) < 59*time.Minute || time.Until(expiresAt) > 61*time.Minute {
				t.Fatalf("expires_at = %v, err=%v", data["expires_at"], err)
			}
		})
	}
}

func TestResolveAttachmentDownloadURLByOwner(t *testing.T) {
	repo := repository.New(setupTaskHandlerTestDB(t))
	userID := uuid.NewString()
	otherUserID := uuid.NewString()
	taskID := uuid.NewString()
	planID := uuid.NewString()
	taskKey := "uploads/finalized/" + userID + "/task/input.png"
	planKey := "uploads/finalized/" + userID + "/plan/reference.png"
	videoKey := "uploads/finalized/" + userID + "/task/video.mp4"
	configKey := "uploads/finalized/" + userID + "/task/config-reference.png"

	task := &model.Task{ID: taskID, UserID: userID, Type: model.PlatformVideoCreator, Status: model.TaskStatusPending}
	task.SetInputAttachments([]model.EntryAttachment{{Type: "image", Key: taskKey, URL: "/api/v1/files/" + taskKey}})
	task.SetVideoInput(model.VideoInput{References: []model.VideoReferenceAsset{{Type: "video_url", URL: "https://owned.example.com/" + videoKey}, {Type: "image_url", URL: "https://external.example.com/public.png"}}})
	task.SetVideoConfig(model.VideoTaskConfig{References: []model.VideoReferenceAsset{{Type: "image_url", URL: "/api/v1/files/" + configKey}}})
	if err := repo.Tasks().Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	otherTask := &model.Task{ID: uuid.NewString(), UserID: otherUserID, Type: model.PlatformArticle, Status: model.TaskStatusPending}
	otherTask.SetInputAttachments([]model.EntryAttachment{{Type: "image", Key: "uploads/finalized/other/secret.png"}})
	if err := repo.Tasks().Create(t.Context(), otherTask); err != nil {
		t.Fatal(err)
	}
	plan := &model.Plan{ID: planID, UserID: userID, Type: model.PlatformArticle, Status: model.PlanStatusActive, ReferenceImageURL: "/api/v1/files/" + planKey}
	if err := repo.Plans().Create(t.Context(), plan); err != nil {
		t.Fatal(err)
	}

	store := &resolveDownloadStore{}
	app, _ := newResolveUploadHandler(t, repo, store, userID)
	tests := []struct {
		name       string
		payload    string
		wantStatus int
		wantKey    string
	}{
		{name: "task attachment key", payload: fmt.Sprintf(`{"key":%q,"owner_type":"task","owner_id":%q}`, taskKey, taskID), wantStatus: fiber.StatusOK, wantKey: taskKey},
		{name: "task owned legacy video URL", payload: fmt.Sprintf(`{"key":%q,"owner_type":"task","owner_id":%q}`, videoKey, taskID), wantStatus: fiber.StatusOK, wantKey: videoKey},
		{name: "task video config reference", payload: fmt.Sprintf(`{"key":%q,"owner_type":"task","owner_id":%q}`, configKey, taskID), wantStatus: fiber.StatusOK, wantKey: configKey},
		{name: "plan legacy reference URL ignored", payload: fmt.Sprintf(`{"key":%q,"owner_type":"plan","owner_id":%q}`, planKey, planID), wantStatus: fiber.StatusForbidden},
		{name: "unrelated key", payload: fmt.Sprintf(`{"key":"uploads/finalized/unrelated.png","owner_type":"task","owner_id":%q}`, taskID), wantStatus: fiber.StatusForbidden},
		{name: "external URL key not accepted", payload: fmt.Sprintf(`{"key":"public.png","owner_type":"task","owner_id":%q}`, taskID), wantStatus: fiber.StatusForbidden},
		{name: "cross user owner", payload: fmt.Sprintf(`{"key":"uploads/finalized/other/secret.png","owner_type":"task","owner_id":%q}`, otherTask.ID), wantStatus: fiber.StatusForbidden},
		{name: "wrong owner type", payload: fmt.Sprintf(`{"key":%q,"owner_type":"project","owner_id":%q}`, taskKey, taskID), wantStatus: fiber.StatusBadRequest},
		{name: "missing owner", payload: fmt.Sprintf(`{"key":%q}`, taskKey), wantStatus: fiber.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(store.calls)
			resp, body := resolveDownloadRequest(t, app, tt.payload)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d body=%v", resp.StatusCode, tt.wantStatus, body)
			}
			if tt.wantKey == "" && len(store.calls) != before {
				t.Fatalf("DownloadURL called for rejected request")
			}
			if tt.wantKey != "" && (len(store.calls) != before+1 || store.calls[before].key != tt.wantKey) {
				t.Fatalf("DownloadURL calls = %+v", store.calls[before:])
			}
		})
	}
}

func TestResolveAttachmentDownloadURLRedactsStorageFailure(t *testing.T) {
	repo := repository.New(setupTaskHandlerTestDB(t))
	userID := uuid.NewString()
	key := "uploads/pending/" + userID + "/input.png"
	createUploadSession(t, repo, &model.UploadSession{ID: "broken", UserID: userID, Purpose: service.DirectUploadPurposeAIEntryAttachment, StagingKey: key, FileName: "input.png", Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour)}, nil)
	store := &resolveDownloadStore{downloadErr: errors.New("backend secret credential leaked")}
	app, _ := newResolveUploadHandler(t, repo, store, userID)
	resp, body := resolveDownloadRequest(t, app, fmt.Sprintf(`{"upload_id":"broken","key":%q}`, key))
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, body=%v", resp.StatusCode, body)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "credential") {
		t.Fatalf("backend error leaked: %s", raw)
	}
}

func TestAttachmentAPIResponseEmitsOwnedKeysWithoutSigningOrMutation(t *testing.T) {
	store := &resolveDownloadStore{}
	ownedKey := "uploads/finalized/user-1/owned.png"
	externalURL := "https://external.example.com/public.png"
	task := &model.Task{ID: "task-1", Type: model.PlatformVideoCreator, ReferenceImageURL: "/api/v1/files/" + ownedKey}
	task.SetInputAttachments([]model.EntryAttachment{{Type: "image", URL: "/api/v1/files/" + ownedKey}, {Type: "image", URL: externalURL}})
	task.SetVideoInput(model.VideoInput{References: []model.VideoReferenceAsset{{Type: "image_url", URL: "/api/v1/files/" + ownedKey}}})
	plan := &model.Plan{ID: "plan-1", Type: model.PlatformArticle, ReferenceImageURL: "/api/v1/files/" + ownedKey}
	plan.SetInputAttachments([]model.EntryAttachment{{Type: "image", URL: "/api/v1/files/" + ownedKey}, {Type: "image", URL: externalURL}})

	taskResp := taskAPIResponse(task, store)
	planResp := planAPIResponse(plan, store)
	for name, resp := range map[string]map[string]any{"task": taskResp, "plan": planResp} {
		raw, err := json.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "token=fresh") {
			t.Fatalf("%s response contains signed URL: %s", name, raw)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		attachments := decoded["input_attachments"].([]any)
		if attachments[0].(map[string]any)["key"] != ownedKey {
			t.Fatalf("%s owned attachment missing key: %v", name, attachments[0])
		}
		if _, ok := attachments[1].(map[string]any)["key"]; ok {
			t.Fatalf("%s external attachment gained key: %v", name, attachments[1])
		}
		if _, exists := decoded["reference_image_key"]; exists {
			t.Fatalf("%s leaked legacy reference_image_key: %v", name, decoded["reference_image_key"])
		}
	}
	taskRaw, _ := json.Marshal(task.InputAttachments.Data())
	planRaw, _ := json.Marshal(plan.InputAttachments.Data())
	if strings.Contains(string(taskRaw), `"key"`) || strings.Contains(string(planRaw), `"key"`) || len(store.calls) != 0 {
		t.Fatalf("serialization mutated models or signed URLs: task=%s plan=%s calls=%v", taskRaw, planRaw, store.calls)
	}
	videoRaw, _ := json.Marshal(taskResp["video_creator_input"])
	if !strings.Contains(string(videoRaw), `"key":"`+ownedKey+`"`) {
		t.Fatalf("video reference is not key-first: %s", videoRaw)
	}
}
