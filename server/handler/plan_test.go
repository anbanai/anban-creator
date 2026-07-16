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

func TestCreatePlanMontageFinalizesSourceAssetUploads(t *testing.T) {
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
		Platform: model.PlatformMontage,
		Name:     "Montage",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.PendingUploads().CreatePendingUpload(ctx, &model.PendingUpload{
		ID:          uploadID,
		UserID:      userID,
		Purpose:     service.DirectUploadPurposeMontageAsset,
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
	h.SetStore(pendingUploadStatStore(repo.PendingUploads()))
	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	req := httptest.NewRequest("POST", "/plans", strings.NewReader(`{
		"project_id": "`+projectID+`",
		"cron_expr": "0 9 * * *",
		"montage_input": {
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
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if strings.Contains(string(responseBody), "uploads/pending/") || !strings.Contains(string(responseBody), "uploads/finalized/") {
		t.Fatalf("plan persisted non-final montage URL: %s", responseBody)
	}
	upload, err := repo.PendingUploads().FindPendingUploadByID(ctx, uploadID)
	if err != nil {
		t.Fatalf("find pending upload: %v", err)
	}
	if upload.Status != model.PendingUploadStatusFinalized || upload.FinalizedKey != "uploads/finalized/"+userID+"/"+uploadID+"/clip.mp4" {
		t.Fatalf("upload identity = %#v", upload)
	}
}

func TestCreatePlanRejectsMontageAssetOnOtherPlatformWithoutFinalizing(t *testing.T) {
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
	if err := repo.PendingUploads().CreatePendingUpload(ctx, &model.PendingUpload{
		ID:          uploadID,
		UserID:      userID,
		Purpose:     service.DirectUploadPurposeMontageAsset,
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
		"montage_input": {
			"brief": "错误平台",
			"source_assets": [{"type": "video", "url": "`+assetURL+`"}]
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
	upload, err := repo.PendingUploads().FindPendingUploadByID(ctx, uploadID)
	if err != nil {
		t.Fatalf("find pending upload: %v", err)
	}
	if upload.Status != model.PendingUploadStatusPending {
		t.Fatalf("upload status = %q, want pending", upload.Status)
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

func TestCreateVideoPlanPersistsFinalReferenceURL(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID, projectID, uploadID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "planvideoref"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformVideoCreator, Name: "Video", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	refURL := seedPendingHandlerUpload(t, repo, userID, uploadID, service.DirectUploadPurposeVideoReference, "reference.mp4", "video/mp4")
	store := pendingUploadStatStore(repo.PendingUploads())
	logger := zerolog.New(io.Discard)
	h := NewPlanHandler(service.NewPlanService(repo, &logger), &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	resp := postJSON(t, app, "/plans", `{"project_id":"`+projectID+`","cron_expr":"0 9 * * *","video_creator_input":{"brief":"每日生成产品视频","references":[{"type":"video_url","url":"`+refURL+`"}]}}`)
	defer resp.Body.Close()
	data := decodeEnvelopeRawData(t, resp)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d data=%s", resp.StatusCode, mustMarshalTaskJSON(t, data))
	}
	encoded := mustMarshalTaskJSON(t, data)
	if strings.Contains(string(encoded), "uploads/pending/") || !strings.Contains(string(encoded), "uploads/finalized/") {
		t.Fatalf("video plan response contains non-final reference: %s", encoded)
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil || response.ID == "" {
		t.Fatalf("decode plan identity: %#v, %v", response, err)
	}
	plan, err := repo.Plans().FindByID(ctx, response.ID)
	if err != nil {
		t.Fatal(err)
	}
	references := plan.VideoInput.Data().References
	wantURL := "/api/v1/files/uploads/finalized/" + userID + "/" + uploadID + "/reference.mp4"
	if len(references) != 1 || references[0].URL != wantURL {
		t.Fatalf("persisted plan references = %#v", references)
	}
	upload, err := repo.PendingUploads().FindPendingUploadByID(ctx, uploadID)
	if err != nil || upload.Status != model.PendingUploadStatusFinalized || upload.FinalizedKey != "uploads/finalized/"+userID+"/"+uploadID+"/reference.mp4" {
		t.Fatalf("finalized plan upload = %#v, %v", upload, err)
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

	const foreignKey = "uploads/pending/foreign/foreign-upload/product.png"
	if err := repo.PendingUploads().CreatePendingUpload(ctx, &model.PendingUpload{
		ID: "foreign-upload", UserID: "foreign-user", Purpose: service.DirectUploadPurposeAIEntryAttachment,
		Key: foreignKey, FileName: "product.png", ContentType: "image/png", Size: 10,
		Status: model.PendingUploadStatusPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create foreign upload: %v", err)
	}
	for _, tt := range handlerAttachmentRouteRejectionCases("foreign-upload", foreignKey) {
		t.Run("create "+tt.name, func(t *testing.T) {
			body := `{"project_id":"` + projectID + `","cron_expr":"0 9 * * *","prompt":"test","input_attachments":` + tt.attachments + `}`
			req := httptest.NewRequest(http.MethodPost, "/plans", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil || resp.StatusCode != fiber.StatusBadRequest {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, err = %v, want 400: %s", resp.StatusCode, err, raw)
			}
		})
	}

	createBody := `{"project_id":"` + projectID + `","cron_expr":"0 9 * * *","prompt":"test","input_attachments":[` +
		`{"type":"image","url":"/api/v1/files/product.png","file_name":"product.png","content_type":"image/png","instruction":"  聚焦包装正面  "},` +
		`{"type":"audio","url":"/api/v1/files/voice.mp3","file_name":"voice.mp3","content_type":"audio/mpeg"},` +
		`{"type":"video","url":"/api/v1/files/demo.mp4","file_name":"demo.mp4","content_type":"video/mp4"},` +
		`{"type":"document","url":"/api/v1/files/brief.pdf","file_name":"brief.pdf","content_type":"application/pdf"},` +
		`{"type":"text","url":"/api/v1/files/notes.txt","file_name":"notes.txt","content_type":"text/plain"}]}`
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
	if len(got) != 5 || got[0].Instruction != "聚焦包装正面" {
		t.Fatalf("created attachments = %#v, want all five normalized types", got)
	}

	omitReq := httptest.NewRequest("PUT", "/plans/"+planID, strings.NewReader(`{"prompt":"updated"}`))
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
			body := `{"input_attachments":` + tt.attachments + `}`
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

	clearReq := httptest.NewRequest("PUT", "/plans/"+planID, strings.NewReader(`{"input_attachments":[]}`))
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

	replaceReq := httptest.NewRequest(http.MethodPut, "/plans/"+planID, strings.NewReader(`{"input_attachments":`+fiveTypeHandlerAttachmentsJSON+`}`))
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
}
