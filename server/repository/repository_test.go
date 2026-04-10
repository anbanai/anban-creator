package repository

import (
	"context"
	"testing"
	"time"

	"github.com/royalrick/anbanwriter/server/model"

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
	if repo.UserConfigs() == nil {
		t.Error("UserConfigs() should not be nil")
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

func TestWithTx_Commit(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)

	ctx := context.Background()

	user := &model.User{
		ID:        "user-tx-1",
		Email:     "tx@example.com",
		Nickname:  "TxUser",
		Password:  "hashed",
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
			ID:        "user-tx-rollback",
			Email:     "rollback@example.com",
			Nickname:  "RollbackUser",
			Password:  "hashed",
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
		Phone:    "13800138000",
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

	// FindByPhone
	found, err = repo.Users().FindByPhone(ctx, "13800138000")
	if err != nil {
		t.Fatalf("FindByPhone: %v", err)
	}
	if found.ID != "user-1" {
		t.Errorf("expected id user-1, got %s", found.ID)
	}

	// FindByOpenID (should not exist)
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
		Type:   model.ScopeRednote,
		Status: model.TaskStatusPending,
		Topic:  "AI trends",
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
	if found.Topic != "AI trends" {
		t.Errorf("expected topic 'AI trends', got %s", found.Topic)
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

// UserConfigRepository tests removed — UserConfig is now a legacy migration model.
