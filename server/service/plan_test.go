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

type rejectingPlanCASRepository struct {
	repository.PlanRepository
}

func (r rejectingPlanCASRepository) UpdateEditableIfReferenceImageAssetID(context.Context, *model.Plan, string, bool) (bool, error) {
	return false, nil
}

type afterFindPlanRepository struct {
	repository.PlanRepository
	afterFind func(*model.Plan)
}

func (r *afterFindPlanRepository) FindByID(ctx context.Context, id string) (*model.Plan, error) {
	plan, err := r.PlanRepository.FindByID(ctx, id)
	if err == nil && r.afterFind != nil {
		hook := r.afterFind
		r.afterFind = nil
		hook(plan)
	}
	return plan, err
}

type planCASRepositoryOverride struct {
	repository.Repository
	plans repository.PlanRepository
}

func (r planCASRepositoryOverride) Plans() repository.PlanRepository {
	return r.plans
}

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
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatalf("NewAgentProfileRegistry: %v", err)
	}
	svc.SetAgentProfileRegistry(registry)
	return svc, repo
}

func newTestPlanService(t *testing.T, repo repository.Repository) *PlanService {
	t.Helper()
	svc := NewPlanService(repo, nil)
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatalf("NewAgentProfileRegistry: %v", err)
	}
	svc.SetAgentProfileRegistry(registry)
	return svc
}

// createTestProject creates a test project for the given user and returns its ID.
func createTestProject(t *testing.T, repo repository.Repository, userID, platform string) string {
	t.Helper()
	ensureTestUser(t, repo, userID)
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

func ensureTestUser(t *testing.T, repo repository.Repository, userID string) {
	t.Helper()
	if _, err := repo.Users().FindByID(context.Background(), userID); errors.Is(err, gorm.ErrRecordNotFound) {
		if err := repo.Users().Create(context.Background(), &model.User{
			ID: userID, Email: uuid.NewString() + "@test.local", Password: "fixture", InviteCode: strings.ReplaceAll(uuid.NewString(), "-", "")[:16], Tier: model.TierFree,
		}); err != nil {
			t.Fatalf("create test user: %v", err)
		}
	} else if err != nil {
		t.Fatalf("find test user: %v", err)
	}
}

func TestPlanServiceRequiresExecutionProfileOnCreateAndUpdate(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	if _, err := svc.Create(ctx, CreatePlanParams{
		UserID: userID, ProjectID: projectID, CronExpr: "0 9 * * *", Prompt: "topic",
	}); err == nil || !strings.Contains(err.Error(), "execution_profile is required") {
		t.Fatalf("Create error = %v, want execution_profile is required", err)
	}

	plan := &model.Plan{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		ExecutionProfile: "effective", CronExpr: "0 9 * * *", Status: model.PlanStatusActive,
	}
	if err := repo.Plans().Create(ctx, plan); err != nil {
		t.Fatalf("create plan fixture: %v", err)
	}
	if _, err := svc.Update(ctx, UpdatePlanParams{ID: plan.ID, Prompt: "updated"}); err == nil || !strings.Contains(err.Error(), "execution_profile is required") {
		t.Fatalf("Update error = %v, want execution_profile is required", err)
	}
}

func TestPlanServiceValidatesProfileTierAndPersistsSelection(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatal(err)
	}
	injector, ok := any(svc).(interface {
		SetAgentProfileRegistry(*AgentProfileRegistry)
	})
	if !ok {
		t.Fatal("PlanService does not expose AgentProfileRegistry injection")
	}
	injector.SetAgentProfileRegistry(registry)

	ctx := context.Background()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Tier: model.TierPro}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	plan, err := svc.Create(ctx, CreatePlanParams{
		UserID: userID, ProjectID: projectID, ExecutionProfile: "balanced",
		CronExpr: "0 9 * * *", Prompt: "topic",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if plan.ExecutionProfile != "balanced" {
		t.Fatalf("created profile = %q", plan.ExecutionProfile)
	}

	if _, err := svc.Update(ctx, UpdatePlanParams{
		ID: plan.ID, ExecutionProfile: "quality", Prompt: plan.Prompt,
	}); !errors.Is(err, ErrAgentProfileAccessDenied) {
		t.Fatalf("enterprise update error = %v, want ErrAgentProfileAccessDenied", err)
	}
	updated, err := svc.Update(ctx, UpdatePlanParams{
		ID: plan.ID, ExecutionProfile: "effective", Prompt: plan.Prompt,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	stored, err := repo.Plans().FindByID(ctx, plan.ID)
	if err != nil || updated.ExecutionProfile != "effective" || stored.ExecutionProfile != "effective" {
		t.Fatalf("updated=%#v stored=%#v err=%v", updated, stored, err)
	}
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
			plan, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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

	plan, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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

	plan, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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

	updated, err := svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
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

	updated, err = svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective", ID: plan.ID, Prompt: "只改提示"})
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

	_, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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

	plan, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		CronExpr:  "0 10 * * *",
		Prompt:    "正常文章计划",
	})
	if err != nil {
		t.Fatalf("Create article plan: %v", err)
	}
	_, err = svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
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
	plan, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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

func TestPlanService_ImageRatioPersistsAndUpdates(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	projectID := createTestProject(t, repo, "user-1", model.PlatformArticle)

	plan, err := svc.Create(ctx, CreatePlanParams{
		ExecutionProfile: "effective",
		UserID:           "user-1",
		ProjectID:        projectID,
		CronExpr:         "0 9 * * *",
		ImageRatio:       "21:9",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if plan.ImageRatio != "21:9" {
		t.Fatalf("created image ratio = %q, want 21:9", plan.ImageRatio)
	}

	nextRatio := "9:16"
	updated, err := svc.Update(ctx, UpdatePlanParams{
		ID:               plan.ID,
		ExecutionProfile: "effective",
		ImageRatio:       &nextRatio,
	})
	if err != nil {
		t.Fatalf("update plan: %v", err)
	}
	if updated.ImageRatio != nextRatio {
		t.Fatalf("updated image ratio = %q, want %q", updated.ImageRatio, nextRatio)
	}
	persisted, err := repo.Plans().FindByID(ctx, plan.ID)
	if err != nil {
		t.Fatalf("find plan: %v", err)
	}
	if persisted.ImageRatio != nextRatio {
		t.Fatalf("persisted image ratio = %q, want %q", persisted.ImageRatio, nextRatio)
	}
}

func TestPlanService_Create_ArticleImageToggles(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	chID := createTestProject(t, repo, "user-1", model.PlatformArticle)

	cover, content := false, false
	plan, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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
	planDefault, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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
	created, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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

			_, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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
		_, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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
	_, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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
	created, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
		UserID:    "user-1",
		ProjectID: chID,
		CronExpr:  "0 9 * * *",
		Prompt:    "old hint",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// Update title only.
	updated, err := svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
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
	updated, err = svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
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
	_, err = svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
		ID:       created.ID,
		CronExpr: "bad cron",
		Prompt:   "hint",
	})
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestPlanService_Update_SkipReferenceImage(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	chID := createTestProject(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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
	updated, err := svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
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

	updated, err = svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
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
	updated, err = svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
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
	created, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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
	updated, err := svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
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
	updated, err = svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
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
	updated, err = svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
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

func TestPlanServiceUpdateIfReferenceImageAssetIDReturnsConflictWithoutWriting(t *testing.T) {
	_, base := setupTestPlanService(t)
	ctx := context.Background()
	projectID := createTestProject(t, base, "user-1", model.PlatformSeednote)
	plan := &model.Plan{
		ID: uuid.NewString(), UserID: "user-1", ProjectID: projectID, Type: model.PlatformSeednote,
		Prompt: "before", CronExpr: "0 9 * * *", Status: model.PlanStatusActive,
		ReferenceImageAssetID: "asset-a",
	}
	if err := base.Plans().Create(ctx, plan); err != nil {
		t.Fatal(err)
	}
	repo := planCASRepositoryOverride{
		Repository: base,
		plans:      rejectingPlanCASRepository{PlanRepository: base.Plans()},
	}
	svc := newTestPlanService(t, repo)

	_, err := svc.UpdateIfReferenceImageAssetID(ctx, UpdatePlanParams{ExecutionProfile: "effective", ID: plan.ID, Prompt: "after"}, "asset-a")
	if !errors.Is(err, ErrPlanUpdateConflict) {
		t.Fatalf("error = %v, want ErrPlanUpdateConflict", err)
	}
	persisted, err := base.Plans().FindByID(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Prompt != "before" || persisted.ReferenceImageAssetID != "asset-a" {
		t.Fatalf("conflict wrote plan = prompt %q reference %q", persisted.Prompt, persisted.ReferenceImageAssetID)
	}
}

func TestPlanServiceUpdatesDoNotOverwriteSchedulerNextRun(t *testing.T) {
	for _, tt := range []struct {
		name       string
		desiredRef *string
		useCAS     bool
		wantRef    string
	}{
		{name: "omission CAS", useCAS: true, wantRef: "asset-a"},
		{name: "explicit clear", desiredRef: stringPointer(""), wantRef: ""},
		{name: "explicit replace", desiredRef: stringPointer("asset-b"), wantRef: "asset-b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, base := setupTestPlanService(t)
			ctx := context.Background()
			projectID := createTestProject(t, base, "user-1", model.PlatformSeednote)
			for _, assetID := range []string{"asset-a", "asset-b"} {
				seedReferenceAsset(t, base, referenceAssetFixture(assetID, "user-1", DirectUploadPurposeTaskReference))
			}
			oldNext := time.Now().Add(-time.Hour).Truncate(time.Second)
			schedulerNext := oldNext.Add(time.Hour)
			plan := &model.Plan{
				ID: uuid.NewString(), UserID: "user-1", ProjectID: projectID, Type: model.PlatformSeednote,
				Prompt: "before", CronExpr: "0 * * * *", Status: model.PlanStatusActive,
				ReferenceImageAssetID: "asset-a", NextRunAt: &oldNext,
			}
			if err := base.Plans().Create(ctx, plan); err != nil {
				t.Fatal(err)
			}
			hookedPlans := &afterFindPlanRepository{PlanRepository: base.Plans()}
			hookedPlans.afterFind = func(*model.Plan) {
				won, err := base.Plans().UpdateNextRunAtIf(ctx, plan.ID, &schedulerNext, &oldNext)
				if err != nil || !won {
					t.Fatalf("scheduler update = %v, %v", won, err)
				}
			}
			repo := planCASRepositoryOverride{Repository: base, plans: hookedPlans}
			svc := newTestPlanService(t, repo)
			svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))
			params := UpdatePlanParams{ExecutionProfile: "effective", ID: plan.ID, Prompt: "after", ReferenceImageAssetID: tt.desiredRef}
			var err error
			if tt.useCAS {
				_, err = svc.UpdateIfReferenceImageAssetID(ctx, params, "asset-a")
			} else {
				_, err = svc.Update(ctx, params)
			}
			if err != nil {
				t.Fatalf("update: %v", err)
			}
			persisted, err := base.Plans().FindByID(ctx, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Prompt != "after" || persisted.ReferenceImageAssetID != tt.wantRef {
				t.Fatalf("updated plan = prompt %q reference %q", persisted.Prompt, persisted.ReferenceImageAssetID)
			}
			if persisted.NextRunAt == nil || !persisted.NextRunAt.Equal(schedulerNext) {
				t.Fatalf("next_run_at = %v, want scheduler value %v", persisted.NextRunAt, schedulerNext)
			}
		})
	}
}

func TestPlanServiceExplicitCronChangeUpdatesSchedule(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	projectID := createTestProject(t, repo, "user-1", model.PlatformSeednote)
	oldNext := time.Now().Add(-time.Hour).Truncate(time.Second)
	plan := &model.Plan{
		ID: uuid.NewString(), UserID: "user-1", ProjectID: projectID, Type: model.PlatformSeednote,
		Prompt: "before", CronExpr: "0 * * * *", Status: model.PlanStatusActive, NextRunAt: &oldNext,
	}
	if err := repo.Plans().Create(ctx, plan); err != nil {
		t.Fatal(err)
	}

	updated, err := svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective", ID: plan.ID, Prompt: "after", CronExpr: "0 */2 * * *"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CronExpr != "0 */2 * * *" || updated.NextRunAt == nil || !updated.NextRunAt.After(time.Now()) {
		t.Fatalf("updated schedule = cron %q next %v", updated.CronExpr, updated.NextRunAt)
	}
	persisted, err := repo.Plans().FindByID(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.CronExpr != updated.CronExpr || persisted.NextRunAt == nil || !persisted.NextRunAt.Equal(*updated.NextRunAt) {
		t.Fatalf("persisted schedule = cron %q next %v", persisted.CronExpr, persisted.NextRunAt)
	}
}

func TestPlanServicePauseResumeDoNotOverwriteConcurrentEditableFields(t *testing.T) {
	for _, tt := range []struct {
		name        string
		initial     string
		apply       func(*PlanService, context.Context, string) error
		wantStatus  string
		wantNextNil bool
	}{
		{name: "pause", initial: model.PlanStatusActive, apply: func(svc *PlanService, ctx context.Context, id string) error {
			return svc.Pause(ctx, id)
		}, wantStatus: model.PlanStatusPaused, wantNextNil: true},
		{name: "resume", initial: model.PlanStatusPaused, apply: func(svc *PlanService, ctx context.Context, id string) error {
			return svc.Resume(ctx, id)
		}, wantStatus: model.PlanStatusActive},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, base := setupTestPlanService(t)
			ctx := context.Background()
			next := time.Now().Add(time.Hour).Truncate(time.Second)
			plan := &model.Plan{
				ID: uuid.NewString(), UserID: "user-1", Type: model.PlatformArticle,
				Prompt: "before", ReferenceImageAssetID: "asset-a", CronExpr: "0 * * * *",
				Status: tt.initial, NextRunAt: &next,
			}
			if err := base.Plans().Create(ctx, plan); err != nil {
				t.Fatal(err)
			}
			hookedPlans := &afterFindPlanRepository{PlanRepository: base.Plans()}
			hookedPlans.afterFind = func(*model.Plan) {
				current, err := base.Plans().FindByID(ctx, plan.ID)
				if err != nil {
					t.Fatal(err)
				}
				current.Prompt = "concurrent prompt"
				current.ReferenceImageAssetID = "asset-b"
				if err := base.Plans().Update(ctx, current); err != nil {
					t.Fatal(err)
				}
			}
			repo := planCASRepositoryOverride{Repository: base, plans: hookedPlans}
			svc := newTestPlanService(t, repo)
			if err := tt.apply(svc, ctx, plan.ID); err != nil {
				t.Fatal(err)
			}
			persisted, err := base.Plans().FindByID(ctx, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Status != tt.wantStatus || persisted.Prompt != "concurrent prompt" || persisted.ReferenceImageAssetID != "asset-b" {
				t.Fatalf("updated plan = status %q prompt %q reference %q", persisted.Status, persisted.Prompt, persisted.ReferenceImageAssetID)
			}
			if tt.wantNextNil && persisted.NextRunAt != nil {
				t.Fatalf("paused next_run_at = %v, want nil", persisted.NextRunAt)
			}
			if !tt.wantNextNil && persisted.NextRunAt == nil {
				t.Fatal("resumed next_run_at is nil")
			}
		})
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestPlanService_Pause_Resume(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	chID := createTestProject(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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
	created, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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
	plan, err := svc.Create(ctx, CreatePlanParams{ExecutionProfile: "effective",
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

func TestCreatePlanRejectsAgentInputWhenPackHasNoSchema(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	_, err := svc.Create(ctx, CreatePlanParams{
		ExecutionProfile: "effective", UserID: userID, ProjectID: projectID,
		CronExpr: "0 9 * * *", AgentInput: map[string]any{"tone": "concise"},
	})
	if !errors.Is(err, ErrInvalidAgentInput) {
		t.Fatalf("Create error = %v, want ErrInvalidAgentInput", err)
	}
}

func TestCreatePlanPersistsEmptyAgentInputSnapshot(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	plan, err := svc.Create(ctx, CreatePlanParams{
		ExecutionProfile: "effective", UserID: userID, ProjectID: projectID,
		CronExpr: "0 9 * * *", AgentInput: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if plan.AgentInput.Data() == nil {
		t.Fatal("plan agent_input was not persisted")
	}
}

func TestUpdatePlanRejectsAgentInputWhenPackHasNoSchema(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	plan := createSeednotePlanWithInputAttachments(t, svc, repo, nil)
	input := map[string]any{"tone": "concise"}

	_, err := svc.Update(context.Background(), UpdatePlanParams{
		ExecutionProfile: "effective", ID: plan.ID, AgentInput: &input,
	})
	if !errors.Is(err, ErrInvalidAgentInput) {
		t.Fatalf("Update error = %v, want ErrInvalidAgentInput", err)
	}
}

func TestUpdatePlanClearsAgentInputWithExplicitEmptyObject(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	plan := createSeednotePlanWithInputAttachments(t, svc, repo, nil)
	empty := map[string]any{}

	updated, err := svc.Update(context.Background(), UpdatePlanParams{
		ExecutionProfile: "effective", ID: plan.ID, AgentInput: &empty,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.AgentInput.Data() == nil {
		t.Fatal("explicit empty agent_input was not applied")
	}
}

func TestUpdatePlanInputAttachmentsOmittedRetainsExisting(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	plan := createSeednotePlanWithInputAttachments(t, svc, repo, []model.EntryAttachment{{
		Type: "image", URL: "https://cdn.example.com/original.png", Instruction: "原始说明",
	}})

	updated, err := svc.Update(context.Background(), UpdatePlanParams{ExecutionProfile: "effective", ID: plan.ID})
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

	updated, err := svc.Update(context.Background(), UpdatePlanParams{ExecutionProfile: "effective",
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

	if _, err := svc.Update(ctx, UpdatePlanParams{ExecutionProfile: "effective",
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
