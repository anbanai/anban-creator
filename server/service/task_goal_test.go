package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// goalStubLLM is an LLMClient stub that returns a configured evaluation
// response (or an error) on each call. Used to drive GoalEvaluator in tests.
type goalStubLLM struct {
	resp  string
	err   error
	calls int
}

func (s *goalStubLLM) Complete(_ context.Context, _, _ string) (string, error) {
	s.calls++
	if s.err != nil {
		return "", s.err
	}
	return s.resp, nil
}

func (s *goalStubLLM) CompleteWithImage(_ context.Context, _, _, _ string) (string, error) {
	return "", errors.New("not implemented")
}

// setupGoalTestService spins up an in-memory SQLite DB with the goal-mode
// tables, wires up a TaskService + GoalEvaluator backed by the provided LLM
// stub, and returns both for the test to drive.
func setupGoalTestService(t *testing.T, llm *goalStubLLM) (*TaskService, repository.Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.New().String()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, _ := db.DB(); sqlDB != nil {
			sqlDB.Close()
		}
	})

	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if llm != nil {
		ev := NewGoalEvaluator(llm, &logger)
		svc.SetGoalEvaluator(ev, 3, 3)
	}
	return svc, repo
}

// createGoalTask inserts a task in "running" status with goal mode enabled
// and returns it. The task references a real channel so retry re-enqueue has
// something to load.
func createGoalTask(t *testing.T, repo repository.Repository, userID string, attempts int) *model.Task {
	t.Helper()
	channelID := uuid.New().String()
	if err := repo.Channels().Create(context.Background(), &model.Channel{
		ID:       channelID,
		UserID:   userID,
		Name:     "Goal Test Channel",
		Platform: model.PlatformSeednote,
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	task := &model.Task{
		ID:              uuid.New().String(),
		UserID:          userID,
		ChannelID:       channelID,
		Type:            model.PlatformSeednote,
		Status:          model.TaskStatusRunning,
		Prompt:          "写一篇种草笔记",
		GoalMode:        true,
		Goal:            "字数 ≥ 100",
		GoalMaxAttempts: 3,
		GoalAttempts:    attempts,
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func createGoalTestUser(t *testing.T, repo repository.Repository) string {
	t.Helper()
	userID := uuid.New().String()
	if err := repo.Users().Create(context.Background(), &model.User{
		ID:             userID,
		Email:          userID + "@example.com",
		Nickname:       "Goal User",
		Password:       "hashed",
		InviteCode:     strings.ReplaceAll(strings.ToUpper(userID[:8]), "-", "X"),
		CreditsBalance: 0,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return userID
}

// TestGoalOrchestration_GoalAchieved_FallsThroughToCompleted verifies that when
// the evaluator says "achieved=true", the orchestrator returns stop=false so the
// caller proceeds with the normal completed transition.
func TestGoalOrchestration_GoalAchieved_FallsThroughToCompleted(t *testing.T) {
	llm := &goalStubLLM{resp: `{"achieved": true, "reason": "字数达标"}`}
	svc, repo := setupGoalTestService(t, llm)
	ctx := context.Background()

	userID := createGoalTestUser(t, repo)
	task := createGoalTask(t, repo, userID, 0)

	stop, err := svc.evaluateGoalAndMaybeRetry(ctx, task, &agent.ExecutionResult{Success: true})
	if err != nil {
		t.Fatalf("evaluateGoalAndMaybeRetry: %v", err)
	}
	if stop {
		t.Errorf("expected stop=false (caller should proceed to completed), got stop=true")
	}

	updated, _ := repo.Tasks().FindByID(ctx, task.ID)
	if updated.Status != model.TaskStatusRunning {
		t.Errorf("status = %q, want %q (orchestrator must NOT mutate status on achieved)", updated.Status, model.TaskStatusRunning)
	}
	if updated.GoalAttempts != 1 {
		t.Errorf("GoalAttempts = %d, want 1 (counter must increment on each evaluation)", updated.GoalAttempts)
	}
}

// TestGoalOrchestration_GoalNotAchieved_RetriesViaCAS verifies that when the
// evaluator says "achieved=false" and attempts remain, the orchestrator CAS
// transitions the task from running→pending for retry. This is the regression
// test for C1 (the original bug CASed completed→pending, which never swapped
// because the task was still in running).
func TestGoalOrchestration_GoalNotAchieved_RetriesViaCAS(t *testing.T) {
	llm := &goalStubLLM{resp: `{"achieved": false, "reason": "字数不足 100"}`}
	svc, repo := setupGoalTestService(t, llm)
	ctx := context.Background()

	userID := createGoalTestUser(t, repo)
	task := createGoalTask(t, repo, userID, 0) // attempt 1 of 3

	stop, err := svc.evaluateGoalAndMaybeRetry(ctx, task, &agent.ExecutionResult{Success: true})
	if err != nil {
		t.Fatalf("evaluateGoalAndMaybeRetry: %v", err)
	}
	if !stop {
		t.Fatalf("expected stop=true (orchestrator took over the status transition), got stop=false")
	}

	updated, _ := repo.Tasks().FindByID(ctx, task.ID)
	if updated.Status != model.TaskStatusPending {
		t.Errorf("status = %q, want %q (retry must CAS running→pending)", updated.Status, model.TaskStatusPending)
	}
	if updated.GoalAttempts != 1 {
		t.Errorf("GoalAttempts = %d, want 1", updated.GoalAttempts)
	}
	if !strings.Contains(updated.ErrorMessage, "目标未达成") {
		t.Errorf("error_message = %q, want substring '目标未达成'", updated.ErrorMessage)
	}
}

// TestGoalOrchestration_GoalNotAchieved_TerminatesAtGoalNotMet verifies that
// when the evaluator says "achieved=false" and the max attempts are exhausted,
// the task transitions to the goal_not_met terminal status.
func TestGoalOrchestration_GoalNotAchieved_TerminatesAtGoalNotMet(t *testing.T) {
	llm := &goalStubLLM{resp: `{"achieved": false, "reason": "字数仍然不足"}`}
	svc, repo := setupGoalTestService(t, llm)
	ctx := context.Background()

	userID := createGoalTestUser(t, repo)
	// attempt = 2 → next attempt will be 3 of 3, hitting the cap.
	task := createGoalTask(t, repo, userID, 2)

	stop, err := svc.evaluateGoalAndMaybeRetry(ctx, task, &agent.ExecutionResult{Success: true})
	if err != nil {
		t.Fatalf("evaluateGoalAndMaybeRetry: %v", err)
	}
	if !stop {
		t.Fatalf("expected stop=true (goal_not_met terminal transition), got stop=false")
	}

	updated, _ := repo.Tasks().FindByID(ctx, task.ID)
	if updated.Status != model.TaskStatusGoalNotMet {
		t.Errorf("status = %q, want %q", updated.Status, model.TaskStatusGoalNotMet)
	}
	if updated.GoalAttempts != 3 {
		t.Errorf("GoalAttempts = %d, want 3", updated.GoalAttempts)
	}
	if updated.CompletedAt == nil {
		t.Errorf("CompletedAt should be set for terminal goal_not_met")
	}
}

// TestGoalOrchestration_EvaluatorFailure_DefaultsToAchieved verifies that
// when the evaluator LLM call itself fails, the orchestrator preserves user
// credits by treating the attempt as achieved (no retry burned).
func TestGoalOrchestration_EvaluatorFailure_DefaultsToAchieved(t *testing.T) {
	llm := &goalStubLLM{err: errors.New("upstream timeout")}
	svc, repo := setupGoalTestService(t, llm)
	ctx := context.Background()

	userID := createGoalTestUser(t, repo)
	task := createGoalTask(t, repo, userID, 0)

	stop, err := svc.evaluateGoalAndMaybeRetry(ctx, task, &agent.ExecutionResult{Success: true})
	if err != nil {
		t.Fatalf("evaluateGoalAndMaybeRetry: %v", err)
	}
	if stop {
		t.Errorf("expected stop=false on evaluator failure (preserve credits, fall through to completed)")
	}

	updated, _ := repo.Tasks().FindByID(ctx, task.ID)
	if updated.Status != model.TaskStatusRunning {
		t.Errorf("status mutated on evaluator failure = %q, want %q", updated.Status, model.TaskStatusRunning)
	}
	if updated.GoalAttempts != 1 {
		t.Errorf("GoalAttempts = %d, want 1 (counter still increments for audit trail)", updated.GoalAttempts)
	}
}

// TestGoalOrchestration_DisabledGoalMode_Noop verifies that when goal mode is
// off, the orchestrator returns immediately without touching the DB.
func TestGoalOrchestration_DisabledGoalMode_Noop(t *testing.T) {
	llm := &goalStubLLM{}
	svc, repo := setupGoalTestService(t, llm)
	ctx := context.Background()

	userID := createGoalTestUser(t, repo)
	task := createGoalTask(t, repo, userID, 0)
	task.GoalMode = false
	if err := repo.Tasks().Update(ctx, &model.Task{
		ID:       task.ID,
		GoalMode: false,
	}); err != nil {
		// Fallback: re-create with GoalMode off via direct DB write is overkill;
		// the in-memory task struct is sufficient for this branch test.
		_ = err
	}

	stop, err := svc.evaluateGoalAndMaybeRetry(ctx, &model.Task{
		ID:       task.ID,
		GoalMode: false,
	}, &agent.ExecutionResult{Success: true})
	if err != nil {
		t.Fatalf("evaluateGoalAndMaybeRetry: %v", err)
	}
	if stop {
		t.Errorf("expected stop=false when goal mode is off")
	}
	if llm.calls != 0 {
		t.Errorf("LLM must not be called when goal mode is off; got %d calls", llm.calls)
	}
}

// TestGoalOrchestration_EvaluatorNotWired_DefaultsToAchieved verifies the
// degraded-mode path: when goalEvaluator is nil (e.g. config error), the
// orchestrator logs a warning and lets the task complete normally.
func TestGoalOrchestration_EvaluatorNotWired_DefaultsToAchieved(t *testing.T) {
	svc, repo := setupGoalTestService(t, nil) // no evaluator
	ctx := context.Background()

	userID := createGoalTestUser(t, repo)
	task := createGoalTask(t, repo, userID, 0)

	stop, err := svc.evaluateGoalAndMaybeRetry(ctx, task, &agent.ExecutionResult{Success: true})
	if err != nil {
		t.Fatalf("evaluateGoalAndMaybeRetry: %v", err)
	}
	if stop {
		t.Errorf("expected stop=false when evaluator is not wired")
	}
}

// TestParseLastGoalFailureReason_MostRecentFailed verifies the pure helper
// that extracts feedback for the next attempt's prompt.
func TestParseLastGoalFailureReason_MostRecentFailed(t *testing.T) {
	cases := []struct {
		name string
		log  string
		want string
	}{
		{name: "empty", log: "", want: ""},
		{name: "all achieved", log: `[{"attempt":1,"achieved":true,"reason":"ok"}]`, want: ""},
		{name: "last failed", log: `[{"attempt":1,"achieved":true,"reason":"ok"},{"attempt":2,"achieved":false,"reason":"字数不足"}]`, want: "字数不足"},
		{name: "middle failed", log: `[{"attempt":1,"achieved":false,"reason":"foo"},{"attempt":2,"achieved":true,"reason":"ok"}]`, want: "foo"},
		{name: "garbage", log: `not json`, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLastGoalFailureReason(tc.log)
			if got != tc.want {
				t.Errorf("parseLastGoalFailureReason(%q) = %q, want %q", tc.log, got, tc.want)
			}
		})
	}
}
