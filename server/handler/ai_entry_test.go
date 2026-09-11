package handler

import (
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
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type fakeAIEntrySubmitter struct {
	req   service.AIEntrySubmitRequest
	res   service.AIEntrySubmitResult
	err   error
	calls int
}

func (f *fakeAIEntrySubmitter) Submit(_ context.Context, req service.AIEntrySubmitRequest) (*service.AIEntrySubmitResult, error) {
	f.calls++
	f.req = req
	return &f.res, f.err
}

func TestAIEntryHandlerSubmitRequiresAuth(t *testing.T) {
	logger := zerolog.New(io.Discard)
	h := NewAIEntryHandler(&fakeAIEntrySubmitter{}, nil, nil, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", h.Submit)

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{"text":"写文章"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAIEntryHandlerMapsAndRedactsReferenceErrors(t *testing.T) {
	const rootText = "database shard secret"
	for _, tt := range []struct {
		name       string
		class      error
		wantStatus int
		wantText   string
	}{
		{name: "forbidden", class: service.ErrReferenceAssetForbidden, wantStatus: fiber.StatusForbidden, wantText: "reference image is not accessible"},
		{name: "purpose mismatch", class: service.ErrReferenceAssetPurposeMismatch, wantStatus: fiber.StatusBadRequest, wantText: "reference image purpose is not allowed"},
		{name: "unavailable", class: service.ErrReferenceAssetUnavailable, wantStatus: fiber.StatusServiceUnavailable, wantText: "reference image storage is temporarily unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.New(io.Discard)
			h := NewAIEntryHandler(&fakeAIEntrySubmitter{err: fmt.Errorf("%w: %s", tt.class, rootText)}, nil, nil, &logger)
			app := fiber.New()
			app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
				c.Locals("user_id", "user-1")
				return h.Submit(c)
			})
			req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{"project_id":"project-1","execution_profile":"effective","text":"write"}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tt.wantStatus || !strings.Contains(string(body), tt.wantText) || strings.Contains(string(body), rootText) {
				t.Fatalf("status/body = %d/%s, want %d containing %q without root", resp.StatusCode, body, tt.wantStatus, tt.wantText)
			}
		})
	}
}

func TestAIEntryHandlerMapsAgentProfileErrors(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   int
		msg    string
	}{
		{service.ErrAgentProfileNotFound, fiber.StatusBadRequest, 46001, "invalid_agent_execution_profile"},
		{service.ErrAgentProfileUnavailable, fiber.StatusUnprocessableEntity, 46002, "agent_profile_unavailable"},
		{service.ErrAgentProfileAccessDenied, fiber.StatusForbidden, 46003, "agent_profile_access_denied"},
		{service.ErrAgentProfileSnapshotInvalid, fiber.StatusBadRequest, 46004, "agent_profile_snapshot_invalid"},
		{service.ErrAgentProfileSnapshotConflict, fiber.StatusConflict, 46005, "agent_profile_snapshot_conflict"},
		{service.ErrAgentProviderUnavailable, fiber.StatusUnprocessableEntity, 46006, "agent_provider_unavailable"},
		{service.ErrAgentModelCostUnmapped, fiber.StatusUnprocessableEntity, 46007, "agent_model_cost_unmapped"},
		{service.ErrBillingProfileSKUNotFound, fiber.StatusUnprocessableEntity, 46008, "billing_profile_sku_not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			logger := zerolog.New(io.Discard)
			h := NewAIEntryHandler(&fakeAIEntrySubmitter{err: fmt.Errorf("submit context: %w", tt.err)}, nil, nil, &logger)
			app := fiber.New()
			app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
				c.Locals("user_id", "user-1")
				return h.Submit(c)
			})
			req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{"project_id":"project-1","execution_profile":"effective","text":"write"}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req, fiber.TestConfig{Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var body Response
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.status || body.Code != tt.code || body.Msg != tt.msg {
				t.Fatalf("status=%d body=%#v", resp.StatusCode, body)
			}
		})
	}
}

func TestAIEntryHandlerSubmitPassesExplicitParametersAndReturnsCreatedTasks(t *testing.T) {
	logger := zerolog.New(io.Discard)
	task := &model.Task{ID: uuid.NewString(), Type: model.PlatformArticle, Prompt: "写文章"}
	submitter := &fakeAIEntrySubmitter{
		res: service.AIEntrySubmitResult{
			Status:  service.AIEntryStatusCreated,
			Tasks:   []*model.Task{task},
			Message: "已创建任务",
		},
	}
	h := NewAIEntryHandler(submitter, nil, nil, &logger)
	userID := uuid.NewString()
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Submit(c)
	})

	body := `{
			"channel":"studio",
			"project_id":"project-1",
			"execution_profile":"effective",
			"text":"帮我写文章",
			"quantity":2,
			"image_ratio":"3:4",
		"image_capability_key":"professional",
		"execution_target":"local",
		"attachments":[{
			"type":"text",
			"text":"补充材料",
			"file_name":"brief.md",
			"content_type":"text/markdown"
		}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if submitter.req.UserID != userID || submitter.req.ProjectID != "project-1" || submitter.req.Text != "帮我写文章" {
		t.Fatalf("submitted req = %#v", submitter.req)
	}
	if submitter.req.Quantity != 2 || submitter.req.ImageRatio != "3:4" || submitter.req.ImageCapabilityKey != "professional" {
		t.Fatalf("explicit parameters = quantity %d, ratio %q, capability %q", submitter.req.Quantity, submitter.req.ImageRatio, submitter.req.ImageCapabilityKey)
	}
	if submitter.req.ExecutionTarget != model.ExecutionTargetLocal {
		t.Fatalf("execution target = %q", submitter.req.ExecutionTarget)
	}
	if len(submitter.req.Attachments) != 1 || submitter.req.Attachments[0].FileName != "brief.md" {
		t.Fatalf("attachments = %#v", submitter.req.Attachments)
	}
	var decoded Response
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	raw, _ := json.Marshal(decoded.Data)
	var data service.AIEntrySubmitResult
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Status != service.AIEntryStatusCreated || len(data.Tasks) != 1 || data.Tasks[0].ID != task.ID {
		t.Fatalf("response data = %#v", data)
	}
}

func TestAIEntryHandlerSubmitValidatesAndDefaultsQuantity(t *testing.T) {
	tests := []struct {
		name       string
		quantity   string
		wantStatus int
		wantCalls  int
		wantValue  int
	}{
		{name: "explicit zero", quantity: `,"quantity":0`, wantStatus: fiber.StatusBadRequest},
		{name: "above maximum", quantity: `,"quantity":6`, wantStatus: fiber.StatusBadRequest},
		{name: "omitted defaults to one", wantStatus: fiber.StatusOK, wantCalls: 1, wantValue: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.New(io.Discard)
			submitter := &fakeAIEntrySubmitter{res: service.AIEntrySubmitResult{Status: service.AIEntryStatusCreated}}
			h := NewAIEntryHandler(submitter, nil, nil, &logger)
			app := fiber.New()
			app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
				c.Locals("user_id", "user-1")
				return h.Submit(c)
			})

			body := `{"project_id":"project-1","execution_profile":"effective","text":"write"` + tt.quantity + `}`
			req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var decoded Response
			if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.wantStatus || submitter.calls != tt.wantCalls {
				t.Fatalf("status/calls = %d/%d, want %d/%d", resp.StatusCode, submitter.calls, tt.wantStatus, tt.wantCalls)
			}
			if tt.wantStatus == fiber.StatusBadRequest && decoded.Msg != "quantity must be between 1 and 5" {
				t.Fatalf("error = %q, want quantity boundary message", decoded.Msg)
			}
			if tt.wantCalls == 1 && submitter.req.Quantity != tt.wantValue {
				t.Fatalf("quantity = %d, want %d", submitter.req.Quantity, tt.wantValue)
			}
		})
	}
}

func TestAIEntryHandlerSubmitFinalizesUploadSession(t *testing.T) {
	logger := zerolog.New(io.Discard)
	task := &model.Task{ID: uuid.NewString(), Type: model.PlatformArticle, Prompt: "写文章"}
	submitter := &fakeAIEntrySubmitter{
		res: service.AIEntrySubmitResult{
			Status:  service.AIEntryStatusCreated,
			Tasks:   []*model.Task{task},
			Message: "已创建任务",
		},
	}
	userID := "user-1"
	session := &model.UploadSession{
		ID:          "upload-1",
		UserID:      userID,
		Purpose:     service.DirectUploadPurposeAIEntryAttachment,
		StagingKey:  "uploads/pending/user-1/upload-1/ref.png",
		FileName:    "ref.png",
		ContentType: "image/png",
		Size:        123,
		Status:      model.UploadSessionPending,
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	uploadRepo := uploadRepositoryFromSessions(t, session)
	h := NewAIEntryHandler(submitter, uploadRepo, uploadSessionStatStore(uploadRepo.UploadSessions()), &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
			"channel":"studio",
			"project_id":"project-1",
			"execution_profile":"effective",
			"text":"写文章",
		"attachments":[{
			"type":"image",
			"url":"https://attacker.example/forged.exe",
			"key":"uploads/pending/user-1/upload-1/ref.png",
			"upload_id":"upload-1",
			"file_name":"forged.exe",
			"content_type":"application/x-msdownload",
			"size":999999
		}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	assertFinalizedAsset(t, uploadRepo, "upload-1", "assets/users/user-1/upload-1/ref.png")
	if got := submitter.req.Attachments[0]; got.AssetID != "upload-1" || got.UploadID != "" || got.Key != "" || got.URL != "" ||
		got.FileName != session.FileName || got.ContentType != session.ContentType || got.Size != session.Size {
		t.Fatalf("verified storage metadata not persisted: %#v", got)
	}
}

func TestAIEntryHandlerSubmitRejectsInvalidAttachmentURL(t *testing.T) {
	logger := zerolog.New(io.Discard)
	submitter := &fakeAIEntrySubmitter{}
	h := NewAIEntryHandler(submitter, nil, nil, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", uuid.NewString())
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
		"channel":"studio",
			"project_id":"project-1",
			"execution_profile":"effective",
			"text":"写文章",
		"attachments":[{"type":"image","url":"file:///etc/passwd","file_name":"x.png"}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if submitter.req.UserID != "" {
		t.Fatalf("submitter should not be called, got %#v", submitter.req)
	}
}

func TestAIEntryHandlerSubmitRedactsAttachmentRepositoryErrors(t *testing.T) {
	logger := zerolog.New(io.Discard)
	submitter := &fakeAIEntrySubmitter{}
	repo := uploadRepositoryFromSessions(t)
	if err := repo.Close(); err != nil {
		t.Fatalf("close repository: %v", err)
	}
	h := NewAIEntryHandler(submitter, repo, nil, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
		"project_id":"project-1",
		"execution_profile":"effective",
		"text":"写文章",
		"attachments":[{
			"type":"image",
			"upload_id":"upload-1",
			"key":"uploads/pending/user-1/upload-1/ref.png"
		}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s; want 500", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "database password secret") {
		t.Fatalf("response leaked repository error: %s", body)
	}
	if submitter.req.UserID != "" {
		t.Fatalf("submitter called after repository error: %#v", submitter.req)
	}
}

func TestAIEntryHandlerSubmitClassifiesStorageStatErrors(t *testing.T) {
	const backendDetail = "OSS endpoint secret: request timed out"
	tests := []struct {
		name       string
		statErr    error
		statInfo   *storage.ObjectInfo
		wantStatus int
	}{
		{name: "not found is bad request", statErr: storage.ErrObjectNotFound, wantStatus: fiber.StatusBadRequest},
		{name: "backend failure is redacted internal error", statErr: errors.New(backendDetail), wantStatus: fiber.StatusInternalServerError},
		{name: "metadata mismatch is bad request", statInfo: &storage.ObjectInfo{Size: 124, ContentType: "image/png"}, wantStatus: fiber.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.New(io.Discard)
			submitter := &fakeAIEntrySubmitter{}
			session := &model.UploadSession{
				ID: "upload-1", UserID: "user-1", Purpose: service.DirectUploadPurposeAIEntryAttachment,
				StagingKey: "uploads/pending/user-1/upload-1/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 123,
				Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
			}
			uploadRepo := uploadRepositoryFromSessions(t, session)
			store := uploadSessionStatStore(uploadRepo.UploadSessions())
			store.statErr = tt.statErr
			store.statInfo = tt.statInfo
			h := NewAIEntryHandler(submitter, uploadRepo, store, &logger)
			app := fiber.New()
			app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
				c.Locals("user_id", "user-1")
				return h.Submit(c)
			})

			req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
					"project_id":"project-1",
					"execution_profile":"effective",
					"text":"write",
				"attachments":[{"type":"image","upload_id":"upload-1","key":"uploads/pending/user-1/upload-1/ref.png"}]
			}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, body = %s; want %d", resp.StatusCode, body, tt.wantStatus)
			}
			if strings.Contains(string(body), backendDetail) {
				t.Fatalf("response leaked storage backend detail: %s", body)
			}
			if submitter.req.UserID != "" {
				t.Fatalf("submitter called after storage error: %#v", submitter.req)
			}
		})
	}
}

func TestAIEntryHandlerSubmitRejectsExternalAttachmentURL(t *testing.T) {
	logger := zerolog.New(io.Discard)
	submitter := &fakeAIEntrySubmitter{}
	h := NewAIEntryHandler(submitter, nil, nil, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", uuid.NewString())
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
			"channel":"studio",
			"project_id":"project-1",
			"execution_profile":"effective",
			"text":"写文章",
		"attachments":[{"type":"image","url":"https://example.com/ref.png","file_name":"ref.png","content_type":"image/png"}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if submitter.req.UserID != "" {
		t.Fatalf("submitter should not be called, got %#v", submitter.req)
	}
}

func TestAIEntryHandlerSubmitRejectsExternalStagingLookingAttachmentURLWithoutUploadRepository(t *testing.T) {
	logger := zerolog.New(io.Discard)
	submitter := &fakeAIEntrySubmitter{}
	h := NewAIEntryHandler(submitter, nil, nil, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", uuid.NewString())
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
			"channel":"studio",
			"project_id":"project-1",
			"execution_profile":"effective",
			"text":"写文章",
		"attachments":[{"type":"image","url":"https://example.com/uploads/pending/user-1/upload-1/ref.png","file_name":"ref.png","content_type":"image/png"}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if submitter.req.UserID != "" {
		t.Fatalf("submitter should not be called, got %#v", submitter.req)
	}
}

func TestAIEntryHandlerSubmitRejectsMalformedUploadSessionAttachmentURL(t *testing.T) {
	logger := zerolog.New(io.Discard)
	submitter := &fakeAIEntrySubmitter{}
	h := NewAIEntryHandler(submitter, nil, nil, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", uuid.NewString())
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
			"channel":"studio",
			"project_id":"project-1",
			"execution_profile":"effective",
			"text":"写文章",
		"attachments":[{"type":"image","url":"https://example.com/uploads/pending/user-only","file_name":"ref.png","content_type":"image/png"}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if submitter.req.UserID != "" {
		t.Fatalf("submitter should not be called, got %#v", submitter.req)
	}
}

func TestAIEntryHandlerSubmitRejectsUnsupportedAttachmentMetadata(t *testing.T) {
	logger := zerolog.New(io.Discard)
	submitter := &fakeAIEntrySubmitter{}
	h := NewAIEntryHandler(submitter, nil, nil, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", uuid.NewString())
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
			"channel":"studio",
			"project_id":"project-1",
			"execution_profile":"effective",
			"text":"写文章",
		"attachments":[{"type":"document","url":"https://cdn.example.com/tool.exe","file_name":"tool.exe","content_type":"application/x-msdownload","size":1}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if submitter.req.UserID != "" {
		t.Fatalf("submitter should not be called, got %#v", submitter.req)
	}
}
