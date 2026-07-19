package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	appdraft "github.com/anbanai/anban-creator/app/draft"
	"github.com/anbanai/anban-creator/app/wechat"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type stubDraftClient struct {
	listDraftsErr    error
	listPublishedErr error
}

func (c *stubDraftClient) CreateDraft([]appdraft.Article) (*appdraft.DraftResult, error) {
	return &appdraft.DraftResult{MediaID: "media-id"}, nil
}

func (c *stubDraftClient) ListDrafts(offset, count int64) (*appdraft.ListDraftsResult, error) {
	if c.listDraftsErr != nil {
		return nil, c.listDraftsErr
	}
	return &appdraft.ListDraftsResult{TotalCount: 1, ItemCount: 1}, nil
}

func (c *stubDraftClient) ListPublished(offset, count int64) (*appdraft.ListPublishedResult, error) {
	if c.listPublishedErr != nil {
		return nil, c.listPublishedErr
	}
	return &appdraft.ListPublishedResult{TotalCount: 1, ItemCount: 1}, nil
}

// setupTestDB creates an in-memory SQLite database for testing.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Plan{}, &model.Task{}, &model.User{},
		&model.LoginSession{}, &model.TaskFile{}, &model.Project{},
		&model.Asset{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

// setupTestPlanService creates a PlanService with a test database.
func setupTestPlanService(t *testing.T) (*PlanService, repository.Repository) {
	t.Helper()
	db := setupTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
	svc := NewPlanService(repo, &logger)
	svc.SetVideoCatalogAndCreditMultiplier(DefaultVideoModelCatalog(), 1000)
	return svc, repo
}

// createTestProject creates a test project for the given user and returns its ID.
func createTestProject(t *testing.T, repo repository.Repository, userID, platform string) string {
	t.Helper()
	ch := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: platform,
		Name:     "Test Project " + platform,
		Status:   model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(context.Background(), ch); err != nil {
		t.Fatalf("create test project: %v", err)
	}
	return ch.ID
}

func createTestVideoProject(t *testing.T, repo repository.Repository, userID string) string {
	t.Helper()
	watermark := false
	ch := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideoCreator,
		Name:     "Test Video Project",
		Status:   model.ProjectStatusActive,
	}
	ch.SetVideoDefaults(model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
		Watermark:  &watermark,
		Preflight:  true,
	})
	ch.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0", "seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "1080p",
		MaxDuration:   15,
	})
	if err := repo.Projects().Create(context.Background(), ch); err != nil {
		t.Fatalf("create test video project: %v", err)
	}
	return ch.ID
}

func createTestWechatProject(t *testing.T, repo repository.Repository, userID string) string {
	t.Helper()
	ch := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Test WeChat Project",
		Status:   model.ProjectStatusActive,
		Config: model.ProjectConfig{
			WechatAppID:  "app-id",
			WechatSecret: "secret",
		},
	}
	if err := repo.Projects().Create(context.Background(), ch); err != nil {
		t.Fatalf("create test wechat project: %v", err)
	}
	return ch.ID
}

func TestPlanService_Create(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	// Create test projects for the user.
	chID1 := createTestProject(t, repo, "user-1", model.PlatformSeednote)
	chID2 := createTestProject(t, repo, "user-1", model.PlatformArticle)

	tests := []struct {
		name      string
		projectID string
		cronExpr  string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "valid plan",
			projectID: chID1,
			cronExpr:  "0 9 * * 1-5",
			wantErr:   false,
		},
		{
			name:      "every minute",
			projectID: chID2,
			cronExpr:  "* * * * *",
			wantErr:   false,
		},
		{
			name:      "empty project_id",
			projectID: "",
			cronExpr:  "0 9 * * *",
			wantErr:   true,
			errSubstr: "project_id is required",
		},
		{
			name:      "empty cron",
			projectID: chID1,
			cronExpr:  "",
			wantErr:   true,
			errSubstr: "cron_expr is required",
		},
		{
			name:      "invalid cron",
			projectID: chID1,
			cronExpr:  "invalid cron",
			wantErr:   true,
			errSubstr: "invalid cron expression",
		},
		{
			name:      "invalid cron fields",
			projectID: chID1,
			cronExpr:  "60 25 * * *",
			wantErr:   true,
			errSubstr: "invalid cron expression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := svc.Create(ctx, CreatePlanParams{
				UserID:    "user-1",
				ProjectID: tt.projectID,
				CronExpr:  tt.cronExpr,
				Prompt:    "topic hint",
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if plan.ID == "" {
				t.Error("expected non-empty plan ID")
			}
			if plan.Status != model.PlanStatusActive {
				t.Errorf("expected status %q, got %q", model.PlanStatusActive, plan.Status)
			}
			if plan.NextRunAt == nil {
				t.Error("expected non-nil next_run_at")
			}
			if plan.CronExpr != tt.cronExpr {
				t.Errorf("expected cron_expr %q, got %q", tt.cronExpr, plan.CronExpr)
			}
			if plan.ProjectID != tt.projectID {
				t.Errorf("expected project_id %q, got %q", tt.projectID, plan.ProjectID)
			}
		})
	}
}

func TestPlanServiceCreateMontagePlanStoresInput(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	userID := "user-om-plan"
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)

	plan, err := svc.Create(ctx, CreatePlanParams{
		UserID:    userID,
		ProjectID: projectID,
		CronExpr:  "0 10 * * *",
		MontageInput: &model.MontageInput{
			Brief:       "每日生成新品短片",
			PipelineKey: "default",
			Preferences: model.MontagePreferences{
				AspectRatio:     "9:16",
				DurationSeconds: 30,
			},
		},
	})
	if err != nil {
		t.Fatalf("Create montage plan: %v", err)
	}
	if plan.Type != model.PlatformMontage {
		t.Fatalf("plan type = %q, want montage", plan.Type)
	}
	got := plan.MontageInput.Data()
	if got.Brief != "每日生成新品短片" || got.PipelineKey != "default" {
		t.Fatalf("montage input = %#v", got)
	}
	if got.Preferences.AspectRatio != "9:16" || got.Preferences.DurationSeconds != 30 {
		t.Fatalf("preferences = %#v", got.Preferences)
	}
}

func TestPlanServiceUpdateMontagePlanStoresInput(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	userID := "user-om-plan-update"
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)

	plan, err := svc.Create(ctx, CreatePlanParams{
		UserID:    userID,
		ProjectID: projectID,
		CronExpr:  "0 10 * * *",
		MontageInput: &model.MontageInput{
			Brief:       "旧短片",
			PipelineKey: "default",
		},
	})
	if err != nil {
		t.Fatalf("Create montage plan: %v", err)
	}

	updated, err := svc.Update(ctx, UpdatePlanParams{
		ID: plan.ID,
		MontageInput: &model.MontageInput{
			Brief:       "更新后的短片",
			PipelineKey: "social-short",
			Preferences: model.MontagePreferences{
				AspectRatio:     "1:1",
				DurationSeconds: 20,
			},
		},
	})
	if err != nil {
		t.Fatalf("Update montage plan: %v", err)
	}
	got := updated.MontageInput.Data()
	if got.Brief != "更新后的短片" || got.PipelineKey != "social-short" {
		t.Fatalf("montage input = %#v", got)
	}
	if got.Preferences.AspectRatio != "1:1" || got.Preferences.DurationSeconds != 20 {
		t.Fatalf("preferences = %#v", got.Preferences)
	}

	updated, err = svc.Update(ctx, UpdatePlanParams{ID: plan.ID, Prompt: "只改提示"})
	if err != nil {
		t.Fatalf("Update without montage input: %v", err)
	}
	got = updated.MontageInput.Data()
	if got.Brief != "更新后的短片" || got.PipelineKey != "social-short" {
		t.Fatalf("montage input changed when omitted: %#v", got)
	}
}

func TestPlanServiceRejectsMontageInputForOtherPlatforms(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	userID := "user-om-plan-reject"
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	_, err := svc.Create(ctx, CreatePlanParams{
		UserID:    userID,
		ProjectID: projectID,
		CronExpr:  "0 10 * * *",
		MontageInput: &model.MontageInput{
			Brief: "错误平台",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "montage_input can only be set on montage plans") {
		t.Fatalf("Create error = %v, want montage input rejection", err)
	}

	plan, err := svc.Create(ctx, CreatePlanParams{
		UserID:    userID,
		ProjectID: projectID,
		CronExpr:  "0 10 * * *",
		Prompt:    "正常文章计划",
	})
	if err != nil {
		t.Fatalf("Create article plan: %v", err)
	}
	_, err = svc.Update(ctx, UpdatePlanParams{
		ID: plan.ID,
		MontageInput: &model.MontageInput{
			Brief: "错误平台更新",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "montage_input can only be set on montage plans") {
		t.Fatalf("Update error = %v, want montage input rejection", err)
	}
}

func TestPlanService_Create_SkipReferenceImage(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	chID := createTestProject(t, repo, "user-1", model.PlatformSeednote)

	skipRef := true
	plan, err := svc.Create(ctx, CreatePlanParams{
		UserID:             "user-1",
		ProjectID:          chID,
		CronExpr:           "0 9 * * *",
		Prompt:             "topic hint",
		SkipReferenceImage: &skipRef,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if !plan.SkipReferenceImage {
		t.Fatal("expected skip_reference_image to be true when explicitly set")
	}
}

func TestPlanService_Create_ArticleImageToggles(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	chID := createTestProject(t, repo, "user-1", model.PlatformArticle)

	cover, content := false, false
	plan, err := svc.Create(ctx, CreatePlanParams{
		UserID:                   "user-1",
		ProjectID:                chID,
		CronExpr:                 "0 9 * * *",
		Prompt:                   "topic hint",
		ArticleWithCover:         &cover,
		ArticleWithContentImages: &content,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if plan.ArticleWithCover == nil || *plan.ArticleWithCover {
		t.Error("expected article_with_cover to be false when explicitly set")
	}
	if plan.ArticleWithContentImages == nil || *plan.ArticleWithContentImages {
		t.Error("expected article_with_content_images to be false when explicitly set")
	}

	// Default (nil flags) → both true (legacy "always generate" behavior).
	planDefault, err := svc.Create(ctx, CreatePlanParams{
		UserID:    "user-1",
		ProjectID: chID,
		CronExpr:  "0 10 * * *",
		Prompt:    "topic hint 2",
	})
	if err != nil {
		t.Fatalf("create default plan: %v", err)
	}
	if ptr := planDefault.ArticleWithCover; ptr == nil || !*ptr {
		t.Error("expected article_with_cover to default true when omitted")
	}
	if ptr := planDefault.ArticleWithContentImages; ptr == nil || !*ptr {
		t.Error("expected article_with_content_images to default true when omitted")
	}
}

func TestPlanService_GetByID(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	// Create a test project and plan.
	chID := createTestProject(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, CreatePlanParams{
		UserID:    "user-1",
		ProjectID: chID,
		CronExpr:  "0 9 * * *",
		Prompt:    "hint",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// Get by ID.
	found, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if found.Prompt != "hint" {
		t.Errorf("expected prompt 'hint', got %q", found.Prompt)
	}

	// Non-existent ID.
	_, err = svc.GetByID(ctx, "non-existent-id")
	if err == nil {
		t.Error("expected error for non-existent ID")
	}
}

func TestPlanService_CreateRejectsUnsupportedPlanPlatforms(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	for _, tt := range []struct {
		name     string
		platform string
	}{
		{name: "ecommerce", platform: model.PlatformEcommerce},
		{name: "moments", platform: model.PlatformMoments},
	} {
		t.Run(tt.name, func(t *testing.T) {
			projectID := createTestProject(t, repo, "user-unsupported-plan-"+tt.name, tt.platform)

			_, err := svc.Create(ctx, CreatePlanParams{
				UserID:    "user-unsupported-plan-" + tt.name,
				ProjectID: projectID,
				CronExpr:  "0 9 * * *",
				Prompt:    "scheduled content",
			})
			if !errors.Is(err, ErrUnsupportedPlanPlatform) {
				t.Fatalf("Create error = %v, want ErrUnsupportedPlanPlatform", err)
			}
		})
	}
}

func TestPlanService_List(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	// Create a test project for the user.
	chID := createTestProject(t, repo, "user-1", model.PlatformSeednote)
	// Create a different project for user-2.
	chID2 := createTestProject(t, repo, "user-2", model.PlatformArticle)

	// Create multiple plans for user-1.
	for i := 0; i < 5; i++ {
		_, err := svc.Create(ctx, CreatePlanParams{
			UserID:    "user-1",
			ProjectID: chID,
			CronExpr:  "0 9 * * *",
			Prompt:    "hint",
		})
		if err != nil {
			t.Fatalf("create plan %d: %v", i, err)
		}
	}

	// Create plans for another user.
	_, err := svc.Create(ctx, CreatePlanParams{
		UserID:    "user-2",
		ProjectID: chID2,
		CronExpr:  "0 10 * * *",
		Prompt:    "hint",
	})
	if err != nil {
		t.Fatalf("create plan for user-2: %v", err)
	}

	// List user-1 plans.
	plans, total, err := svc.List(ctx, "user-1", 0, 10, "")
	if err != nil {
		t.Fatalf("list plans: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(plans) != 5 {
		t.Errorf("expected 5 plans, got %d", len(plans))
	}

	// Test pagination: offset=0, limit=2.
	plans, _, err = svc.List(ctx, "user-1", 0, 2, "")
	if err != nil {
		t.Fatalf("list plans paginated: %v", err)
	}
	if len(plans) != 2 {
		t.Errorf("expected 2 plans, got %d", len(plans))
	}

	// List for different user.
	plans, total, err = svc.List(ctx, "user-2", 0, 10, "")
	if err != nil {
		t.Fatalf("list user-2 plans: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
}

func TestPlanService_Update(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	chID := createTestProject(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, CreatePlanParams{
		UserID:    "user-1",
		ProjectID: chID,
		CronExpr:  "0 9 * * *",
		Prompt:    "old hint",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// Update title only.
	updated, err := svc.Update(ctx, UpdatePlanParams{
		ID:     created.ID,
		Prompt: "new hint",
	})
	if err != nil {
		t.Fatalf("update plan: %v", err)
	}
	if updated.Prompt != "new hint" {
		t.Errorf("expected prompt 'new hint', got %q", updated.Prompt)
	}
	// Cron should not have changed since we passed empty.
	if updated.CronExpr != "0 9 * * *" {
		t.Errorf("expected cron_expr unchanged, got %q", updated.CronExpr)
	}

	// Update with new cron expression.
	updated, err = svc.Update(ctx, UpdatePlanParams{
		ID:       created.ID,
		CronExpr: "0 18 * * *",
		Prompt:   "new hint",
	})
	if err != nil {
		t.Fatalf("update plan cron: %v", err)
	}
	if updated.CronExpr != "0 18 * * *" {
		t.Errorf("expected cron_expr '0 18 * * *', got %q", updated.CronExpr)
	}
	if updated.NextRunAt == nil {
		t.Error("expected non-nil next_run_at after cron update")
	}

	// Update with invalid cron.
	_, err = svc.Update(ctx, UpdatePlanParams{
		ID:       created.ID,
		CronExpr: "bad cron",
		Prompt:   "hint",
	})
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestPlanService_UpdateVideoConfigStoresVideoInputOnly(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	projectID := createTestVideoProject(t, repo, "user-1")
	created, err := svc.Create(ctx, CreatePlanParams{
		UserID:    "user-1",
		ProjectID: projectID,
		CronExpr:  "0 9 * * *",
		Prompt:    "old video prompt",
		VideoCreatorInput: &model.VideoInput{
			Brief: "old video prompt",
		},
	})
	if err != nil {
		t.Fatalf("create video plan: %v", err)
	}
	if created.VideoEstimatedCredits != 0 {
		t.Fatalf("initial estimated credits = %d, want 0 before MCP video_gen", created.VideoEstimatedCredits)
	}
	watermark := true
	updated, err := svc.Update(ctx, UpdatePlanParams{
		ID:     created.ID,
		Prompt: "updated video prompt",
		VideoCreatorInput: &model.VideoInput{
			Brief: "updated video prompt",
			References: []model.VideoReferenceAsset{{
				Type: "image_url",
				URL:  "https://cdn.example.com/cup.png",
			}},
			HardConstraints: model.VideoHardConstraints{
				Ratio:     "16:9",
				Duration:  5,
				Watermark: &watermark,
			},
		},
	})
	if err != nil {
		t.Fatalf("update video plan: %v", err)
	}
	vi := updated.VideoInput.Data()
	if vi.Brief != "updated video prompt" || vi.HardConstraints.Ratio != "16:9" || vi.HardConstraints.Duration != 5 || vi.HardConstraints.Watermark == nil || !*vi.HardConstraints.Watermark {
		t.Fatalf("video input = %#v", vi)
	}
	if len(vi.References) != 1 || vi.References[0].URL != "https://cdn.example.com/cup.png" {
		t.Fatalf("video input references = %#v", vi.References)
	}
	if vc := updated.VideoConfig.Data(); vc.ModelKey != "" || vc.EstimatedCredits != 0 {
		t.Fatalf("video config should stay empty before agent/MCP execution, got %#v", vc)
	}
	if updated.VideoEstimatedCredits != 0 {
		t.Fatalf("estimated credits = %d, want 0 before MCP video_gen", updated.VideoEstimatedCredits)
	}
}

func TestPlanService_UpdateVideoInputRejectsNonVideoPlans(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	projectID := createTestProject(t, repo, "user-1", model.PlatformArticle)
	created, err := svc.Create(ctx, CreatePlanParams{
		UserID:    "user-1",
		ProjectID: projectID,
		CronExpr:  "0 9 * * *",
		Prompt:    "article prompt",
	})
	if err != nil {
		t.Fatalf("create article plan: %v", err)
	}

	_, err = svc.Update(ctx, UpdatePlanParams{
		ID: created.ID,
		VideoCreatorInput: &model.VideoInput{
			Brief: "should not attach to article plan",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "video_creator_input can only be set on videocreator plans") {
		t.Fatalf("Update error = %v, want video_creator_input rejection for non-video plan", err)
	}

	found, err := repo.Plans().FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("find plan: %v", err)
	}
	if vi := found.VideoInput.Data(); vi.Brief != "" {
		t.Fatalf("article plan video creator input = %#v, want empty", vi)
	}
}

func TestPlanService_CreateRejectsVideoEditorPlans(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	projectID := createTestProject(t, repo, "user-1", model.PlatformVideoEditor)

	_, err := svc.Create(ctx, CreatePlanParams{
		UserID:    "user-1",
		ProjectID: projectID,
		CronExpr:  "0 9 * * *",
		Prompt:    "把素材剪成一条短视频",
	})
	if err == nil || !strings.Contains(err.Error(), "plans are not supported for video editor projects") {
		t.Fatalf("Create error = %v, want videoeditor plan rejection", err)
	}
}

func TestPlanService_CreateVideoPlanDoesNotRequireLegacyMinimumBalance(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	creditSvc := newPricedCreditService(repo)
	svc.SetCreditService(creditSvc)
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:             userID,
		Email:          userID + "@example.com",
		Password:       "hashed",
		InviteCode:     "invite-" + userID[:8],
		CreditsBalance: 99_999,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := createTestVideoProject(t, repo, userID)

	created, err := svc.Create(ctx, CreatePlanParams{
		UserID:    userID,
		ProjectID: projectID,
		CronExpr:  "0 9 * * *",
		Prompt:    "计划生成视频",
	})
	if err != nil {
		t.Fatalf("Create video plan: %v", err)
	}
	if created.VideoEstimatedCredits != 0 {
		t.Fatalf("video plan estimated credits = %d, want 0 before MCP video_gen", created.VideoEstimatedCredits)
	}
	vi := created.VideoInput.Data()
	if vi.Brief != "计划生成视频" {
		t.Fatalf("video input brief = %q, want prompt copied", vi.Brief)
	}
	if vc := created.VideoConfig.Data(); vc.ModelKey != "" || vc.EstimatedCredits != 0 {
		t.Fatalf("video config should stay empty before agent/MCP execution, got %#v", vc)
	}
}

func TestPlanService_Update_SkipReferenceImage(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	chID := createTestProject(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, CreatePlanParams{
		UserID:    "user-1",
		ProjectID: chID,
		CronExpr:  "0 9 * * *",
		Prompt:    "old hint",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if created.SkipReferenceImage {
		t.Fatal("expected default skip_reference_image to be false")
	}

	skipRef := true
	updated, err := svc.Update(ctx, UpdatePlanParams{
		ID:                 created.ID,
		Prompt:             "new hint",
		SkipReferenceImage: &skipRef,
	})
	if err != nil {
		t.Fatalf("update skip_reference_image true: %v", err)
	}
	if !updated.SkipReferenceImage {
		t.Fatal("expected skip_reference_image to update to true")
	}

	updated, err = svc.Update(ctx, UpdatePlanParams{
		ID:     created.ID,
		Prompt: "unchanged hint",
	})
	if err != nil {
		t.Fatalf("update without skip_reference_image: %v", err)
	}
	if !updated.SkipReferenceImage {
		t.Fatal("expected skip_reference_image to stay true when omitted")
	}

	skipRef = false
	updated, err = svc.Update(ctx, UpdatePlanParams{
		ID:                 created.ID,
		Prompt:             "final hint",
		SkipReferenceImage: &skipRef,
	})
	if err != nil {
		t.Fatalf("update skip_reference_image false: %v", err)
	}
	if updated.SkipReferenceImage {
		t.Fatal("expected skip_reference_image to update to false")
	}
}

func TestPlanService_Update_ReferenceImageAssetID(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))

	chID := createTestProject(t, repo, "user-1", model.PlatformSeednote)
	initialRef := "asset-initial"
	seedReferenceAsset(t, repo, referenceAssetFixture(initialRef, "user-1", DirectUploadPurposeTaskReference))
	created, err := svc.Create(ctx, CreatePlanParams{
		UserID:                "user-1",
		ProjectID:             chID,
		CronExpr:              "0 9 * * *",
		Prompt:                "hint",
		ReferenceImageAssetID: initialRef,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if created.ReferenceImageAssetID != initialRef {
		t.Fatalf("expected initial reference asset %q, got %q", initialRef, created.ReferenceImageAssetID)
	}

	// nil = leave unchanged (this is the regression fix: editing a plan without
	// resending reference_image_url must preserve the existing value).
	updated, err := svc.Update(ctx, UpdatePlanParams{
		ID:     created.ID,
		Prompt: "hint",
	})
	if err != nil {
		t.Fatalf("update with nil reference_image_url: %v", err)
	}
	if updated.ReferenceImageAssetID != initialRef {
		t.Errorf("nil reference asset should leave unchanged; got %q, want %q", updated.ReferenceImageAssetID, initialRef)
	}

	// &"" = clear.
	emptyRef := ""
	updated, err = svc.Update(ctx, UpdatePlanParams{
		ID:                    created.ID,
		Prompt:                "hint",
		ReferenceImageAssetID: &emptyRef,
	})
	if err != nil {
		t.Fatalf("update with empty reference_image_url: %v", err)
	}
	if updated.ReferenceImageAssetID != "" {
		t.Errorf("empty reference asset should clear; got %q", updated.ReferenceImageAssetID)
	}

	// &"new" = set.
	newRef := "asset-new"
	seedReferenceAsset(t, repo, referenceAssetFixture(newRef, "user-1", DirectUploadPurposeTaskReference))
	updated, err = svc.Update(ctx, UpdatePlanParams{
		ID:                    created.ID,
		Prompt:                "hint",
		ReferenceImageAssetID: &newRef,
	})
	if err != nil {
		t.Fatalf("update with new reference_image_url: %v", err)
	}
	if updated.ReferenceImageAssetID != newRef {
		t.Errorf("new reference asset should set; got %q, want %q", updated.ReferenceImageAssetID, newRef)
	}
}

func TestPlanService_Pause_Resume(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	chID := createTestProject(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, CreatePlanParams{
		UserID:    "user-1",
		ProjectID: chID,
		CronExpr:  "0 9 * * *",
		Prompt:    "hint",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// Pause.
	err = svc.Pause(ctx, created.ID)
	if err != nil {
		t.Fatalf("pause plan: %v", err)
	}
	paused, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get paused plan: %v", err)
	}
	if paused.Status != model.PlanStatusPaused {
		t.Errorf("expected status %q, got %q", model.PlanStatusPaused, paused.Status)
	}
	if paused.NextRunAt != nil {
		t.Error("expected nil next_run_at when paused")
	}

	// Resume.
	err = svc.Resume(ctx, created.ID)
	if err != nil {
		t.Fatalf("resume plan: %v", err)
	}
	resumed, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get resumed plan: %v", err)
	}
	if resumed.Status != model.PlanStatusActive {
		t.Errorf("expected status %q, got %q", model.PlanStatusActive, resumed.Status)
	}
	if resumed.NextRunAt == nil {
		t.Error("expected non-nil next_run_at when resumed")
	}
}

func TestPlanService_Delete(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	chID := createTestProject(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, CreatePlanParams{
		UserID:    "user-1",
		ProjectID: chID,
		CronExpr:  "0 9 * * *",
		Prompt:    "hint",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// Delete.
	err = svc.Delete(ctx, created.ID)
	if err != nil {
		t.Fatalf("delete plan: %v", err)
	}

	// Verify deletion.
	_, err = repo.Plans().FindByID(ctx, created.ID)
	if err == nil {
		t.Error("expected error when finding deleted plan")
	}
}

func TestPublishingService_ListDrafts_DegradesWechat48001(t *testing.T) {
	_, repo := setupTestPlanService(t)
	ctx := context.Background()
	chID := createTestWechatProject(t, repo, "user-1")
	logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
	svc := NewPublishingService(repo, &logger)
	svc.createDraftServiceFn = func(*model.Project) (draftClient, error) {
		return &stubDraftClient{
			listDraftsErr: &wechat.WechatAPIError{ErrCode: 48001, UserMsg: "API 功能未授权"},
		}, nil
	}

	result, err := svc.ListDrafts(ctx, "user-1", chID, 0, 20)
	if err != nil {
		t.Fatalf("expected 48001 to degrade without error, got %v", err)
	}
	if result.TotalCount != 0 || result.ItemCount != 0 {
		t.Fatalf("expected empty result counts, got total=%d item=%d", result.TotalCount, result.ItemCount)
	}
	if result.Note == "" {
		t.Fatal("expected explanatory note for 48001")
	}
}

func TestPublishingService_ListPublished_DegradesWrappedWechat48001(t *testing.T) {
	_, repo := setupTestPlanService(t)
	ctx := context.Background()
	chID := createTestWechatProject(t, repo, "user-1")
	logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
	svc := NewPublishingService(repo, &logger)
	svc.createDraftServiceFn = func(*model.Project) (draftClient, error) {
		return &stubDraftClient{
			listPublishedErr: fmt.Errorf("list published: %w", &wechat.WechatAPIError{ErrCode: 48001, UserMsg: "API 功能未授权"}),
		}, nil
	}

	result, err := svc.ListPublished(ctx, "user-1", chID, 0, 20)
	if err != nil {
		t.Fatalf("expected wrapped 48001 to degrade without error, got %v", err)
	}
	if result.TotalCount != 0 || result.ItemCount != 0 {
		t.Fatalf("expected empty result counts, got total=%d item=%d", result.TotalCount, result.ItemCount)
	}
	if result.Note == "" {
		t.Fatal("expected explanatory note for 48001")
	}
}

func TestPublishingService_ListDrafts_ReturnsNon48001Error(t *testing.T) {
	_, repo := setupTestPlanService(t)
	ctx := context.Background()
	chID := createTestWechatProject(t, repo, "user-1")
	logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
	svc := NewPublishingService(repo, &logger)
	svc.createDraftServiceFn = func(*model.Project) (draftClient, error) {
		return &stubDraftClient{
			listDraftsErr: &wechat.WechatAPIError{ErrCode: 40164, UserMsg: "IP 不在白名单"},
		}, nil
	}

	_, err := svc.ListDrafts(ctx, "user-1", chID, 0, 20)
	if err == nil {
		t.Fatal("expected non-48001 error to be returned")
	}
	if !strings.Contains(err.Error(), "list drafts") {
		t.Fatalf("expected wrapped list drafts error, got %v", err)
	}
}

func createSeednotePlanWithInputAttachments(t *testing.T, svc *PlanService, repo repository.Repository, attachments []model.EntryAttachment) *model.Plan {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "planattachments",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	plan, err := svc.Create(ctx, CreatePlanParams{
		UserID:           userID,
		ProjectID:        projectID,
		CronExpr:         "0 9 * * *",
		Prompt:           "使用参考素材生成种草内容",
		InputAttachments: attachments,
	})
	if err != nil {
		t.Fatalf("Create plan: %v", err)
	}
	return plan
}

func TestCreatePlanClonesInputAttachments(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	attachments := []model.EntryAttachment{{
		Type:        "image",
		URL:         "https://cdn.example.com/product.png",
		FileName:    "product.png",
		ContentType: "image/png",
		Instruction: "保留产品包装细节",
	}}

	plan := createSeednotePlanWithInputAttachments(t, svc, repo, attachments)
	attachments[0].Instruction = "调用方后续修改"

	stored, err := repo.Plans().FindByID(ctx, plan.ID)
	if err != nil {
		t.Fatalf("find plan: %v", err)
	}
	got := stored.InputAttachments.Data()
	if len(got) != 1 || got[0].Instruction != "保留产品包装细节" {
		t.Fatalf("stored attachments = %#v, want independent original snapshot", got)
	}
}

func TestUpdatePlanInputAttachmentsOmittedRetainsExisting(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	plan := createSeednotePlanWithInputAttachments(t, svc, repo, []model.EntryAttachment{{
		Type: "image", URL: "https://cdn.example.com/original.png", Instruction: "原始说明",
	}})

	updated, err := svc.Update(context.Background(), UpdatePlanParams{ID: plan.ID})
	if err != nil {
		t.Fatalf("Update plan: %v", err)
	}
	got := updated.InputAttachments.Data()
	if len(got) != 1 || got[0].Instruction != "原始说明" {
		t.Fatalf("attachments after omitted update = %#v, want original", got)
	}
}

func TestUpdatePlanInputAttachmentsEmptyClears(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	plan := createSeednotePlanWithInputAttachments(t, svc, repo, []model.EntryAttachment{{
		Type: "image", URL: "https://cdn.example.com/original.png", Instruction: "原始说明",
	}})
	empty := []model.EntryAttachment{}

	updated, err := svc.Update(context.Background(), UpdatePlanParams{
		ID:               plan.ID,
		InputAttachments: &empty,
	})
	if err != nil {
		t.Fatalf("Update plan: %v", err)
	}
	if got := updated.InputAttachments.Data(); len(got) != 0 {
		t.Fatalf("attachments after explicit empty update = %#v, want empty", got)
	}
}

func TestUpdatePlanInputAttachmentsNonEmptyReplaces(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	plan := createSeednotePlanWithInputAttachments(t, svc, repo, []model.EntryAttachment{{
		Type: "image", URL: "https://cdn.example.com/original.png", Instruction: "原始说明",
	}})
	replacement := []model.EntryAttachment{{
		Type: "image", URL: "https://cdn.example.com/replacement.png", Instruction: "替换说明",
	}}

	if _, err := svc.Update(ctx, UpdatePlanParams{
		ID:               plan.ID,
		InputAttachments: &replacement,
	}); err != nil {
		t.Fatalf("Update plan: %v", err)
	}
	replacement[0].Instruction = "调用方后续修改"

	stored, err := repo.Plans().FindByID(ctx, plan.ID)
	if err != nil {
		t.Fatalf("find plan: %v", err)
	}
	got := stored.InputAttachments.Data()
	if len(got) != 1 || got[0].URL != "https://cdn.example.com/replacement.png" || got[0].Instruction != "替换说明" {
		t.Fatalf("stored attachments = %#v, want replacement snapshot", got)
	}
}
