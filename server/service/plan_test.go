package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	appdraft "github.com/royalrick/anbanwriter/app/draft"
	"github.com/royalrick/anbanwriter/app/wechat"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
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
		&model.LoginSession{}, &model.TaskFile{}, &model.Channel{},
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
	return svc, repo
}

// createTestChannel creates a test channel for the given user and returns its ID.
func createTestChannel(t *testing.T, repo repository.Repository, userID, platform string) string {
	t.Helper()
	ch := &model.Channel{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: platform,
		Name:     "Test Channel " + platform,
		Status:   model.ChannelStatusActive,
	}
	if err := repo.Channels().Create(context.Background(), ch); err != nil {
		t.Fatalf("create test channel: %v", err)
	}
	return ch.ID
}

func createTestWechatChannel(t *testing.T, repo repository.Repository, userID string) string {
	t.Helper()
	ch := &model.Channel{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Test WeChat Channel",
		Status:   model.ChannelStatusActive,
		Config: model.ChannelConfig{
			WechatAppID:  "app-id",
			WechatSecret: "secret",
		},
	}
	if err := repo.Channels().Create(context.Background(), ch); err != nil {
		t.Fatalf("create test wechat channel: %v", err)
	}
	return ch.ID
}

func TestPlanService_Create(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	// Create test channels for the user.
	chID1 := createTestChannel(t, repo, "user-1", model.PlatformSeednote)
	chID2 := createTestChannel(t, repo, "user-1", model.PlatformArticle)

	tests := []struct {
		name      string
		channelID string
		cronExpr  string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "valid plan",
			channelID: chID1,
			cronExpr:  "0 9 * * 1-5",
			wantErr:   false,
		},
		{
			name:      "every minute",
			channelID: chID2,
			cronExpr:  "* * * * *",
			wantErr:   false,
		},
		{
			name:      "empty channel_id",
			channelID: "",
			cronExpr:  "0 9 * * *",
			wantErr:   true,
			errSubstr: "channel_id is required",
		},
		{
			name:      "empty cron",
			channelID: chID1,
			cronExpr:  "",
			wantErr:   true,
			errSubstr: "cron_expr is required",
		},
		{
			name:      "invalid cron",
			channelID: chID1,
			cronExpr:  "invalid cron",
			wantErr:   true,
			errSubstr: "invalid cron expression",
		},
		{
			name:      "invalid cron fields",
			channelID: chID1,
			cronExpr:  "60 25 * * *",
			wantErr:   true,
			errSubstr: "invalid cron expression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := svc.Create(ctx, "user-1", tt.channelID, tt.cronExpr, "topic hint", nil, "")
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
			if plan.ChannelID != tt.channelID {
				t.Errorf("expected channel_id %q, got %q", tt.channelID, plan.ChannelID)
			}
		})
	}
}

func TestPlanService_Create_SkipReferenceImage(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()
	chID := createTestChannel(t, repo, "user-1", model.PlatformSeednote)

	skipRef := true
	plan, err := svc.Create(ctx, "user-1", chID, "0 9 * * *", "topic hint", &skipRef, "")
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if !plan.SkipReferenceImage {
		t.Fatal("expected skip_reference_image to be true when explicitly set")
	}
}

func TestPlanService_GetByID(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	// Create a test channel and plan.
	chID := createTestChannel(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, "user-1", chID, "0 9 * * *", "hint", nil, "")
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

func TestPlanService_List(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	// Create a test channel for the user.
	chID := createTestChannel(t, repo, "user-1", model.PlatformSeednote)
	// Create a different channel for user-2.
	chID2 := createTestChannel(t, repo, "user-2", model.PlatformArticle)

	// Create multiple plans for user-1.
	for i := 0; i < 5; i++ {
		_, err := svc.Create(ctx, "user-1", chID, "0 9 * * *", "hint", nil, "")
		if err != nil {
			t.Fatalf("create plan %d: %v", i, err)
		}
	}

	// Create plans for another user.
	_, err := svc.Create(ctx, "user-2", chID2, "0 10 * * *", "hint", nil, "")
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

	chID := createTestChannel(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, "user-1", chID, "0 9 * * *", "old hint", nil, "")
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// Update title only.
	updated, err := svc.Update(ctx, created.ID, "", "new hint", nil, "")
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
	updated, err = svc.Update(ctx, created.ID, "0 18 * * *", "new hint", nil, "")
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
	_, err = svc.Update(ctx, created.ID, "bad cron", "hint", nil, "")
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestPlanService_Update_SkipReferenceImage(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	chID := createTestChannel(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, "user-1", chID, "0 9 * * *", "old hint", nil, "")
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if created.SkipReferenceImage {
		t.Fatal("expected default skip_reference_image to be false")
	}

	skipRef := true
	updated, err := svc.Update(ctx, created.ID, "", "new hint", &skipRef, "")
	if err != nil {
		t.Fatalf("update skip_reference_image true: %v", err)
	}
	if !updated.SkipReferenceImage {
		t.Fatal("expected skip_reference_image to update to true")
	}

	updated, err = svc.Update(ctx, created.ID, "", "unchanged hint", nil, "")
	if err != nil {
		t.Fatalf("update without skip_reference_image: %v", err)
	}
	if !updated.SkipReferenceImage {
		t.Fatal("expected skip_reference_image to stay true when omitted")
	}

	skipRef = false
	updated, err = svc.Update(ctx, created.ID, "", "final hint", &skipRef, "")
	if err != nil {
		t.Fatalf("update skip_reference_image false: %v", err)
	}
	if updated.SkipReferenceImage {
		t.Fatal("expected skip_reference_image to update to false")
	}
}

func TestPlanService_Pause_Resume(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	chID := createTestChannel(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, "user-1", chID, "0 9 * * *", "hint", nil, "")
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

	chID := createTestChannel(t, repo, "user-1", model.PlatformSeednote)
	created, err := svc.Create(ctx, "user-1", chID, "0 9 * * *", "hint", nil, "")
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
	chID := createTestWechatChannel(t, repo, "user-1")
	logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
	svc := NewPublishingService(repo, &logger)
	svc.createDraftServiceFn = func(*model.Channel) (draftClient, error) {
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
	chID := createTestWechatChannel(t, repo, "user-1")
	logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
	svc := NewPublishingService(repo, &logger)
	svc.createDraftServiceFn = func(*model.Channel) (draftClient, error) {
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
	chID := createTestWechatChannel(t, repo, "user-1")
	logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
	svc := NewPublishingService(repo, &logger)
	svc.createDraftServiceFn = func(*model.Channel) (draftClient, error) {
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
