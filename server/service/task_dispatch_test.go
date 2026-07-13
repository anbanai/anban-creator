package service

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type dispatchTestDispatcher struct {
	mu                sync.Mutex
	calls             int
	creates           int
	seen              map[string]struct{}
	err               error
	sideEffectOnError bool
	started           chan struct{}
	release           chan struct{}
}

func (d *dispatchTestDispatcher) Dispatch(_ context.Context, execution *model.TaskExecution, _ *model.Task) error {
	d.mu.Lock()
	d.calls++
	if d.seen == nil {
		d.seen = make(map[string]struct{})
	}
	if _, ok := d.seen[execution.ID]; !ok && (d.err == nil || d.sideEffectOnError) {
		d.seen[execution.ID] = struct{}{}
		d.creates++
	}
	started := d.started
	release := d.release
	err := d.err
	d.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	return err
}

func (d *dispatchTestDispatcher) Delete(context.Context, *model.TaskExecution) error {
	return nil
}

func (d *dispatchTestDispatcher) DeleteProjectMemory(context.Context, string) error {
	return nil
}

func (d *dispatchTestDispatcher) Inspect(context.Context, *model.TaskExecution) (*agent.KubernetesExecutionState, error) {
	return nil, nil
}

func (d *dispatchTestDispatcher) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func (d *dispatchTestDispatcher) createCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.creates
}

func setupDispatchTest(t *testing.T) (*TaskService, repository.Repository, *gorm.DB, *dispatchTestDispatcher, *model.Task) {
	t.Helper()
	db := setupTaskTestDB(t)
	if err := db.AutoMigrate(&model.TaskExecution{}); err != nil {
		t.Fatalf("migrate task executions: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	dispatcher := &dispatchTestDispatcher{}
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	svc.SetKubernetesDispatcher(dispatcher)
	task := &model.Task{
		ID:        uuid.NewString(),
		UserID:    uuid.NewString(),
		ProjectID: uuid.NewString(),
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusPending,
		Prompt:    "dispatch me",
	}
	if err := repo.Projects().Create(context.Background(), &model.Project{
		ID:       task.ProjectID,
		UserID:   task.UserID,
		Name:     "dispatch project",
		Platform: task.Type,
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return svc, repo, db, dispatcher, task
}

func mustCurrentExecution(t *testing.T, repo repository.Repository, taskID string) *model.TaskExecution {
	t.Helper()
	execution, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatalf("find current execution: %v", err)
	}
	return execution
}

func ageDispatchClaim(t *testing.T, db *gorm.DB, executionID string, age time.Duration) {
	t.Helper()
	var raw string
	if err := db.Raw("SELECT CURRENT_TIMESTAMP").Row().Scan(&raw); err != nil {
		t.Fatalf("sample database time: %v", err)
	}
	databaseNow, err := time.ParseInLocation("2006-01-02 15:04:05", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse database time %q: %v", raw, err)
	}
	if err := db.Model(&model.TaskExecution{}).Where("id = ?", executionID).
		Update("dispatch_claimed_at", databaseNow.Add(-age)).Error; err != nil {
		t.Fatalf("age dispatch claim: %v", err)
	}
}

func TestDispatchCloudTaskCreatesOneAttemptAndReturnsAfterJobAccepted(t *testing.T) {
	svc, repo, _, dispatcher, task := setupDispatchTest(t)
	ctx := context.Background()
	if err := svc.HandleExecutionFromPayload(ctx, task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	if dispatcher.callCount() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", dispatcher.callCount())
	}
	current := mustCurrentExecution(t, repo, task.ID)
	if current.Attempt != 1 || current.Target != "kubernetes" || current.Status != model.TaskExecutionStarting {
		t.Fatalf("current execution = %+v", current)
	}
	if err := svc.HandleExecutionFromPayload(ctx, task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	if dispatcher.callCount() != 1 {
		t.Fatalf("duplicate dispatch calls = %d", dispatcher.callCount())
	}
}

func TestDispatchCloudTaskConcurrentHandlersCreateAndDispatchOneAttempt(t *testing.T) {
	svc, repo, db, dispatcher, task := setupDispatchTest(t)
	dispatcher.started = make(chan struct{}, 1)
	dispatcher.release = make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID)
	}()
	<-dispatcher.started

	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); !errors.Is(err, ErrDispatchInProgress) {
		t.Fatalf("overlapping handler error = %v, want ErrDispatchInProgress", err)
	}
	close(dispatcher.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first handler: %v", err)
	}

	var count int64
	if err := db.Model(&model.TaskExecution{}).Where("task_id = ?", task.ID).Count(&count).Error; err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if count != 1 || dispatcher.callCount() != 1 {
		t.Fatalf("attempts = %d, dispatch calls = %d; want 1, 1", count, dispatcher.callCount())
	}
	if got := mustCurrentExecution(t, repo, task.ID).Attempt; got != 1 {
		t.Fatalf("current attempt = %d, want 1", got)
	}
}

func TestDispatchCloudTaskFailureTerminalizesAttemptAndTask(t *testing.T) {
	svc, repo, _, dispatcher, task := setupDispatchTest(t)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	logger := zerolog.New(io.Discard)
	svc.pubsub = NewRedisPubSub(rdb, &logger)
	if _, ok, err := svc.pubsub.TryReserveSlot(context.Background(), task.ProjectID, 10); err != nil || !ok {
		t.Fatalf("reserve slot = %v, %v", ok, err)
	}
	dispatcher.err = agent.NewPermanentDispatchError(apierrors.NewForbidden(schema.GroupResource{Resource: "jobs"}, "job-1", errors.New("denied")))
	err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID)
	if err == nil || !errors.Is(err, dispatcher.err) {
		t.Fatalf("dispatch error = %v, want %v", err, dispatcher.err)
	}
	execution := mustCurrentExecution(t, repo, task.ID)
	if execution.Status != model.TaskExecutionFailed || execution.CompletedAt == nil || execution.TerminalReason != "dispatch_failed" || execution.FinalizationStatus != model.TaskExecutionFinalizationDone || execution.CleanupStatus != model.TaskExecutionCleanupPending {
		t.Fatalf("failed execution = %+v", execution)
	}
	failedTask, findErr := repo.Tasks().FindByID(context.Background(), task.ID)
	if findErr != nil {
		t.Fatalf("find failed task: %v", findErr)
	}
	if failedTask.Status != model.TaskStatusFailed || failedTask.CompletedAt == nil || failedTask.ErrorMessage == "" {
		t.Fatalf("failed task = %+v", failedTask)
	}
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err == nil {
		t.Fatal("terminal current attempt was silently acknowledged")
	}
	if dispatcher.callCount() != 1 {
		t.Fatalf("replayed dispatch calls = %d, want 1", dispatcher.callCount())
	}
	if exists, err := rdb.Exists(context.Background(), projectRunningCountPrefix+task.ProjectID).Result(); err != nil || exists != 0 {
		t.Fatalf("terminal slot key exists = %d, %v; want 0", exists, err)
	}
}

func TestDispatchCloudTaskAmbiguousErrorAbandonsClaimAndRetriesWithoutTerminalizing(t *testing.T) {
	svc, repo, _, dispatcher, task := setupDispatchTest(t)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	logger := zerolog.New(io.Discard)
	svc.pubsub = NewRedisPubSub(rdb, &logger)
	if _, ok, err := svc.pubsub.TryReserveSlot(context.Background(), task.ProjectID, 10); err != nil || !ok {
		t.Fatalf("reserve slot = %v, %v", ok, err)
	}
	dispatcher.err = apierrors.NewTooManyRequests("retry", 1)
	dispatcher.sideEffectOnError = true

	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err == nil {
		t.Fatal("expected ambiguous dispatch error")
	}
	execution := mustCurrentExecution(t, repo, task.ID)
	currentTask, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != model.TaskExecutionCreated || execution.DispatchClaimToken != "" || execution.DispatchClaimedAt != nil {
		t.Fatalf("abandoned execution = %+v", execution)
	}
	if currentTask.Status != model.TaskStatusRunning || currentTask.CompletedAt != nil || currentTask.ErrorMessage != "" {
		t.Fatalf("ambiguous task was terminalized: %+v", currentTask)
	}
	if count, err := rdb.Get(context.Background(), projectRunningCountPrefix+task.ProjectID).Int64(); err != nil || count != 1 {
		t.Fatalf("ambiguous slot count = %d, %v; want 1", count, err)
	}

	dispatcher.err = nil
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatalf("retry ambiguous dispatch: %v", err)
	}
	if got := mustCurrentExecution(t, repo, task.ID).Status; got != model.TaskExecutionStarting {
		t.Fatalf("retry status = %s, want starting", got)
	}
	if dispatcher.callCount() != 2 || dispatcher.createCount() != 1 {
		t.Fatalf("dispatcher calls=%d logical jobs=%d, want 2 and 1", dispatcher.callCount(), dispatcher.createCount())
	}
}

func TestDispatchCloudTaskDoesNotAcknowledgeStartingAttemptOnTerminalTask(t *testing.T) {
	svc, repo, _, dispatcher, task := setupDispatchTest(t)
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().UpdateStatus(context.Background(), task.ID, model.TaskStatusFailed); err != nil {
		t.Fatalf("make task inconsistent: %v", err)
	}
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err == nil {
		t.Fatal("terminal task with starting execution was silently acknowledged")
	}
	if dispatcher.callCount() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", dispatcher.callCount())
	}
}

func TestDispatchCloudTaskAcceptedTransitionFailureIsReclaimed(t *testing.T) {
	svc, repo, db, dispatcher, task := setupDispatchTest(t)
	svc.SetKubernetesDispatchLease(time.Minute)
	trigger := `CREATE TRIGGER reject_starting BEFORE UPDATE OF status ON task_executions
		WHEN NEW.status = 'starting' BEGIN SELECT RAISE(ABORT, 'starting rejected'); END`
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatalf("create transition trigger: %v", err)
	}

	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err == nil {
		t.Fatal("expected transition-to-starting error")
	}
	if got := mustCurrentExecution(t, repo, task.ID).Status; got != model.TaskExecutionDispatching {
		t.Fatalf("status after transition error = %s, want dispatching", got)
	}
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); !errors.Is(err, ErrDispatchInProgress) {
		t.Fatalf("active claim replay error = %v, want ErrDispatchInProgress", err)
	}
	ageDispatchClaim(t, db, mustCurrentExecution(t, repo, task.ID).ID, 2*time.Minute)
	if err := db.Exec("DROP TRIGGER reject_starting").Error; err != nil {
		t.Fatalf("drop transition trigger: %v", err)
	}
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatalf("reclaim accepted dispatch: %v", err)
	}
	if got := mustCurrentExecution(t, repo, task.ID).Status; got != model.TaskExecutionStarting {
		t.Fatalf("reclaimed status = %s, want starting", got)
	}
	if dispatcher.callCount() != 2 || dispatcher.createCount() != 1 {
		t.Fatalf("dispatcher calls=%d creates=%d, want 2 calls and 1 idempotent create", dispatcher.callCount(), dispatcher.createCount())
	}
}

func TestDispatchCloudTaskUsesDatabaseTimeForActiveClaim(t *testing.T) {
	svc, repo, db, dispatcher, task := setupDispatchTest(t)
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	execution := mustCurrentExecution(t, repo, task.ID)
	if err := db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).Updates(map[string]any{
		"status":               model.TaskExecutionDispatching,
		"dispatch_claim_token": "active-owner",
	}).Error; err != nil {
		t.Fatalf("seed active claim: %v", err)
	}
	ageDispatchClaim(t, db, execution.ID, 0)
	svc.SetKubernetesDispatchLease(time.Minute)

	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); !errors.Is(err, ErrDispatchInProgress) {
		t.Fatalf("active replay error = %v, want ErrDispatchInProgress", err)
	}
	if dispatcher.callCount() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", dispatcher.callCount())
	}
}

func TestDispatchCloudTaskFailureFinalizationResumesWithoutRedispatch(t *testing.T) {
	svc, repo, db, dispatcher, task := setupDispatchTest(t)
	dispatcher.err = agent.NewPermanentDispatchError(errors.New("job identity mismatch"))
	trigger := `CREATE TRIGGER reject_task_failure BEFORE UPDATE OF status ON tasks
		WHEN NEW.status = 'failed' BEGIN SELECT RAISE(ABORT, 'task failure rejected'); END`
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err == nil {
		t.Fatal("expected dispatch/finalization error")
	}
	execution := mustCurrentExecution(t, repo, task.ID)
	currentTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
	if execution.Status != model.TaskExecutionFailed || execution.FinalizationStatus != model.TaskExecutionFinalizationPublishing || currentTask.Status != model.TaskStatusRunning {
		t.Fatalf("durable interrupted state: execution=%s finalization=%s task=%s", execution.Status, execution.FinalizationStatus, currentTask.Status)
	}
	if err := db.Exec("DROP TRIGGER reject_task_failure").Error; err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
	if err := svc.ResumeExecutionFinalization(context.Background(), execution.ID); err != nil {
		t.Fatalf("resume failed dispatch finalization: %v", err)
	}
	execution = mustCurrentExecution(t, repo, task.ID)
	currentTask, _ = repo.Tasks().FindByID(context.Background(), task.ID)
	if execution.Status != model.TaskExecutionFailed || execution.FinalizationStatus != model.TaskExecutionFinalizationDone || currentTask.Status != model.TaskStatusFailed {
		t.Fatalf("repaired terminal state: execution=%s finalization=%s task=%s", execution.Status, execution.FinalizationStatus, currentTask.Status)
	}
	if dispatcher.callCount() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", dispatcher.callCount())
	}
}

func TestDispatchCloudTaskInitialTransactionContentionCreatesOneAttempt(t *testing.T) {
	svc, _, db, dispatcher, task := setupDispatchTest(t)
	entered := make(chan struct{}, 2)
	releaseCreate := make(chan struct{})
	svc.dispatchBeforeCreate = func() {
		entered <- struct{}{}
		<-releaseCreate
	}
	dispatcher.started = make(chan struct{}, 1)
	dispatcher.release = make(chan struct{})

	results := make(chan error, 2)
	go func() { results <- svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID) }()
	<-entered
	go func() { results <- svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID) }()
	<-entered
	releaseCreate <- struct{}{}
	<-dispatcher.started
	releaseCreate <- struct{}{}
	if err := <-results; !errors.Is(err, ErrDispatchInProgress) {
		t.Fatalf("creation CAS loser = %v, want ErrDispatchInProgress", err)
	}
	close(dispatcher.release)
	if err := <-results; err != nil {
		t.Fatalf("creation CAS winner: %v", err)
	}
	var count int64
	if err := db.Model(&model.TaskExecution{}).Where("task_id = ?", task.ID).Count(&count).Error; err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if count != 1 || dispatcher.callCount() != 1 {
		t.Fatalf("attempts=%d dispatches=%d, want 1 and 1", count, dispatcher.callCount())
	}
}

func TestDispatchCloudTaskStaleClaimRaceHasOneOwner(t *testing.T) {
	svc, _, _, dispatcher, task := setupDispatchTest(t)
	svc.SetKubernetesDispatchLease(time.Minute)
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	// Replace the completed first call's dispatcher barrier with a stale durable claim.
	execution := mustCurrentExecution(t, svc.repo, task.ID)
	if won, err := svc.repo.TaskExecutions().Transition(context.Background(), execution.ID,
		[]string{model.TaskExecutionStarting}, model.TaskExecutionDispatching, model.ExecutionTransition{}); err != nil || !won {
		t.Fatalf("make claim stale: %v", err)
	}
	dispatcher.started = make(chan struct{}, 1)
	dispatcher.release = make(chan struct{})
	results := make(chan error, 2)
	go func() { results <- svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID) }()
	<-dispatcher.started
	go func() { results <- svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID) }()
	second := <-results
	if !errors.Is(second, ErrDispatchInProgress) {
		t.Fatalf("stale claim loser = %v, want ErrDispatchInProgress", second)
	}
	close(dispatcher.release)
	if err := <-results; err != nil {
		t.Fatalf("stale claim winner: %v", err)
	}
}

func TestDispatchCloudTaskRollsBackWhenCurrentPointerUpdateFails(t *testing.T) {
	svc, repo, db, dispatcher, task := setupDispatchTest(t)
	trigger := `CREATE TRIGGER reject_current_execution BEFORE UPDATE OF current_execution_id ON tasks
		BEGIN SELECT RAISE(ABORT, 'current pointer rejected'); END`
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err == nil {
		t.Fatal("expected current pointer update failure")
	}
	var count int64
	if err := db.Model(&model.TaskExecution{}).Where("task_id = ?", task.ID).Count(&count).Error; err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	found, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("find rolled back task: %v", err)
	}
	if count != 0 || found.Status != model.TaskStatusPending || found.CurrentExecutionID != nil || dispatcher.callCount() != 0 {
		t.Fatalf("rollback state: attempts=%d task=%+v dispatches=%d", count, found, dispatcher.callCount())
	}
}

func TestHandleExecutionFromPayloadWithoutDispatcherUsesSynchronousExecutor(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := db.AutoMigrate(&model.TaskExecution{}); err != nil {
		t.Fatalf("migrate task executions: %v", err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusPending}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	executor := &fakeTaskExecutor{err: errors.New("synchronous executor called")}
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, executor, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	if err := svc.HandleExecutionFromPayload(ctx, task.ID, task.UserID); err != nil {
		t.Fatalf("HandleExecutionFromPayload: %v", err)
	}
	if executor.opts == nil {
		t.Fatal("synchronous executor was not called")
	}
	var count int64
	if err := db.Model(&model.TaskExecution{}).Where("task_id = ?", task.ID).Count(&count).Error; err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if count != 0 {
		t.Fatalf("TaskExecution rows = %d, want 0", count)
	}
}

func TestDispatchCloudTaskKeepsProjectConcurrencySlot(t *testing.T) {
	svc, _, _, _, task := setupDispatchTest(t)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	logger := zerolog.New(io.Discard)
	svc.pubsub = NewRedisPubSub(rdb, &logger)
	ctx := context.Background()
	if _, ok, err := svc.pubsub.TryReserveSlot(ctx, task.ProjectID, 10); err != nil || !ok {
		t.Fatalf("reserve slot = %v, %v", ok, err)
	}
	if err := svc.HandleExecutionFromPayload(ctx, task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	count, err := rdb.Get(ctx, projectRunningCountPrefix+task.ProjectID).Int64()
	if err != nil {
		t.Fatalf("read slot count: %v", err)
	}
	if count != 1 {
		t.Fatalf("slot count = %d, want 1 until terminal finalization", count)
	}
}
