package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// These tests cover the Batch 4A publish-approval state machine (holdApprove/
// reject guards). The actual publish goroutine (autoPublishWithData) is the
// same code path exercised by the existing auto-publish tests, so we focus on
// the synchronous guards and the Reject happy path (which never touches the
// publishing service).

// setupApprovalService builds a TaskService on a fresh sqlite DB with a user +
// article project that has publishing + require-approval enabled. Pass
// withPublishingSvc=true to inject a real (unconfigured) PublishingService so
// ApprovePublish can pass its nil-publishing guard.
func setupApprovalService(t *testing.T, withPublishingSvc bool) (*TaskService, repository.Repository, string, string) {
	t.Helper()
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		if sqlDB, _ := db.DB(); sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()

	var pubSvc *PublishingService
	if withPublishingSvc {
		pubSvc = NewPublishingService(repo, &logger)
	}
	svc := NewTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, pubSvc)

	userID := uuid.New().String()
	projectID := uuid.New().String()
	// Email must be unique per test: setupTaskTestDB uses a shared in-memory
	// sqlite cache that persists across tests in this package, so a fixed email
	// would collide on the second test. Key it off the (random) userID.
	if err := repo.Users().Create(context.Background(), &model.User{ID: userID, Email: userID + "@approval.test", Password: "x", InviteCode: userID}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(context.Background(), &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Approval",
		Status:   model.ProjectStatusActive,
		Config: model.ProjectConfig{
			EnablePublishing:       true,
			RequirePublishApproval: true,
		},
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return svc, repo, userID, projectID
}

// seedApprovalTask inserts a completed article task in the given approval state
// with the given frozen articles (nil → no pending blob).
func seedApprovalTask(t *testing.T, repo repository.Repository, userID, projectID, state string, articles []DraftArticleInput) string {
	t.Helper()
	task := &model.Task{
		ID:                   uuid.New().String(),
		UserID:               userID,
		ProjectID:            projectID,
		Type:                 model.ScopeArticle,
		Status:               model.TaskStatusCompleted,
		PublishApprovalState: state,
	}
	if articles != nil {
		blob, err := json.Marshal(articles)
		if err != nil {
			t.Fatalf("marshal articles: %v", err)
		}
		task.PendingDraftArticles = blob
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return task.ID
}

func TestRejectPublish_PendingToRejected(t *testing.T) {
	svc, repo, uid, pid := setupApprovalService(t, false)
	id := seedApprovalTask(t, repo, uid, pid, model.PublishApprovalStatePending, []DraftArticleInput{{Title: "T", Content: "<p>c</p>"}})

	if err := svc.RejectPublish(context.Background(), id, "需要改标题"); err != nil {
		t.Fatalf("RejectPublish: %v", err)
	}
	got, err := repo.Tasks().FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if got.PublishApprovalState != model.PublishApprovalStateRejected {
		t.Fatalf("state = %q, want %q", got.PublishApprovalState, model.PublishApprovalStateRejected)
	}
	// Spec: rejecting clears the frozen blob (the gate is closed; the held
	// draft is no longer needed). CompareAndSwapPublishApproval clears in the
	// same atomic update.
	if len(got.PendingDraftArticles) != 0 {
		t.Fatalf("pending draft articles not cleared on reject; want cleared per spec")
	}
}

func TestRejectPublish_NotPending(t *testing.T) {
	svc, repo, uid, pid := setupApprovalService(t, false)
	// Already-approved task cannot be rejected again.
	id := seedApprovalTask(t, repo, uid, pid, model.PublishApprovalStateApproved, nil)

	err := svc.RejectPublish(context.Background(), id, "")
	if !errors.Is(err, ErrPublishApprovalNotPending) {
		t.Fatalf("err = %v, want ErrPublishApprovalNotPending", err)
	}
}

func TestApprovePublish_NotPending(t *testing.T) {
	svc, repo, uid, pid := setupApprovalService(t, true)
	// A task that never entered the gate (empty state) cannot be approved.
	id := seedApprovalTask(t, repo, uid, pid, model.PublishApprovalStateEmpty, nil)

	err := svc.ApprovePublish(context.Background(), id)
	if !errors.Is(err, ErrPublishApprovalNotPending) {
		t.Fatalf("err = %v, want ErrPublishApprovalNotPending", err)
	}
}

func TestApprovePublish_NilPublishingService(t *testing.T) {
	// Degraded mode: no publishing service → cannot resume publish even if pending.
	svc, repo, uid, pid := setupApprovalService(t, false)
	id := seedApprovalTask(t, repo, uid, pid, model.PublishApprovalStatePending, []DraftArticleInput{{Title: "T"}})

	err := svc.ApprovePublish(context.Background(), id)
	if !errors.Is(err, ErrPublishApprovalUnavailable) {
		t.Fatalf("err = %v, want ErrPublishApprovalUnavailable", err)
	}
}

func TestApprovePublish_NoArticles(t *testing.T) {
	// Pending but the frozen blob decoded to zero articles (data-integrity edge).
	svc, repo, uid, pid := setupApprovalService(t, true)
	id := seedApprovalTask(t, repo, uid, pid, model.PublishApprovalStatePending, []DraftArticleInput{})

	err := svc.ApprovePublish(context.Background(), id)
	if !errors.Is(err, ErrPublishApprovalUnavailable) {
		t.Fatalf("err = %v, want ErrPublishApprovalUnavailable", err)
	}
}

// TestCompareAndSwapPublishApproval_FlipIsAtomicAndClearsBlob proves the
// double-publish guard at the repository level: a second CAS against the same
// expected state loses (RowsAffected=0 → swapped=false), so ApprovePublish —
// which only spawns its publish goroutine when swapped is true — can never
// publish twice under concurrent calls. It also asserts the frozen blob is
// cleared on approve (spec). We exercise the primitive directly instead of the
// service to avoid the real publish goroutine (autoPublishWithData) racing the
// test DB on close; the wiring (gate goroutine on swapped) is the trivial one
// line in ApprovePublish.
func TestCompareAndSwapPublishApproval_FlipIsAtomicAndClearsBlob(t *testing.T) {
	_, repo, uid, pid := setupApprovalService(t, false)
	id := seedApprovalTask(t, repo, uid, pid, model.PublishApprovalStatePending, []DraftArticleInput{{Title: "T", Content: "<p>c</p>"}})

	// First CAS wins: pending → approved, clears the frozen blob.
	swapped, err := repo.Tasks().CompareAndSwapPublishApproval(context.Background(), id,
		model.PublishApprovalStatePending, model.PublishApprovalStateApproved, true)
	if err != nil || !swapped {
		t.Fatalf("first CAS swapped=%v err=%v, want true/nil", swapped, err)
	}
	// Second CAS against the same expected state must lose — the state is now
	// approved, so the WHERE clause matches zero rows. This is the exact
	// guarantee that prevents a double publish under concurrent approve calls.
	swapped2, err := repo.Tasks().CompareAndSwapPublishApproval(context.Background(), id,
		model.PublishApprovalStatePending, model.PublishApprovalStateApproved, true)
	if err != nil {
		t.Fatalf("second CAS err=%v", err)
	}
	if swapped2 {
		t.Fatalf("second CAS swapped=true, want false (state already flipped)")
	}

	got, err := repo.Tasks().FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if got.PublishApprovalState != model.PublishApprovalStateApproved {
		t.Fatalf("state = %q, want approved", got.PublishApprovalState)
	}
	if len(got.PendingDraftArticles) != 0 {
		t.Fatalf("frozen blob not cleared on approve; want cleared per spec")
	}
}
