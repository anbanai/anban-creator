package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

func setupTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Plan{}, &model.Task{}, &model.User{},
		&model.LoginSession{}, &model.TaskFile{}, &model.Project{},
		&model.CreditTransaction{}, &model.TopicPool{}, &model.VideoGeneration{},
		&model.IlinkBinding{}, &model.IlinkNotification{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func setupTaskServiceWithEnqueuer(t *testing.T) (*TaskService, repository.Repository) {
	t.Helper()
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	svc.SetVideoCatalogAndCreditMultiplier(DefaultVideoModelCatalog(), 1000)
	return svc, repo
}

func setupTaskServiceWithCredits(t *testing.T, creditSvc *CreditService) (*TaskService, repository.Repository) {
	t.Helper()
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, nil, creditSvc, &logger, "", nil, "", nil, nil)
	svc.SetVideoCatalogAndCreditMultiplier(DefaultVideoModelCatalog(), 1000)
	return svc, repo
}

// mockEnqueuer captures enqueued tasks without executing them.
type mockEnqueuer struct {
	enqueued []string
}

func (m *mockEnqueuer) Enqueue(taskType string, payload []byte) error {
	m.enqueued = append(m.enqueued, taskType)
	return nil
}

func (m *mockEnqueuer) EnqueueIn(taskType string, payload []byte, delay time.Duration) error {
	m.enqueued = append(m.enqueued, taskType)
	return nil
}

type fakePublishedTrackingService struct {
	calls []struct {
		userID string
		taskID string
	}
	err error
}

func (f *fakePublishedTrackingService) EnsureTrackingForPublishedTask(ctx context.Context, userID, taskID string) error {
	f.calls = append(f.calls, struct {
		userID string
		taskID string
	}{userID: userID, taskID: taskID})
	return f.err
}

func TestTaskService_FinalizeTitleUpdatesCanonicalTitle(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	title, err := svc.FinalizeTitle(ctx, userID, task.ID, "  月薪5000和月薪5万的人，喝茶差距在哪  ")
	if err != nil {
		t.Fatalf("FinalizeTitle: %v", err)
	}
	if title != "月薪5000和月薪5万的人，喝茶差距在哪" {
		t.Fatalf("title = %q", title)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Title != title {
		t.Fatalf("stored title = %q, want %q", found.Title, title)
	}
}

func TestTaskService_FinalizeTitleRejectsInvalidTitles(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"empty", "   ", "title is required"},
		{"too long", strings.Repeat("长", 201), "title must be <= 200 characters"},
		{"artifact", "图片内容规划", "artifact title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.FinalizeTitle(ctx, userID, task.ID, tt.title)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("FinalizeTitle error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestTaskService_FinalizeTitleRejectsForeignTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	ownerID := uuid.New().String()
	otherID := uuid.New().String()
	projectID := createTestProject(t, repo, ownerID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    ownerID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.FinalizeTitle(ctx, otherID, task.ID, "真实标题")
	if err == nil || !strings.Contains(err.Error(), "task not found") {
		t.Fatalf("FinalizeTitle error = %v, want task not found", err)
	}
}

func TestTaskService_FinalizeTitleRejectsUserKeyForUnownedTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    "",
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.FinalizeTitle(ctx, userID, task.ID, "真实标题")
	if err == nil || !strings.Contains(err.Error(), "task not found") {
		t.Fatalf("FinalizeTitle error = %v, want task not found", err)
	}
}

func TestTaskService_FinalizeTitleRejectsDuplicateWithinProject(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	otherProjectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	existing := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Title:     "新手咖啡豆怎么选",
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}
	current := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	foreignProject := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: otherProjectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	for _, task := range []*model.Task{existing, current, foreignProject} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	_, err := svc.FinalizeTitle(ctx, userID, current.ID, " 新手 咖啡豆怎么选 ")
	if err == nil || !strings.Contains(err.Error(), "duplicate title") {
		t.Fatalf("FinalizeTitle error = %v, want duplicate title", err)
	}

	if _, err := svc.FinalizeTitle(ctx, userID, foreignProject.ID, "新手咖啡豆怎么选"); err != nil {
		t.Fatalf("other project duplicate should be allowed: %v", err)
	}
}

func TestTaskService_FinalizeTitleRejectsDuplicateEvenWhenCurrentTaskAlreadyHasTitle(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	for _, task := range []*model.Task{
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "新手咖啡豆怎么选",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        "task-current-with-same-title",
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusRunning,
			Title:     "新手咖啡豆怎么选",
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	_, err := svc.FinalizeTitle(ctx, userID, "task-current-with-same-title", "新手 咖啡豆怎么选")
	if err == nil || !strings.Contains(err.Error(), "duplicate title") {
		t.Fatalf("FinalizeTitle error = %v, want duplicate title", err)
	}
}

func TestTaskService_CreateManualSnapshotsProjectConfig(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	project := &model.Project{
		ID:                uuid.New().String(),
		UserID:            userID,
		Platform:          model.PlatformArticle,
		Name:              "旧项目名",
		Status:            model.ProjectStatusActive,
		Instructions:      "旧定位",
		Keywords:          "旧关键词",
		VisualStyle:       "旧视觉",
		ReferenceImageURL: "/api/v1/files/ref-old",
		ImageRatio:        "3:4",
		Writer:            "dan-koe",
		Theme:             "autumn-warm",
		Author:            "旧署名",
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: project.ID,
		Prompt:    "topic",
		Quantity:  1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("created tasks = %d, want 1", len(tasks))
	}

	project.Name = "新项目名"
	project.Instructions = "新定位"
	project.VisualStyle = "新视觉"
	project.Author = "新署名"
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatalf("update project: %v", err)
	}

	found, err := repo.Tasks().FindByID(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	snap := found.ProjectSnapshot.Data()
	if snap.ProjectName != "旧项目名" || snap.Instructions != "旧定位" ||
		snap.VisualStyle != "旧视觉" || snap.Author != "旧署名" {
		t.Fatalf("snapshot = %+v, want original project values", snap)
	}
	if snap.ReferenceImageURL != "/api/v1/files/ref-old" || snap.ImageRatio != "3:4" {
		t.Fatalf("snapshot image fields = %q/%q", snap.ReferenceImageURL, snap.ImageRatio)
	}
}

func TestTaskService_CreateFromPlanSnapshotsProjectWithoutPlanStyleOverrides(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	project := &model.Project{
		ID:           uuid.New().String(),
		UserID:       userID,
		Platform:     model.PlatformSeednote,
		Name:         "项目",
		Status:       model.ProjectStatusActive,
		Instructions: "项目定位",
		VisualStyle:  "项目视觉",
		Writer:       "project-writer",
		Theme:        "project-theme",
		Author:       "项目署名",
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	plan := &model.Plan{
		ID:          uuid.New().String(),
		UserID:      userID,
		ProjectID:   project.ID,
		Type:        model.PlatformSeednote,
		Status:      model.PlanStatusActive,
		Prompt:      "topic",
		VisualStyle: "计划视觉不应进入任务",
		Writer:      "plan-writer",
		Theme:       "plan-theme",
		Author:      "计划署名",
	}

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	snap := found.ProjectSnapshot.Data()
	if snap.VisualStyle != "项目视觉" || snap.Writer != "project-writer" ||
		snap.Theme != "project-theme" || snap.Author != "项目署名" {
		t.Fatalf("snapshot = %+v, want project values", snap)
	}
	if overrides := found.Overrides.Data(); overrides != (model.StyleOverrides{}) {
		t.Fatalf("overrides = %+v, want empty", overrides)
	}
	if found.PlanID == nil || *found.PlanID != plan.ID {
		t.Fatalf("plan_id = %v, want %q", found.PlanID, plan.ID)
	}
}

func TestTaskService_ListFiltersByPlanID(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "项目",
		Status:   model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	planA := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: project.ID,
		Type:      model.PlatformSeednote,
		Status:    model.PlanStatusActive,
		Prompt:    "计划 A",
	}
	planB := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: project.ID,
		Type:      model.PlatformSeednote,
		Status:    model.PlanStatusActive,
		Prompt:    "计划 B",
	}
	taskA, err := svc.CreateFromPlan(ctx, planA)
	if err != nil {
		t.Fatalf("CreateFromPlan A: %v", err)
	}
	if _, err := svc.CreateFromPlan(ctx, planB); err != nil {
		t.Fatalf("CreateFromPlan B: %v", err)
	}

	tasks, total, err := svc.List(ctx, userID, 0, 20, "", "", planA.ID)
	if err != nil {
		t.Fatalf("List by plan_id: %v", err)
	}
	if total != 1 || len(tasks) != 1 {
		t.Fatalf("filtered tasks len/total = %d/%d, want 1/1", len(tasks), total)
	}
	if tasks[0].ID != taskA.ID || tasks[0].PlanID == nil || *tasks[0].PlanID != planA.ID {
		t.Fatalf("filtered task = %+v, want task %s for plan %s", tasks[0], taskA.ID, planA.ID)
	}
}

func TestTaskService_CreateManualVideoTaskSnapshotsProfileAndChargesDynamicCredits(t *testing.T) {
	repoForCredits := setupCreditTestRepo(t)
	creditSvc := newPricedCreditService(repoForCredits)
	svc, repo := setupTaskServiceWithCredits(t, creditSvc)
	// Use the same repository for service data and credits.
	creditSvc.repo = repo
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "openid-video", CreditsBalance: 200_000}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideo,
		Name:     "视频项目",
		Status:   model.ProjectStatusActive,
	}
	watermark := false
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "16:9",
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

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: project.ID,
		Prompt:    "生成一条咖啡杯种草视频",
		Quantity:  1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("created tasks = %d, want 1", len(tasks))
	}
	found, err := repo.Tasks().FindByID(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	vc := found.VideoConfig.Data()
	if vc.ModelKey != "seedance-2.0-mini" || vc.Model != "doubao-seedance-2-0-mini-260615" || vc.EstimatedCredits != 2480 {
		t.Fatalf("video config = %+v", vc)
	}
	if found.VideoEstimatedCredits != 2480 || found.VideoCreditsCharged != 2480 {
		t.Fatalf("task video credits = estimated %d charged %d", found.VideoEstimatedCredits, found.VideoCreditsCharged)
	}
	bal, err := creditSvc.GetBalance(ctx, userID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if bal != 200_000-2480 {
		t.Fatalf("balance = %d, want %d", bal, 200_000-2480)
	}
}

func TestTaskService_CreateManualVideoTaskStoresReferenceAssets(t *testing.T) {
	repoForCredits := setupCreditTestRepo(t)
	creditSvc := newPricedCreditService(repoForCredits)
	svc, repo := setupTaskServiceWithCredits(t, creditSvc)
	creditSvc.repo = repo
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", OpenID: "openid-video-refs", InviteCode: "invite-" + userID, CreditsBalance: 200_000}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideo,
		Name:     "视频项目",
		Status:   model.ProjectStatusActive,
	}
	watermark := false
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "16:9",
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

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: project.ID,
		Prompt:    "生成一条咖啡杯种草视频",
		Quantity:  1,
		Video: &model.VideoTaskConfig{
			ScenarioKey:     "live_selling",
			ProductionMode:  VideoProductionModeGuided,
			RetakeBudget:    4,
			DeliveryTargets: []string{"vertical_9x16", "textless_master"},
			References: []model.VideoReferenceAsset{
				{
					Type:                 VideoReferenceImage,
					URL:                  "https://cdn.example.com/cup.png",
					ReferenceRole:        "product appearance",
					MustKeep:             []string{"logo-free cup shape"},
					CanChange:            []string{"countertop"},
					MustNotTransfer:      []string{"donor hand"},
					FileName:             "cup.png",
					MimeType:             "image/png",
					FileSize:             1234,
					InputDurationSeconds: 0,
				},
				{
					Type:                 VideoReferenceVideo,
					URL:                  "https://cdn.example.com/source.mp4",
					ReferenceRole:        "motion reference",
					FileName:             "source.mp4",
					MimeType:             "video/mp4",
					FileSize:             5678,
					InputDurationSeconds: 4.2,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	vc := found.VideoConfig.Data()
	if len(vc.References) != 2 {
		t.Fatalf("references = %+v, want 2", vc.References)
	}
	if vc.ScenarioKey != "live_selling" || vc.ProductionMode != VideoProductionModeGuided || vc.RetakeBudget != 4 {
		t.Fatalf("production config = %+v", vc)
	}
	if len(vc.DeliveryTargets) != 2 || vc.DeliveryTargets[1] != "textless_master" {
		t.Fatalf("delivery targets = %+v", vc.DeliveryTargets)
	}
	if got := vc.References[0].MustNotTransfer; len(got) != 1 || got[0] != "donor hand" {
		t.Fatalf("reference transfer rules = %+v", vc.References[0])
	}
	if vc.PricingBreakdown == nil || !vc.PricingBreakdown.InputVideo || vc.PricingBreakdown.InputSeconds != 4.2 {
		t.Fatalf("pricing breakdown = %+v, want input video with measured seconds", vc.PricingBreakdown)
	}
}

func TestTaskService_CreateManualVideoTaskRequiresMinimumBalance(t *testing.T) {
	repoForCredits := setupCreditTestRepo(t)
	creditSvc := newPricedCreditService(repoForCredits)
	svc, repo := setupTaskServiceWithCredits(t, creditSvc)
	creditSvc.repo = repo
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", OpenID: "openid-video-min-balance", InviteCode: "invite-" + userID, CreditsBalance: 99_999}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideo,
		Name:     "视频项目",
		Status:   model.ProjectStatusActive,
	}
	watermark := false
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "16:9",
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

	_, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: project.ID,
		Prompt:    "生成一条咖啡杯种草视频",
	})
	if err == nil || !strings.Contains(err.Error(), "video tasks require at least 100000 credits") {
		t.Fatalf("CreateManual error = %v, want minimum balance error", err)
	}
	bal, err := creditSvc.GetBalance(ctx, userID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if bal != 99_999 {
		t.Fatalf("balance = %d, want unchanged 99999", bal)
	}
}

func TestTaskService_CreateManualVideoTaskUsesConfiguredCreditMultiplier(t *testing.T) {
	repoForCredits := setupCreditTestRepo(t)
	creditSvc := newPricedCreditService(repoForCredits)
	svc, repo := setupTaskServiceWithCredits(t, creditSvc)
	svc.SetVideoCatalogAndCreditMultiplier(DefaultVideoModelCatalog(), 1200)
	creditSvc.repo = repo
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", OpenID: "openid-video-multiplier", InviteCode: "invite-" + userID, CreditsBalance: 200_000}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideo,
		Name:     "视频项目",
		Status:   model.ProjectStatusActive,
	}
	watermark := false
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "16:9",
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

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: project.ID,
		Prompt:    "生成一条咖啡杯种草视频",
		Quantity:  1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.VideoEstimatedCredits != 2976 || found.VideoCreditsCharged != 2976 {
		t.Fatalf("task video credits = estimated %d charged %d, want 2976", found.VideoEstimatedCredits, found.VideoCreditsCharged)
	}
}

func TestTaskService_CreateManualVideoTaskAppliesUserBillingMultiplier(t *testing.T) {
	repoForCredits := setupCreditTestRepo(t)
	creditSvc := newPricedCreditService(repoForCredits)
	svc, repo := setupTaskServiceWithCredits(t, creditSvc)
	svc.SetVideoCatalogAndCreditMultiplier(DefaultVideoModelCatalog(), 1200)
	creditSvc.repo = repo
	ctx := context.Background()
	userID := uuid.New().String()
	multiplier := 0.5
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", OpenID: "openid-video-user-multiplier", InviteCode: "invite-" + userID, CreditsBalance: 200_000, BillingMultiplier: &multiplier}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideo,
		Name:     "视频项目",
		Status:   model.ProjectStatusActive,
	}
	watermark := false
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "16:9",
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

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: project.ID,
		Prompt:    "生成一条咖啡杯种草视频",
		Quantity:  1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.VideoEstimatedCredits != 1488 || found.VideoCreditsCharged != 1488 {
		t.Fatalf("task video credits = estimated %d charged %d, want 1488", found.VideoEstimatedCredits, found.VideoCreditsCharged)
	}
}

func TestTaskService_CreateFromPlanVideoTaskRecomputesCurrentBilling(t *testing.T) {
	repoForCredits := setupCreditTestRepo(t)
	creditSvc := newPricedCreditService(repoForCredits)
	svc, repo := setupTaskServiceWithCredits(t, creditSvc)
	svc.SetVideoCatalogAndCreditMultiplier(DefaultVideoModelCatalog(), 1200)
	creditSvc.repo = repo
	ctx := context.Background()
	userID := uuid.New().String()
	multiplier := 0.5
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", OpenID: "openid-video-plan-reprice", InviteCode: "invite-" + userID, CreditsBalance: 200_000, BillingMultiplier: &multiplier}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideo,
		Name:     "视频项目",
		Status:   model.ProjectStatusActive,
	}
	watermark := false
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "16:9",
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

	staleConfig := model.VideoTaskConfig{
		ModelKey:         "seedance-2.0-mini",
		Model:            "doubao-seedance-2-0-mini-260615",
		Resolution:       "720p",
		Ratio:            "16:9",
		Duration:         5,
		EstimatedCredits: 2480,
	}
	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: project.ID,
		Type:      model.PlatformVideo,
		Status:    model.PlanStatusActive,
		Prompt:    "生成一条咖啡杯种草视频",
	}
	plan.SetVideoConfig(staleConfig)

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.VideoEstimatedCredits != 1488 || found.VideoCreditsCharged != 1488 {
		t.Fatalf("task video credits = estimated %d charged %d, want current billing 1488", found.VideoEstimatedCredits, found.VideoCreditsCharged)
	}
	bal, err := creditSvc.GetBalance(ctx, userID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if bal != 200_000-1488 {
		t.Fatalf("balance = %d, want %d", bal, 200_000-1488)
	}
}

func TestTaskService_CreateManualVideoTaskRequiresProjectProfile(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideo,
		Name:     "未配置视频项目",
		Status:   model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	_, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: project.ID,
		Prompt:    "生成视频",
	})
	if err == nil || !strings.Contains(err.Error(), "project video profile is not configured") {
		t.Fatalf("CreateManual error = %v", err)
	}
}

func TestTaskService_ClearArtifactTitles(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	for _, task := range []*model.Task{
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "图片内容规划",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "真实茶饮标题",
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	count, err := svc.ClearArtifactTitles(ctx)
	if err != nil {
		t.Fatalf("ClearArtifactTitles: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	titles, err := svc.ListTitles(ctx, projectID)
	if err != nil {
		t.Fatalf("ListTitles: %v", err)
	}
	if len(titles) != 1 || titles[0] != "真实茶饮标题" {
		t.Fatalf("titles = %v, want [真实茶饮标题]", titles)
	}
}

type fakeTaskExecutor struct {
	result *agent.ExecutionResult
	err    error
}

func (f *fakeTaskExecutor) Execute(ctx context.Context, opts *agent.ExecutionOptions) (*agent.ExecutionResult, error) {
	return f.result, f.err
}

func TestTaskService_ExecuteDoesNotExtractTitleFromWorkspace(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	workDir := t.TempDir()
	outputDir := filepath.Join(workDir, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "image-plan.md"), []byte("# 图片内容规划\n\ninternal"), 0644); err != nil {
		t.Fatalf("write image plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "content.md"), []byte("# 真实最终标题\n\ncontent"), 0644); err != nil {
		t.Fatalf("write content: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "cover.png"), []byte("png"), 0644); err != nil {
		t.Fatalf("write cover: %v", err)
	}
	task := &model.Task{
		ID:              uuid.New().String(),
		UserID:          userID,
		ProjectID:       projectID,
		Type:            model.PlatformSeednote,
		Status:          model.TaskStatusRunning,
		Title:           "AI 已上报标题",
		HasContentImage: false,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success: true,
		WorkDir: workDir,
	}}, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Title != "AI 已上报标题" {
		t.Fatalf("title = %q, want existing AI-reported title", found.Title)
	}
	if _, err := os.Stat(workDir); err != nil {
		t.Fatalf("workDir should remain for possible resume, stat error: %v", err)
	}
}

func TestTaskService_HandleExecutionRejectsNestedAgentOnlyResult(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "CLAUDE.md"), []byte("# project"), 0644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	task := &model.Task{
		ID:              uuid.New().String(),
		UserID:          userID,
		ProjectID:       projectID,
		Type:            model.PlatformSeednote,
		Status:          model.TaskStatusRunning,
		HasContentImage: true,
		MaxRetries:      model.DefaultRetries,
		RetryCount:      model.DefaultRetries,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success:        true,
		WorkDir:        workDir,
		ToolUseSummary: map[string]int{"Agent": 1},
	}}, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want failed", found.Status)
	}
	if found.ErrorMessage != agent.NestedAgentDelegationError {
		t.Fatalf("error = %q, want %q", found.ErrorMessage, agent.NestedAgentDelegationError)
	}
}

func TestTaskService_HandleExecutionRejectsSeednoteWithoutWorkDir(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:              uuid.New().String(),
		UserID:          userID,
		ProjectID:       projectID,
		Type:            model.PlatformSeednote,
		Status:          model.TaskStatusRunning,
		HasContentImage: true,
		MaxRetries:      model.DefaultRetries,
		RetryCount:      model.DefaultRetries,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success: true,
	}}, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want failed", found.Status)
	}
	if !strings.Contains(found.ErrorMessage, "seednote missing required deliverables") {
		t.Fatalf("error = %q, want missing deliverables", found.ErrorMessage)
	}
}

func TestTaskServiceHandleExecutionRejectsVideoWithoutRegisteredVideoFile(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformVideo)
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "input-manifest.md"), []byte("workflow: dreamina-video"), 0644); err != nil {
		t.Fatalf("write input manifest: %v", err)
	}
	task := &model.Task{
		ID:                    uuid.New().String(),
		UserID:                userID,
		ProjectID:             projectID,
		Type:                  model.PlatformVideo,
		Status:                model.TaskStatusRunning,
		VideoEstimatedCredits: 2400,
		VideoCreditsCharged:   2400,
		MaxRetries:            model.DefaultRetries,
		RetryCount:            model.DefaultRetries,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success: true,
		WorkDir: workDir,
		ToolUseSummary: map[string]int{
			"Write":                    1,
			"register_video_reference": 1,
		},
	}}, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want failed", found.Status)
	}
	if found.ErrorMessage != "video missing final video task file" {
		t.Fatalf("error = %q, want video missing final video task file", found.ErrorMessage)
	}
}

func TestTaskServiceHandleExecutionRejectsVideoWithoutRegisteredVideoFileWhenWorkDirMissing(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformVideo)
	task := &model.Task{
		ID:                    uuid.New().String(),
		UserID:                userID,
		ProjectID:             projectID,
		Type:                  model.PlatformVideo,
		Status:                model.TaskStatusRunning,
		VideoEstimatedCredits: 2400,
		VideoCreditsCharged:   2400,
		MaxRetries:            model.DefaultRetries,
		RetryCount:            model.DefaultRetries,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success: true,
		ToolUseSummary: map[string]int{
			"create_video_generation_job": 1,
			"query_video_generation_job":  1,
		},
	}}, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want failed", found.Status)
	}
	if found.ErrorMessage != "video missing final video task file" {
		t.Fatalf("error = %q, want video missing final video task file", found.ErrorMessage)
	}
}

func TestTaskServiceHandleExecutionRejectsGeneratedVideoTaskWithOnlyInputVideoReference(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformVideo)
	task := &model.Task{
		ID:                    uuid.New().String(),
		UserID:                userID,
		ProjectID:             projectID,
		Type:                  model.PlatformVideo,
		Status:                model.TaskStatusRunning,
		VideoEstimatedCredits: 2400,
		VideoCreditsCharged:   2400,
		MaxRetries:            model.DefaultRetries,
		RetryCount:            model.DefaultRetries,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID:       uuid.New().String(),
		TaskID:   task.ID,
		Role:     model.FileRoleOther,
		FileName: "input-reference.mp4",
		FilePath: "input-reference.mp4",
		MimeType: "video/mp4",
		FileSize: 4096,
	}); err != nil {
		t.Fatalf("create input video task file: %v", err)
	}
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success: true,
		ToolUseSummary: map[string]int{
			"register_video_reference":    1,
			"create_video_generation_job": 1,
			"query_video_generation_job":  1,
		},
	}}, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want failed", found.Status)
	}
	if found.ErrorMessage != "video missing final video task file" {
		t.Fatalf("error = %q, want video missing final video task file", found.ErrorMessage)
	}
}

func TestTaskServiceHandleExecutionAcceptsRegisteredFinalVideoFile(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformVideo)
	generationID := uuid.New().String()
	fileID := uuid.New().String()
	task := &model.Task{
		ID:                    uuid.New().String(),
		UserID:                userID,
		ProjectID:             projectID,
		Type:                  model.PlatformVideo,
		Status:                model.TaskStatusRunning,
		VideoGenerationID:     generationID,
		VideoEstimatedCredits: 2400,
		VideoCreditsCharged:   2400,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID:       fileID,
		TaskID:   task.ID,
		Role:     model.FileRoleOther,
		FileName: "final.mp4",
		FilePath: "final.mp4",
		MimeType: "video/mp4",
		FileSize: 4096,
	}); err != nil {
		t.Fatalf("create video task file: %v", err)
	}
	if err := repo.VideoGenerations().Create(ctx, &model.VideoGeneration{
		ID:          generationID,
		UserID:      userID,
		ProjectID:   projectID,
		TaskID:      task.ID,
		Status:      "archived",
		TaskFileIDs: datatypes.JSON([]byte(fmt.Sprintf(`{"final_video":%q}`, fileID))),
	}); err != nil {
		t.Fatalf("create video generation: %v", err)
	}
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success: true,
		ToolUseSummary: map[string]int{
			"create_video_generation_job":       1,
			"query_video_generation_job":        1,
			"download_video_generation_results": 1,
		},
	}}, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusCompleted {
		t.Fatalf("status = %q error=%q, want completed with registered final_video", found.Status, found.ErrorMessage)
	}
}

func TestTaskServiceHandleExecutionAcceptsNonGenerationVideoWorkflowArtifact(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformVideo)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformVideo,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID:       uuid.New().String(),
		TaskID:   task.ID,
		Role:     model.FileRoleCover,
		FileName: "cover.png",
		FilePath: "cover.png",
		MimeType: "image/png",
		FileSize: 2048,
	}); err != nil {
		t.Fatalf("create cover task file: %v", err)
	}
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success: true,
		ToolUseSummary: map[string]int{
			"generate_image": 1,
			"download_image": 1,
		},
	}}, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status == model.TaskStatusFailed {
		t.Fatalf("non-generation video workflow artifact should not fail validation: %#v", found)
	}
}

func TestTaskService_HandleExecutionCompletesWithSeednoteDeliverables(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	workDir := t.TempDir()
	outputDir := filepath.Join(workDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	for name, data := range map[string][]byte{
		"content.md":     []byte("# 标题\n\n正文"),
		"image-plan.md":  []byte("# 图片内容规划"),
		"cover.png":      []byte("png"),
		"image_01.png":   []byte("png"),
		"compliance.md":  []byte("ok"),
		"topic-note.txt": []byte("ok"),
	} {
		if err := os.WriteFile(filepath.Join(outputDir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	task := &model.Task{
		ID:              uuid.New().String(),
		UserID:          userID,
		ProjectID:       projectID,
		Type:            model.PlatformSeednote,
		Status:          model.TaskStatusRunning,
		HasContentImage: true,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success:        true,
		WorkDir:        workDir,
		ToolUseSummary: map[string]int{"generate_image": 2, "Bash": 3},
	}}, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusCompleted {
		t.Fatalf("status = %q, want completed; err=%q", found.Status, found.ErrorMessage)
	}
	if found.CompletedAt == nil {
		t.Fatal("completed_at not set")
	}
}

func TestTaskService_HandleExecutionFailure_PermanentAuthErrorDoesNotRetry(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enqueuer := &mockEnqueuer{}
	svc := NewTaskService(repo, nil, enqueuer, nil, nil, &logger, "", nil, "", nil, nil)

	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:                  uuid.New().String(),
		UserID:              userID,
		ProjectID:           projectID,
		Type:                model.PlatformArticle,
		Status:              model.TaskStatusRunning,
		Prompt:              "auth failure",
		MaxRetries:          model.DefaultRetries,
		RetryCount:          0,
		RateLimitRetryCount: 0,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	execErr := fmt.Errorf("agent execution failed: Failed to authenticate. API Error: 403 {\"error\":{\"type\":\"forbidden\",\"message\":\"Request not allowed\"}}")
	if err := svc.HandleExecutionFailure(ctx, task, execErr); err == nil {
		t.Fatal("expected permanent auth error to be returned")
	}

	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want %q", found.Status, model.TaskStatusFailed)
	}
	if found.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want 0", found.RetryCount)
	}
	if found.RateLimitRetryCount != 0 {
		t.Fatalf("rate_limit_retry_count = %d, want 0", found.RateLimitRetryCount)
	}
	if found.CompletedAt == nil {
		t.Fatal("completed_at was not set")
	}
	if !strings.Contains(found.ErrorMessage, "API Error: 403") {
		t.Fatalf("error_message = %q, want API Error: 403", found.ErrorMessage)
	}
	if len(enqueuer.enqueued) != 0 {
		t.Fatalf("auth error should not enqueue retries, got %d", len(enqueuer.enqueued))
	}
}

func TestTaskService_HandleExecutionFailure_DoesNotAutoRetry(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enqueuer := &mockEnqueuer{}
	svc := NewTaskService(repo, nil, enqueuer, nil, nil, &logger, "", nil, "", nil, nil)

	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:         uuid.New().String(),
		UserID:     userID,
		ProjectID:  projectID,
		Type:       model.PlatformArticle,
		Status:     model.TaskStatusRunning,
		Prompt:     "manual recovery only",
		MaxRetries: model.DefaultRetries,
		RetryCount: 0,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	execErr := fmt.Errorf("agent execution failed: transient model error")
	if err := svc.HandleExecutionFailure(ctx, task, execErr); err == nil {
		t.Fatal("expected execution error to be returned")
	}

	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want %q", found.Status, model.TaskStatusFailed)
	}
	if found.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want 0", found.RetryCount)
	}
	if found.CompletedAt == nil {
		t.Fatal("completed_at was not set")
	}
	if !strings.Contains(found.ErrorMessage, "transient model error") {
		t.Fatalf("error_message = %q, want transient model error", found.ErrorMessage)
	}
	if len(enqueuer.enqueued) != 0 {
		t.Fatalf("execution failure should not enqueue retries, got %d", len(enqueuer.enqueued))
	}
}

func TestTaskService_CloneClonesCompletedTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	src := &model.Task{
		ID:         uuid.New().String(),
		UserID:     userID,
		ProjectID:  projectID,
		Type:       model.PlatformArticle,
		Status:     model.TaskStatusCompleted,
		Prompt:     "finished topic",
		ImageRatio: "16:9",
		Goal:       "keep the same goal",
		GoalMode:   true,
	}
	if err := repo.Tasks().Create(ctx, src); err != nil {
		t.Fatalf("create source task: %v", err)
	}

	clone, err := svc.Clone(ctx, src.ID)
	if err != nil {
		t.Fatalf("Clone completed task: %v", err)
	}
	if clone.ID == src.ID {
		t.Fatal("clone reused the original task id")
	}
	if clone.Status != model.TaskStatusPending {
		t.Fatalf("clone status = %q, want %q", clone.Status, model.TaskStatusPending)
	}
	if clone.Prompt != src.Prompt || clone.ImageRatio != src.ImageRatio || clone.Goal != src.Goal || clone.GoalMode != src.GoalMode {
		t.Fatalf("clone config = prompt %q ratio %q goal %q mode %v, want source config", clone.Prompt, clone.ImageRatio, clone.Goal, clone.GoalMode)
	}
	foundSrc, err := repo.Tasks().FindByID(ctx, src.ID)
	if err != nil {
		t.Fatalf("find source task: %v", err)
	}
	if foundSrc.Status != model.TaskStatusCompleted {
		t.Fatalf("source status = %q, want completed", foundSrc.Status)
	}
}

func TestTaskService_ResumeReusesTaskAndWritesPromptAndFiles(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enqueuer := &mockEnqueuer{}
	workspaceRoot := t.TempDir()
	svc := NewTaskService(repo, nil, enqueuer, nil, nil, &logger, "", nil, workspaceRoot, nil, nil)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	completedAt := time.Now().Add(-time.Minute)
	resultJSON := `{"success":true}`
	workflowStatus := `{"current_stage":"done"}`
	task := &model.Task{
		ID:             uuid.New().String(),
		UserID:         userID,
		ProjectID:      projectID,
		Type:           model.PlatformArticle,
		Status:         model.TaskStatusCompleted,
		Prompt:         "finished topic",
		Progress:       100,
		Result:         &resultJSON,
		ErrorMessage:   "old error",
		CompletedAt:    &completedAt,
		WorkflowStatus: &workflowStatus,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	workDir := filepath.Join(workspaceRoot, task.ID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{
		Prompt: "请基于现有草稿补充案例",
		Files: []ResumeTaskFile{
			{
				OriginalName: "客户 反馈.txt",
				Label:        "客户反馈",
				Reader:       strings.NewReader("feedback"),
				Size:         int64(len("feedback")),
			},
		},
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.ID != task.ID {
		t.Fatalf("resumed id = %q, want original %q", resumed.ID, task.ID)
	}
	if resumed.Status != model.TaskStatusPending {
		t.Fatalf("status = %q, want pending", resumed.Status)
	}
	if resumed.CompletedAt != nil || resumed.Result != nil || resumed.ErrorMessage != "" || resumed.Progress != 0 || resumed.WorkflowStatus != nil {
		t.Fatalf("resume did not reset execution state: %+v", resumed)
	}
	if len(enqueuer.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(enqueuer.enqueued))
	}

	resumeRoot := filepath.Join(workDir, ".anban-creator", "resume")
	latest, err := os.ReadFile(filepath.Join(resumeRoot, "latest.md"))
	if err != nil {
		t.Fatalf("read latest.md: %v", err)
	}
	latestText := string(latest)
	for _, want := range []string{"请基于现有草稿补充案例", "客户反馈", "客户 反馈.txt", "attachments/客户_反馈.txt"} {
		if !strings.Contains(latestText, want) {
			t.Fatalf("latest.md missing %q:\n%s", want, latestText)
		}
	}
	matches, err := filepath.Glob(filepath.Join(resumeRoot, "*", "attachments", "客户_反馈.txt"))
	if err != nil {
		t.Fatalf("glob attachment: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("attachment matches = %v, want one sanitized file", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read attachment: %v", err)
	}
	if string(data) != "feedback" {
		t.Fatalf("attachment = %q, want feedback", string(data))
	}
}

func TestWriteResumeInputsPublishesLatestOnlyWhenRequested(t *testing.T) {
	workDir := t.TempDir()
	runDir, body, err := writeResumeInputs(context.Background(), workDir, "第一次补充", nil)
	if err != nil {
		t.Fatalf("writeResumeInputs: %v", err)
	}
	if runDir == "" || body == "" {
		t.Fatalf("writeResumeInputs returned runDir=%q body=%q", runDir, body)
	}
	latestPath := filepath.Join(workDir, ".anban-creator", "resume", "latest.md")
	if _, err := os.Stat(latestPath); !os.IsNotExist(err) {
		t.Fatalf("latest.md should not be published before CAS success, stat error: %v", err)
	}

	if err := writeResumeLatest(workDir, body); err != nil {
		t.Fatalf("writeResumeLatest: %v", err)
	}
	latest, err := os.ReadFile(latestPath)
	if err != nil {
		t.Fatalf("read latest.md: %v", err)
	}
	if string(latest) != body {
		t.Fatalf("latest.md = %q, want body %q", string(latest), body)
	}
}

func TestTaskService_ResumeRejectsNoInputAndNonTerminal(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{})
	if !errors.Is(err, ErrTaskResumeNoInput) {
		t.Fatalf("Resume empty input error = %v, want ErrTaskResumeNoInput", err)
	}

	task.Status = model.TaskStatusRunning
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task: %v", err)
	}
	_, err = svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if !errors.Is(err, ErrTaskResumeNotTerminal) {
		t.Fatalf("Resume running task error = %v, want ErrTaskResumeNotTerminal", err)
	}
}

func TestTaskRepository_ResetTerminalTaskForResumeOnlyOneStatusSwap(t *testing.T) {
	_, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusFailed,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	first, err := repo.Tasks().ResetTerminalTaskForResume(ctx, task.ID)
	if err != nil {
		t.Fatalf("first ResetTerminalTaskForResume: %v", err)
	}
	if !first {
		t.Fatal("first ResetTerminalTaskForResume = false, want true")
	}
	second, err := repo.Tasks().ResetTerminalTaskForResume(ctx, task.ID)
	if err != nil {
		t.Fatalf("second ResetTerminalTaskForResume: %v", err)
	}
	if second {
		t.Fatal("second ResetTerminalTaskForResume = true, want false after status changed")
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusPending {
		t.Fatalf("status = %q, want pending", found.Status)
	}
}

func TestTaskService_ResumeRejectsMissingWorkspace(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusFailed,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if !errors.Is(err, ErrTaskResumeWorkspaceMissing) {
		t.Fatalf("Resume error = %v, want ErrTaskResumeWorkspaceMissing", err)
	}
}

func TestBuildNoOutputFilesErrorIncludesLastToolError(t *testing.T) {
	result := &agent.ExecutionResult{
		Model:             "claude-test",
		NumTurns:          10,
		ToolUseCount:      9,
		ToolErrorCount:    1,
		LastToolErrorTool: "generate_image",
		LastToolError:     "provider rejected model",
	}

	msg := buildNoOutputFilesError(result)
	if !strings.Contains(msg, "tool_errors=1") {
		t.Fatalf("error = %q, want tool error count", msg)
	}
	if !strings.Contains(msg, "generate_image failed: provider rejected model") {
		t.Fatalf("error = %q, want last MCP tool error", msg)
	}
	if strings.Contains(msg, "check user model config") {
		t.Fatalf("error = %q, should not use old generic model-config hint", msg)
	}
}

func TestBuildNoOutputFilesErrorWithoutToolErrorKeepsFallback(t *testing.T) {
	result := &agent.ExecutionResult{
		Model:        "",
		NumTurns:     10,
		ToolUseCount: 9,
	}

	msg := buildNoOutputFilesError(result)
	if !strings.Contains(msg, "tool_uses=9") {
		t.Fatalf("error = %q, want tool use count", msg)
	}
	if !strings.Contains(msg, "files may have been written to an unexpected location") {
		t.Fatalf("error = %q, want fallback location diagnostic", msg)
	}
}

func TestTaskService_CreateManual(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	tasks, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Test topic",
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.ID == "" {
		t.Error("task.ID should not be empty")
	}
	if task.UserID != userID {
		t.Errorf("UserID = %q, want %q", task.UserID, userID)
	}
	if task.ProjectID != projectID {
		t.Errorf("ProjectID = %q, want %q", task.ProjectID, projectID)
	}
	if task.Status != model.TaskStatusPending {
		t.Errorf("Status = %q, want %q", task.Status, model.TaskStatusPending)
	}
	if task.Prompt != "Test topic" {
		t.Errorf("Prompt = %q, want %q", task.Prompt, "Test topic")
	}
}

func TestTaskService_CreateManual_NoProject(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	_, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    "user1",
		ProjectID: "",
		Prompt:    "topic",
	})
	if err == nil {
		t.Error("expected error for empty project_id")
	}
}

func TestTaskService_CreateManual_WrongUser(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	_, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    "wrong-user",
		ProjectID: projectID,
		Prompt:    "topic",
	})
	if err == nil {
		t.Error("expected error for wrong user")
	}
}

func TestTaskService_GetByID(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Find me",
	})
	task := taskSlice[0]

	found, err := svc.GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found.ID != task.ID {
		t.Errorf("ID = %q, want %q", found.ID, task.ID)
	}
}

func TestTaskService_GetByID_NotFound(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	_, err := svc.GetByID(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestTaskService_Cancel(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Cancel me",
	})
	task := taskSlice[0]

	err := svc.Cancel(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	found, _ := svc.GetByID(context.Background(), task.ID)
	if found.Status != model.TaskStatusCancelled {
		t.Errorf("Status = %q, want %q", found.Status, model.TaskStatusCancelled)
	}
}

func TestTaskService_CancelEnqueuesIlinkNotification(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")
	if err := repo.IlinkBindings().Create(context.Background(), &model.IlinkBinding{
		ID:                uuid.NewString(),
		UserID:            userID,
		PlatformAccountID: stringPtr("platform-1"),
		ExternalUserID:    stringPtr("wx-user-1"),
		Status:            model.IlinkBindingStatusActive,
	}); err != nil {
		t.Fatalf("create ilink binding: %v", err)
	}
	log := zerolog.Nop()
	svc.SetIlinkNotifier(NewIlinkNotifier(repo, true, &log))

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Cancel me",
	})
	task := taskSlice[0]

	if err := svc.Cancel(context.Background(), task.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	items, err := repo.IlinkNotifications().ListDue(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("outbox items = %d, want 1", len(items))
	}
	if items[0].TaskID != task.ID || items[0].TaskStatus != model.TaskStatusCancelled {
		t.Fatalf("unexpected notification: %+v", items[0])
	}
}

func TestTaskService_HandleExecutionEarlyFailureEnqueuesIlinkNotification(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	if err := repo.Users().Create(context.Background(), &model.User{
		ID:         userID,
		Email:      "early-failure@example.com",
		Password:   "x",
		InviteCode: "early-failure",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.IlinkBindings().Create(context.Background(), &model.IlinkBinding{
		ID:                uuid.NewString(),
		UserID:            userID,
		PlatformAccountID: stringPtr("platform-1"),
		ExternalUserID:    stringPtr("wx-user-1"),
		Status:            model.IlinkBindingStatusActive,
	}); err != nil {
		t.Fatalf("create ilink binding: %v", err)
	}
	log := zerolog.Nop()
	svc.SetIlinkNotifier(NewIlinkNotifier(repo, true, &log))
	task := &model.Task{
		ID:        uuid.NewString(),
		UserID:    userID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
		Prompt:    "Missing project",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.HandleExecution(context.Background(), task, nil); err == nil {
		t.Fatal("HandleExecution error = nil, want missing project error")
	}

	items, err := repo.IlinkNotifications().ListDue(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("outbox items = %d, want 1", len(items))
	}
	if items[0].TaskID != task.ID || items[0].TaskStatus != model.TaskStatusFailed {
		t.Fatalf("unexpected notification: %+v", items[0])
	}
}

func TestTaskService_List(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Task 1",
	})
	svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Task 2",
	})

	tasks, total, err := svc.List(context.Background(), userID, 0, 10, "", "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total < 2 {
		t.Errorf("total = %d, want >= 2", total)
	}
	if len(tasks) < 2 {
		t.Errorf("len(tasks) = %d, want >= 2", len(tasks))
	}
}

func TestTaskService_List_ByStatus(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Pending task",
	})
	task := taskSlice[0]
	svc.Cancel(context.Background(), task.ID)

	// Filter by pending — should find none
	tasks, total, err := svc.List(context.Background(), userID, 0, 10, model.TaskStatusPending, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 0 {
		t.Errorf("pending total = %d, want 0", total)
	}
	if len(tasks) != 0 {
		t.Errorf("pending tasks = %d, want 0", len(tasks))
	}

	// Filter by cancelled
	tasks, total, err = svc.List(context.Background(), userID, 0, 10, model.TaskStatusCancelled, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Errorf("cancelled total = %d, want 1", total)
	}
}

func TestTaskService_GetFiles(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	files, err := svc.GetFiles(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestTaskService_SetPublishedCreatesSeednoteTracking(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	trackingSvc := &fakePublishedTrackingService{}
	svc.SetSeednoteTrackingService(trackingSvc)

	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.SetPublished(ctx, userID, task.ID, true); err != nil {
		t.Fatalf("SetPublished: %v", err)
	}

	if len(trackingSvc.calls) != 1 {
		t.Fatalf("tracking calls = %d, want 1", len(trackingSvc.calls))
	}
	if trackingSvc.calls[0].userID != userID || trackingSvc.calls[0].taskID != task.ID {
		t.Fatalf("tracking call = %+v", trackingSvc.calls[0])
	}
}

func TestTaskService_SetPublishedSkipsTrackingForNonSeednoteOrUnpublish(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	trackingSvc := &fakePublishedTrackingService{}
	svc.SetSeednoteTrackingService(trackingSvc)

	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.SetPublished(ctx, userID, task.ID, true); err != nil {
		t.Fatalf("SetPublished article: %v", err)
	}
	if err := svc.SetPublished(ctx, userID, task.ID, false); err != nil {
		t.Fatalf("SetPublished false: %v", err)
	}
	if len(trackingSvc.calls) != 0 {
		t.Fatalf("tracking calls = %d, want 0", len(trackingSvc.calls))
	}
}

func TestTaskService_DownloadTasksZip(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	svc.store = store

	ctx := context.Background()
	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	otherProjectID := createTestProject(t, repo, otherUserID, model.PlatformSeednote)

	completed := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Prompt:    "A finished task",
		Title:     "Finished",
	}
	pending := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusPending,
		Prompt:    "A pending task",
	}
	foreign := &model.Task{
		ID:        uuid.New().String(),
		UserID:    otherUserID,
		ProjectID: otherProjectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Prompt:    "Foreign task",
	}
	for _, task := range []*model.Task{completed, pending, foreign} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	if _, err := svc.UploadTaskFileFromReader(ctx, completed.ID, userID, "output/article.md", strings.NewReader("# hello"), "text/markdown", 7); err != nil {
		t.Fatalf("upload completed task file: %v", err)
	}
	if _, err := svc.UploadTaskFileFromReader(ctx, foreign.ID, otherUserID, "output/secret.md", strings.NewReader("secret"), "text/markdown", 6); err != nil {
		t.Fatalf("upload foreign task file: %v", err)
	}

	buf, zipName, err := svc.DownloadTasksZip(ctx, userID, []string{completed.ID, pending.ID, foreign.ID})
	if err != nil {
		t.Fatalf("DownloadTasksZip: %v", err)
	}
	if !strings.HasPrefix(zipName, "tasks_export_") || !strings.HasSuffix(zipName, ".zip") {
		t.Fatalf("unexpected zip name: %s", zipName)
	}

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	var hasManifest bool
	var hasCompletedFile bool
	for _, file := range reader.File {
		if file.Name == "manifest.json" {
			hasManifest = true
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("open manifest: %v", err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			manifest := string(data)
			var parsedManifest BulkDownloadZipManifest
			if err := json.Unmarshal(data, &parsedManifest); err != nil {
				t.Fatalf("parse manifest: %v", err)
			}
			for _, want := range []string{completed.ID, pending.ID, foreign.ID, "task_not_completed", "unavailable"} {
				if !strings.Contains(manifest, want) {
					t.Fatalf("manifest missing %q: %s", want, manifest)
				}
			}
			if strings.Contains(manifest, "forbidden") || strings.Contains(manifest, "task_not_found") {
				t.Fatalf("manifest uses distinguishable unavailable reasons: %s", manifest)
			}
			for _, task := range parsedManifest.Tasks {
				if task.TaskID == foreign.ID && (task.Title != "" || task.Status != "") {
					t.Fatalf("manifest leaked foreign task metadata: %+v", task)
				}
			}
		}
		if strings.HasSuffix(file.Name, "output/article.md") {
			hasCompletedFile = true
		}
		if strings.Contains(file.Name, "secret.md") {
			t.Fatalf("foreign file leaked into zip: %s", file.Name)
		}
	}
	if !hasManifest {
		t.Fatal("manifest.json missing")
	}
	if !hasCompletedFile {
		t.Fatal("completed task file missing")
	}
}

func TestTaskService_RebuildWorkflowStatus(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	svc.store = store

	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
		Prompt:    "workflow task",
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	files := []struct {
		path    string
		body    string
		mime    string
		size    int64
		wantErr bool
	}{
		{"output/03-draft.md", "# Draft", "text/markdown", 7, false},
		{"output/04-final.md", "# Final", "text/markdown", 7, false},
		{"output/review.json", `{"overall_score":91,"readiness":"ready","strengths":["清晰"],"risks":[],"next_actions":["发布"]}`, "application/json", 92, false},
	}
	for _, file := range files {
		if _, err := svc.UploadTaskFileFromReader(ctx, task.ID, userID, file.path, strings.NewReader(file.body), file.mime, file.size); err != nil {
			t.Fatalf("upload %s: %v", file.path, err)
		}
	}

	if err := svc.RebuildWorkflowStatus(ctx, task.ID); err != nil {
		t.Fatalf("RebuildWorkflowStatus: %v", err)
	}

	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.WorkflowStatus == nil || *found.WorkflowStatus == "" {
		t.Fatal("workflow_status was not persisted")
	}

	var status WorkflowStatus
	if err := json.Unmarshal([]byte(*found.WorkflowStatus), &status); err != nil {
		t.Fatalf("unmarshal workflow status: %v", err)
	}
	if status.Review == nil {
		t.Fatal("expected review summary")
	}
	if status.Review.OverallScore != 91 {
		t.Fatalf("review score = %d, want 91", status.Review.OverallScore)
	}
}
