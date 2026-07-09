package handler

import (
	"context"
	"encoding/json"
	"io"
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
)

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
	planSvc := service.NewPlanService(repo, &logger)
	h := NewPlanHandler(planSvc, &logger)

	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	body := `{"project_id":"` + projectID + `","cron_expr":"0 9 * * *","prompt":"计划开关持久化测试","article_with_cover":false,"article_with_content_images":false}`
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

func TestCreatePlan_VideoPlanAllowsLowBalanceWithoutLegacyMinimumGate(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:             userID,
		Email:          "video-plan-balance@example.com",
		Password:       "hashed",
		InviteCode:     "videoplanbalance",
		CreditsBalance: 99_999,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	project := &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformVideoCreator,
		Name:     "Video",
		Status:   model.ProjectStatusActive,
	}
	watermark := false
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    service.VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
		Watermark:  &watermark,
		Preflight:  true,
	})
	project.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   15,
	})
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	creditSvc := service.NewCreditService(repo, nil, &logger)
	planSvc := service.NewPlanService(repo, &logger)
	planSvc.SetCreditService(creditSvc)
	planSvc.SetVideoCatalogAndCreditMultiplier(service.DefaultVideoModelCatalog(), 1000)
	h := NewPlanHandler(planSvc, &logger)

	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	body := `{"project_id":"` + projectID + `","cron_expr":"0 9 * * *","prompt":"计划生成视频"}`
	req := httptest.NewRequest("POST", "/plans", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestCreatePlanOpenMontageFinalizesSourceAssetUploads(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	uploadID := uuid.New().String()
	assetURL := "https://cdn.example.com/uploads/pending/" + userID + "/" + uploadID + "/clip.mp4"
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
		Platform: model.PlatformOpenMontage,
		Name:     "OpenMontage",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.PendingUploads().CreatePendingUpload(ctx, &model.PendingUpload{
		ID:          uploadID,
		UserID:      userID,
		Purpose:     service.DirectUploadPurposeOpenMontageAsset,
		Key:         "uploads/pending/" + userID + "/" + uploadID + "/clip.mp4",
		PublicURL:   assetURL,
		FileName:    "clip.mp4",
		ContentType: "video/mp4",
		Size:        1234,
		Status:      model.PendingUploadStatusPending,
		ExpiresAt:   time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("create pending upload: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	planSvc := service.NewPlanService(repo, &logger)
	h := NewPlanHandler(planSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	req := httptest.NewRequest("POST", "/plans", strings.NewReader(`{
		"project_id": "`+projectID+`",
		"cron_expr": "0 9 * * *",
		"openmontage_input": {
			"brief": "每天剪一条发布会短片",
			"source_assets": [{"type": "video", "url": "`+assetURL+`"}]
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
	upload, err := repo.PendingUploads().FindPendingUploadByID(ctx, uploadID)
	if err != nil {
		t.Fatalf("find pending upload: %v", err)
	}
	if upload.Status != model.PendingUploadStatusFinalized {
		t.Fatalf("upload status = %q, want finalized", upload.Status)
	}
}

func TestPlanHandler_VideoCreatorSplitInputContract(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "video-plan-split@example.com",
		Password:   "hashed",
		InviteCode: "videoplansplit",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformVideoCreator,
		Name:     "Video Creator",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	planSvc := service.NewPlanService(repo, &logger)
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

	createBody := `{"project_id":"` + projectID + `","cron_expr":"0 9 * * *","video_creator_input":{"brief":"每日生成新品短视频","hard_constraints":{"duration":12}}}`
	createResp := postJSON(t, app, "/plans", createBody)
	if createResp.StatusCode != fiber.StatusOK {
		raw, _ := io.ReadAll(createResp.Body)
		t.Fatalf("create status = %d, want 200 body=%s", createResp.StatusCode, raw)
	}
	createData := decodeEnvelopeRawData(t, createResp)
	if _, ok := createData["video_creator_input"]; !ok {
		t.Fatalf("create response missing video_creator_input: %s", string(mustMarshalTaskJSON(t, createData)))
	}
	if _, ok := createData["video_input"]; ok {
		t.Fatalf("create response exposed generic video_input: %s", string(mustMarshalTaskJSON(t, createData)))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(mustMarshalTaskJSON(t, createData), &created); err != nil {
		t.Fatalf("decode created plan: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("created plan id is empty")
	}

	updateBody := `{"video_creator_input":{"brief":"改成周更产品视频","hard_constraints":{"ratio":"16:9"}}}`
	updateReq := httptest.NewRequest("PUT", "/plans/"+created.ID, strings.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp, err := app.Test(updateReq)
	if err != nil {
		t.Fatalf("update request failed: %v", err)
	}
	if updateResp.StatusCode != fiber.StatusOK {
		raw, _ := io.ReadAll(updateResp.Body)
		t.Fatalf("update status = %d, want 200 body=%s", updateResp.StatusCode, raw)
	}
	updateData := decodeEnvelopeRawData(t, updateResp)
	if _, ok := updateData["video_creator_input"]; !ok {
		t.Fatalf("update response missing video_creator_input: %s", string(mustMarshalTaskJSON(t, updateData)))
	}
	if _, ok := updateData["video_input"]; ok {
		t.Fatalf("update response exposed generic video_input: %s", string(mustMarshalTaskJSON(t, updateData)))
	}

	legacyBody := `{"project_id":"` + projectID + `","cron_expr":"0 10 * * *","video_input":{"brief":"旧字段"},"video_config":{"duration":9}}`
	legacyResp := postJSON(t, app, "/plans", legacyBody)
	if legacyResp.StatusCode != fiber.StatusBadRequest {
		raw, _ := io.ReadAll(legacyResp.Body)
		t.Fatalf("legacy create status = %d, want 400 body=%s", legacyResp.StatusCode, raw)
	}

	legacyUpdateReq := httptest.NewRequest("PUT", "/plans/"+created.ID, strings.NewReader(`{"video_input":{"brief":"旧字段更新"}}`))
	legacyUpdateReq.Header.Set("Content-Type", "application/json")
	legacyUpdateResp, err := app.Test(legacyUpdateReq)
	if err != nil {
		t.Fatalf("legacy update request failed: %v", err)
	}
	if legacyUpdateResp.StatusCode != fiber.StatusBadRequest {
		raw, _ := io.ReadAll(legacyUpdateResp.Body)
		t.Fatalf("legacy update status = %d, want 400 body=%s", legacyUpdateResp.StatusCode, raw)
	}

	editorBody := `{"project_id":"` + projectID + `","cron_expr":"0 11 * * *","video_editor_input":{"brief":"计划不支持剪辑输入"}}`
	editorResp := postJSON(t, app, "/plans", editorBody)
	if editorResp.StatusCode != fiber.StatusBadRequest {
		raw, _ := io.ReadAll(editorResp.Body)
		t.Fatalf("editor create status = %d, want 400 body=%s", editorResp.StatusCode, raw)
	}

	editorUpdateReq := httptest.NewRequest("PUT", "/plans/"+created.ID, strings.NewReader(`{"video_editor_input":{"brief":"计划不支持剪辑更新"}}`))
	editorUpdateReq.Header.Set("Content-Type", "application/json")
	editorUpdateResp, err := app.Test(editorUpdateReq)
	if err != nil {
		t.Fatalf("editor update request failed: %v", err)
	}
	if editorUpdateResp.StatusCode != fiber.StatusBadRequest {
		raw, _ := io.ReadAll(editorUpdateResp.Body)
		t.Fatalf("editor update status = %d, want 400 body=%s", editorUpdateResp.StatusCode, raw)
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
	planSvc := service.NewPlanService(repo, &logger)
	h := NewPlanHandler(planSvc, &logger)

	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	body := `{"project_id":"` + projectID + `","cron_expr":"0 9 * * *","prompt":"每日朋友圈"}`
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
