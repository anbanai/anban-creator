package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
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
	if err := completeLocalForCurrentExecution(ctx, svc, repo, taskID, result); err != nil {
		t.Fatalf("same duplicate completion: %v", err)
	}
	events, err := costRepo.ListEventsByExecution(ctx, executionID)
	if err != nil || len(events) != 2 {
		t.Fatalf("provider cost events = %#v err=%v, want two exactly once", events, err)
	}
}

func TestCompleteLocalTaskResponseLossRetryDoesNotRepeatTerminalSideEffects(t *testing.T) {
	svc, repo, costRepo := setupLocalProviderCostTest(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)
	addLocalSeednoteDeliverables(t, repo, taskID)
	task, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil || task.CurrentExecutionID == nil {
		t.Fatalf("claimed task = %#v, %v", task, err)
	}
	executionID := *task.CurrentExecutionID

	if err := repo.IlinkBindings().Create(ctx, &model.IlinkBinding{
		ID: uuid.NewString(), UserID: userID, PlatformAccountID: stringPtr("platform-1"),
		ExternalUserID: stringPtr("wx-user-1"), Status: model.IlinkBindingStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	svc.SetIlinkNotifier(NewIlinkNotifier(repo, true, &logger))

	miniRedis := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: miniRedis.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	svc.pubsub = NewRedisPubSub(rdb, &logger)
	if count, reserved, err := svc.pubsub.TryReserveSlot(ctx, projectID, 10); err != nil || !reserved || count != 1 {
		t.Fatalf("reserve current local slot = %d/%v/%v", count, reserved, err)
	}
	pending := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
		Status: model.TaskStatusPending, ExecutionTarget: model.ExecutionTargetCloud,
	}
	if err := repo.Tasks().Create(ctx, pending); err != nil {
		t.Fatal(err)
	}

	svc.SetProviderCostService(NewProviderCostService(costRepo, providerCostBundleWithTurbo()))
	result := &agent.ExecutionResult{Success: true, CostStatus: agent.CostStatusReconciled, ModelUsage: []agent.ModelTokenUsage{{
		Provider: "volcengine_ark", Model: "doubao-seed-evolving", InputTokens: 10, OutputTokens: 2,
	}}}
	if err := svc.CompleteLocalTask(ctx, taskID, executionID, result); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	if err := svc.CompleteLocalTask(ctx, taskID, executionID, result); err != nil {
		t.Fatalf("response-loss retry: %v", err)
	}

	events, err := costRepo.ListEventsByExecution(ctx, executionID)
	if err != nil || len(events) != 1 {
		t.Fatalf("provider events after retry = %#v, %v", events, err)
	}
	if got := len(svc.enqueuer.(*mockEnqueuer).enqueued); got != 1 {
		t.Fatalf("dispatch enqueue count = %d, want 1", got)
	}
	if count, err := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int(); err != nil || count != 1 {
		t.Fatalf("slot count after retry = %d/%v, want replacement slot only", count, err)
	}
	notifications, err := repo.IlinkNotifications().ListDue(ctx, 10)
	if err != nil || len(notifications) != 1 {
		t.Fatalf("terminal notifications after retry = %#v, %v", notifications, err)
	}
}

func TestCompleteLocalTaskConcurrentIdenticalOutcomeRunsTerminalSideEffectsOnce(t *testing.T) {
	for _, success := range []bool{true, false} {
		name := "failure"
		if success {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			svc, repo, costRepo := setupLocalProviderCostTest(t)
			ctx := context.Background()
			userID := uuid.NewString()
			projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
			taskID := claimOneLocal(t, svc, repo, userID, projectID)
			if success {
				addLocalSeednoteDeliverables(t, repo, taskID)
			}
			claimed, err := repo.Tasks().FindByID(ctx, taskID)
			if err != nil || claimed.CurrentExecutionID == nil {
				t.Fatalf("claimed task = %#v, %v", claimed, err)
			}
			executionID := *claimed.CurrentExecutionID

			if err := repo.IlinkBindings().Create(ctx, &model.IlinkBinding{
				ID: uuid.NewString(), UserID: userID, PlatformAccountID: stringPtr("platform-1"),
				ExternalUserID: stringPtr("wx-user-1"), Status: model.IlinkBindingStatusActive,
			}); err != nil {
				t.Fatal(err)
			}
			logger := zerolog.New(io.Discard)
			svc.SetIlinkNotifier(NewIlinkNotifier(repo, true, &logger))
			miniRedis := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: miniRedis.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			svc.pubsub = NewRedisPubSub(rdb, &logger)
			if count, reserved, err := svc.pubsub.TryReserveSlot(ctx, projectID, 10); err != nil || !reserved || count != 1 {
				t.Fatalf("reserve current local slot = %d/%v/%v", count, reserved, err)
			}
			pending := &model.Task{
				ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
				Status: model.TaskStatusPending, ExecutionTarget: model.ExecutionTargetCloud,
			}
			if err := repo.Tasks().Create(ctx, pending); err != nil {
				t.Fatal(err)
			}
			svc.SetProviderCostService(NewProviderCostService(costRepo, providerCostBundleWithTurbo()))
			svc.repo = newLocalCompletionRaceRepository(repo)
			newResult := func() *agent.ExecutionResult {
				return &agent.ExecutionResult{
					Success: success, Error: "provider unavailable", TerminalReason: model.TaskBillingTerminalProviderError,
					CostStatus: agent.CostStatusReconciled, ModelUsage: []agent.ModelTokenUsage{{
						Provider: "volcengine_ark", Model: "doubao-seed-evolving", InputTokens: 10, OutputTokens: 2,
					}},
				}
			}

			var wg sync.WaitGroup
			errs := make(chan error, 2)
			wg.Add(2)
			for range 2 {
				go func() {
					defer wg.Done()
					errs <- svc.CompleteLocalTask(ctx, taskID, executionID, newResult())
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatalf("identical concurrent completion = %v, want nil", err)
				}
			}

			events, err := costRepo.ListEventsByExecution(ctx, executionID)
			if err != nil || len(events) != 1 {
				t.Fatalf("provider events = %#v, %v; want one", events, err)
			}
			if got := len(svc.enqueuer.(*mockEnqueuer).enqueued); got != 1 {
				t.Fatalf("dispatch enqueue count = %d, want 1", got)
			}
			if count, err := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int(); err != nil || count != 1 {
				t.Fatalf("slot count = %d/%v, want replacement slot only", count, err)
			}
			notifications, err := repo.IlinkNotifications().ListDue(ctx, 10)
			if err != nil || len(notifications) != 1 {
				t.Fatalf("terminal notifications = %#v, %v; want one", notifications, err)
			}
			terminal, err := repo.Tasks().FindByID(ctx, taskID)
			wantReason := model.TaskBillingTerminalProviderError
			if success {
				wantReason = model.TaskBillingTerminalCompleted
			}
			if err != nil || terminal.BillingTerminalReason != wantReason {
				t.Fatalf("terminal billing evidence = %#v, %v; want reason %q", terminal, err, wantReason)
			}
		})
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

func TestCompleteLocalTaskUnpricedUsageStopsFinalizationAsUnreconciled(t *testing.T) {
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
	if err := completeLocalForCurrentExecution(ctx, svc, repo, taskID, result); err == nil {
		t.Fatal("unpriced usage completion = nil, want retryable provider-cost error")
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
	execution, err := repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil || execution.FinalizationStatus != model.TaskExecutionFinalizationTerminal {
		t.Fatalf("execution finalization = %#v, %v; want terminal", execution, err)
	}
}

func TestCompleteLocalTaskResumesEveryDurableTailStage(t *testing.T) {
	stages := []string{
		model.TaskExecutionFinalizationResult,
		model.TaskExecutionFinalizationSlot,
		model.TaskExecutionFinalizationDispatch,
		model.TaskExecutionFinalizationNotification,
		model.TaskExecutionFinalizationDone,
	}
	for _, success := range []bool{true, false} {
		for _, interruptedAt := range stages {
			t.Run(fmt.Sprintf("success=%v/%s", success, interruptedAt), func(t *testing.T) {
				svc, repo, costRepo := setupLocalProviderCostTest(t)
				ctx := context.Background()
				userID := uuid.NewString()
				projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
				taskID := claimOneLocal(t, svc, repo, userID, projectID)
				if success {
					addLocalSeednoteDeliverables(t, repo, taskID)
				}
				claimed, err := repo.Tasks().FindByID(ctx, taskID)
				if err != nil || claimed.CurrentExecutionID == nil {
					t.Fatalf("claimed task = %#v, %v", claimed, err)
				}
				executionID := *claimed.CurrentExecutionID
				svc.SetProviderCostService(NewProviderCostService(costRepo, providerCostBundleWithTurbo()))
				injected := false
				svc.finalizationAfterStage = func(stage string) error {
					if stage == interruptedAt && !injected {
						injected = true
						return errors.New("injected local finalization crash")
					}
					return nil
				}
				result := &agent.ExecutionResult{
					Success: success, Error: "provider unavailable", TerminalReason: model.TaskBillingTerminalProviderError,
					CostStatus: agent.CostStatusReconciled, ModelUsage: []agent.ModelTokenUsage{{
						Provider: "volcengine_ark", Model: "doubao-seed-evolving", InputTokens: 5,
					}},
				}
				if err := svc.CompleteLocalTask(ctx, taskID, executionID, result); err == nil {
					t.Fatal("first completion = nil, want injected crash")
				}
				svc.finalizationAfterStage = nil
				if err := svc.CompleteLocalTask(ctx, taskID, executionID, result); err != nil {
					t.Fatalf("resume completion: %v", err)
				}
				execution, err := repo.TaskExecutions().FindByID(ctx, executionID)
				if err != nil || execution.FinalizationStatus != model.TaskExecutionFinalizationDone {
					t.Fatalf("resumed execution = %#v, %v", execution, err)
				}
			})
		}
	}
}

func TestConcurrentIdenticalLocalCompletionWaitsForDurableTail(t *testing.T) {
	svc, repo, _ := setupLocalProviderCostTest(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)
	addLocalSeednoteDeliverables(t, repo, taskID)
	claimed, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil || claimed.CurrentExecutionID == nil {
		t.Fatalf("claimed task = %#v, %v", claimed, err)
	}
	executionID := *claimed.CurrentExecutionID
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	svc.finalizationAfterStage = func(stage string) error {
		if stage == model.TaskExecutionFinalizationResult {
			once.Do(func() { close(entered) })
			<-release
		}
		return nil
	}
	result := &agent.ExecutionResult{Success: true}
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- svc.CompleteLocalTask(ctx, taskID, executionID, result) }()
	<-entered
	go func() { second <- svc.CompleteLocalTask(ctx, taskID, executionID, result) }()
	select {
	case err := <-second:
		t.Fatalf("duplicate returned before durable tail finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
}

func TestCompleteLocalTaskCostFailureLeavesDurableFinalizationRetryable(t *testing.T) {
	svc, repo, baseCostRepo := setupLocalProviderCostTest(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)
	addLocalSeednoteDeliverables(t, repo, taskID)
	svc.SetProviderCostService(NewProviderCostService(failingExecutionCostRepository{BillingCostRepository: baseCostRepo}, providerCostBundleWithTurbo()))

	result := &agent.ExecutionResult{Success: true, CostStatus: agent.CostStatusReconciled, ModelUsage: []agent.ModelTokenUsage{{
		Provider: "volcengine_ark", Model: "doubao-seed-evolving", InputTokens: 1,
	}}}
	err := completeLocalForCurrentExecution(ctx, svc, repo, taskID, result)
	if err == nil || !strings.Contains(err.Error(), "cost database unavailable") {
		t.Fatalf("provider cost failure = %v, want retryable cost error", err)
	}
	got, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil || got.Status != model.TaskStatusCompleted || got.Result == nil {
		t.Fatalf("terminal task = %#v err=%v", got, err)
	}
	execution, err := repo.TaskExecutions().FindByID(ctx, *got.CurrentExecutionID)
	if err != nil || execution.FinalizationStatus != model.TaskExecutionFinalizationTerminal {
		t.Fatalf("interrupted execution = %#v err=%v, want terminal stage", execution, err)
	}

	svc.SetProviderCostService(NewProviderCostService(baseCostRepo, providerCostBundleWithTurbo()))
	if err := svc.CompleteLocalTask(ctx, taskID, execution.ID, result); err != nil {
		t.Fatalf("resume local finalization: %v", err)
	}
	execution, err = repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil || execution.FinalizationStatus != model.TaskExecutionFinalizationDone {
		t.Fatalf("resumed execution = %#v err=%v, want done", execution, err)
	}
}
