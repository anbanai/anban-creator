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

	serverbilling "github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

type hookedPlanRepository struct {
	repository.PlanRepository
	casCalls  int
	beforeCAS func(int)
}

func (r *hookedPlanRepository) UpdateEditableIfReferenceImageAssetID(ctx context.Context, plan *model.Plan, expectedID string, scheduleChanged bool) (bool, error) {
	r.casCalls++
	if r.beforeCAS != nil {
		r.beforeCAS(r.casCalls)
	}
	return r.PlanRepository.UpdateEditableIfReferenceImageAssetID(ctx, plan, expectedID, scheduleChanged)
}

type planHandlerRepositoryOverride struct {
	repository.Repository
	plans repository.PlanRepository
}

func (r planHandlerRepositoryOverride) Plans() repository.PlanRepository {
	return r.plans
}

func newPlanHandlerUpdateTestApp(t *testing.T, repo repository.Repository, store *projectReferenceStore) *fiber.App {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	planSvc := newHandlerPlanService(t, repo, &logger)
	referenceAssets := service.NewReferenceAssetService(repo, store, time.Now)
	planSvc.SetReferenceAssetService(referenceAssets)
	h := NewPlanHandler(planSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	h.SetReferenceAssetService(referenceAssets)
	app := fiber.New()
	app.Put("/api/v1/plans/:id", func(c fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-User-ID"))
		return h.Update(c)
	})
	return app
}

func seedPlanHandlerUser(t *testing.T, repo repository.Repository, userID string) {
	t.Helper()
	if err := repo.Users().Create(t.Context(), &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "planupdate",
		Tier:       model.TierFree,
	}); err != nil {
		t.Fatalf("create plan owner: %v", err)
	}
}

func TestCreatePlanRejectsMissingExecutionProfile(t *testing.T) {
	logger := zerolog.New(io.Discard)
	h := NewPlanHandler(nil, &logger)
	app := fiber.New()
	app.Post("/plans", h.Create)

	req := httptest.NewRequest(http.MethodPost, "/plans", strings.NewReader(`{"project_id":"project-id","cron_expr":"0 9 * * *"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "execution_profile is required") {
		t.Fatalf("body = %s, want execution_profile validation error", body)
	}
}

func TestCreatePlanRejectsAgentInputWhenPackHasNoSchema(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "planagentinput"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}

	logger := zerolog.New(io.Discard)
	h := NewPlanHandler(newHandlerPlanService(t, repo, &logger), &logger)
	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })

	resp := postJSON(t, app, "/plans", `{"project_id":"`+projectID+`","execution_profile":"effective","cron_expr":"0 9 * * *","agent_input":{"tone":"concise"}}`)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(string(body), "invalid_agent_input") {
		t.Fatalf("status/body = %d/%s, want 400 invalid_agent_input", resp.StatusCode, body)
	}
}

func TestPlanHandlerScheduleRecommendation(t *testing.T) {
	logger := zerolog.New(io.Discard)
	repo := repository.New(setupTaskHandlerTestDB(t))
	rule := serverbilling.TaskTimePricing{Timezone: "Asia/Shanghai", OffPeakWindows: []serverbilling.TimeWindow{{Start: "12:00", End: "13:00"}}, OffPeakRatePercent: 80}
	h := NewPlanHandler(nil, &logger)
	h.SetScheduleRecommendationService(service.NewScheduleRecommendationService(repo, "retail-test", rule, &logger))
	app := fiber.New()
	app.Get("/plans/schedule-recommendation", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-a")
		return h.ScheduleRecommendation(c)
	})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/plans/schedule-recommendation", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload struct {
		Data service.ScheduleRecommendation `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || payload.Data.Time == "" || payload.Data.Timezone != "Asia/Shanghai" || payload.Data.GranularityMinutes != 15 || !payload.Data.LoadBalanced {
		t.Fatalf("status=%d recommendation=%#v", resp.StatusCode, payload.Data)
	}
}

// TestCreatePlan_ArticleImageTogglesPersist verifies the plan handler→service→
// model→DB round-trip persists an explicit `false` for both article image
// toggles. Same regression guard as TestCreateTask_ArticleImageTogglesPersist
// (*bool / gorm:"default:true" mitigation + handler wiring omission): the
// plan-level toggles propagate to spawned tasks via CreateFromPlan, so a plan
// created with cover/content off must persist those choices. The value is
// re-read from the repo to assert the persisted state.
func TestCreatePlan_ArticleImageTogglesPersist(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "plan-toggles@example.com",
		Password:   "hashed",
		InviteCode: "plantoggles",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	planSvc := newHandlerPlanService(t, repo, &logger)
	h := NewPlanHandler(planSvc, &logger)

	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	body := `{"execution_profile":"effective","project_id":"` + projectID + `","cron_expr":"0 9 * * *","prompt":"计划开关持久化测试","article_with_cover":false,"article_with_content_images":false}`
	req := httptest.NewRequest("POST", "/plans", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	plans, err := repo.Plans().FindByUserID(ctx, userID, projectID, 0, 10)
	if err != nil {
		t.Fatalf("find plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	got := plans[0]
	for label, ptr := range map[string]*bool{
		"ArticleWithCover":         got.ArticleWithCover,
		"ArticleWithContentImages": got.ArticleWithContentImages,
	} {
		switch {
		case ptr == nil:
			t.Errorf("%s = nil, want non-nil false", label)
		case *ptr:
			t.Errorf("%s = true, want false", label)
		}
	}
}

func TestCreatePlanMontageFinalizesSourceAssetUploads(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	uploadID := uuid.New().String()
	stagingURL := "https://cdn.example.com/uploads/pending/" + userID + "/" + uploadID + "/clip.mp4"
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "omplanasset",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformMontage,
		Name:     "Montage",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID:         uploadID,
		UserID:     userID,
		Purpose:    service.DirectUploadPurposeMontageAsset,
		StagingKey: "uploads/pending/" + userID + "/" + uploadID + "/clip.mp4",

		FileName:    "clip.mp4",
		ContentType: "video/mp4",
		Size:        1234,
		Status:      model.UploadSessionPending,
		ExpiresAt:   time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("create upload session: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	planSvc := newHandlerPlanService(t, repo, &logger)
	h := NewPlanHandler(planSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(uploadSessionStatStore(repo.UploadSessions()))
	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	req := httptest.NewRequest("POST", "/plans", strings.NewReader(`{
		"execution_profile":"effective","project_id": "`+projectID+`",
		"cron_expr": "0 9 * * *",
		"montage_input": {
			"brief": "每天剪一条发布会短片",
			"source_assets": [{"type": "video", "url": "`+stagingURL+`"}]
		}
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if strings.Contains(string(responseBody), "uploads/pending/") || !strings.Contains(string(responseBody), "assets/users/") {
		t.Fatalf("plan persisted non-final montage URL: %s", responseBody)
	}
	assertFinalizedAsset(t, repo, uploadID, "assets/users/"+userID+"/"+uploadID+"/clip.mp4")
}

func TestCreatePlanRejectsMontageAssetOnOtherPlatformWithoutFinalizing(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	uploadID := uuid.New().String()
	stagingURL := "https://cdn.example.com/uploads/pending/" + userID + "/" + uploadID + "/clip.mp4"
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "omplanwrong",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID:         uploadID,
		UserID:     userID,
		Purpose:    service.DirectUploadPurposeMontageAsset,
		StagingKey: "uploads/pending/" + userID + "/" + uploadID + "/clip.mp4",

		FileName:    "clip.mp4",
		ContentType: "video/mp4",
		Size:        1234,
		Status:      model.UploadSessionPending,
		ExpiresAt:   time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("create upload session: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	planSvc := newHandlerPlanService(t, repo, &logger)
	h := NewPlanHandler(planSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	req := httptest.NewRequest("POST", "/plans", strings.NewReader(`{
		"execution_profile":"effective","project_id": "`+projectID+`",
		"cron_expr": "0 9 * * *",
		"montage_input": {
			"brief": "错误平台",
			"source_assets": [{"type": "video", "url": "`+stagingURL+`"}]
		}
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400 body=%s", resp.StatusCode, body)
	}
	session, err := repo.UploadSessions().FindByID(ctx, uploadID)
	if err != nil {
		t.Fatalf("find upload session: %v", err)
	}
	if session.Status != model.UploadSessionPending || session.AssetID != "" {
		t.Fatalf("upload session changed: %#v", session)
	}
}

func TestCreatePlan_MomentsProjectReturnsBadRequest(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "moments-plan@example.com",
		Password:   "hashed",
		InviteCode: "momplan",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformMoments,
		Name:     "Moments",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	planSvc := newHandlerPlanService(t, repo, &logger)
	h := NewPlanHandler(planSvc, &logger)

	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	body := `{"execution_profile":"effective","project_id":"` + projectID + `","cron_expr":"0 9 * * *","prompt":"每日朋友圈"}`
	req := httptest.NewRequest("POST", "/plans", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), "plans are not supported for moments projects") {
		t.Fatalf("response = %s, want unsupported moments message", raw)
	}
}

func TestPlanHandlerInputAttachmentSemantics(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "planhandlerattachments",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	planSvc := newHandlerPlanService(t, repo, &logger)
	h := NewPlanHandler(planSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})
	app.Put("/plans/:id", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Update(c)
	})

	const foreignKey = "uploads/pending/foreign/foreign-upload/product.png"
	if err := repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID: "foreign-upload", UserID: "foreign-user", Purpose: service.DirectUploadPurposeAIEntryAttachment,
		StagingKey: foreignKey, FileName: "product.png", ContentType: "image/png", Size: 10,
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create foreign upload: %v", err)
	}
	for _, tt := range handlerAttachmentRouteRejectionCases("foreign-upload", foreignKey) {
		t.Run("create "+tt.name, func(t *testing.T) {
			body := `{"execution_profile":"effective","project_id":"` + projectID + `","cron_expr":"0 9 * * *","prompt":"test","input_attachments":` + tt.attachments + `}`
			req := httptest.NewRequest(http.MethodPost, "/plans", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil || resp.StatusCode != fiber.StatusBadRequest {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, err = %v, want 400: %s", resp.StatusCode, err, raw)
			}
		})
	}

	createBody := `{"execution_profile":"effective","project_id":"` + projectID + `","cron_expr":"0 9 * * *","prompt":"test","input_attachments":[` +
		`{"type":"image","url":"/api/v1/files/product.png","file_name":"product.png","content_type":"image/png","instruction":"  聚焦包装正面  "},` +
		`{"type":"audio","url":"/api/v1/files/voice.ogg","file_name":"voice.ogg","content_type":"application/ogg"},` +
		`{"type":"video","url":"/api/v1/files/demo.mp4","file_name":"demo.mp4","content_type":"video/mp4"},` +
		`{"type":"document","url":"/api/v1/files/brief.pdf","file_name":"brief.pdf","content_type":"application/pdf"},` +
		`{"type":"text","url":"/api/v1/files/notes.csv","file_name":"notes.csv","content_type":"application/csv"}]}`
	createReq := httptest.NewRequest("POST", "/plans", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := app.Test(createReq)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	if createResp.StatusCode != fiber.StatusOK {
		raw, _ := io.ReadAll(createResp.Body)
		t.Fatalf("create status = %d, want 200; response = %s", createResp.StatusCode, raw)
	}

	plans, err := repo.Plans().FindByUserID(ctx, userID, projectID, 0, 10)
	if err != nil {
		t.Fatalf("find plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans len = %d, want 1", len(plans))
	}
	planID := plans[0].ID
	got := plans[0].InputAttachments.Data()
	if len(got) != 5 || got[0].Instruction != "聚焦包装正面" || got[1].ContentType != "application/ogg" || got[4].ContentType != "application/csv" {
		t.Fatalf("created attachments = %#v, want all five normalized types", got)
	}

	omitReq := httptest.NewRequest("PUT", "/plans/"+planID, strings.NewReader(`{"execution_profile":"effective","prompt":"updated"}`))
	omitReq.Header.Set("Content-Type", "application/json")
	omitResp, err := app.Test(omitReq)
	if err != nil {
		t.Fatalf("omitted update request failed: %v", err)
	}
	if omitResp.StatusCode != fiber.StatusOK {
		t.Fatalf("omitted update status = %d, want 200", omitResp.StatusCode)
	}
	retained, err := repo.Plans().FindByID(ctx, planID)
	if err != nil {
		t.Fatalf("find retained plan: %v", err)
	}
	if got := retained.InputAttachments.Data(); len(got) != 5 || got[0].Instruction != "聚焦包装正面" {
		t.Fatalf("attachments after omitted update = %#v, want retained attachments", got)
	}

	for _, tt := range handlerAttachmentRouteRejectionCases("foreign-upload", foreignKey) {
		t.Run("update "+tt.name, func(t *testing.T) {
			body := `{"execution_profile":"effective","input_attachments":` + tt.attachments + `}`
			req := httptest.NewRequest(http.MethodPut, "/plans/"+planID, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil || resp.StatusCode != fiber.StatusBadRequest {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, err = %v, want 400: %s", resp.StatusCode, err, raw)
			}
			unchanged, findErr := repo.Plans().FindByID(ctx, planID)
			if findErr != nil || len(unchanged.InputAttachments.Data()) != 5 {
				t.Fatalf("invalid update changed attachments = %#v, err = %v", unchanged, findErr)
			}
		})
	}

	clearReq := httptest.NewRequest("PUT", "/plans/"+planID, strings.NewReader(`{"execution_profile":"effective","input_attachments":[]}`))
	clearReq.Header.Set("Content-Type", "application/json")
	clearResp, err := app.Test(clearReq)
	if err != nil {
		t.Fatalf("clear update request failed: %v", err)
	}
	if clearResp.StatusCode != fiber.StatusOK {
		t.Fatalf("clear update status = %d, want 200", clearResp.StatusCode)
	}
	cleared, err := repo.Plans().FindByID(ctx, planID)
	if err != nil {
		t.Fatalf("find cleared plan: %v", err)
	}
	if got := cleared.InputAttachments.Data(); len(got) != 0 {
		t.Fatalf("attachments after explicit empty update = %#v, want empty", got)
	}

	replaceReq := httptest.NewRequest(http.MethodPut, "/plans/"+planID, strings.NewReader(`{"execution_profile":"effective","input_attachments":`+fiveTypeHandlerAttachmentsJSON+`}`))
	replaceReq.Header.Set("Content-Type", "application/json")
	replaceResp, err := app.Test(replaceReq)
	if err != nil || replaceResp.StatusCode != fiber.StatusOK {
		raw, _ := io.ReadAll(replaceResp.Body)
		t.Fatalf("replace status = %d, err = %v, want 200: %s", replaceResp.StatusCode, err, raw)
	}
	replaced, err := repo.Plans().FindByID(ctx, planID)
	if err != nil || len(replaced.InputAttachments.Data()) != 5 {
		t.Fatalf("replacement attachments = %#v, err = %v", replaced, err)
	}
	replacedAttachments := replaced.InputAttachments.Data()
	if replacedAttachments[1].ContentType != "application/ogg" || replacedAttachments[4].ContentType != "application/csv" {
		t.Fatalf("replacement canonical application MIME attachments = %#v", replacedAttachments)
	}
}

func TestPlanUpdateReferenceOmissionRetriesCASAndReturnsMatchingView(t *testing.T) {
	base := repository.New(setupTaskHandlerTestDB(t))
	userID := uuid.NewString()
	planID := uuid.NewString()
	seedPlanHandlerUser(t, base, userID)
	for _, assetID := range []string{"asset-a", "asset-b"} {
		if err := base.Assets().Create(t.Context(), &model.Asset{
			ID: assetID, UserID: userID, Purpose: service.DirectUploadPurposeTaskReference,
			StorageKey: "assets/users/" + userID + "/" + assetID + "/ref.png", FileName: "ref.png",
			ContentType: "image/png", Size: 3, ETag: "etag-" + assetID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := base.Plans().Create(t.Context(), &model.Plan{
		ID: planID, UserID: userID, Type: model.PlatformArticle, Prompt: "before",
		CronExpr: "0 9 * * *", ReferenceImageAssetID: "asset-a", Status: model.PlanStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	hooked := &hookedPlanRepository{PlanRepository: base.Plans()}
	hooked.beforeCAS = func(call int) {
		if call != 1 {
			return
		}
		plan, err := base.Plans().FindByID(t.Context(), planID)
		if err != nil {
			t.Fatal(err)
		}
		plan.ReferenceImageAssetID = "asset-b"
		if err := base.Plans().Update(t.Context(), plan); err != nil {
			t.Fatal(err)
		}
	}
	repo := planHandlerRepositoryOverride{Repository: base, plans: hooked}
	store := &projectReferenceStore{fakeStorageProvider: uploadSessionStatStore(base.UploadSessions())}
	app := newPlanHandlerUpdateTestApp(t, repo, store)

	resp := doRequest(t, app, http.MethodPut, "/api/v1/plans/"+planID, userID, map[string]any{"execution_profile": "effective", "prompt": "after"})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"asset_id":"asset-b"`) || strings.Contains(string(body), `"asset_id":"asset-a"`) {
		t.Fatalf("response does not match retried reference: %s", body)
	}
	wantSigned := []string{
		"assets/users/" + userID + "/asset-a/ref.png",
		"assets/users/" + userID + "/asset-b/ref.png",
	}
	if strings.Join(store.signedKeys, "|") != strings.Join(wantSigned, "|") {
		t.Fatalf("signed keys=%#v want %#v", store.signedKeys, wantSigned)
	}
	persisted, err := base.Plans().FindByID(t.Context(), planID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Prompt != "after" || persisted.ReferenceImageAssetID != "asset-b" {
		t.Fatalf("persisted plan prompt=%q reference=%q", persisted.Prompt, persisted.ReferenceImageAssetID)
	}
}

func TestPlanUpdateReferenceOmissionReturnsConflictAfterBoundedCASRetries(t *testing.T) {
	base := repository.New(setupTaskHandlerTestDB(t))
	userID := uuid.NewString()
	planID := uuid.NewString()
	seedPlanHandlerUser(t, base, userID)
	assetIDs := []string{"asset-a", "asset-b", "asset-c", "asset-d"}
	for _, assetID := range assetIDs {
		if err := base.Assets().Create(t.Context(), &model.Asset{
			ID: assetID, UserID: userID, Purpose: service.DirectUploadPurposeTaskReference,
			StorageKey: "assets/users/" + userID + "/" + assetID + "/ref.png", FileName: "ref.png",
			ContentType: "image/png", Size: 3, ETag: "etag-" + assetID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := base.Plans().Create(t.Context(), &model.Plan{
		ID: planID, UserID: userID, Type: model.PlatformArticle, Prompt: "before",
		CronExpr: "0 9 * * *", ReferenceImageAssetID: assetIDs[0], Status: model.PlanStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	hooked := &hookedPlanRepository{PlanRepository: base.Plans()}
	hooked.beforeCAS = func(call int) {
		plan, err := base.Plans().FindByID(t.Context(), planID)
		if err != nil {
			t.Fatal(err)
		}
		plan.ReferenceImageAssetID = assetIDs[call]
		if err := base.Plans().Update(t.Context(), plan); err != nil {
			t.Fatal(err)
		}
	}
	repo := planHandlerRepositoryOverride{Repository: base, plans: hooked}
	store := &projectReferenceStore{fakeStorageProvider: uploadSessionStatStore(base.UploadSessions())}
	app := newPlanHandlerUpdateTestApp(t, repo, store)

	resp := doRequest(t, app, http.MethodPut, "/api/v1/plans/"+planID, userID, map[string]any{"execution_profile": "effective", "prompt": "must-not-write"})
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status=%d want 409 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	if hooked.casCalls != 3 {
		t.Fatalf("CAS calls=%d want 3", hooked.casCalls)
	}
	wantSigned := []string{
		"assets/users/" + userID + "/asset-a/ref.png",
		"assets/users/" + userID + "/asset-b/ref.png",
		"assets/users/" + userID + "/asset-c/ref.png",
	}
	if strings.Join(store.signedKeys, "|") != strings.Join(wantSigned, "|") {
		t.Fatalf("signed keys=%#v want %#v", store.signedKeys, wantSigned)
	}
	persisted, err := base.Plans().FindByID(t.Context(), planID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Prompt != "before" || persisted.ReferenceImageAssetID != "asset-d" {
		t.Fatalf("request mutated plan prompt=%q reference=%q", persisted.Prompt, persisted.ReferenceImageAssetID)
	}
}

func TestPlanUpdateReferenceOmissionMatchesNullReferenceRow(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	base := repository.New(db)
	userID := uuid.NewString()
	planID := uuid.NewString()
	seedPlanHandlerUser(t, base, userID)
	if err := base.Plans().Create(t.Context(), &model.Plan{
		ID: planID, UserID: userID, Type: model.PlatformArticle, Prompt: "before",
		CronExpr: "0 9 * * *", Status: model.PlanStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE plans SET reference_image_asset_id = NULL WHERE id = ?", planID).Error; err != nil {
		t.Fatal(err)
	}
	store := &projectReferenceStore{fakeStorageProvider: uploadSessionStatStore(base.UploadSessions())}
	app := newPlanHandlerUpdateTestApp(t, base, store)

	resp := doRequest(t, app, http.MethodPut, "/api/v1/plans/"+planID, userID, map[string]any{"execution_profile": "effective", "prompt": "after"})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	persisted, err := base.Plans().FindByID(t.Context(), planID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Prompt != "after" || persisted.ReferenceImageAssetID != "" {
		t.Fatalf("persisted plan prompt=%q reference=%q", persisted.Prompt, persisted.ReferenceImageAssetID)
	}
	if len(store.signedKeys) != 0 {
		t.Fatalf("empty reference signed keys=%#v", store.signedKeys)
	}
}
