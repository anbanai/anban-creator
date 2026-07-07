package handler

import (
	"context"
	"encoding/json"
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
)

type fakeAIEntrySubmitter struct {
	req service.AIEntrySubmitRequest
	res service.AIEntrySubmitResult
	err error
}

func (f *fakeAIEntrySubmitter) Submit(_ context.Context, req service.AIEntrySubmitRequest) (*service.AIEntrySubmitResult, error) {
	f.req = req
	return &f.res, f.err
}

type aiEntryPendingRepo struct {
	upload       *model.PendingUpload
	finalizedIDs []string
}

func (r *aiEntryPendingRepo) CreatePendingUpload(context.Context, *model.PendingUpload) error {
	return nil
}

func (r *aiEntryPendingRepo) FindPendingUploadByID(_ context.Context, id string) (*model.PendingUpload, error) {
	if r.upload != nil && r.upload.ID == id {
		cp := *r.upload
		return &cp, nil
	}
	return nil, service.ErrPendingUploadNotFound
}

func (r *aiEntryPendingRepo) FinalizePendingUploads(_ context.Context, ids []string, _ time.Time) error {
	r.finalizedIDs = append(r.finalizedIDs, ids...)
	return nil
}

func (r *aiEntryPendingRepo) FindExpiredPendingUploads(context.Context, time.Time, int) ([]*model.PendingUpload, error) {
	return nil, nil
}

func (r *aiEntryPendingRepo) MarkPendingUploadExpired(context.Context, string, time.Time) error {
	return nil
}

func TestAIEntryHandlerSubmitRequiresAuth(t *testing.T) {
	logger := zerolog.New(io.Discard)
	h := NewAIEntryHandler(&fakeAIEntrySubmitter{}, nil, &logger)
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

func TestAIEntryHandlerSubmitPassesRequestAndReturnsCreatedTask(t *testing.T) {
	logger := zerolog.New(io.Discard)
	task := &model.Task{ID: uuid.NewString(), Type: model.PlatformArticle, Prompt: "写文章"}
	submitter := &fakeAIEntrySubmitter{
		res: service.AIEntrySubmitResult{
			Status:  service.AIEntryStatusCreated,
			Task:    task,
			Message: "已创建任务",
		},
	}
	h := NewAIEntryHandler(submitter, nil, &logger)
	userID := uuid.NewString()
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Submit(c)
	})

	body := `{
		"channel":"studio",
		"project_id":"project-1",
		"text":"帮我写文章",
		"execution_target":"local",
		"attachments":[{
			"type":"image",
			"url":"/api/v1/files/uploads/references/user-1/ref.png",
			"file_name":"ref.png",
			"content_type":"image/png",
			"size":123
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
	if submitter.req.ExecutionTarget != model.ExecutionTargetLocal {
		t.Fatalf("execution target = %q", submitter.req.ExecutionTarget)
	}
	if len(submitter.req.Attachments) != 1 || submitter.req.Attachments[0].FileName != "ref.png" {
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
	if data.Status != service.AIEntryStatusCreated || data.Task == nil || data.Task.ID != task.ID {
		t.Fatalf("response data = %#v", data)
	}
}

func TestAIEntryHandlerSubmitFinalizesPendingUpload(t *testing.T) {
	logger := zerolog.New(io.Discard)
	task := &model.Task{ID: uuid.NewString(), Type: model.PlatformArticle, Prompt: "写文章"}
	submitter := &fakeAIEntrySubmitter{
		res: service.AIEntrySubmitResult{
			Status:  service.AIEntryStatusCreated,
			Task:    task,
			Message: "已创建任务",
		},
	}
	userID := "user-1"
	publicURL := "https://cdn.example.com/uploads/pending/user-1/upload-1/ref.png"
	pending := &aiEntryPendingRepo{upload: &model.PendingUpload{
		ID:          "upload-1",
		UserID:      userID,
		Purpose:     service.DirectUploadPurposeAIEntryAttachment,
		Key:         "uploads/pending/user-1/upload-1/ref.png",
		PublicURL:   publicURL,
		FileName:    "ref.png",
		ContentType: "image/png",
		Size:        123,
		Status:      model.PendingUploadStatusPending,
		ExpiresAt:   time.Now().Add(time.Hour),
	}}
	h := NewAIEntryHandler(submitter, pending, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
		"channel":"studio",
		"project_id":"project-1",
		"text":"写文章",
		"attachments":[{
			"type":"image",
			"url":"https://cdn.example.com/uploads/pending/user-1/upload-1/ref.png",
			"file_name":"ref.png",
			"content_type":"image/png",
			"size":123
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
	if len(pending.finalizedIDs) != 1 || pending.finalizedIDs[0] != "upload-1" {
		t.Fatalf("finalized IDs = %#v, want upload-1", pending.finalizedIDs)
	}
}

func TestAIEntryHandlerSubmitRejectsInvalidAttachmentURL(t *testing.T) {
	logger := zerolog.New(io.Discard)
	submitter := &fakeAIEntrySubmitter{}
	h := NewAIEntryHandler(submitter, nil, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", uuid.NewString())
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
		"channel":"studio",
		"project_id":"project-1",
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

func TestAIEntryHandlerSubmitRejectsExternalAttachmentURL(t *testing.T) {
	logger := zerolog.New(io.Discard)
	submitter := &fakeAIEntrySubmitter{}
	h := NewAIEntryHandler(submitter, nil, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", uuid.NewString())
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
		"channel":"studio",
		"project_id":"project-1",
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

func TestAIEntryHandlerSubmitRejectsExternalPendingLookingAttachmentURLWithoutPendingRepo(t *testing.T) {
	logger := zerolog.New(io.Discard)
	submitter := &fakeAIEntrySubmitter{}
	h := NewAIEntryHandler(submitter, nil, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", uuid.NewString())
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
		"channel":"studio",
		"project_id":"project-1",
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

func TestAIEntryHandlerSubmitRejectsMalformedPendingAttachmentURL(t *testing.T) {
	logger := zerolog.New(io.Discard)
	submitter := &fakeAIEntrySubmitter{}
	h := NewAIEntryHandler(submitter, &aiEntryPendingRepo{}, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", uuid.NewString())
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
		"channel":"studio",
		"project_id":"project-1",
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
	h := NewAIEntryHandler(submitter, nil, &logger)
	app := fiber.New()
	app.Post("/ai-entry/submit", func(c fiber.Ctx) error {
		c.Locals("user_id", uuid.NewString())
		return h.Submit(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai-entry/submit", strings.NewReader(`{
		"channel":"studio",
		"project_id":"project-1",
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
