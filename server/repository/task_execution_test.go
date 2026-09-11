package repository

import (
	"context"
	"errors"
	"fmt"
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
	localActive := seedTaskExecution(t, repo, model.TaskExecutionRunning)
	localTerminal := seedTaskExecution(t, repo, model.TaskExecutionFailed)
	cutoff := time.Now().UTC().Add(-time.Minute)
	old := cutoff.Add(-time.Minute)
	if err := repo.db.Model(&model.TaskExecution{}).Where("id IN ?", []string{localActive.ID, localTerminal.ID}).Update("target", model.ExecutionTargetLocalClaimed).Error; err != nil {
		t.Fatalf("mark local executions: %v", err)
	}
	if err := repo.db.Model(&model.TaskExecution{}).Where("id = ?", localTerminal.ID).Updates(map[string]any{
		"finalization_status": model.TaskExecutionFinalizationTerminal,
		"cleanup_status":      model.TaskExecutionCleanupDone,
	}).Error; err != nil {
		t.Fatalf("mark terminal local finalization: %v", err)
	}
	if err := repo.db.Model(&model.TaskExecution{}).Where("id IN ?", []string{stale.ID, terminal.ID, localActive.ID, localTerminal.ID}).Update("updated_at", old).Error; err != nil {
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

	// Only cloud runtime attempts are selected. Local attempts have no Docker or
	// Kubernetes workload and are owned by the local execution reaper.
	reconcilable, err := repo.TaskExecutions().FindReconcilable(ctx, cutoff, 10)
	if err != nil {
		t.Fatalf("find reconcilable: %v", err)
	}
	if len(reconcilable) != 1 || reconcilable[0].ID != stale.ID {
		t.Fatalf("reconcilable executions = %+v, want only cloud active %s", reconcilable, stale.ID)
	}
}

func TestTaskExecutionRepositoryFindsLocalReconcileCandidates(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	stale := now.Add(-10 * time.Minute)
	fresh := now.Add(-time.Minute)
	deadlineStale := now.Add(-2 * time.Hour)

	newLocal := func(startedAt, heartbeatAt time.Time) *model.TaskExecution {
		execution := &model.TaskExecution{
			ID: uuid.NewString(), TaskID: uuid.NewString(), Attempt: 1,
			Target: model.ExecutionTargetLocalClaimed, Status: model.TaskExecutionRunning,
			Started: true, StartedAt: &startedAt, LastHeartbeatAt: &heartbeatAt, CreatedAt: startedAt,
		}
		if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
			t.Fatalf("create local execution: %v", err)
		}
		return execution
	}
	staleHeartbeat := newLocal(stale, stale)
	staleDeadline := newLocal(deadlineStale, fresh)
	_ = newLocal(stale, fresh)
	incompleteFinalization := newLocal(fresh, fresh)
	if err := repo.db.Model(&model.TaskExecution{}).Where("id = ?", incompleteFinalization.ID).Updates(map[string]any{
		"status": model.TaskExecutionFailed, "finalization_status": model.TaskExecutionFinalizationTerminal,
	}).Error; err != nil {
		t.Fatal(err)
	}
	cloud := seedTaskExecution(t, repo, model.TaskExecutionRunning)
	if err := repo.db.Model(&model.TaskExecution{}).Where("id = ?", cloud.ID).Updates(map[string]any{
		"started": true, "started_at": stale, "last_heartbeat_at": stale, "created_at": stale,
	}).Error; err != nil {
		t.Fatal(err)
	}

	found, err := repo.TaskExecutions().FindLocalReconcileCandidates(ctx, now.Add(-5*time.Minute), now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("FindLocalReconcileCandidates: %v", err)
	}
	got := make(map[string]bool, len(found))
	for _, execution := range found {
		got[execution.ID] = true
	}
	if len(got) != 3 || !got[staleHeartbeat.ID] || !got[staleDeadline.ID] || !got[incompleteFinalization.ID] {
		t.Fatalf("local reconcile candidates = %#v, want heartbeat %s, deadline %s, and finalization %s", got, staleHeartbeat.ID, staleDeadline.ID, incompleteFinalization.ID)
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

func TestTaskExecutionDispatchBarrierUsesDatabaseClockAndExactToken(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	execution := seedTaskExecution(t, repo, model.TaskExecutionDispatching)
	token := uuid.NewString()
	won, err := repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, token, time.Minute)
	if err != nil || !won {
		t.Fatalf("claim dispatch won=%v err=%v", won, err)
	}
	active, err := repo.TaskExecutions().DispatchClaimActive(ctx, execution.ID, time.Minute)
	if err != nil || !active {
		t.Fatalf("fresh dispatch barrier active=%v err=%v", active, err)
	}
	if err := repo.db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).
		Update("dispatch_claimed_at", gorm.Expr("DATETIME('now', '-2 minutes')")).Error; err != nil {
		t.Fatal(err)
	}
	active, err = repo.TaskExecutions().DispatchClaimActive(ctx, execution.ID, time.Minute)
	if err != nil || active {
		t.Fatalf("expired dispatch barrier active=%v err=%v", active, err)
	}
	refreshed, err := repo.TaskExecutions().RefreshDispatchClaim(ctx, execution.ID, uuid.NewString())
	if err != nil || refreshed {
		t.Fatalf("stale token refresh=%v err=%v", refreshed, err)
	}
	refreshed, err = repo.TaskExecutions().RefreshDispatchClaim(ctx, execution.ID, token)
	if err != nil || !refreshed {
		t.Fatalf("owner refresh=%v err=%v", refreshed, err)
	}
	active, err = repo.TaskExecutions().DispatchClaimActive(ctx, execution.ID, time.Minute)
	if err != nil || !active {
		t.Fatalf("refreshed dispatch barrier active=%v err=%v", active, err)
	}

	won, err = repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionDispatching}, model.TaskExecutionCancelled,
		model.ExecutionTransition{TerminalReason: "user_cancelled"})
	if err != nil || !won {
		t.Fatalf("terminalize dispatch won=%v err=%v", won, err)
	}
	refreshed, err = repo.TaskExecutions().RefreshDispatchClaim(ctx, execution.ID, token)
	if err != nil || !refreshed {
		t.Fatalf("terminal barrier refresh=%v err=%v", refreshed, err)
	}
}

func TestTaskExecutionRefreshDispatchClaimReloadsExactTokenWhenUpdateReportsZeroRows(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	execution := seedTaskExecution(t, repo, model.TaskExecutionDispatching)
	token := uuid.NewString()
	won, err := repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, token, time.Minute)
	if err != nil || !won {
		t.Fatalf("claim dispatch won=%v err=%v", won, err)
	}
	trigger := fmt.Sprintf(`CREATE TRIGGER ignore_dispatch_refresh
		BEFORE UPDATE OF dispatch_claimed_at ON task_executions
		WHEN OLD.id = '%s'
		BEGIN
			SELECT RAISE(IGNORE);
		END`, execution.ID)
	if err := repo.db.Exec(trigger).Error; err != nil {
		t.Fatalf("create zero-row refresh trigger: %v", err)
	}

	refreshed, err := repo.TaskExecutions().RefreshDispatchClaim(ctx, execution.ID, token)
	if err != nil || !refreshed {
		t.Fatalf("exact-token zero-row refresh=%v err=%v, want ownership confirmed", refreshed, err)
	}
}

func TestTaskExecutionRefreshDispatchClaimRejectsEmptyTokenWithoutMutation(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	execution := seedTaskExecution(t, repo, model.TaskExecutionDispatching)

	refreshed, err := repo.TaskExecutions().RefreshDispatchClaim(ctx, execution.ID, "")
	if err == nil || refreshed {
		t.Fatalf("empty-token refresh=%v err=%v, want rejection", refreshed, err)
	}
	found, findErr := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if found.DispatchClaimToken != "" || found.DispatchClaimedAt != nil {
		t.Fatalf("empty-token refresh mutated claim: token=%q claimed_at=%v", found.DispatchClaimToken, found.DispatchClaimedAt)
	}
}

func TestTaskExecutionLeaseTokenAuthorityRejectsBlankTokensWithoutMutation(t *testing.T) {
	tokens := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "whitespace", value: " \t "},
	}
	dispatchMutations := []struct {
		name string
		call func(TaskExecutionRepository, string, string) error
	}{
		{name: "claim", call: func(repo TaskExecutionRepository, id, token string) error {
			_, err := repo.ClaimDispatch(context.Background(), id, token, time.Minute)
			return err
		}},
		{name: "refresh", call: func(repo TaskExecutionRepository, id, token string) error {
			_, err := repo.RefreshDispatchClaim(context.Background(), id, token)
			return err
		}},
		{name: "abandon", call: func(repo TaskExecutionRepository, id, token string) error {
			_, err := repo.AbandonDispatch(context.Background(), id, token)
			return err
		}},
		{name: "complete", call: func(repo TaskExecutionRepository, id, token string) error {
			_, err := repo.CompleteDispatch(context.Background(), id, token, model.RuntimeIdentity{Scope: "docker", Workload: "container-1", InstanceID: "instance-1"})
			return err
		}},
		{name: "fail", call: func(repo TaskExecutionRepository, id, token string) error {
			_, err := repo.FailDispatch(context.Background(), id, token, "dispatch_failed", []byte(`{"error":"failed"}`), []byte(`{"success":false}`))
			return err
		}},
	}
	for _, mutation := range dispatchMutations {
		for _, token := range tokens {
			t.Run("dispatch/"+mutation.name+"/"+token.name, func(t *testing.T) {
				repo := setupTaskExecutionRepository(t)
				execution := seedTaskExecution(t, repo, model.TaskExecutionDispatching)
				if err := mutation.call(repo.TaskExecutions(), execution.ID, token.value); err == nil {
					t.Fatal("blank dispatch lease token was accepted")
				}
				found, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
				if err != nil {
					t.Fatal(err)
				}
				if found.Status != model.TaskExecutionDispatching || found.DispatchClaimToken != "" || found.DispatchClaimedAt != nil ||
					found.RuntimeScope != "" || found.RuntimeWorkload != "" || found.RuntimeInstanceID != "" ||
					found.TerminalReason != "" || found.CleanupStatus != "" || found.CompletedAt != nil {
					t.Fatalf("blank dispatch token mutated execution: %+v", found)
				}
			})
		}
	}

	cleanupMutations := []struct {
		name string
		call func(TaskExecutionRepository, string, string) error
	}{
		{name: "claim", call: func(repo TaskExecutionRepository, id, token string) error {
			_, err := repo.ClaimCleanup(context.Background(), id, token, time.Minute)
			return err
		}},
		{name: "set identity", call: func(repo TaskExecutionRepository, id, token string) error {
			_, err := repo.SetCleanupRuntimeIdentity(context.Background(), id, token, model.RuntimeIdentity{Scope: "docker", Workload: "container-1", InstanceID: "instance-1"})
			return err
		}},
		{name: "complete", call: func(repo TaskExecutionRepository, id, token string) error {
			_, err := repo.CompleteCleanup(context.Background(), id, token)
			return err
		}},
		{name: "fail", call: func(repo TaskExecutionRepository, id, token string) error {
			_, err := repo.FailCleanup(context.Background(), id, token, time.Minute)
			return err
		}},
		{name: "release", call: func(repo TaskExecutionRepository, id, token string) error {
			return repo.ReleaseCleanup(context.Background(), id, token)
		}},
	}
	for _, mutation := range cleanupMutations {
		for _, token := range tokens {
			t.Run("cleanup/"+mutation.name+"/"+token.name, func(t *testing.T) {
				repo := setupTaskExecutionRepository(t)
				execution := seedTaskExecution(t, repo, model.TaskExecutionFailed)
				if err := repo.db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).Updates(map[string]any{
					"cleanup_status":       model.TaskExecutionCleanupPending,
					"dispatch_claim_token": "retained-dispatch-barrier",
					"dispatch_claimed_at":  gorm.Expr("CURRENT_TIMESTAMP"),
				}).Error; err != nil {
					t.Fatal(err)
				}
				if err := mutation.call(repo.TaskExecutions(), execution.ID, token.value); err == nil {
					t.Fatal("blank cleanup lease token was accepted")
				}
				found, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
				if err != nil {
					t.Fatal(err)
				}
				if found.Status != model.TaskExecutionFailed || found.CleanupStatus != model.TaskExecutionCleanupPending ||
					found.CleanupToken != "" || found.CleanupAt != nil || found.CleanupNextAt != nil || found.CleanupAttempts != 0 ||
					found.RuntimeScope != "" || found.RuntimeWorkload != "" || found.RuntimeInstanceID != "" ||
					found.DispatchClaimToken != "retained-dispatch-barrier" || found.DispatchClaimedAt == nil {
					t.Fatalf("blank cleanup token mutated execution: %+v", found)
				}
			})
		}
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

func TestTaskExecutionSetCleanupRuntimeIdentityIsLeaseFencedAndFillOnly(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	execution := seedTaskExecution(t, repo, model.TaskExecutionFailed)
	if err := repo.db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).
		Update("cleanup_status", model.TaskExecutionCleanupPending).Error; err != nil {
		t.Fatal(err)
	}
	token := uuid.NewString()
	won, err := repo.TaskExecutions().ClaimCleanup(ctx, execution.ID, token, time.Minute)
	if err != nil || !won {
		t.Fatalf("claim cleanup won=%v err=%v", won, err)
	}
	identity := model.RuntimeIdentity{Scope: "anban", Workload: "creator-agent-job-1", InstanceID: "job-uid-1"}

	bound, err := repo.TaskExecutions().SetCleanupRuntimeIdentity(ctx, execution.ID, token, identity)
	if err != nil || !bound {
		t.Fatalf("bind recovered identity=%v err=%v", bound, err)
	}
	bound, err = repo.TaskExecutions().SetCleanupRuntimeIdentity(ctx, execution.ID, token, identity)
	if err != nil || !bound {
		t.Fatalf("replay recovered identity=%v err=%v", bound, err)
	}
	bound, err = repo.TaskExecutions().SetCleanupRuntimeIdentity(ctx, execution.ID, uuid.NewString(), identity)
	if err != nil || bound {
		t.Fatalf("stale cleanup identity bind=%v err=%v", bound, err)
	}
	for name, drift := range map[string]model.RuntimeIdentity{
		"scope":    {Scope: "other", Workload: identity.Workload, InstanceID: identity.InstanceID},
		"workload": {Scope: identity.Scope, Workload: "creator-agent-job-2", InstanceID: identity.InstanceID},
		"instance": {Scope: identity.Scope, Workload: identity.Workload, InstanceID: "job-uid-2"},
	} {
		t.Run(name+" drift", func(t *testing.T) {
			bound, err := repo.TaskExecutions().SetCleanupRuntimeIdentity(ctx, execution.ID, token, drift)
			if bound || !errors.Is(err, ErrRuntimeIdentityConflict) {
				t.Fatalf("drift bind=%v err=%v, want conflict", bound, err)
			}
		})
	}

	found, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := runtimeIdentityFromExecution(found); got != identity {
		t.Fatalf("runtime identity = %#v, want %#v", got, identity)
	}
}

func runtimeIdentityFromExecution(execution *model.TaskExecution) model.RuntimeIdentity {
	return model.RuntimeIdentity{
		Scope: execution.RuntimeScope, Workload: execution.RuntimeWorkload, InstanceID: execution.RuntimeInstanceID,
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
