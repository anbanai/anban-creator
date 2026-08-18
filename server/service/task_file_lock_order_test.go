package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

type taskFileLockOrderRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *taskFileLockOrderRecorder) record(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *taskFileLockOrderRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

type taskFileLockOrderRepository struct {
	repository.Repository
	recorder             *taskFileLockOrderRecorder
	executionWon         *bool
	executionErr         error
	markDeleting         bool
	executionLocked      chan<- struct{}
	releaseExecutionLock <-chan struct{}
}

func (r *taskFileLockOrderRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&taskFileLockOrderTxRepository{
			Repository:           tx,
			recorder:             r.recorder,
			executionWon:         r.executionWon,
			executionErr:         r.executionErr,
			markDeleting:         r.markDeleting,
			executionLocked:      r.executionLocked,
			releaseExecutionLock: r.releaseExecutionLock,
		})
	})
}

type taskFileLockOrderTxRepository struct {
	repository.Repository
	recorder             *taskFileLockOrderRecorder
	executionWon         *bool
	executionErr         error
	markDeleting         bool
	executionLocked      chan<- struct{}
	releaseExecutionLock <-chan struct{}
}

func (r *taskFileLockOrderTxRepository) Tasks() repository.TaskRepository {
	return &taskFileLockOrderTasks{
		TaskRepository: r.Repository.Tasks(),
		recorder:       r.recorder,
		markDeleting:   r.markDeleting,
	}
}

func (r *taskFileLockOrderTxRepository) TaskExecutions() repository.TaskExecutionRepository {
	return &taskFileLockOrderExecutions{
		TaskExecutionRepository: r.Repository.TaskExecutions(),
		recorder:                r.recorder,
		won:                     r.executionWon,
		err:                     r.executionErr,
		executionLocked:         r.executionLocked,
		releaseExecutionLock:    r.releaseExecutionLock,
	}
}

func (r *taskFileLockOrderTxRepository) TaskFiles() repository.TaskFileRepository {
	return &taskFileLockOrderFiles{
		TaskFileRepository: r.Repository.TaskFiles(),
		recorder:           r.recorder,
	}
}

type taskFileLockOrderTasks struct {
	repository.TaskRepository
	recorder     *taskFileLockOrderRecorder
	markDeleting bool
}

func (r *taskFileLockOrderTasks) FindByIDForUpdate(ctx context.Context, id string) (*model.Task, error) {
	r.recorder.record("task")
	task, err := r.TaskRepository.FindByIDForUpdate(ctx, id)
	if err != nil || !r.markDeleting {
		return task, err
	}
	copy := *task
	now := time.Now()
	copy.DeletingAt = &now
	return &copy, nil
}

type taskFileLockOrderExecutions struct {
	repository.TaskExecutionRepository
	recorder             *taskFileLockOrderRecorder
	won                  *bool
	err                  error
	executionLocked      chan<- struct{}
	releaseExecutionLock <-chan struct{}
}

func (r *taskFileLockOrderExecutions) LockCurrentForArtifactMutation(ctx context.Context, executionID, taskID string) (bool, error) {
	r.recorder.record("execution")
	if r.err != nil {
		return false, r.err
	}
	if r.won != nil {
		return *r.won, nil
	}
	locker, ok := any(r.TaskExecutionRepository).(interface {
		LockCurrentForArtifactMutation(context.Context, string, string) (bool, error)
	})
	if !ok {
		return true, nil
	}
	locked, err := locker.LockCurrentForArtifactMutation(ctx, executionID, taskID)
	if err != nil || !locked || r.executionLocked == nil {
		return locked, err
	}
	close(r.executionLocked)
	select {
	case <-ctx.Done():
		return false, context.Cause(ctx)
	case <-r.releaseExecutionLock:
		return true, nil
	}
}

type taskFileLockOrderFiles struct {
	repository.TaskFileRepository
	recorder *taskFileLockOrderRecorder
}

func (r *taskFileLockOrderFiles) UpsertPendingCurrentExecution(ctx context.Context, taskID, executionID string, file *model.TaskFile) (*model.TaskFile, error) {
	r.recorder.record("artifact")
	return r.TaskFileRepository.UpsertPendingCurrentExecution(ctx, taskID, executionID, file)
}

func assertTaskFileExecutionLockOrder(t *testing.T, recorder *taskFileLockOrderRecorder) {
	t.Helper()
	calls := recorder.snapshot()
	if len(calls) < 3 || calls[0] != "execution" || calls[1] != "task" || calls[2] != "artifact" {
		t.Fatalf("transaction lock order = %v, want prefix [execution task artifact]", calls)
	}
}

func TestUploadExecutionTaskFileLocksExecutionBeforeTask(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	executionID := startTaskArtifactExecution(t, repo, task)
	recorder := &taskFileLockOrderRecorder{}
	svc.repo = &taskFileLockOrderRepository{Repository: repo, recorder: recorder}

	if _, err := svc.UploadExecutionTaskFileFromReader(
		context.Background(), task.ID, task.UserID, executionID,
		"output/ordered.txt", strings.NewReader("ordered"), "text/plain", 7,
	); err != nil {
		t.Fatal(err)
	}
	assertTaskFileExecutionLockOrder(t, recorder)
}

func TestGenerateTaskImageLocksExecutionBeforeTask(t *testing.T) {
	f := newTaskImageFixture(t)
	recorder := &taskFileLockOrderRecorder{}
	f.service.tasks.repo = &taskFileLockOrderRepository{Repository: f.repo, recorder: recorder}

	if _, err := f.service.Generate(context.Background(), f.request()); err != nil {
		t.Fatal(err)
	}
	assertTaskFileExecutionLockOrder(t, recorder)
}

func TestUploadExecutionTaskFileExecutionGuardFailures(t *testing.T) {
	t.Run("stale execution", func(t *testing.T) {
		svc, repo, _, task := newTaskArtifactTestService(t)
		executionID := startTaskArtifactExecution(t, repo, task)
		won := false
		svc.repo = &taskFileLockOrderRepository{
			Repository: repo, recorder: &taskFileLockOrderRecorder{}, executionWon: &won,
		}

		_, err := svc.UploadExecutionTaskFileFromReader(
			context.Background(), task.ID, task.UserID, executionID,
			"output/stale.txt", strings.NewReader("stale"), "text/plain", 5,
		)
		if !errors.Is(err, repository.ErrTaskFileExecutionNotCurrent) {
			t.Fatalf("error = %v, want ErrTaskFileExecutionNotCurrent", err)
		}
	})

	t.Run("lock error", func(t *testing.T) {
		svc, repo, _, task := newTaskArtifactTestService(t)
		executionID := startTaskArtifactExecution(t, repo, task)
		wantErr := errors.New("forced execution lock failure")
		svc.repo = &taskFileLockOrderRepository{
			Repository: repo, recorder: &taskFileLockOrderRecorder{}, executionErr: wantErr,
		}

		_, err := svc.UploadExecutionTaskFileFromReader(
			context.Background(), task.ID, task.UserID, executionID,
			"output/error.txt", strings.NewReader("error"), "text/plain", 5,
		)
		if !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want cause %v", err, wantErr)
		}
	})
}

func TestUploadExecutionTaskFileStillRejectsDeletingTaskAfterExecutionLock(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	executionID := startTaskArtifactExecution(t, repo, task)
	recorder := &taskFileLockOrderRecorder{}
	svc.repo = &taskFileLockOrderRepository{
		Repository: repo, recorder: recorder, markDeleting: true,
	}

	_, err := svc.UploadExecutionTaskFileFromReader(
		context.Background(), task.ID, task.UserID, executionID,
		"output/deleting.txt", strings.NewReader("deleting"), "text/plain", 8,
	)
	if !errors.Is(err, ErrTaskDeleting) {
		t.Fatalf("error = %v, want ErrTaskDeleting", err)
	}
	calls := recorder.snapshot()
	if len(calls) < 2 || calls[0] != "execution" || calls[1] != "task" {
		t.Fatalf("transaction lock order = %v, want prefix [execution task]", calls)
	}
}

func TestUploadExecutionTaskFileRacingLocalCompletionLeavesLegalTerminalState(t *testing.T) {
	db := setupTaskTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// SQLite does not provide MySQL row locks. A single connection makes the
	// behavioral race deterministic; the SQL contract proves production order.
	sqlDB.SetMaxOpenConns(1)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	executionID := uuid.NewString()
	task := &model.Task{
		ID: userID + "-task", UserID: userID, ProjectID: projectID,
		Type: model.PlatformArticle, Status: model.TaskStatusRunning,
		ExecutionTarget: model.ExecutionTargetLocalClaimed, CurrentExecutionID: &executionID,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1,
		Target: model.ExecutionTargetLocalClaimed, Status: model.TaskExecutionRunning, Started: true,
	}); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	svc := newTestTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	executionLocked := make(chan struct{})
	releaseExecutionLock := make(chan struct{})
	uploadSvc := newTestTaskService(&taskFileLockOrderRepository{
		Repository: repo, recorder: &taskFileLockOrderRecorder{},
		executionLocked: executionLocked, releaseExecutionLock: releaseExecutionLock,
	}, &mockEnqueuer{}, store, &logger, "", nil, nil)

	uploaded := make(chan error, 1)
	completed := make(chan error, 1)
	go func() {
		_, uploadErr := uploadSvc.UploadExecutionTaskFileFromReader(
			ctx, task.ID, task.UserID, executionID,
			"output/race.txt", strings.NewReader("race"), "text/plain", 4,
		)
		uploaded <- uploadErr
	}()
	select {
	case <-executionLocked:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not acquire the execution lock")
	}
	go func() {
		completed <- svc.CompleteLocalTask(ctx, task.ID, executionID, &agent.ExecutionResult{
			Success: false, Error: "intentional terminal race",
		})
	}()
	close(releaseExecutionLock)

	var uploadErr, completionErr error
	for received := 0; received < 2; received++ {
		select {
		case uploadErr = <-uploaded:
			uploaded = nil
		case completionErr = <-completed:
			completed = nil
		case <-time.After(5 * time.Second):
			t.Fatal("upload/local completion race did not finish")
		}
	}
	if completionErr != nil {
		t.Fatalf("CompleteLocalTask error = %v", completionErr)
	}
	if uploadErr != nil {
		t.Fatalf("upload that held the execution lock failed: %v", uploadErr)
	}

	persistedTask, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	persistedExecution, err := repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedTask.Status != model.TaskStatusFailed || persistedTask.CompletedAt == nil ||
		persistedExecution.Status != model.TaskExecutionFailed || persistedExecution.CompletedAt == nil {
		t.Fatalf("terminal state = task:%q/%v execution:%q/%v",
			persistedTask.Status, persistedTask.CompletedAt, persistedExecution.Status, persistedExecution.CompletedAt)
	}
	files, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].FilePath != "output/race.txt" {
		t.Fatalf("artifact rows after race = %#v", files)
	}
}
