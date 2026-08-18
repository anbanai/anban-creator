package service

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type failingExecutionCostRepository struct {
	repository.BillingCostRepository
}

func (f failingExecutionCostRepository) AppendEventsAndUpsertExecutionCostStatus(context.Context, []*model.BillingProviderCostEvent, *model.BillingExecutionCostStatus) ([]*model.BillingProviderCostEvent, error) {
	return nil, errors.New("cost database unavailable")
}

func setupLocalProviderCostTest(t *testing.T) (*TaskService, repository.Repository, repository.BillingCostRepository) {
	t.Helper()
	db := setupTaskTestDB(t)
	if err := db.AutoMigrate(&model.BillingProviderCostEvent{}, &model.BillingExecutionCostStatus{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
	return svc, repo, repository.NewBillingCostRepository(db)
}

func TestCompleteLocalTaskRecordsTerminalProviderCostOnce(t *testing.T) {
	svc, repo, costRepo := setupLocalProviderCostTest(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)
	addLocalSeednoteDeliverables(t, repo, taskID)
	claimed, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil || claimed.CurrentExecutionID == nil {
		t.Fatalf("claimed local task has no durable execution identity: task=%#v err=%v", claimed, err)
	}
	executionID := *claimed.CurrentExecutionID

	costSvc := NewProviderCostService(costRepo, providerCostBundleWithTurbo())
	svc.SetProviderCostService(costSvc)
	result := &agent.ExecutionResult{Success: true, CostStatus: agent.CostStatusReconciled, ModelUsage: []agent.ModelTokenUsage{
		{Provider: "volcengine_ark", Model: "doubao-seed-evolving", InputTokens: 10, OutputTokens: 2, CacheReadInputTokens: 3, CacheCreationInputTokens: 4},
		{Provider: "volcengine_ark", Model: "doubao-seed-2-1-turbo-260628", InputTokens: 7, OutputTokens: 1},
	}}
	if err := completeLocalForCurrentExecution(ctx, svc, repo, taskID, result); err != nil {
		t.Fatal(err)
	}
	terminalExecution, err := repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil || terminalExecution.Status != model.TaskExecutionSucceeded || terminalExecution.FinalizationStatus != model.TaskExecutionFinalizationDone || terminalExecution.CleanupStatus != model.TaskExecutionCleanupDone || len(terminalExecution.Result) == 0 {
		t.Fatalf("local execution terminal state = %#v err=%v", terminalExecution, err)
	}
	if err := completeLocalForCurrentExecution(ctx, svc, repo, taskID, result); !errors.Is(err, ErrStaleTaskExecution) {
		t.Fatalf("duplicate completion error = %v, want ErrStaleTaskExecution", err)
	}
	events, err := costRepo.ListEventsByExecution(ctx, executionID)
	if err != nil || len(events) != 2 {
		t.Fatalf("provider cost events = %#v err=%v, want two exactly once", events, err)
	}
}

func TestCompleteLocalTaskRollsBackWhenDurableExecutionIsMissing(t *testing.T) {
	svc, repo, _ := setupLocalProviderCostTest(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)
	addLocalSeednoteDeliverables(t, repo, taskID)
	claimed, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil || claimed.CurrentExecutionID == nil {
		t.Fatal("missing claimed execution")
	}
	// Simulate corruption at the task/execution boundary. Finalization must not
	// commit only the task half.
	missingID := uuid.NewString()
	claimed.CurrentExecutionID = &missingID
	if err := repo.Tasks().Update(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	if err := completeLocalForCurrentExecution(ctx, svc, repo, taskID, &agent.ExecutionResult{Success: true}); err == nil {
		t.Fatal("missing durable execution did not fail atomic finalization")
	}
	got, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil || got.Status != model.TaskStatusRunning || got.Result != nil {
		t.Fatalf("task half committed without execution: task=%#v err=%v", got, err)
	}
}

func TestCompleteLocalTaskMissingUsageMarksUnreconciled(t *testing.T) {
	svc, repo, costRepo := setupLocalProviderCostTest(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)
	claimed, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil || claimed.CurrentExecutionID == nil {
		t.Fatalf("claimed local task has no durable execution identity: task=%#v err=%v", claimed, err)
	}
	costSvc := NewProviderCostService(costRepo, providerCostBundleWithTurbo())
	svc.SetProviderCostService(costSvc)

	if err := completeLocalForCurrentExecution(ctx, svc, repo, taskID, &agent.ExecutionResult{Success: false, Error: "provider failed"}); err != nil {
		t.Fatal(err)
	}
	status, err := costRepo.FindExecutionCostStatus(ctx, *claimed.CurrentExecutionID)
	if err != nil || status.Status != model.BillingProviderCostStatusUnreconciled || status.ReasonCode != model.BillingExecutionCostReasonMissingTerminalModelUsage {
		t.Fatalf("execution cost status = %#v err=%v", status, err)
	}
}

func TestCompleteLocalTaskUnpricedUsageMarksUnreconciled(t *testing.T) {
	svc, repo, costRepo := setupLocalProviderCostTest(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)
	addLocalSeednoteDeliverables(t, repo, taskID)
	claimed, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil || claimed.CurrentExecutionID == nil {
		t.Fatalf("claimed local task has no durable execution identity: task=%#v err=%v", claimed, err)
	}
	costSvc := NewProviderCostService(costRepo, providerCostBundleWithTurbo())
	svc.SetProviderCostService(costSvc)

	result := &agent.ExecutionResult{Success: true, CostStatus: agent.CostStatusReconciled, ModelUsage: []agent.ModelTokenUsage{{
		Provider: "moonshot", Model: "unpriced-model", InputTokens: 1,
	}}}
	if err := completeLocalForCurrentExecution(ctx, svc, repo, taskID, result); err != nil {
		t.Fatal(err)
	}
	executionID := *claimed.CurrentExecutionID
	status, err := costRepo.FindExecutionCostStatus(ctx, executionID)
	if err != nil || status.Status != model.BillingProviderCostStatusUnreconciled || status.ReasonCode != model.BillingExecutionCostReasonInvalidTerminalModelUsage {
		t.Fatalf("execution cost status = %#v err=%v", status, err)
	}
	events, err := costRepo.ListEventsByExecution(ctx, executionID)
	if err != nil || len(events) != 0 {
		t.Fatalf("provider cost events = %#v err=%v, want none", events, err)
	}
}

func TestCompleteLocalTaskCostFailureDoesNotChangeTerminalOutcome(t *testing.T) {
	svc, repo, baseCostRepo := setupLocalProviderCostTest(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)
	addLocalSeednoteDeliverables(t, repo, taskID)
	svc.SetProviderCostService(NewProviderCostService(failingExecutionCostRepository{BillingCostRepository: baseCostRepo}, providerCostBundleWithTurbo()))

	err := completeLocalForCurrentExecution(ctx, svc, repo, taskID, &agent.ExecutionResult{Success: true, CostStatus: agent.CostStatusReconciled, ModelUsage: []agent.ModelTokenUsage{{
		Provider: "volcengine_ark", Model: "doubao-seed-evolving", InputTokens: 1,
	}}})
	if err != nil {
		t.Fatalf("provider cost failure escaped terminal completion: %v", err)
	}
	got, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil || got.Status != model.TaskStatusCompleted || got.Result == nil {
		t.Fatalf("terminal task = %#v err=%v", got, err)
	}
}
