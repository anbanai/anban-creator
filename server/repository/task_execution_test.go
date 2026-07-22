package repository

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type taskExecutionTestRepository struct {
	Repository
	db *gorm.DB
}

func setupTaskExecutionRepository(t *testing.T) taskExecutionTestRepository {
	t.Helper()

	db := setupTestDB(t)
	return taskExecutionTestRepository{Repository: New(db), db: db}
}

func seedTaskExecution(t *testing.T, repo taskExecutionTestRepository, status string) *model.TaskExecution {
	t.Helper()

	execution := &model.TaskExecution{
		ID:      uuid.NewString(),
		TaskID:  uuid.NewString(),
		Attempt: 1,
		Target:  "kubernetes",
		Status:  status,
	}
	if err := repo.TaskExecutions().Create(context.Background(), execution); err != nil {
		t.Fatalf("create task execution: %v", err)
	}
	return execution
}

func TestTaskExecutionRepositoryTransitionIsCAS(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	execution := seedTaskExecution(t, repo, model.TaskExecutionStarting)

	won, err := repo.TaskExecutions().Transition(context.Background(), execution.ID,
		[]string{model.TaskExecutionStarting}, model.TaskExecutionRunning,
		model.ExecutionTransition{Started: true, RuntimeInstanceID: "pod-1"})
	if err != nil || !won {
		t.Fatalf("first transition = %v, %v", won, err)
	}

	won, err = repo.TaskExecutions().Transition(context.Background(), execution.ID,
		[]string{model.TaskExecutionStarting}, model.TaskExecutionFailed,
		model.ExecutionTransition{TerminalReason: "late"})
	if err != nil || won {
		t.Fatalf("stale transition = %v, %v", won, err)
	}

	found, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatalf("find transitioned execution: %v", err)
	}
	if found.Status != model.TaskExecutionRunning || !found.Started || found.StartedAt == nil || found.RuntimeInstanceID != "pod-1" {
		t.Fatalf("transitioned execution = %+v", found)
	}
	if found.CompletedAt != nil || found.TerminalReason != "" {
		t.Fatalf("stale transition changed terminal fields: %+v", found)
	}
}

func TestTaskExecutionRepositoryTerminalTransitionIsAtomic(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	execution := seedTaskExecution(t, repo, model.TaskExecutionRunning)
	diagnostics := datatypes.JSON(`{"exit_code":137}`)

	won, err := repo.TaskExecutions().Transition(context.Background(), execution.ID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionFailed,
		model.ExecutionTransition{
			ManifestStatus: "rejected",
			TerminalReason: "oom_killed",
			Diagnostics:    diagnostics,
		})
	if err != nil || !won {
		t.Fatalf("terminal transition = %v, %v", won, err)
	}

	found, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatalf("find terminal execution: %v", err)
	}
	if found.Status != model.TaskExecutionFailed || found.CompletedAt == nil {
		t.Fatalf("terminal status = %q, completed_at = %v", found.Status, found.CompletedAt)
	}
	if found.ManifestStatus != "rejected" || found.TerminalReason != "oom_killed" || string(found.Diagnostics) != string(diagnostics) {
		t.Fatalf("terminal transition fields = %+v", found)
	}
}

func TestTaskExecutionRepositoryFindsCurrentAttempt(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	taskID := uuid.NewString()
	currentID := uuid.NewString()
	task := &model.Task{
		ID:                 taskID,
		UserID:             uuid.NewString(),
		Type:               "article",
		Prompt:             "topic",
		CurrentExecutionID: &currentID,
	}
	if err := repo.db.WithContext(ctx).Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	for attempt, id := range []string{uuid.NewString(), currentID} {
		execution := &model.TaskExecution{
			ID:      id,
			TaskID:  taskID,
			Attempt: attempt + 1,
			Target:  "kubernetes",
			Status:  model.TaskExecutionStarting,
		}
		if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
			t.Fatalf("create attempt %d: %v", attempt+1, err)
		}
	}

	found, err := repo.TaskExecutions().FindCurrentByTaskID(ctx, taskID)
	if err != nil {
		t.Fatalf("find current execution: %v", err)
	}
	if found.ID != currentID || found.Attempt != 2 {
		t.Fatalf("current execution = %+v", found)
	}
}

func TestTaskExecutionRepositoryRuntimeAndReconciliation(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	refreshed := seedTaskExecution(t, repo, model.TaskExecutionStarting)
	stale := seedTaskExecution(t, repo, model.TaskExecutionDispatching)
	terminal := seedTaskExecution(t, repo, model.TaskExecutionSucceeded)
	cutoff := time.Now().UTC().Add(-time.Minute)
	old := cutoff.Add(-time.Minute)
	if err := repo.db.Model(&model.TaskExecution{}).Where("id IN ?", []string{stale.ID, terminal.ID}).Update("updated_at", old).Error; err != nil {
		t.Fatalf("age executions: %v", err)
	}

	if err := repo.TaskExecutions().SetRuntimeIdentity(ctx, refreshed.ID, model.RuntimeIdentity{Scope: "agent-system", Workload: "agent-job-1", InstanceID: "pod-1"}); err != nil {
		t.Fatalf("set runtime identity: %v", err)
	}
	heartbeat := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.TaskExecutions().UpdateHeartbeat(ctx, refreshed.ID, heartbeat); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}
	found, err := repo.TaskExecutions().FindByID(ctx, refreshed.ID)
	if err != nil {
		t.Fatalf("find execution: %v", err)
	}
	if found.RuntimeScope != "agent-system" || found.RuntimeWorkload != "agent-job-1" || found.RuntimeInstanceID != "pod-1" {
		t.Fatalf("runtime identity = %+v", found)
	}
	if found.LastHeartbeatAt == nil || !found.LastHeartbeatAt.Equal(heartbeat) {
		t.Fatalf("last heartbeat = %v, want %v", found.LastHeartbeatAt, heartbeat)
	}

	// The stale active attempt is selected; refreshed and terminal attempts are not.
	reconcilable, err := repo.TaskExecutions().FindReconcilable(ctx, cutoff, 10)
	if err != nil {
		t.Fatalf("find reconcilable: %v", err)
	}
	if len(reconcilable) != 1 || reconcilable[0].ID != stale.ID {
		t.Fatalf("reconcilable executions = %+v, want only %s", reconcilable, stale.ID)
	}
}

func TestTaskExecutionRepositoryRuntimeIdentityIsFillOnlyAndIdempotent(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	execution := seedTaskExecution(t, repo, model.TaskExecutionStarting)
	identity := model.RuntimeIdentity{Scope: "agent-system", Workload: "agent-job-1", InstanceID: "pod-1"}

	if err := repo.TaskExecutions().SetRuntimeIdentity(ctx, execution.ID, identity); err != nil {
		t.Fatalf("set full runtime identity: %v", err)
	}
	if err := repo.TaskExecutions().SetRuntimeIdentity(ctx, execution.ID, identity); err != nil {
		t.Fatalf("replay runtime identity: %v", err)
	}
	for name, drift := range map[string]model.RuntimeIdentity{
		"scope":    {Scope: "other-system"},
		"workload": {Workload: "agent-job-2"},
		"instance": {InstanceID: "pod-2"},
	} {
		t.Run(name+" drift", func(t *testing.T) {
			if err := repo.TaskExecutions().SetRuntimeIdentity(ctx, execution.ID, drift); !errors.Is(err, ErrRuntimeIdentityConflict) {
				t.Fatalf("SetRuntimeIdentity error = %v, want ErrRuntimeIdentityConflict", err)
			}
		})
	}

	found, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatalf("find execution: %v", err)
	}
	if found.RuntimeScope != identity.Scope || found.RuntimeWorkload != identity.Workload || found.RuntimeInstanceID != identity.InstanceID {
		t.Fatalf("runtime identity = (%q, %q, %q), want (%q, %q, %q)",
			found.RuntimeScope, found.RuntimeWorkload, found.RuntimeInstanceID,
			identity.Scope, identity.Workload, identity.InstanceID)
	}
}

func TestTaskExecutionRepositoryRuntimeIdentityRejectsInactiveAndMissingExecutions(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	terminal := seedTaskExecution(t, repo, model.TaskExecutionSucceeded)

	if err := repo.TaskExecutions().SetRuntimeIdentity(ctx, terminal.ID, model.RuntimeIdentity{InstanceID: "pod-1"}); !errors.Is(err, ErrRuntimeIdentityInactive) {
		t.Fatalf("terminal identity error = %v, want ErrRuntimeIdentityInactive", err)
	}
	if err := repo.TaskExecutions().SetRuntimeIdentity(ctx, "missing", model.RuntimeIdentity{InstanceID: "pod-1"}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing identity error = %v, want gorm.ErrRecordNotFound", err)
	}
	found, err := repo.TaskExecutions().FindByID(ctx, terminal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.RuntimeInstanceID != "" {
		t.Fatalf("terminal runtime instance = %q, want empty", found.RuntimeInstanceID)
	}
}

func TestTaskExecutionRepositoryConcurrentRuntimeInstanceBindingHasOneWinner(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	sqlDB, err := repo.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	execution := seedTaskExecution(t, repo, model.TaskExecutionStarting)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, instanceID := range []string{"pod-1", "pod-2"} {
		wg.Add(1)
		go func(instanceID string) {
			defer wg.Done()
			<-start
			errs <- repo.TaskExecutions().SetRuntimeIdentity(context.Background(), execution.ID, model.RuntimeIdentity{InstanceID: instanceID})
		}(instanceID)
	}
	close(start)
	wg.Wait()
	close(errs)

	succeeded, conflicted := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrRuntimeIdentityConflict):
			conflicted++
		default:
			t.Fatalf("concurrent binding error = %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent bindings succeeded=%d conflicted=%d, want 1/1", succeeded, conflicted)
	}
}

func TestTaskExecutionFinalizationLeaseRenewalAndTakeover(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	execution := seedTaskExecution(t, repo, model.TaskExecutionFailed)
	if err := repo.db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).
		Updates(map[string]any{"finalization_status": model.TaskExecutionFinalizationArtifacts, "cleanup_status": model.TaskExecutionCleanupPending}).Error; err != nil {
		t.Fatal(err)
	}
	first := uuid.NewString()
	won, err := repo.TaskExecutions().ClaimFinalization(ctx, execution.ID, first, 30*time.Millisecond)
	if err != nil || !won {
		t.Fatalf("first claim won=%v err=%v", won, err)
	}
	time.Sleep(20 * time.Millisecond)
	renewed, err := repo.TaskExecutions().RenewFinalizationClaim(ctx, execution.ID, first)
	if err != nil || !renewed {
		t.Fatalf("renewed=%v err=%v", renewed, err)
	}
	time.Sleep(20 * time.Millisecond)
	won, err = repo.TaskExecutions().ClaimFinalization(ctx, execution.ID, uuid.NewString(), 30*time.Millisecond)
	if err != nil || won {
		t.Fatalf("renewed claim stolen won=%v err=%v", won, err)
	}
	if err := repo.db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).Update("finalization_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	second := uuid.NewString()
	won, err = repo.TaskExecutions().ClaimFinalization(ctx, execution.ID, second, 30*time.Millisecond)
	if err != nil || !won {
		t.Fatalf("abandoned lease takeover won=%v err=%v", won, err)
	}
	renewed, err = repo.TaskExecutions().RenewFinalizationClaim(ctx, execution.ID, first)
	if err != nil || renewed {
		t.Fatalf("stale owner renewed=%v err=%v", renewed, err)
	}
}

func TestTaskExecutionCleanupClaimIsDurableAndExclusive(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	execution := seedTaskExecution(t, repo, model.TaskExecutionFailed)
	if err := repo.db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).
		Updates(map[string]any{"finalization_status": model.TaskExecutionFinalizationDone, "cleanup_status": model.TaskExecutionCleanupPending}).Error; err != nil {
		t.Fatal(err)
	}
	first, second := uuid.NewString(), uuid.NewString()
	won, err := repo.TaskExecutions().ClaimCleanup(ctx, execution.ID, first, time.Minute)
	if err != nil || !won {
		t.Fatalf("first cleanup claim won=%v err=%v", won, err)
	}
	won, err = repo.TaskExecutions().ClaimCleanup(ctx, execution.ID, second, time.Minute)
	if err != nil || won {
		t.Fatalf("second cleanup claim won=%v err=%v", won, err)
	}
	completed, err := repo.TaskExecutions().CompleteCleanup(ctx, execution.ID, first)
	if err != nil || !completed {
		t.Fatalf("complete cleanup=%v err=%v", completed, err)
	}
	found, _ := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if found.CleanupStatus != model.TaskExecutionCleanupDone {
		t.Fatalf("cleanup status=%s", found.CleanupStatus)
	}
}

func TestTaskExecutionCleanupFailureBacksOff(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	execution := seedTaskExecution(t, repo, model.TaskExecutionFailed)
	if err := repo.db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).Update("cleanup_status", model.TaskExecutionCleanupPending).Error; err != nil {
		t.Fatal(err)
	}
	token := uuid.NewString()
	won, err := repo.TaskExecutions().ClaimCleanup(ctx, execution.ID, token, time.Minute)
	if err != nil || !won {
		t.Fatalf("claim won=%v err=%v", won, err)
	}
	failed, err := repo.TaskExecutions().FailCleanup(ctx, execution.ID, token, 30*time.Millisecond)
	if err != nil || !failed {
		t.Fatalf("fail cleanup=%v err=%v", failed, err)
	}
	won, err = repo.TaskExecutions().ClaimCleanup(ctx, execution.ID, uuid.NewString(), time.Minute)
	if err != nil || won {
		t.Fatalf("cleanup ignored backoff won=%v err=%v", won, err)
	}
	time.Sleep(35 * time.Millisecond)
	won, err = repo.TaskExecutions().ClaimCleanup(ctx, execution.ID, uuid.NewString(), time.Minute)
	if err != nil || !won {
		t.Fatalf("cleanup not retryable after backoff won=%v err=%v", won, err)
	}
}

func TestTaskExecutionRepositoryRejectsDuplicateAttempt(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	taskID := uuid.NewString()
	first := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: taskID, Attempt: 1,
		Target: "kubernetes", Status: model.TaskExecutionCreated,
	}
	if err := repo.TaskExecutions().Create(ctx, first); err != nil {
		t.Fatalf("create first attempt: %v", err)
	}
	duplicate := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: taskID, Attempt: 1,
		Target: "kubernetes", Status: model.TaskExecutionCreated,
	}
	if err := repo.TaskExecutions().Create(ctx, duplicate); err == nil {
		t.Fatal("duplicate (task_id, attempt) was accepted")
	}
}

func TestTaskExecutionDispatchClaimLeaseIsTokenGuarded(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	execution := seedTaskExecution(t, repo, model.TaskExecutionCreated)

	won, err := repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, "owner-1", time.Minute)
	if err != nil || !won {
		t.Fatalf("initial claim = %v, %v", won, err)
	}
	won, err = repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, "owner-2", time.Minute)
	if err != nil || won {
		t.Fatalf("active lease claim = %v, %v", won, err)
	}
	if err := repo.db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).
		Update("dispatch_claimed_at", gorm.Expr("DATETIME('now', '-2 minutes')")).Error; err != nil {
		t.Fatalf("age dispatch claim: %v", err)
	}
	won, err = repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, "owner-2", time.Minute)
	if err != nil || !won {
		t.Fatalf("stale lease reclaim = %v, %v", won, err)
	}
	won, err = repo.TaskExecutions().AbandonDispatch(ctx, execution.ID, "owner-1")
	if err != nil || won {
		t.Fatalf("stale owner abandonment = %v, %v", won, err)
	}
	won, err = repo.TaskExecutions().CompleteDispatch(ctx, execution.ID, "owner-1", model.RuntimeIdentity{Scope: "stale-namespace", Workload: "stale-job"})
	if err != nil || won {
		t.Fatalf("stale owner completion = %v, %v", won, err)
	}
	identity := model.RuntimeIdentity{Scope: "docker", Workload: "creator-agent-exec-1"}
	won, err = repo.TaskExecutions().CompleteDispatch(ctx, execution.ID, "owner-2", identity)
	if err != nil || !won {
		t.Fatalf("current owner completion = %v, %v", won, err)
	}
	found, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatalf("find completed dispatch: %v", err)
	}
	if found.Status != model.TaskExecutionStarting || found.DispatchClaimToken != "" || found.DispatchClaimedAt != nil || found.RuntimeScope != identity.Scope || found.RuntimeWorkload != identity.Workload {
		t.Fatalf("completed dispatch = %+v", found)
	}
}

func TestTaskExecutionCompleteDispatchPreservesInstanceAndRejectsIdentityDrift(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	execution := seedTaskExecution(t, repo, model.TaskExecutionCreated)
	if err := repo.TaskExecutions().SetRuntimeIdentity(ctx, execution.ID, model.RuntimeIdentity{InstanceID: "container-1"}); err != nil {
		t.Fatalf("record runtime instance: %v", err)
	}
	if won, err := repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, "owner-1", time.Minute); err != nil || !won {
		t.Fatalf("claim dispatch = %v, %v", won, err)
	}
	if won, err := repo.TaskExecutions().CompleteDispatch(ctx, execution.ID, "stale-owner", model.RuntimeIdentity{Scope: "docker", Workload: "workload-1"}); err != nil || won {
		t.Fatalf("stale completion = %v, %v", won, err)
	}
	if won, err := repo.TaskExecutions().CompleteDispatch(ctx, execution.ID, "owner-1", model.RuntimeIdentity{Scope: "docker", Workload: "workload-1"}); err != nil || !won {
		t.Fatalf("complete partial identity = %v, %v", won, err)
	}
	found, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.RuntimeScope != "docker" || found.RuntimeWorkload != "workload-1" || found.RuntimeInstanceID != "container-1" {
		t.Fatalf("completed identity = %+v", found)
	}

	conflicted := seedTaskExecution(t, repo, model.TaskExecutionCreated)
	if err := repo.TaskExecutions().SetRuntimeIdentity(ctx, conflicted.ID, model.RuntimeIdentity{Workload: "workload-1"}); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.TaskExecutions().ClaimDispatch(ctx, conflicted.ID, "owner-2", time.Minute); err != nil || !won {
		t.Fatalf("claim conflicting dispatch = %v, %v", won, err)
	}
	won, err := repo.TaskExecutions().CompleteDispatch(ctx, conflicted.ID, "owner-2", model.RuntimeIdentity{Scope: "docker", Workload: "workload-2"})
	if won || !errors.Is(err, ErrRuntimeIdentityConflict) {
		t.Fatalf("conflicting completion = %v, %v, want false/ErrRuntimeIdentityConflict", won, err)
	}
	found, err = repo.TaskExecutions().FindByID(ctx, conflicted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != model.TaskExecutionDispatching || found.DispatchClaimToken != "owner-2" || found.RuntimeScope != "" || found.RuntimeWorkload != "workload-1" {
		t.Fatalf("conflicting completion mutated execution = %+v", found)
	}
}

func TestTaskExecutionDispatchClaimUsesDatabaseTime(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	execution := seedTaskExecution(t, repo, model.TaskExecutionCreated)
	won, err := repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, "owner-1", time.Minute)
	if err != nil || !won {
		t.Fatalf("initial claim = %v, %v", won, err)
	}
	// There is deliberately no caller timestamp: claim freshness is evaluated
	// against the database clock sampled inside ClaimDispatch.
	won, err = repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, "second-owner", time.Minute)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if won {
		t.Fatal("active database lease was stolen")
	}
}

func TestDispatchClaimSQLUsesOneDatabaseTimedUpdate(t *testing.T) {
	tests := []struct {
		name       string
		db         *gorm.DB
		assignment string
		cutoff     string
	}{
		{
			name:       "sqlite",
			db:         setupTestDB(t).Session(&gorm.Session{DryRun: true}),
			assignment: "STRFTIME",
			cutoff:     "JULIANDAY",
		},
	}
	mysqlDB, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "root@tcp(localhost:3306)/test?parseTime=true",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open dry-run mysql: %v", err)
	}
	tests = append(tests, struct {
		name       string
		db         *gorm.DB
		assignment string
		cutoff     string
	}{name: "mysql", db: mysqlDB, assignment: "CURRENT_TIMESTAMP(6)", cutoff: "DATE_SUB"})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statement, err := buildDispatchClaimUpdate(tt.db, "execution-1", "token-1", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if statement.Error != nil {
				t.Fatalf("build claim statement: %v", statement.Error)
			}
			sql := strings.ToUpper(statement.Statement.SQL.String())
			if !strings.HasPrefix(sql, "UPDATE") || !strings.Contains(sql, tt.assignment) || !strings.Contains(sql, tt.cutoff) {
				t.Fatalf("claim SQL = %s", sql)
			}
			for _, value := range statement.Statement.Vars {
				if _, ok := value.(time.Time); ok {
					t.Fatalf("claim SQL includes application timestamp arg: %#v", statement.Statement.Vars)
				}
			}
		})
	}
}

func TestTaskExecutionRepositoryIsAvailableInTransactions(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	executionID := uuid.NewString()

	err := repo.WithTx(ctx, func(txRepo Repository) error {
		if txRepo.TaskExecutions() == nil {
			t.Fatal("TaskExecutions() should not be nil")
		}
		return txRepo.TaskExecutions().Create(ctx, &model.TaskExecution{
			ID: executionID, TaskID: uuid.NewString(), Attempt: 1,
			Target: "kubernetes", Status: model.TaskExecutionCreated,
		})
	})
	if err != nil {
		t.Fatalf("create in transaction: %v", err)
	}
	if _, err := repo.TaskExecutions().FindByID(ctx, executionID); err != nil {
		t.Fatalf("find committed execution: %v", err)
	}

	_, err = repo.TaskExecutions().FindByID(ctx, "missing")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing execution error = %v, want %v", err, gorm.ErrRecordNotFound)
	}
}
