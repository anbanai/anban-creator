package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/agent"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func setupCloudCompletionTest(t *testing.T, withArtifact bool, startedOverride ...bool) (*TaskService, repository.Repository, *model.Task, *model.TaskExecution) {
	t.Helper()
	db := setupTaskTestDB(t)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
	task := &model.Task{ID: uuid.NewString(), UserID: uuid.NewString(), Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	started := true
	if len(startedOverride) > 0 {
		started = startedOverride[0]
	}
	executionStatus := model.TaskExecutionRunning
	if !started {
		executionStatus = model.TaskExecutionStarting
	}
	execution := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: 1, Target: "kubernetes", Status: executionStatus, Started: started,
		RuntimeProfile: "article", RuntimeImage: "registry/content@sha256:test",
	}
	if withArtifact {
		execution.ManifestStatus = model.TaskExecutionManifestPending
	}
	if err := repo.TaskExecutions().Create(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.Tasks().SetCurrentExecution(context.Background(), task.ID, execution.ID); err != nil || !won {
		t.Fatalf("set current execution: won=%v err=%v", won, err)
	}
	task.CurrentExecutionID = &execution.ID
	if withArtifact {
		if err := repo.TaskFiles().BatchCreate(context.Background(), []*model.TaskFile{{
			ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStatePending,
			Role: "content", FilePath: "output/content.md", FileName: "content.md", FileSize: 8,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	return svc, repo, task, execution
}

type replaySafeEnqueuer struct {
	calls    int
	accepted int
	seen     map[string]struct{}
}

func (e *replaySafeEnqueuer) Enqueue(string, []byte) error { return nil }

func (e *replaySafeEnqueuer) EnqueueIn(string, []byte, time.Duration) error { return nil }

func (e *replaySafeEnqueuer) EnqueueUnique(_ string, _ []byte, uniqueKey string) (bool, error) {
	e.calls++
	if e.seen == nil {
		e.seen = make(map[string]struct{})
	}
	if _, exists := e.seen[uniqueKey]; exists {
		return false, nil
	}
	e.seen[uniqueKey] = struct{}{}
	e.accepted++
	return true, nil
}

func TestCompleteCloudExecutionCurrentAttemptAndDuplicate(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	old := &model.TaskExecution{ID: uuid.NewString(), TaskID: task.ID, Attempt: 0, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true}
	if err := repo.TaskExecutions().Create(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	if err := svc.CompleteCloudExecution(context.Background(), old.ID, result); !errors.Is(err, ErrStaleTaskExecution) {
		t.Fatalf("stale completion error = %v", err)
	}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatalf("duplicate completion: %v", err)
	}
	foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	files, _ := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if foundTask.Status != model.TaskStatusCompleted || foundExecution.FinalizationStatus != model.TaskExecutionFinalizationDone || len(files) != 1 {
		t.Fatalf("task=%s execution=%s files=%d", foundTask.Status, foundExecution.FinalizationStatus, len(files))
	}
}

func TestCompleteCloudExecutionFencesEvidenceWhenAttemptBecomesStale(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	ctx := context.Background()
	next := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: 2, Target: "kubernetes",
		Status: model.TaskExecutionRunning, Started: true,
	}
	if err := repo.TaskExecutions().Create(ctx, next); err != nil {
		t.Fatal(err)
	}

	var switched bool
	svc.finalizationAfterAdvance = func(stage string) error {
		if stage != model.TaskExecutionFinalizationArtifacts || switched {
			return nil
		}
		switched = true
		won, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, next.ID)
		if err != nil || !won {
			t.Fatalf("switch current execution: won=%v err=%v", won, err)
		}
		return nil
	}
	result := &agent.ExecutionResult{
		Success: true, RemoteArtifacts: true,
		ModelUsage: []agent.ModelTokenUsage{{Provider: "provider", Model: "stale-attempt", InputTokens: 31}},
		CostStatus: agent.CostStatusReconciled,
	}
	err := svc.CompleteCloudExecution(ctx, execution.ID, result)
	if !errors.Is(err, ErrStaleTaskExecution) {
		t.Fatalf("stale evidence finalization error = %v, want ErrStaleTaskExecution", err)
	}

	foundTask, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundTask.Result != nil || foundTask.CostStatus != "" || len(foundTask.TerminalModelUsage.Data()) != 0 {
		t.Fatalf("stale execution wrote task evidence: result=%v cost_status=%q usage=%+v", foundTask.Result, foundTask.CostStatus, foundTask.TerminalModelUsage.Data())
	}
	foundExecution, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundExecution.FinalizationStatus != model.TaskExecutionFinalizationArtifacts {
		t.Fatalf("stale execution finalization advanced to %q, want %q", foundExecution.FinalizationStatus, model.TaskExecutionFinalizationArtifacts)
	}
}

func TestStaleCloudFinalizerStopsBeforeTaskAndSettlementSideEffects(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	ctx := context.Background()
	if err := repo.Users().Create(ctx, &model.User{
		ID: task.UserID, Email: task.UserID + "@example.com", Password: "x", InviteCode: uuid.NewString()[:12],
	}); err != nil {
		t.Fatal(err)
	}
	terminalResult := &agent.ExecutionResult{Success: false, Error: "old attempt failed", RemoteArtifacts: true}
	encoded, err := json.Marshal(terminalResult)
	if err != nil {
		t.Fatal(err)
	}
	won, err := repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionFailed,
		model.ExecutionTransition{
			TerminalReason:     "old_attempt_failed",
			Result:             encoded,
			FinalizationStatus: model.TaskExecutionFinalizationResult,
		})
	if err != nil || !won {
		t.Fatalf("terminalize old execution: won=%v err=%v", won, err)
	}
	oldExecution, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: 2, Target: "kubernetes",
		Status: model.TaskExecutionRunning, Started: true,
	}
	if err := repo.TaskExecutions().Create(ctx, next); err != nil {
		t.Fatal(err)
	}
	if swapped, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, next.ID); err != nil || !swapped {
		t.Fatalf("switch current execution: swapped=%v err=%v", swapped, err)
	}

	err = svc.finalizeTaskFromExecution(ctx, task, oldExecution)
	if !errors.Is(err, ErrStaleTaskExecution) {
		t.Fatalf("stale finalizer error = %v, want ErrStaleTaskExecution", err)
	}
	foundTask, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundTask.Status != model.TaskStatusRunning || foundTask.WorkflowStatus != nil || foundTask.CurrentExecutionID == nil || *foundTask.CurrentExecutionID != next.ID {
		t.Fatalf("stale finalizer mutated current task: status=%q workflow=%v current=%v", foundTask.Status, foundTask.WorkflowStatus, foundTask.CurrentExecutionID)
	}
	foundOld, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundOld.FinalizationStatus != model.TaskExecutionFinalizationResult || foundOld.PublishingStatus != "" {
		t.Fatalf("stale finalizer advanced execution: stage=%q publishing=%q", foundOld.FinalizationStatus, foundOld.PublishingStatus)
	}
}

func TestCompleteCloudExecutionRejectsSuccessWithoutManifest(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if foundTask.Status != model.TaskStatusFailed || foundExecution.Status != model.TaskExecutionFailed || foundExecution.ManifestStatus != model.TaskExecutionManifestCollected {
		t.Fatalf("task=%s execution=%s manifest=%s", foundTask.Status, foundExecution.Status, foundExecution.ManifestStatus)
	}
}

func TestCompleteCloudExecutionCollectsArtifactsWhenValidationFails(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	task.Type = model.PlatformSeednote
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().DeleteByTaskID(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	failureFile := &model.TaskFile{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID,
		State: model.TaskFileStatePending, Role: model.FileRoleOther,
		FilePath: "output/failure-state.json", FileName: "failure-state.json", FileSize: 96,
	}
	if err := repo.TaskFiles().Create(context.Background(), failureFile); err != nil {
		t.Fatal(err)
	}

	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	foundExecution, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	files, err := repo.TaskFiles().FindByExecutionID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundExecution.Status != model.TaskExecutionFailed || foundExecution.ManifestStatus != model.TaskExecutionManifestCollected {
		t.Fatalf("execution status=%s manifest=%s", foundExecution.Status, foundExecution.ManifestStatus)
	}
	if len(files) != 1 || files[0].State != model.TaskFileStateCollected || files[0].FileName != "failure-state.json" {
		t.Fatalf("collected files = %#v", files)
	}
}

func TestCompleteCloudExecutionConcurrentDuplicate(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.CompleteCloudExecution(context.Background(), execution.ID, result)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatal(err)
	}
	found, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	files, _ := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if found.FinalizationStatus != model.TaskExecutionFinalizationDone || len(files) != 1 {
		t.Fatalf("finalization=%s files=%d", found.FinalizationStatus, len(files))
	}
}

func TestCompleteCloudExecutionResumesEveryDurableStage(t *testing.T) {
	stages := []string{
		model.TaskExecutionFinalizationTerminal,
		model.TaskExecutionFinalizationArtifacts,
		model.TaskExecutionFinalizationResult,
		model.TaskExecutionFinalizationWorkflow,
		model.TaskExecutionFinalizationPublishing,
		model.TaskExecutionFinalizationTask,
		model.TaskExecutionFinalizationSettlement,
		model.TaskExecutionFinalizationSlot,
		model.TaskExecutionFinalizationDispatch,
		model.TaskExecutionFinalizationNotification,
	}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			svc, repo, task, execution := setupCloudCompletionTest(t, true)
			injected := false
			svc.finalizationAfterStage = func(got string) error {
				if got == stage && !injected {
					injected = true
					return errors.New("injected finalization failure")
				}
				return nil
			}
			result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err == nil {
				t.Fatal("expected injected failure")
			}
			svc.finalizationAfterStage = nil
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
				t.Fatalf("resume: %v", err)
			}
			foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
			foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
			if foundTask.Status != model.TaskStatusCompleted || foundExecution.FinalizationStatus != model.TaskExecutionFinalizationDone {
				t.Fatalf("task=%s finalization=%s", foundTask.Status, foundExecution.FinalizationStatus)
			}
		})
	}
	markerStages := append(append([]string(nil), stages[1:]...), model.TaskExecutionFinalizationDone)
	for _, stage := range markerStages {
		t.Run("after-marker-"+stage, func(t *testing.T) {
			svc, repo, task, execution := setupCloudCompletionTest(t, true)
			injected := false
			svc.finalizationAfterAdvance = func(got string) error {
				if got == stage && !injected {
					injected = true
					return errors.New("injected failure after durable marker")
				}
				return nil
			}
			result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err == nil {
				t.Fatal("expected injected failure")
			}
			svc.finalizationAfterAdvance = nil
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
				t.Fatalf("resume: %v", err)
			}
			foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
			foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
			if foundTask.Status != model.TaskStatusCompleted || foundExecution.FinalizationStatus != model.TaskExecutionFinalizationDone {
				t.Fatalf("task=%s finalization=%s", foundTask.Status, foundExecution.FinalizationStatus)
			}
		})
	}
}

func TestFinalizationDispatchReplayDoesNotDuplicateQueueOrInflateSlot(t *testing.T) {
	db := setupTaskTestDB(t)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	repo := repository.New(db)
	ctx := context.Background()
	userID, projectID := uuid.NewString(), uuid.NewString()
	project := &model.Project{ID: projectID, UserID: userID, Name: "dispatch", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	executionID := uuid.NewString()
	completedTask := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning, CurrentExecutionID: &executionID}
	if err := repo.Tasks().Create(ctx, completedTask); err != nil {
		t.Fatal(err)
	}
	pendingTask := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusPending}
	if err := repo.Tasks().Create(ctx, pendingTask); err != nil {
		t.Fatal(err)
	}
	execution := &model.TaskExecution{ID: executionID, TaskID: completedTask.ID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true, ManifestStatus: model.TaskExecutionManifestPending}
	if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{{
		ID: uuid.NewString(), TaskID: completedTask.ID, ExecutionID: executionID, State: model.TaskFileStatePending,
		Role: "content", FilePath: "output/content.md", FileName: "content.md", FileSize: 8,
	}}); err != nil {
		t.Fatal(err)
	}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	logger := zerolog.New(io.Discard)
	enqueuer := &replaySafeEnqueuer{}
	svc := NewTaskService(repo, nil, enqueuer, nil, &logger, "", nil, "", NewRedisPubSub(rdb, &logger), nil)
	injected := false
	svc.finalizationAfterStage = func(stage string) error {
		if stage == model.TaskExecutionFinalizationDispatch && !injected {
			injected = true
			return errors.New("crash after pending dispatch")
		}
		return nil
	}
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	if err := svc.CompleteCloudExecution(ctx, executionID, result); err == nil {
		t.Fatal("expected injected crash after dispatch side effect")
	}
	found, _ := repo.TaskExecutions().FindByID(ctx, executionID)
	if found.FinalizationStatus != model.TaskExecutionFinalizationSlot || enqueuer.calls != 1 || enqueuer.accepted != 1 {
		t.Fatalf("interrupted stage=%s enqueue calls=%d accepted=%d", found.FinalizationStatus, enqueuer.calls, enqueuer.accepted)
	}
	if count, err := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int64(); err != nil || count != 1 {
		t.Fatalf("slot count after first dispatch=%d err=%v, want 1", count, err)
	}

	svc.finalizationAfterStage = nil
	if err := svc.ResumeExecutionFinalization(ctx, executionID); err != nil {
		t.Fatalf("resume finalization: %v", err)
	}
	found, _ = repo.TaskExecutions().FindByID(ctx, executionID)
	if found.FinalizationStatus != model.TaskExecutionFinalizationDone || enqueuer.calls != 2 || enqueuer.accepted != 1 {
		t.Fatalf("resumed stage=%s enqueue calls=%d accepted=%d", found.FinalizationStatus, enqueuer.calls, enqueuer.accepted)
	}
	if count, err := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int64(); err != nil || count != 1 {
		t.Fatalf("slot count after replay=%d err=%v, want stable 1", count, err)
	}
}

func TestFinalizationLeaseRenewsAcrossLongStage(t *testing.T) {
	svc, repo, _, execution := setupCloudCompletionTest(t, true)
	svc.finalizationLease = 40 * time.Millisecond
	svc.finalizationRenewEvery = 5 * time.Millisecond
	entered, release := make(chan struct{}), make(chan struct{})
	svc.finalizationAfterStage = func(stage string) error {
		if stage == model.TaskExecutionFinalizationArtifacts {
			close(entered)
			<-release
		}
		return nil
	}
	done := make(chan error, 1)
	go func() {
		done <- svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true})
	}()
	<-entered
	time.Sleep(80 * time.Millisecond)
	won, err := repo.TaskExecutions().ClaimFinalization(context.Background(), execution.ID, uuid.NewString(), 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if won {
		t.Fatal("renewed finalization lease was stolen")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFinalizationRenewalLossPreventsStageAdvance(t *testing.T) {
	svc, repo, _, execution := setupCloudCompletionTest(t, true)
	svc.finalizationLease = 30 * time.Millisecond
	svc.finalizationRenewEvery = 2 * time.Millisecond
	stageFinished := make(chan struct{})
	svc.finalizationRenewClaim = func(context.Context, string, string) (bool, error) {
		<-stageFinished
		return false, nil
	}
	svc.finalizationAfterStage = func(stage string) error {
		if stage == model.TaskExecutionFinalizationArtifacts {
			close(stageFinished)
			time.Sleep(20 * time.Millisecond)
		}
		return nil
	}
	err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true})
	if !errors.Is(err, ErrFinalizationLeaseLost) {
		t.Fatalf("error=%v, want lease lost", err)
	}
	found, findErr := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if found.FinalizationStatus != model.TaskExecutionFinalizationTerminal {
		t.Fatalf("stage advanced after lease loss: %s", found.FinalizationStatus)
	}
}

type ambiguousPublishFake struct {
	mu    sync.Mutex
	calls int
}

func (p *ambiguousPublishFake) PublishDraft(context.Context, string, string, []DraftArticleInput) (*PublishDraftResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return &PublishDraftResult{MediaID: "media-1"}, nil
}

func (p *ambiguousPublishFake) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestCloudPublishingAmbiguityNeverCallsProviderTwice(t *testing.T) {
	db := setupTaskTestDB(t)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	repo := repository.New(db)
	ctx := context.Background()
	userID, projectID, taskID, executionID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	project := &model.Project{ID: projectID, UserID: userID, Name: "publish", Platform: model.PlatformArticle, Status: model.ProjectStatusActive, Config: model.ProjectConfig{EnablePublishing: true}}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning, CurrentExecutionID: &executionID}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	execution := &model.TaskExecution{ID: executionID, TaskID: taskID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true, ManifestStatus: model.TaskExecutionManifestPending}
	if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
		t.Fatal(err)
	}
	store := &fakeTaskStorage{name: "oss", files: map[string][]byte{"draft-key": []byte(`{"articles":[{"title":"T","content":"<p>body</p>"}]}`)}}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{ID: uuid.NewString(), TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "output/draft.json", FileName: "draft.json", FileSize: 50, OSSKey: "draft-key", StorageProvider: "oss"}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	publisher := &ambiguousPublishFake{}
	svc.cloudPublisher = publisher
	svc.finalizationAfterEffect = func(stage string) error {
		if stage == model.TaskExecutionFinalizationPublishing {
			return errors.New("crash after provider success")
		}
		return nil
	}
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	if err := svc.CompleteCloudExecution(ctx, executionID, result); err == nil {
		t.Fatal("expected injected publish crash")
	}
	svc.finalizationAfterEffect = nil
	if err := svc.CompleteCloudExecution(ctx, executionID, result); !errors.Is(err, ErrCloudPublishingAmbiguous) {
		t.Fatalf("retry error=%v, want ambiguous", err)
	}
	if publisher.callCount() != 1 {
		t.Fatalf("provider calls=%d, want 1", publisher.callCount())
	}
	found, _ := repo.TaskExecutions().FindByID(ctx, executionID)
	if found.PublishingStatus != model.TaskExecutionPublishingInFlight || found.FinalizationStatus != model.TaskExecutionFinalizationWorkflow {
		t.Fatalf("publishing=%s stage=%s", found.PublishingStatus, found.FinalizationStatus)
	}
	if err := svc.ResolveCloudPublishing(ctx, executionID, true); err != nil {
		t.Fatalf("resolve confirmed provider success: %v", err)
	}
	if publisher.callCount() != 1 {
		t.Fatalf("provider calls after resolution=%d, want 1", publisher.callCount())
	}
	found, _ = repo.TaskExecutions().FindByID(ctx, executionID)
	if found.PublishingStatus != model.TaskExecutionPublishingSucceeded || found.FinalizationStatus != model.TaskExecutionFinalizationDone {
		t.Fatalf("resolved publishing=%s stage=%s", found.PublishingStatus, found.FinalizationStatus)
	}
}

type cancelOrderingDispatcher struct {
	repo           repository.Repository
	statusAtDelete string
	deleteErr      error
}

func (*cancelOrderingDispatcher) ResolveRuntime(string) srvconfig.RuntimeImageSelection {
	return srvconfig.RuntimeImageSelection{Profile: "article", Image: "registry/content@sha256:test"}
}

func (*cancelOrderingDispatcher) Dispatch(_ context.Context, execution *model.TaskExecution, _ *model.Task) (*agent.KubernetesRuntimeIdentity, error) {
	return &agent.KubernetesRuntimeIdentity{Namespace: "anban", JobName: "job-" + execution.ID}, nil
}
func (d *cancelOrderingDispatcher) Delete(_ context.Context, execution *model.TaskExecution) error {
	found, _ := d.repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	d.statusAtDelete = found.Status
	return d.deleteErr
}
func (*cancelOrderingDispatcher) DeleteProjectMemory(context.Context, string) error { return nil }
func (*cancelOrderingDispatcher) Inspect(context.Context, *model.TaskExecution) (*agent.KubernetesExecutionState, error) {
	return nil, nil
}

func TestCancelCloudMarksAttemptBeforeDeleteAndDoesNotRollBack(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	dispatcher := &cancelOrderingDispatcher{repo: repo, deleteErr: errors.New("delete unavailable")}
	svc.SetKubernetesDispatcher(dispatcher)
	svc.cleanupRetryBackoff = time.Millisecond
	err := svc.CancelForUser(context.Background(), task.UserID, task.ID)
	if err == nil {
		t.Fatal("expected delete error")
	}
	foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if dispatcher.statusAtDelete != model.TaskExecutionCancelled || foundTask.Status != model.TaskStatusCancelled || foundExecution.Status != model.TaskExecutionCancelled {
		t.Fatalf("delete=%s task=%s execution=%s", dispatcher.statusAtDelete, foundTask.Status, foundExecution.Status)
	}
	if foundExecution.CompletedAt == nil || time.Since(*foundExecution.CompletedAt) > time.Minute {
		t.Fatalf("completed_at = %v", foundExecution.CompletedAt)
	}
	if foundExecution.CleanupStatus != model.TaskExecutionCleanupPending {
		t.Fatalf("cleanup status after delete error=%s", foundExecution.CleanupStatus)
	}
	dispatcher.deleteErr = nil
	time.Sleep(2 * time.Millisecond)
	if err := svc.CancelForUser(context.Background(), task.UserID, task.ID); err != nil {
		t.Fatalf("retry cancellation cleanup: %v", err)
	}
	foundExecution, _ = repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if foundExecution.CleanupStatus != model.TaskExecutionCleanupDone {
		t.Fatalf("cleanup status after retry=%s", foundExecution.CleanupStatus)
	}
}

func TestReconcileExecutionFailureRetriesOnlyPreStart(t *testing.T) {
	t.Run("one configured replacement", func(t *testing.T) {
		svc, repo, task, execution := setupCloudCompletionTest(t, true, false)
		dispatcher := &dispatchTestDispatcher{}
		svc.SetKubernetesDispatcher(dispatcher)
		if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "image_pull_failed", nil, 1); err != nil {
			t.Fatal(err)
		}
		current, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		old, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
		foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
		if current.Attempt != 2 || current.Status != model.TaskExecutionStarting || old.Status != model.TaskExecutionFailed || foundTask.Status != model.TaskStatusRunning {
			t.Fatalf("current=%+v old=%s task=%s", current, old.Status, foundTask.Status)
		}
		if old.CleanupStatus != model.TaskExecutionCleanupPending {
			t.Fatalf("old cleanup status=%s", old.CleanupStatus)
		}
		reconcilable, err := repo.TaskExecutions().FindReconcilable(context.Background(), time.Now().Add(time.Second), 10)
		if err != nil {
			t.Fatal(err)
		}
		foundOld := false
		for _, candidate := range reconcilable {
			foundOld = foundOld || candidate.ID == old.ID
		}
		if !foundOld {
			t.Fatal("replaced terminal execution was excluded before cleanup")
		}
	})

	t.Run("post-start enters terminal finalizer", func(t *testing.T) {
		svc, repo, task, execution := setupCloudCompletionTest(t, true, true)
		dispatcher := &dispatchTestDispatcher{}
		svc.SetKubernetesDispatcher(dispatcher)
		if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "job_failed", nil, 1); err != nil {
			t.Fatal(err)
		}
		current, _ := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
		foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
		if current.ID != execution.ID || current.Status != model.TaskExecutionFailed || foundTask.Status != model.TaskStatusFailed || dispatcher.callCount() != 0 {
			t.Fatalf("current=%+v task=%s dispatches=%d", current, foundTask.Status, dispatcher.callCount())
		}
	})
}

func TestReplacePreStartExecutionPreservesRuntimeImage(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true, false)
	dispatcher := &dispatchTestDispatcher{}
	svc.SetKubernetesDispatcher(dispatcher)
	if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "image_pull_failed", nil, 1); err != nil {
		t.Fatal(err)
	}
	replacement, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.RuntimeProfile != execution.RuntimeProfile || replacement.RuntimeImage != execution.RuntimeImage {
		t.Fatalf("replacement runtime = %q %q, want %q %q", replacement.RuntimeProfile, replacement.RuntimeImage, execution.RuntimeProfile, execution.RuntimeImage)
	}
}

func TestReplacementDispatchResumesSameAttemptAfterTransientFailure(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true, false)
	dispatcher := &dispatchTestDispatcher{err: errors.New("temporary Kubernetes API failure")}
	svc.SetKubernetesDispatcher(dispatcher)
	if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "image_pull_failed", nil, 1); err == nil {
		t.Fatal("expected first replacement dispatch to fail")
	}
	replacement, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Attempt != 2 || replacement.Status != model.TaskExecutionCreated {
		t.Fatalf("replacement=%+v", replacement)
	}
	dispatcher.mu.Lock()
	dispatcher.err = nil
	dispatcher.mu.Unlock()
	if err := svc.ResumeExecutionDispatch(context.Background(), replacement.ID); err != nil {
		t.Fatal(err)
	}
	current, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != replacement.ID || current.Attempt != 2 || current.Status != model.TaskExecutionStarting || dispatcher.callCount() != 2 || dispatcher.createCount() != 1 {
		t.Fatalf("current=%+v calls=%d creates=%d", current, dispatcher.callCount(), dispatcher.createCount())
	}
}

func TestConcurrentPreStartReconcileCreatesOneReplacement(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true, false)
	dispatcher := &dispatchTestDispatcher{}
	svc.SetKubernetesDispatcher(dispatcher)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "scheduling_failed", nil, 1)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrStaleTaskExecution) {
			t.Fatal(err)
		}
	}
	current, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := repo.TaskExecutions().NextAttempt(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Attempt != 2 || next != 3 || dispatcher.createCount() != 1 {
		t.Fatalf("current attempt=%d next=%d jobs=%d", current.Attempt, next, dispatcher.createCount())
	}
}

func TestBootstrapStartedBoundaryPreventsPreStartReplacement(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true, false)
	dispatcher := &dispatchTestDispatcher{}
	svc.SetKubernetesDispatcher(dispatcher)
	if won, err := repo.TaskExecutions().Transition(context.Background(), execution.ID,
		[]string{model.TaskExecutionStarting}, model.TaskExecutionRunning,
		model.ExecutionTransition{Started: true, PodUID: "pod-1"}); err != nil || !won {
		t.Fatalf("mark bootstrap started: won=%v err=%v", won, err)
	}
	if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "job_failed", nil, 1); err != nil {
		t.Fatal(err)
	}
	current, _ := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if current.ID != execution.ID || current.Attempt != 1 || !current.Started || current.Status != model.TaskExecutionFailed {
		t.Fatalf("current=%+v", current)
	}
	if dispatcher.createCount() != 0 {
		t.Fatalf("replacement jobs=%d", dispatcher.createCount())
	}
}
