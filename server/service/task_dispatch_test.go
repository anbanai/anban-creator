package service

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type dispatchTestDispatcher struct {
	mu      sync.Mutex
	calls   int
	err     error
	started chan struct{}
	release chan struct{}
}

func (d *dispatchTestDispatcher) Dispatch(_ context.Context, _ *model.TaskExecution, _ *model.Task) error {
	d.mu.Lock()
	d.calls++
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

	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatalf("overlapping handler: %v", err)
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
	dispatcher.err = errors.New("job admission denied")
	err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID)
	if err == nil || !errors.Is(err, dispatcher.err) {
		t.Fatalf("dispatch error = %v, want %v", err, dispatcher.err)
	}
	execution := mustCurrentExecution(t, repo, task.ID)
	if execution.Status != model.TaskExecutionFailed || execution.CompletedAt == nil || execution.TerminalReason != "dispatch_failed" {
		t.Fatalf("failed execution = %+v", execution)
	}
	failedTask, findErr := repo.Tasks().FindByID(context.Background(), task.ID)
	if findErr != nil {
		t.Fatalf("find failed task: %v", findErr)
	}
	if failedTask.Status != model.TaskStatusFailed || failedTask.CompletedAt == nil || failedTask.ErrorMessage == "" {
		t.Fatalf("failed task = %+v", failedTask)
	}
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatalf("replay failed dispatch: %v", err)
	}
	if dispatcher.callCount() != 1 {
		t.Fatalf("replayed dispatch calls = %d, want 1", dispatcher.callCount())
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
