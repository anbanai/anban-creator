package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
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
	serverconfig "github.com/anbanai/anban-creator/server/config"
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
	runtimeSelection  serverconfig.RuntimeImageSelection
	identity          *model.RuntimeIdentity
}

type scopedDispatchTestDispatcher struct {
	*dispatchTestDispatcher
	scope string
}

func (d *scopedDispatchTestDispatcher) Scope() string { return d.scope }

func (d *dispatchTestDispatcher) ResolveRuntime(string) serverconfig.RuntimeImageSelection {
	if d.runtimeSelection.Profile != "" || d.runtimeSelection.Image != "" {
		return d.runtimeSelection
	}
	return serverconfig.RuntimeImageSelection{Profile: "article", Image: "registry/content@sha256:test"}
}

func (d *dispatchTestDispatcher) Scope() string { return "docker" }

type cancelingReferenceAssetRepository struct {
	repository.AssetRepository
	cancel func()
}

type executionPreparationRepository struct {
	repository.Repository
	tasks  repository.TaskRepository
	assets repository.AssetRepository
	wrapTx func(repository.Repository) repository.Repository
}

func (r *executionPreparationRepository) Tasks() repository.TaskRepository {
	return r.tasks
}

func (r *executionPreparationRepository) Assets() repository.AssetRepository {
	return r.assets
}

func (r *executionPreparationRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(txRepo repository.Repository) error {
		if r.wrapTx != nil {
			txRepo = r.wrapTx(txRepo)
		}
		return fn(txRepo)
	})
}

type failPendingOnceState struct {
	err          error
	mu           sync.Mutex
	calls        int
	firstFailure chan struct{}
}

type failPendingOnceTaskRepository struct {
	repository.TaskRepository
	state *failPendingOnceState
}

type failRunningTaskRepository struct {
	repository.TaskRepository
	err error
}

type losingFailPendingTaskRepository struct {
	repository.TaskRepository
}

func (r *losingFailPendingTaskRepository) FailPendingTask(context.Context, string, string) (bool, error) {
	return false, nil
}

func (r *failRunningTaskRepository) FailRunningTask(context.Context, string, string) (bool, error) {
	return false, r.err
}

func (r *failPendingOnceTaskRepository) FailPendingTask(ctx context.Context, taskID, errorMsg string) (bool, error) {
	r.state.mu.Lock()
	r.state.calls++
	call := r.state.calls
	if call == 1 && r.state.firstFailure != nil {
		close(r.state.firstFailure)
	}
	r.state.mu.Unlock()
	if call == 1 {
		return false, r.state.err
	}
	return r.TaskRepository.FailPendingTask(ctx, taskID, errorMsg)
}

func (r *failPendingOnceTaskRepository) callCount() int {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	return r.state.calls
}

func newFailPendingOnceRepository(base repository.Repository, state *failPendingOnceState, assets repository.AssetRepository) (*executionPreparationRepository, *failPendingOnceTaskRepository) {
	tasks := &failPendingOnceTaskRepository{TaskRepository: base.Tasks(), state: state}
	repo := &executionPreparationRepository{Repository: base, tasks: tasks, assets: assets}
	repo.wrapTx = func(txRepo repository.Repository) repository.Repository {
		return &executionPreparationRepository{
			Repository: txRepo,
			tasks:      &failPendingOnceTaskRepository{TaskRepository: txRepo.Tasks(), state: state},
			assets:     txRepo.Assets(),
		}
	}
	return repo, tasks
}

type contextCapturingAssetRepository struct {
	repository.AssetRepository
	contexts chan context.Context
}

func (r *contextCapturingAssetRepository) FindOwnedByID(ctx context.Context, id, userID string) (*model.Asset, error) {
	r.contexts <- ctx
	return r.AssetRepository.FindOwnedByID(ctx, id, userID)
}

func (r *cancelingReferenceAssetRepository) FindOwnedByID(context.Context, string, string) (*model.Asset, error) {
	r.cancel()
	return nil, context.Canceled
}

func (d *dispatchTestDispatcher) Dispatch(_ context.Context, execution *model.TaskExecution, _ *model.Task) (*model.RuntimeIdentity, error) {
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
	var identity *model.RuntimeIdentity
	if d.identity != nil {
		value := *d.identity
		identity = &value
	}
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
	if err != nil {
		return nil, err
	}
	if identity != nil {
		return identity, nil
	}
	return &model.RuntimeIdentity{Scope: "daemon-a", Workload: "container-" + execution.ID}, nil
}

func (d *dispatchTestDispatcher) Delete(context.Context, *model.TaskExecution) error {
	return nil
}

func (d *dispatchTestDispatcher) DeleteProjectMemory(context.Context, string) error {
	return nil
}

func (d *dispatchTestDispatcher) Inspect(context.Context, *model.TaskExecution) (*agent.RuntimeExecutionState, error) {
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
	svc := NewTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
	svc.SetRuntimeDispatcher(dispatcher)
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

func TestDispatchCloudTaskCreatesOneAttemptAndReturnsAfterRuntimeAccepted(t *testing.T) {
	svc, repo, _, dispatcher, task := setupDispatchTest(t)
	ctx := context.Background()
	if err := svc.HandleExecutionFromPayload(ctx, task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	if dispatcher.callCount() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", dispatcher.callCount())
	}
	current := mustCurrentExecution(t, repo, task.ID)
	if current.Attempt != 1 || current.Target != "docker" || current.Status != model.TaskExecutionStarting {
		t.Fatalf("current execution = %+v", current)
	}
	if current.RuntimeScope != "daemon-a" || current.RuntimeWorkload != "container-"+current.ID {
		t.Fatalf("runtime identity = %q/%q, want persisted scope and workload", current.RuntimeScope, current.RuntimeWorkload)
	}
	if err := svc.HandleExecutionFromPayload(ctx, task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	if dispatcher.callCount() != 1 {
		t.Fatalf("duplicate dispatch calls = %d", dispatcher.callCount())
	}
}

func TestDispatchCurrentExecutionRejectsRuntimeTargetMismatchBeforeClaim(t *testing.T) {
	svc, repo, db, dispatcher, task := setupDispatchTest(t)
	ctx := context.Background()
	execution, created, err := svc.createCurrentExecution(ctx, task)
	if err != nil || !created {
		t.Fatalf("create execution: created=%v err=%v", created, err)
	}
	if err := db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).Update("target", "kubernetes").Error; err != nil {
		t.Fatal(err)
	}
	task, execution, err = svc.reloadCurrentDispatchState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	err = svc.dispatchCurrentExecution(ctx, task, execution)
	if !errors.Is(err, ErrRuntimeDispatcherTargetMismatch) {
		t.Fatalf("dispatch error = %v, want ErrRuntimeDispatcherTargetMismatch", err)
	}
	current, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher.callCount() != 0 || current.Status != model.TaskExecutionCreated || current.DispatchClaimToken != "" || current.DispatchClaimedAt != nil {
		t.Fatalf("mismatched dispatch mutated state: calls=%d execution=%+v", dispatcher.callCount(), current)
	}
}

func TestDispatchCurrentExecutionNormalizesTargetForComparison(t *testing.T) {
	svc, _, db, dispatcher, task := setupDispatchTest(t)
	ctx := context.Background()
	execution, created, err := svc.createCurrentExecution(ctx, task)
	if err != nil || !created {
		t.Fatalf("create execution: created=%v err=%v", created, err)
	}
	if err := db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).Update("target", " docker ").Error; err != nil {
		t.Fatal(err)
	}
	svc.SetRuntimeDispatcher(&scopedDispatchTestDispatcher{dispatchTestDispatcher: dispatcher, scope: " docker "})
	task, execution, err = svc.reloadCurrentDispatchState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.dispatchCurrentExecution(ctx, task, execution); err != nil {
		t.Fatalf("dispatch normalized target: %v", err)
	}
	if dispatcher.callCount() != 1 || mustCurrentExecution(t, svc.repo, task.ID).Status != model.TaskExecutionStarting {
		t.Fatalf("normalized dispatch calls=%d execution=%+v", dispatcher.callCount(), mustCurrentExecution(t, svc.repo, task.ID))
	}
}

func TestDispatchReturnedIdentityIsNormalizedBeforePersistence(t *testing.T) {
	svc, repo, _, dispatcher, task := setupDispatchTest(t)
	dispatcher.identity = &model.RuntimeIdentity{
		Scope:      "  daemon-a  ",
		Workload:   "  container-a  ",
		InstanceID: "  instance-a  ",
	}
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	execution := mustCurrentExecution(t, repo, task.ID)
	if execution.RuntimeScope != "daemon-a" || execution.RuntimeWorkload != "container-a" || execution.RuntimeInstanceID != "instance-a" {
		t.Fatalf("persisted identity = %q/%q/%q, want normalized values", execution.RuntimeScope, execution.RuntimeWorkload, execution.RuntimeInstanceID)
	}
}

func TestDispatchIncompleteIdentityFailsPermanently(t *testing.T) {
	svc, repo, _, dispatcher, task := setupDispatchTest(t)
	dispatcher.identity = &model.RuntimeIdentity{Scope: "  ", Workload: "container-a"}
	err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID)
	if err == nil || !agent.IsPermanentDispatchError(err) {
		t.Fatalf("dispatch error = %v, want permanent incomplete identity failure", err)
	}
	execution := mustCurrentExecution(t, repo, task.ID)
	if execution.Status != model.TaskExecutionFailed || execution.DispatchClaimToken != "" || execution.DispatchClaimedAt != nil || execution.RuntimeScope != "" || execution.RuntimeWorkload != "" {
		t.Fatalf("failed incomplete dispatch = %+v", execution)
	}
}

func TestDispatchRuntimeIdentityConflictFailsPermanently(t *testing.T) {
	svc, repo, _, dispatcher, task := setupDispatchTest(t)
	ctx := context.Background()
	execution, created, err := svc.createCurrentExecution(ctx, task)
	if err != nil || !created {
		t.Fatalf("create execution: created=%v err=%v", created, err)
	}
	if err := repo.TaskExecutions().SetRuntimeIdentity(ctx, execution.ID, model.RuntimeIdentity{Scope: "daemon-old"}); err != nil {
		t.Fatal(err)
	}
	dispatcher.identity = &model.RuntimeIdentity{Scope: "daemon-new", Workload: "container-a"}
	task, execution, err = svc.reloadCurrentDispatchState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	err = svc.dispatchCurrentExecution(ctx, task, execution)
	if !errors.Is(err, repository.ErrRuntimeIdentityConflict) || !agent.IsPermanentDispatchError(err) {
		t.Fatalf("dispatch error = %v, want permanent ErrRuntimeIdentityConflict", err)
	}
	execution = mustCurrentExecution(t, repo, task.ID)
	if execution.Status != model.TaskExecutionFailed || execution.DispatchClaimToken != "" || execution.DispatchClaimedAt != nil {
		t.Fatalf("conflicted dispatch was not terminalized: %+v", execution)
	}
}

func TestDispatchRuntimeIdentitySchemaLengths(t *testing.T) {
	tests := []struct {
		name      string
		identity  model.RuntimeIdentity
		permanent bool
	}{
		{name: "maximum", identity: model.RuntimeIdentity{Scope: strings.Repeat("s", 63), Workload: strings.Repeat("w", 63), InstanceID: strings.Repeat("i", 64)}},
		{name: "scope over limit", identity: model.RuntimeIdentity{Scope: strings.Repeat("s", 64), Workload: "workload"}, permanent: true},
		{name: "workload over limit", identity: model.RuntimeIdentity{Scope: "scope", Workload: strings.Repeat("w", 64)}, permanent: true},
		{name: "instance over limit", identity: model.RuntimeIdentity{Scope: "scope", Workload: "workload", InstanceID: strings.Repeat("i", 65)}, permanent: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, dispatcher, task := setupDispatchTest(t)
			dispatcher.identity = &tc.identity
			err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID)
			execution := mustCurrentExecution(t, repo, task.ID)
			if tc.permanent {
				if err == nil || !agent.IsPermanentDispatchError(err) {
					t.Fatalf("dispatch error = %v, want permanent schema-length failure", err)
				}
				if execution.Status != model.TaskExecutionFailed || execution.RuntimeScope != "" || execution.RuntimeWorkload != "" || execution.RuntimeInstanceID != "" {
					t.Fatalf("over-limit identity persisted: %+v", execution)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if execution.Status != model.TaskExecutionStarting || execution.RuntimeScope != tc.identity.Scope || execution.RuntimeWorkload != tc.identity.Workload || execution.RuntimeInstanceID != tc.identity.InstanceID {
				t.Fatalf("maximum identity = %+v", execution)
			}
		})
	}
}

func TestRecordExecutionInstanceIsIdempotentAndPropagatesIdentityConflict(t *testing.T) {
	svc, repo, _, _, task := setupDispatchTest(t)
	ctx := context.Background()
	if err := svc.HandleExecutionFromPayload(ctx, task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	execution := mustCurrentExecution(t, repo, task.ID)
	if err := svc.RecordExecutionInstance(ctx, execution.ID, "instance-1"); err != nil {
		t.Fatalf("record instance: %v", err)
	}
	if err := svc.RecordExecutionInstance(ctx, execution.ID, "instance-1"); err != nil {
		t.Fatalf("replay instance: %v", err)
	}
	if err := svc.RecordExecutionInstance(ctx, execution.ID, "instance-2"); !errors.Is(err, repository.ErrRuntimeIdentityConflict) {
		t.Fatalf("drift error = %v, want ErrRuntimeIdentityConflict", err)
	}
}

func TestCreateCurrentExecutionPersistsRuntimeImage(t *testing.T) {
	svc, repo, _, dispatcher, task := setupDispatchTest(t)
	dispatcher.runtimeSelection = serverconfig.RuntimeImageSelection{
		Profile: model.PlatformMontage,
		Image:   "registry/montage@sha256:abc",
	}
	task.Type = model.PlatformMontage
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	execution := mustCurrentExecution(t, repo, task.ID)
	if execution.RuntimeProfile != model.PlatformMontage || execution.RuntimeImage != "registry/montage@sha256:abc" {
		t.Fatalf("runtime identity = %q %q", execution.RuntimeProfile, execution.RuntimeImage)
	}
}

func TestCreateCurrentExecutionRejectsMissingRuntimeDispatcher(t *testing.T) {
	svc, _, _, _, task := setupDispatchTest(t)
	svc.SetRuntimeDispatcher(nil)
	if _, _, err := svc.createCurrentExecution(context.Background(), task); err == nil || !strings.Contains(err.Error(), "runtime dispatcher is not configured") {
		t.Fatalf("create error = %v, want missing runtime dispatcher", err)
	}
}

func TestCreateCurrentExecutionRejectsEmptyRuntimeScope(t *testing.T) {
	svc, _, _, dispatcher, task := setupDispatchTest(t)
	svc.SetRuntimeDispatcher(&scopedDispatchTestDispatcher{dispatchTestDispatcher: dispatcher})
	if _, _, err := svc.createCurrentExecution(context.Background(), task); err == nil || !strings.Contains(err.Error(), "runtime dispatcher scope is required") {
		t.Fatalf("create error = %v, want empty runtime scope", err)
	}
}

func TestCreateCurrentExecutionRejectsIncompleteRuntimeSelection(t *testing.T) {
	svc, _, _, dispatcher, task := setupDispatchTest(t)
	dispatcher.runtimeSelection = serverconfig.RuntimeImageSelection{Profile: "article"}
	if _, _, err := svc.createCurrentExecution(context.Background(), task); err == nil || !strings.Contains(err.Error(), "returned incomplete identity") {
		t.Fatalf("create error = %v, want incomplete runtime selection", err)
	}
}

func TestDispatchResumedTaskCreatesExecutionLineageWithClaudeSession(t *testing.T) {
	svc, repo, _, _, task := setupDispatchTest(t)
	ctx := context.Background()
	const sessionID = "bba21f1d-70b8-4157-917b-f9802c2b1740"
	result, err := json.Marshal(&agent.ExecutionResult{Success: false, Error: "max turns", SessionID: sessionID, RemoteArtifacts: true})
	if err != nil {
		t.Fatal(err)
	}
	parent := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionFailed, Result: result,
		RuntimeProfile: model.PlatformMontage, RuntimeImage: "registry/montage@sha256:parent",
	}
	if err := repo.TaskExecutions().Create(ctx, parent); err != nil {
		t.Fatal(err)
	}
	task.CurrentExecutionID = &parent.ID
	task.SetInputAttachments([]model.EntryAttachment{{Role: model.EntryAttachmentRoleResumeLatest, Text: "continue"}})
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleExecutionFromPayload(ctx, task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	current := mustCurrentExecution(t, repo, task.ID)
	if current.Attempt != 2 || current.ParentExecutionID != parent.ID || current.ResumeSessionID != sessionID {
		t.Fatalf("resumed execution = %#v", current)
	}
	if current.RuntimeProfile != parent.RuntimeProfile || current.RuntimeImage != parent.RuntimeImage {
		t.Fatalf("resumed runtime = %q %q, want parent %q %q", current.RuntimeProfile, current.RuntimeImage, parent.RuntimeProfile, parent.RuntimeImage)
	}
	if current.Target != "docker" {
		t.Fatalf("resumed target = %q, want selected dispatcher scope", current.Target)
	}
}

func TestDispatchAutocompactThrashingStartsFreshClaudeSessionAndRuntime(t *testing.T) {
	svc, repo, _, dispatcher, task := setupDispatchTest(t)
	dispatcher.runtimeSelection = serverconfig.RuntimeImageSelection{
		Profile: "article", Image: "registry/content@sha256:current",
	}
	ctx := context.Background()
	result, err := json.Marshal(&agent.ExecutionResult{
		Success:   false,
		Error:     "Autocompact is thrashing: the context refilled to the limit within 3 turns",
		SessionID: "bloated-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	parent := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionFailed, Result: result,
		RuntimeProfile: model.PlatformMontage, RuntimeImage: "registry/montage@sha256:parent",
	}
	if err := repo.TaskExecutions().Create(ctx, parent); err != nil {
		t.Fatal(err)
	}
	task.CurrentExecutionID = &parent.ID
	task.SetInputAttachments([]model.EntryAttachment{{Role: model.EntryAttachmentRoleResumeLatest, Text: "continue"}})
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}

	if err := svc.HandleExecutionFromPayload(ctx, task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	current := mustCurrentExecution(t, repo, task.ID)
	if current.ParentExecutionID != parent.ID {
		t.Fatalf("parent execution = %q, want %q", current.ParentExecutionID, parent.ID)
	}
	if current.ResumeSessionID != "" {
		t.Fatalf("resume session = %q, want a fresh Claude session after autocompact thrashing", current.ResumeSessionID)
	}
	if current.RuntimeProfile != model.PlatformArticle || current.RuntimeImage != "registry/content@sha256:current" {
		t.Fatalf("resumed runtime = %q %q, want current deployed runtime", current.RuntimeProfile, current.RuntimeImage)
	}
}

func TestResumeExecutionReusesParentRuntimeImage(t *testing.T) {
	svc, repo, _, dispatcher, task := setupDispatchTest(t)
	dispatcher.runtimeSelection = serverconfig.RuntimeImageSelection{Profile: "article", Image: "registry/content@sha256:new"}
	parent := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionFailed,
		RuntimeProfile: model.PlatformMontage, RuntimeImage: "registry/montage@sha256:original",
	}
	if err := repo.TaskExecutions().Create(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	task.CurrentExecutionID = &parent.ID
	task.SetInputAttachments([]model.EntryAttachment{{Role: model.EntryAttachmentRoleResumeLatest, Text: "continue"}})
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatal(err)
	}
	current := mustCurrentExecution(t, repo, task.ID)
	if current.RuntimeProfile != parent.RuntimeProfile || current.RuntimeImage != parent.RuntimeImage {
		t.Fatalf("resumed runtime = %q %q, want parent %q %q", current.RuntimeProfile, current.RuntimeImage, parent.RuntimeProfile, parent.RuntimeImage)
	}
}

func TestResumeExecutionRejectsParentWithoutRuntimeIdentity(t *testing.T) {
	svc, repo, _, _, task := setupDispatchTest(t)
	parent := &model.TaskExecution{ID: uuid.NewString(), TaskID: task.ID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionFailed}
	if err := repo.TaskExecutions().Create(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	task.CurrentExecutionID = &parent.ID
	task.SetInputAttachments([]model.EntryAttachment{{Role: model.EntryAttachmentRoleResumeLatest, Text: "continue"}})
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID)
	if err == nil || !strings.Contains(err.Error(), "resume parent execution runtime identity is missing") {
		t.Fatalf("resume error = %v, want missing runtime identity", err)
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
	svc.SetRuntimeDispatchLease(time.Minute)
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
	svc.SetRuntimeDispatchLease(time.Minute)

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
	svc.SetRuntimeDispatchLease(time.Minute)
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

func TestHandleExecutionFromPayloadWithoutDispatcherReturnsConfigurationError(t *testing.T) {
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
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
	if err := svc.HandleExecutionFromPayload(ctx, task.ID, task.UserID); err == nil || !strings.Contains(err.Error(), "runtime dispatcher is not configured") {
		t.Fatalf("HandleExecutionFromPayload error = %v, want dispatcher configuration error", err)
	}
	var count int64
	if err := db.Model(&model.TaskExecution{}).Where("task_id = ?", task.ID).Count(&count).Error; err != nil {
		t.Fatalf("count executions: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 || found.Status != model.TaskStatusPending {
		t.Fatalf("TaskExecution rows = %d, task status = %s; want no execution and pending task", count, found.Status)
	}
}

func TestHandleExecutionFromPayloadReferenceFailureFinalizesRunningTask(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "openid-" + userID}); err != nil {
		t.Fatal(err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusPending, ReferenceImageAssetID: "missing"}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Set(ctx, projectRunningCountPrefix+projectID, 1, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(repo, &mockEnqueuer{}, nil, &logger, "", NewRedisPubSub(rdb, &logger), nil)

	if err := svc.HandleExecutionFromPayload(ctx, task.ID, userID); err != nil {
		t.Fatalf("HandleExecutionFromPayload: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != model.TaskStatusFailed || found.CompletedAt == nil || !strings.Contains(found.ErrorMessage, "reference asset") {
		t.Fatalf("task after reference failure = %#v", found)
	}
	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("load user = %#v, err=%v", user, err)
	}
	if count, err := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int(); err != nil || count != 0 {
		t.Fatalf("running slot count = %d, err=%v", count, err)
	}
}

func TestHandleExecutionFromPayloadPendingReferenceFailureRetriesAfterTerminalPersistenceError(t *testing.T) {
	db := setupTaskTestDB(t)
	baseRepo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := baseRepo.Users().Create(ctx, &model.User{ID: userID, OpenID: "openid-" + userID}); err != nil {
		t.Fatal(err)
	}
	projectID := createTestProject(t, baseRepo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusPending, ReferenceImageAssetID: "missing"}
	if err := baseRepo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Set(ctx, projectRunningCountPrefix+projectID, 1, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	dbErr := errors.New("fail pending unavailable")
	state := &failPendingOnceState{err: dbErr}
	repo, tasks := newFailPendingOnceRepository(baseRepo, state, baseRepo.Assets())
	svc := NewTaskService(repo, &mockEnqueuer{}, nil, &logger, "", NewRedisPubSub(rdb, &logger), nil)

	if err := svc.HandleExecutionFromPayload(ctx, task.ID, userID); !errors.Is(err, dbErr) {
		t.Fatalf("first error = %v, want %v", err, dbErr)
	}
	found, err := baseRepo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	user, userErr := baseRepo.Users().FindByID(ctx, userID)
	count, countErr := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int()
	if found.Status != model.TaskStatusPending || found.CompletedAt != nil || userErr != nil || countErr != nil || count != 1 {
		t.Fatalf("state after persistence error: task=%#v user=%#v userErr=%v slot=%d slotErr=%v", found, user, userErr, count, countErr)
	}

	if err := svc.HandleExecutionFromPayload(ctx, task.ID, userID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	found, err = baseRepo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	user, userErr = baseRepo.Users().FindByID(ctx, userID)
	count, countErr = rdb.Get(ctx, projectRunningCountPrefix+projectID).Int()
	if found.Status != model.TaskStatusFailed || found.CompletedAt == nil || userErr != nil || countErr != nil || count != 0 {
		t.Fatalf("state after retry: task=%#v user=%#v userErr=%v slot=%d slotErr=%v", found, user, userErr, count, countErr)
	}
	if tasks.callCount() != 2 {
		t.Fatalf("fail pending calls=%d", tasks.callCount())
	}
}

func TestHandleExecutionFromPayloadPendingReferenceFailureAtomicallyRetriesRefund(t *testing.T) {
	t.Skip("dynamic credit refund contract was removed by fixed-SKU billing")
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "openid-" + userID}); err != nil {
		t.Fatal(err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusPending, ReferenceImageAssetID: "missing"}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Set(ctx, projectRunningCountPrefix+projectID, 1, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_task_refund BEFORE INSERT ON credit_transactions
		WHEN NEW.type = 'task_refund' BEGIN SELECT RAISE(ABORT, 'refund insert unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(repo, &mockEnqueuer{}, nil, &logger, "", NewRedisPubSub(rdb, &logger), nil)

	if err := svc.HandleExecutionFromPayload(ctx, task.ID, userID); err == nil || !strings.Contains(err.Error(), "refund insert unavailable") {
		t.Fatalf("first HandleExecutionFromPayload error = %v, want refund persistence failure", err)
	}
	assertPendingReferenceRefundState(t, db, repo, rdb, task, userID, 900, 0, 1)
	if err := db.Exec("DROP TRIGGER fail_task_refund").Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.HandleExecutionFromPayload(ctx, task.ID, userID); err != nil {
		t.Fatalf("retry HandleExecutionFromPayload: %v", err)
	}
	assertPendingReferenceRefundState(t, db, repo, rdb, task, userID, 1000, 1, 0)
}

func assertPendingReferenceRefundState(t *testing.T, db *gorm.DB, repo repository.Repository, rdb *redis.Client, task *model.Task, userID string, wantBalance int, wantRefunds int64, wantSlots int) {
	t.Helper()
	ctx := t.Context()
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus := model.TaskStatusPending
	if wantRefunds > 0 {
		wantStatus = model.TaskStatusFailed
	}
	if found.Status != wantStatus || (wantStatus == model.TaskStatusPending && found.CompletedAt != nil) || (wantStatus == model.TaskStatusFailed && found.CompletedAt == nil) {
		t.Fatalf("task state = %#v, want status %s", found, wantStatus)
	}
	_ = db
	_ = userID
	_ = wantBalance
	slots, err := rdb.Get(ctx, projectRunningCountPrefix+task.ProjectID).Int()
	if err != nil || slots != wantSlots {
		t.Fatalf("slot count = %d, err=%v, want %d", slots, err, wantSlots)
	}
}

func TestHandleExecutionFromPayloadReferenceFailureDoesNotOverwriteConcurrentCancellation(t *testing.T) {
	db := setupTaskTestDB(t)
	baseRepo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, baseRepo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusPending, ReferenceImageAssetID: "asset-1"}
	if err := baseRepo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	repo := &referenceAssetRepositoryOverride{
		Repository: baseRepo,
		assets: &cancelingReferenceAssetRepository{AssetRepository: baseRepo.Assets(), cancel: func() {
			swapped, err := baseRepo.Tasks().CompareAndSwapStatusAndError(context.Background(), task.ID, model.TaskStatusPending, model.TaskStatusCancelled, "cancelled")
			if err != nil || !swapped {
				t.Errorf("cancel task: swapped=%v err=%v", swapped, err)
				return
			}
			if err := baseRepo.Tasks().SetCompletedAt(context.Background(), task.ID); err != nil {
				t.Errorf("complete cancelled task: %v", err)
			}
		}},
	}
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)

	if err := svc.HandleExecutionFromPayload(ctx, task.ID, userID); err != nil {
		t.Fatalf("HandleExecutionFromPayload: %v", err)
	}
	found, err := baseRepo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != model.TaskStatusCancelled || found.CompletedAt == nil || found.ErrorMessage != "cancelled" {
		t.Fatalf("task after cancellation = %#v", found)
	}
}

func TestEnqueueExecutionFallbackUsesPendingReferenceFailureFinalization(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "openid-" + userID}); err != nil {
		t.Fatal(err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusPending, ReferenceImageAssetID: "missing"}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	svc := NewTaskService(repo, nil, nil, &logger, "", NewRedisPubSub(rdb, &logger), nil)

	if err := svc.EnqueueExecution(ctx, task, project); err != nil {
		t.Fatalf("EnqueueExecution: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var found *model.Task
	for time.Now().Before(deadline) {
		found, err = repo.Tasks().FindByID(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if found.Status == model.TaskStatusFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	user, userErr := repo.Users().FindByID(ctx, userID)
	count, countErr := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int()
	if found == nil || found.Status != model.TaskStatusFailed || found.CompletedAt == nil || userErr != nil || countErr != nil || count != 0 {
		t.Fatalf("fallback state: task=%#v user=%#v userErr=%v slot=%d slotErr=%v", found, user, userErr, count, countErr)
	}
}

func TestEnqueueExecutionFallbackReleasesSlotAfterPreparationPersistenceErrorForPeriodicRetry(t *testing.T) {
	db := setupTaskTestDB(t)
	baseRepo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := baseRepo.Users().Create(ctx, &model.User{ID: userID, OpenID: "openid-" + userID}); err != nil {
		t.Fatal(err)
	}
	projectID := createTestProject(t, baseRepo, userID, model.PlatformArticle)
	project, err := baseRepo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusPending, ReferenceImageAssetID: "missing"}
	if err := baseRepo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	assetContexts := make(chan context.Context, 2)
	assets := &contextCapturingAssetRepository{AssetRepository: baseRepo.Assets(), contexts: assetContexts}
	state := &failPendingOnceState{err: errors.New("database table is locked"), firstFailure: make(chan struct{})}
	repo, tasks := newFailPendingOnceRepository(baseRepo, state, assets)
	svc := NewTaskService(repo, nil, nil, &logger, "", NewRedisPubSub(rdb, &logger), nil)

	if err := svc.EnqueueExecution(ctx, task, project); err != nil {
		t.Fatalf("first EnqueueExecution: %v", err)
	}
	select {
	case <-state.firstFailure:
	case <-time.After(2 * time.Second):
		t.Fatal("fallback did not attempt pending failure persistence")
	}
	firstFallbackCtx := waitCapturedContext(t, assetContexts, "first fallback")
	waitForContextDone(t, firstFallbackCtx, "first fallback")
	found, err := baseRepo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	user, userErr := baseRepo.Users().FindByID(ctx, userID)
	slots, slotErr := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int()
	if found.Status != model.TaskStatusPending || found.CompletedAt != nil || userErr != nil || slotErr != nil || slots != 0 {
		t.Fatalf("state after preparation persistence error: task=%#v user=%#v userErr=%v slots=%d slotErr=%v", found, user, userErr, slots, slotErr)
	}

	if err := svc.DispatchPendingTasks(ctx, projectID); err != nil {
		t.Fatalf("periodic DispatchPendingTasks: %v", err)
	}
	secondFallbackCtx := waitCapturedContext(t, assetContexts, "second fallback")
	waitForContextDone(t, secondFallbackCtx, "second fallback")
	found, err = baseRepo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	user, userErr = baseRepo.Users().FindByID(ctx, userID)
	slots, slotErr = rdb.Get(ctx, projectRunningCountPrefix+projectID).Int()
	if found.Status != model.TaskStatusFailed || found.CompletedAt == nil || userErr != nil || slotErr != nil || slots != 0 || tasks.callCount() != 2 {
		t.Fatalf("state after periodic retry: task=%#v user=%#v userErr=%v slots=%d slotErr=%v calls=%d", found, user, userErr, slots, slotErr, tasks.callCount())
	}
}

func waitForContextDone(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("%s goroutine did not exit", name)
	}
}

func waitCapturedContext(t *testing.T, contexts <-chan context.Context, name string) context.Context {
	t.Helper()
	select {
	case ctx := <-contexts:
		return ctx
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not begin asset resolution", name)
		return nil
	}
}

func TestEnqueueExecutionFallbackReleasesOnlyOwnedSlotWhenPreparationCASLoses(t *testing.T) {
	db := setupTaskTestDB(t)
	baseRepo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, baseRepo, userID, model.PlatformArticle)
	project, err := baseRepo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusPending, ReferenceImageAssetID: "asset-1"}
	if err := baseRepo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	assetContexts := make(chan context.Context, 1)
	assets := &contextCapturingAssetRepository{AssetRepository: baseRepo.Assets(), contexts: assetContexts}
	repo := &executionPreparationRepository{Repository: baseRepo, tasks: &losingFailPendingTaskRepository{TaskRepository: baseRepo.Tasks()}, assets: assets}
	repo.wrapTx = func(txRepo repository.Repository) repository.Repository {
		return &executionPreparationRepository{Repository: txRepo, tasks: &losingFailPendingTaskRepository{TaskRepository: txRepo.Tasks()}, assets: txRepo.Assets()}
	}
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Set(ctx, projectRunningCountPrefix+projectID, 1, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, nil, nil, &logger, "", NewRedisPubSub(rdb, &logger), nil)

	if err := svc.EnqueueExecution(ctx, task, project); err != nil {
		t.Fatalf("EnqueueExecution: %v", err)
	}
	fallbackCtx := waitCapturedContext(t, assetContexts, "CAS loser fallback")
	waitForContextDone(t, fallbackCtx, "CAS loser fallback")
	found, err := baseRepo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	count, countErr := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int()
	if found.Status != model.TaskStatusPending || found.CompletedAt != nil || countErr != nil || count != 1 {
		t.Fatalf("preparation CAS loser state: task=%#v count=%d countErr=%v", found, count, countErr)
	}
}

func TestEnqueueExecutionFallbackDoesNotReleaseReplacementSlotAfterCancelWins(t *testing.T) {
	db := setupTaskTestDB(t)
	baseRepo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, baseRepo, userID, model.PlatformArticle)
	project, err := baseRepo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusPending}
	if err := baseRepo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFallbacks := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseFallbacks)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	logger := zerolog.New(io.Discard)
	dispatcher := &dispatchTestDispatcher{started: make(chan struct{}, 1), release: release}
	svc := NewTaskService(baseRepo, nil, nil, &logger, "", NewRedisPubSub(rdb, &logger), nil)
	svc.SetRuntimeDispatcher(dispatcher)

	if err := svc.EnqueueExecution(ctx, task, project); err != nil {
		t.Fatalf("EnqueueExecution A: %v", err)
	}
	select {
	case <-dispatcher.started:
	case <-time.After(2 * time.Second):
		t.Fatal("fallback A did not reach runtime dispatch")
	}
	if err := svc.EnqueueExecution(ctx, task, project); err != nil {
		t.Fatalf("EnqueueExecution duplicate: %v", err)
	}
	count, err := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int()
	if err != nil || count != 1 {
		t.Fatalf("slot count after duplicate fallback = %d, err=%v, want only A slot", count, err)
	}
	if err := svc.Cancel(ctx, task.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, ok, err := svc.pubsub.TryReserveSlot(ctx, projectID, 10); err != nil || !ok {
		t.Fatalf("reserve replacement slot B: ok=%v err=%v", ok, err)
	}
	releaseFallbacks()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		exists, existsErr := rdb.Exists(ctx, fallbackDispatchClaimPrefix+task.ID).Result()
		if existsErr != nil {
			t.Fatal(existsErr)
		}
		if exists == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if exists, existsErr := rdb.Exists(ctx, fallbackDispatchClaimPrefix+task.ID).Result(); existsErr != nil || exists != 0 {
		t.Fatalf("fallback A claim was not released: exists=%d err=%v", exists, existsErr)
	}
	count, err = rdb.Get(ctx, projectRunningCountPrefix+projectID).Int()
	if err != nil || count != 1 {
		t.Fatalf("slot count after A exits = %d, err=%v, want replacement B slot", count, err)
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
