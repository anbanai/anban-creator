package repository

import (
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database with all tables migrated.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	return db
}

func TestNew(t *testing.T) {
	db := setupTestDB(t)

	repo := New(db)

	// Verify all sub-repositories are non-nil.
	if repo.Users() == nil {
		t.Error("Users() should not be nil")
	}
	if repo.Sessions() == nil {
		t.Error("Sessions() should not be nil")
	}
	if repo.Plans() == nil {
		t.Error("Plans() should not be nil")
	}
	if repo.Tasks() == nil {
		t.Error("Tasks() should not be nil")
	}
	if repo.TaskFiles() == nil {
		t.Error("TaskFiles() should not be nil")
	}
}

func TestNew_SeednoteTrackingRepositories(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)

	if repo.SeednoteTrackings() == nil {
		t.Fatal("SeednoteTrackings() should not be nil")
	}
	if repo.SeednoteMetricSnapshots() == nil {
		t.Fatal("SeednoteMetricSnapshots() should not be nil")
	}
}

func TestSeednoteTrackingRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	nextRun := now.Add(24 * time.Hour)

	tracking := &model.SeednotePostTracking{
		ID:                "tracking-1",
		TaskID:            "task-1",
		UserID:            "user-1",
		ProjectID:         "project-1",
		Status:            model.SeednoteTrackingStatusWaitingDiscovery,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/abc",
		PublishedMarkedAt: now,
		NextRunAt:         &nextRun,
	}

	if err := repo.SeednoteTrackings().Create(ctx, tracking); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.SeednoteTrackings().FindByTaskID(ctx, "task-1")
	if err != nil {
		t.Fatalf("FindByTaskID: %v", err)
	}
	if found.ID != "tracking-1" {
		t.Fatalf("ID = %q, want tracking-1", found.ID)
	}

	due, err := repo.SeednoteTrackings().FindDue(ctx, nextRun.Add(time.Second), 10)
	if err != nil {
		t.Fatalf("FindDue: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("due length = %d, want 1", len(due))
	}

	found.Status = model.SeednoteTrackingStatusTracking
	found.NoteID = "note-1"
	if err := repo.SeednoteTrackings().Update(ctx, found); err != nil {
		t.Fatalf("Update: %v", err)
	}

	updated, err := repo.SeednoteTrackings().FindByID(ctx, "tracking-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated.Status != model.SeednoteTrackingStatusTracking || updated.NoteID != "note-1" {
		t.Fatalf("updated tracking = %+v", updated)
	}
}

func TestSeednoteMetricSnapshotRepository_UpsertAndSeries(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	captured := time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)

	first := &model.SeednoteMetricSnapshot{
		ID:           "snapshot-1",
		TrackingID:   "tracking-1",
		TaskID:       "task-1",
		CapturedAt:   captured,
		CapturedDate: model.SeednoteCapturedDate(captured),
		LikeCount:    10,
		CollectCount: 2,
		CommentCount: 1,
		ShareCount:   0,
	}
	if err := repo.SeednoteMetricSnapshots().UpsertByTrackingAndDate(ctx, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second := *first
	second.ID = "snapshot-2"
	second.LikeCount = 15
	if err := repo.SeednoteMetricSnapshots().UpsertByTrackingAndDate(ctx, &second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	series, err := repo.SeednoteMetricSnapshots().FindByTaskID(ctx, "task-1")
	if err != nil {
		t.Fatalf("FindByTaskID: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("series length = %d, want 1", len(series))
	}
	if series[0].LikeCount != 15 {
		t.Fatalf("LikeCount = %d, want 15", series[0].LikeCount)
	}

	latest, err := repo.SeednoteMetricSnapshots().FindLatestByTrackingID(ctx, "tracking-1")
	if err != nil {
		t.Fatalf("FindLatestByTrackingID: %v", err)
	}
	if latest.ID != "snapshot-1" {
		t.Fatalf("latest ID = %q, want snapshot-1 because upsert keeps primary ID", latest.ID)
	}
}

func TestWithTx_Commit(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)

	ctx := context.Background()

	user := &model.User{
		ID:       "user-tx-1",
		Email:    "tx@example.com",
		Nickname: "TxUser",
		Password: "hashed",
	}

	// Successful transaction -- data should be persisted.
	err := repo.WithTx(ctx, func(txRepo Repository) error {
		return txRepo.Users().Create(ctx, user)
	})
	if err != nil {
		t.Fatalf("WithTx commit: %v", err)
	}

	// Verify the user exists via the original repo.
	found, err := repo.Users().FindByID(ctx, "user-tx-1")
	if err != nil {
		t.Fatalf("FindByID after commit: %v", err)
	}
	if found.Nickname != "TxUser" {
		t.Errorf("expected nickname TxUser, got %s", found.Nickname)
	}
}

func TestWithTx_Rollback(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)

	ctx := context.Background()

	// Transaction that returns an error -- data should NOT be persisted.
	err := repo.WithTx(ctx, func(txRepo Repository) error {
		user := &model.User{
			ID:       "user-tx-rollback",
			Email:    "rollback@example.com",
			Nickname: "RollbackUser",
			Password: "hashed",
		}
		if err := txRepo.Users().Create(ctx, user); err != nil {
			return err
		}
		return context.DeadlineExceeded // trigger rollback
	})
	if err == nil {
		t.Fatal("expected error from WithTx, got nil")
	}

	// Verify the user does NOT exist.
	_, err = repo.Users().FindByID(ctx, "user-tx-rollback")
	if err == nil {
		t.Fatal("expected error (not found) after rollback, got nil")
	}
}

func TestUserRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()

	user := &model.User{
		ID:       "user-1",
		Email:    "test@example.com",
		Nickname: "TestUser",
		Password: "hashed",
		Avatar:   "https://example.com/avatar.png",
	}

	// Create
	if err := repo.Users().Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// FindByID
	found, err := repo.Users().FindByID(ctx, "user-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %s", found.Email)
	}

	// FindByEmail
	found, err = repo.Users().FindByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if found.Nickname != "TestUser" {
		t.Errorf("expected nickname TestUser, got %s", found.Nickname)
	}

	_, err = repo.Users().FindByOpenID(ctx, "nonexistent-openid")
	if err == nil {
		t.Fatal("expected error for FindByOpenID with nonexistent ID")
	}

	// Update
	user.Nickname = "UpdatedUser"
	if err := repo.Users().Update(ctx, user); err != nil {
		t.Fatalf("Update: %v", err)
	}
	found, _ = repo.Users().FindByID(ctx, "user-1")
	if found.Nickname != "UpdatedUser" {
		t.Errorf("expected nickname UpdatedUser, got %s", found.Nickname)
	}
}

func TestSessionRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()

	// Create a user first for FK constraint.
	_ = repo.Users().Create(ctx, &model.User{
		ID:       "user-session-1",
		Email:    "session@example.com",
		Nickname: "SessUser",
		Password: "hashed",
	})

	session := &model.LoginSession{
		ID:           "session-1",
		UserID:       "user-session-1",
		Token:        "access-token-abc",
		RefreshToken: "refresh-token-xyz",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}

	// Create
	if err := repo.Sessions().Create(ctx, session); err != nil {
		t.Fatalf("Create session: %v", err)
	}

	// FindByToken
	found, err := repo.Sessions().FindByToken(ctx, "access-token-abc")
	if err != nil {
		t.Fatalf("FindByToken: %v", err)
	}
	if found.ID != "session-1" {
		t.Errorf("expected session id session-1, got %s", found.ID)
	}

	// FindByRefreshToken
	found, err = repo.Sessions().FindByRefreshToken(ctx, "refresh-token-xyz")
	if err != nil {
		t.Fatalf("FindByRefreshToken: %v", err)
	}
	if found.UserID != "user-session-1" {
		t.Errorf("expected user_id user-session-1, got %s", found.UserID)
	}

	// Delete
	if err := repo.Sessions().Delete(ctx, "access-token-abc"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = repo.Sessions().FindByToken(ctx, "access-token-abc")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestTaskRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()

	task := &model.Task{
		ID:     "task-abc123def456",
		UserID: "user-task-1",
		Type:   model.ScopeSeednote,
		Status: model.TaskStatusPending,
		Prompt: "AI trends",
	}

	// Create
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("Create task: %v", err)
	}

	// FindByID
	found, err := repo.Tasks().FindByID(ctx, "task-abc123def456")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.Prompt != "AI trends" {
		t.Errorf("expected topic 'AI trends', got %s", found.Prompt)
	}

	// UpdateStatus
	if err := repo.Tasks().UpdateStatus(ctx, "task-abc123def456", model.TaskStatusRunning); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	found, _ = repo.Tasks().FindByID(ctx, "task-abc123def456")
	if found.Status != model.TaskStatusRunning {
		t.Errorf("expected status running, got %s", found.Status)
	}

	// UpdateStatusAndError
	if err := repo.Tasks().UpdateStatusAndError(ctx, "task-abc123def456", model.TaskStatusFailed, "something went wrong"); err != nil {
		t.Fatalf("UpdateStatusAndError: %v", err)
	}
	found, _ = repo.Tasks().FindByID(ctx, "task-abc123def456")
	if found.ErrorMessage != "something went wrong" {
		t.Errorf("expected error message, got %s", found.ErrorMessage)
	}

	// UpdateProgressLog
	if err := repo.Tasks().UpdateProgressLog(ctx, "task-abc123def456", "step 1 done"); err != nil {
		t.Fatalf("UpdateProgressLog: %v", err)
	}
	found, _ = repo.Tasks().FindByID(ctx, "task-abc123def456")
	if found.ProgressLog != "step 1 done" {
		t.Errorf("expected progress log, got %s", found.ProgressLog)
	}

	// UpdateLatestProgress — verify JSON round-trip via datatypes.JSONType.
	lp := model.ProgressPayload{
		Stage:       "research",
		Title:       "选题研究",
		Description: "采集热门笔记数据",
		Percent:     15,
	}
	if err := repo.Tasks().UpdateLatestProgress(ctx, "task-abc123def456", lp); err != nil {
		t.Fatalf("UpdateLatestProgress: %v", err)
	}
	found, _ = repo.Tasks().FindByID(ctx, "task-abc123def456")
	if got := found.LatestProgress.Data(); got.Stage != "research" || got.Title != "选题研究" || got.Percent != 15 {
		t.Errorf("latest_progress round-trip mismatch: %+v", got)
	}
	// Second write overwrites (last-writer-wins) — verifies the column is updatable, not append-only.
	lp2 := model.ProgressPayload{Stage: "writing", Title: "内容写作", Percent: 45}
	if err := repo.Tasks().UpdateLatestProgress(ctx, "task-abc123def456", lp2); err != nil {
		t.Fatalf("UpdateLatestProgress (overwrite): %v", err)
	}
	found, _ = repo.Tasks().FindByID(ctx, "task-abc123def456")
	if got := found.LatestProgress.Data(); got.Stage != "writing" || got.Description != "" {
		t.Errorf("latest_progress overwrite mismatch: %+v", got)
	}

	// UpdateResult
	if err := repo.Tasks().UpdateResult(ctx, "task-abc123def456", `{"url":"https://example.com"}`); err != nil {
		t.Fatalf("UpdateResult: %v", err)
	}
	found, _ = repo.Tasks().FindByID(ctx, "task-abc123def456")
	if found.Result == nil || *found.Result != `{"url":"https://example.com"}` {
		t.Errorf("expected result json, got %v", found.Result)
	}

	// SetStartedAt
	if err := repo.Tasks().SetStartedAt(ctx, "task-abc123def456"); err != nil {
		t.Fatalf("SetStartedAt: %v", err)
	}
	found, _ = repo.Tasks().FindByID(ctx, "task-abc123def456")
	if found.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}

	// SetCompletedAt
	if err := repo.Tasks().SetCompletedAt(ctx, "task-abc123def456"); err != nil {
		t.Fatalf("SetCompletedAt: %v", err)
	}
	found, _ = repo.Tasks().FindByID(ctx, "task-abc123def456")
	if found.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}

	// CountByUserID
	count, err := repo.Tasks().CountByUserID(ctx, "user-task-1", "")
	if err != nil {
		t.Fatalf("CountByUserID: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
}

func TestTaskRepository_FindTitlesByProjectID(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()

	tasks := []*model.Task{
		{
			ID:        "task-title-1",
			UserID:    "user-title-1",
			ProjectID: "project-title-1",
			Type:      model.ScopeSeednote,
			Status:    model.TaskStatusCompleted,
			Prompt:    "this prompt must not be returned",
			Title:     "早餐店爆款标题",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        "task-title-2",
			UserID:    "user-title-1",
			ProjectID: "project-title-1",
			Type:      model.ScopeSeednote,
			Status:    model.TaskStatusCompleted,
			Prompt:    "empty title prompt must not be returned",
			Title:     "",
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        "task-title-3",
			UserID:    "user-title-1",
			ProjectID: "project-title-1",
			Type:      model.ScopeSeednote,
			Status:    model.TaskStatusCompleted,
			Prompt:    "another prompt must not be returned",
			Title:     "咖啡探店避坑指南",
			CreatedAt: time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        "task-title-4",
			UserID:    "user-title-1",
			ProjectID: "other-project",
			Type:      model.ScopeSeednote,
			Status:    model.TaskStatusCompleted,
			Prompt:    "other project prompt",
			Title:     "其他项目标题",
			CreatedAt: time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC),
		},
	}

	for _, task := range tasks {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("Create task %s: %v", task.ID, err)
		}
	}

	titles, err := repo.Tasks().FindTitlesByProjectID(ctx, "project-title-1")
	if err != nil {
		t.Fatalf("FindTitlesByProjectID: %v", err)
	}

	want := []string{"咖啡探店避坑指南", "早餐店爆款标题"}
	if len(titles) != len(want) {
		t.Fatalf("titles len = %d, want %d: %v", len(titles), len(want), titles)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Errorf("titles[%d] = %q, want %q", i, titles[i], want[i])
		}
	}
}

func TestTaskRepository_ClearArtifactTitles(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()

	for _, task := range []*model.Task{
		{
			ID:        "task-artifact-title-1",
			UserID:    "user-artifact-title",
			ProjectID: "project-artifact-title",
			Type:      model.ScopeSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "图片内容规划",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        "task-artifact-title-2",
			UserID:    "user-artifact-title",
			ProjectID: "project-artifact-title",
			Type:      model.ScopeSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "真实标题",
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("Create task %s: %v", task.ID, err)
		}
	}

	count, err := repo.Tasks().ClearTitles(ctx, []string{"图片内容规划", "标题候选与评分", "选题研究报告", "违禁词合规检查报告"})
	if err != nil {
		t.Fatalf("ClearTitles: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	titles, err := repo.Tasks().FindTitlesByProjectID(ctx, "project-artifact-title")
	if err != nil {
		t.Fatalf("FindTitlesByProjectID: %v", err)
	}
	if len(titles) != 1 || titles[0] != "真实标题" {
		t.Fatalf("titles = %v, want [真实标题]", titles)
	}
}

func TestPlanRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()

	plan := &model.Plan{
		ID:       "plan-1",
		UserID:   "user-plan-1",
		Type:     model.ScopeArticle,
		Title:    "Daily articles",
		CronExpr: "0 9 * * *",
		Status:   model.PlanStatusActive,
	}

	// Create
	if err := repo.Plans().Create(ctx, plan); err != nil {
		t.Fatalf("Create plan: %v", err)
	}

	// FindByID
	found, err := repo.Plans().FindByID(ctx, "plan-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.Title != "Daily articles" {
		t.Errorf("expected title 'Daily articles', got %s", found.Title)
	}

	// ListActiveByUserID
	activePlans, err := repo.Plans().ListActiveByUserID(ctx, "user-plan-1", "")
	if err != nil {
		t.Fatalf("ListActiveByUserID: %v", err)
	}
	if len(activePlans) != 1 {
		t.Errorf("expected 1 active plan, got %d", len(activePlans))
	}

	// ListActive
	allActive, err := repo.Plans().ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(allActive) != 1 {
		t.Errorf("expected 1 active plan globally, got %d", len(allActive))
	}

	// Update
	plan.Status = model.PlanStatusPaused
	if err := repo.Plans().Update(ctx, plan); err != nil {
		t.Fatalf("Update: %v", err)
	}
	activePlans, _ = repo.Plans().ListActiveByUserID(ctx, "user-plan-1", "")
	if len(activePlans) != 0 {
		t.Errorf("expected 0 active plans after pause, got %d", len(activePlans))
	}

	// Delete
	if err := repo.Plans().Delete(ctx, "plan-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = repo.Plans().FindByID(ctx, "plan-1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestTaskFileRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()

	file := &model.TaskFile{
		TaskID:    "task-file-1",
		Role:      "cover",
		FilePath:  "/tmp/cover.png",
		MediaID:   "media-123",
		WechatURL: "https://mmbiz.qpic.cn/cover.png",
	}

	// Create
	if err := repo.TaskFiles().Create(ctx, file); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// FindByTaskID
	files, err := repo.TaskFiles().FindByTaskID(ctx, "task-file-1")
	if err != nil {
		t.Fatalf("FindByTaskID: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Role != "cover" {
		t.Errorf("expected role cover, got %s", files[0].Role)
	}

	// BatchCreate
	moreFiles := []*model.TaskFile{
		{TaskID: "task-batch-1", Role: "content", FilePath: "/tmp/a.md"},
		{TaskID: "task-batch-1", Role: "image", FilePath: "/tmp/b.png"},
	}
	if err := repo.TaskFiles().BatchCreate(ctx, moreFiles); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}
	found, err := repo.TaskFiles().FindByTaskID(ctx, "task-batch-1")
	if err != nil {
		t.Fatalf("FindByTaskID after batch: %v", err)
	}
	if len(found) != 2 {
		t.Errorf("expected 2 batch files, got %d", len(found))
	}
}
