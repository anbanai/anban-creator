package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// newLocalSeedTask creates and persists a pending local-target task owned by
// userID with the given claim deadline. Mirrors the shape CreateManual produces
// for an execution_target=local task, but built directly via the repo so the
// claim / fallback logic is exercised in isolation.
func newLocalSeedTask(t *testing.T, repo repository.Repository, userID, projectID string, deadline *time.Time) *model.Task {
	t.Helper()
	task := &model.Task{
		ID:                 uuid.New().String(),
		UserID:             userID,
		ProjectID:          projectID,
		Type:               model.PlatformSeednote,
		Status:             model.TaskStatusPending,
		Prompt:             "测试选题：茶饮爆款",
		HasContentImage:    true,
		ExecutionTarget:    model.ExecutionTargetLocal,
		LocalClaimDeadline: deadline,
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create seed task: %v", err)
	}
	return task
}

func newLocalMontageTask(t *testing.T, repo repository.Repository, userID, projectID string, deadline *time.Time) *model.Task {
	t.Helper()
	task := &model.Task{
		ID:                 uuid.New().String(),
		UserID:             userID,
		ProjectID:          projectID,
		Type:               model.PlatformMontage,
		Status:             model.TaskStatusPending,
		Prompt:             "做一条品牌短片",
		ExecutionTarget:    model.ExecutionTargetLocal,
		LocalClaimDeadline: deadline,
	}
	task.SetMontageInput(model.MontageInput{Brief: "做一条品牌短片"})
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create montage task: %v", err)
	}
	return task
}

// TestClaimLocalTask_HappyPath verifies a pending local-target task is claimed
// atomically (status→running, target→local_claimed) and its config is returned
// with the agent argv inputs resolved.
func TestClaimLocalTask_HappyPath(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetExecutorDefaults("claude-test-model", nil)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	deadline := time.Now().Add(LocalClaimWindow)
	task := newLocalSeedTask(t, repo, userID, projectID, &deadline)

	cfg, err := svc.ClaimLocalTask(ctx, userID, `{"hostname":"mbp","version":"0.1"}`)
	if err != nil {
		t.Fatalf("ClaimLocalTask: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected config, got nil")
	}
	if cfg.TaskID != task.ID {
		t.Fatalf("cfg.TaskID = %q, want %q", cfg.TaskID, task.ID)
	}
	if cfg.ProjectID != projectID {
		t.Fatalf("cfg.ProjectID = %q, want %q", cfg.ProjectID, projectID)
	}
	if cfg.TaskType != model.PlatformSeednote {
		t.Fatalf("cfg.TaskType = %q", cfg.TaskType)
	}
	if cfg.Model != "claude-test-model" {
		t.Fatalf("cfg.Model = %q, want claude-test-model", cfg.Model)
	}
	if cfg.MaxTurns <= 0 {
		t.Fatalf("cfg.MaxTurns = %d, want > 0", cfg.MaxTurns)
	}
	if cfg.AgentFlag == "" {
		t.Fatalf("cfg.AgentFlag empty, expected anban:<agent>")
	}
	if cfg.Topic != task.Prompt {
		t.Fatalf("cfg.Topic = %q, want %q", cfg.Topic, task.Prompt)
	}

	// Task must now be running + local_claimed so cloud Asynq never picks it up.
	got, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.TaskStatusRunning {
		t.Fatalf("status = %q, want running", got.Status)
	}
	if got.ExecutionTarget != model.ExecutionTargetLocalClaimed {
		t.Fatalf("execution_target = %q, want local_claimed", got.ExecutionTarget)
	}
	// ExecutorInfo blob recorded for diagnostics (non-null).
	raw, err := got.ExecutorInfo.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal executor info: %v", err)
	}
	if string(raw) == "null" || string(raw) == "" {
		t.Fatalf("executor_info not recorded, got %s", string(raw))
	}
}

func TestBuildLocalExecutionConfigCarriesArticleImageSwitches(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	cover := false
	content := true
	task := &model.Task{
		ID:                       "task-article-local",
		Type:                     model.PlatformArticle,
		Prompt:                   "时间管理",
		ArticleWithCover:         &cover,
		ArticleWithContentImages: &content,
	}

	cfg := svc.buildLocalExecutionConfig(task)
	if cfg.ArticleWithCover {
		t.Fatal("expected article_with_cover=false to be carried to local executor config")
	}
	if !cfg.ArticleWithContentImages {
		t.Fatal("expected article_with_content_images=true to be carried to local executor config")
	}
}

func TestBuildLocalExecutionConfigRoutesSplitVideoAgents(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	tests := []struct {
		name string
		task *model.Task
		want string
	}{
		{
			name: "creator",
			task: &model.Task{ID: "task-video-creator-local", Type: model.PlatformVideoCreator, Prompt: "生成一条短视频"},
			want: "anban:videocreator",
		},
		{
			name: "editor",
			task: &model.Task{ID: "task-video-editor-local", Type: model.PlatformVideoEditor, Prompt: "给素材加字幕并剪成短视频"},
			want: "anban:videoeditor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := svc.buildLocalExecutionConfig(tt.task)
			if cfg.AgentFlag != tt.want {
				t.Fatalf("AgentFlag = %q, want %s", cfg.AgentFlag, tt.want)
			}
		})
	}
}

func TestBuildLocalExecutionConfigDefaultsArticleImageSwitchesOn(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	task := &model.Task{
		ID:     "task-article-local-default",
		Type:   model.PlatformArticle,
		Prompt: "时间管理",
	}

	cfg := svc.buildLocalExecutionConfig(task)
	if !cfg.ArticleWithCover || !cfg.ArticleWithContentImages {
		t.Fatalf("article image switches should default true for local executor config, got cover=%v content=%v", cfg.ArticleWithCover, cfg.ArticleWithContentImages)
	}
}

// TestClaimLocalTask_SecondClaimReturnsNil confirms only one claimer wins.
func TestClaimLocalTask_SecondClaimReturnsNil(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	deadline := time.Now().Add(LocalClaimWindow)
	newLocalSeedTask(t, repo, userID, projectID, &deadline)

	cfg, err := svc.ClaimLocalTask(ctx, userID, `{}`)
	if err != nil || cfg == nil {
		t.Fatalf("first claim failed: cfg=%v err=%v", cfg, err)
	}
	cfg2, err := svc.ClaimLocalTask(ctx, userID, `{}`)
	if err != nil {
		t.Fatalf("second claim errored: %v", err)
	}
	if cfg2 != nil {
		t.Fatalf("expected nil on second claim, got %+v", cfg2)
	}
}

// TestClaimLocalTask_IgnoresOtherUserAndCloudTasks confirms the claim only
// matches pending local tasks for the claiming user.
func TestClaimLocalTask_IgnoresOtherUserAndCloudTasks(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	owner := uuid.New().String()
	intruder := uuid.New().String()
	projectID := createTestProject(t, repo, owner, model.PlatformSeednote)
	deadline := time.Now().Add(LocalClaimWindow)

	// Local task owned by someone else.
	newLocalSeedTask(t, repo, owner, projectID, &deadline)
	// Cloud-target pending task owned by intruder (must be invisible to claim).
	cloudTask := &model.Task{
		ID:                 uuid.New().String(),
		UserID:             intruder,
		ProjectID:          projectID,
		Type:               model.PlatformSeednote,
		Status:             model.TaskStatusPending,
		ExecutionTarget:    model.ExecutionTargetCloud,
		LocalClaimDeadline: &deadline,
	}
	if err := repo.Tasks().Create(ctx, cloudTask); err != nil {
		t.Fatalf("create cloud task: %v", err)
	}

	cfg, err := svc.ClaimLocalTask(ctx, intruder, `{}`)
	if err != nil {
		t.Fatalf("ClaimLocalTask: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil (no matching local task for intruder), got %+v", cfg)
	}
}

// TestReclaimExpiredLocalTasks_RequeuesToCloud verifies the fallback worker
// flips an unclaimed, deadline-expired local task back to cloud dispatch.
func TestReclaimExpiredLocalTasks_RequeuesToCloud(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	// Deadline already in the past → unclaimed + expired.
	past := time.Now().Add(-1 * time.Minute)
	task := newLocalSeedTask(t, repo, userID, projectID, &past)

	// A desktop must NOT be able to claim an expired task.
	if cfg, err := svc.ClaimLocalTask(ctx, userID, `{}`); err != nil {
		t.Fatalf("ClaimLocalTask: %v", err)
	} else if cfg != nil {
		t.Fatalf("expired task should not be claimable, got %+v", cfg)
	}

	n, err := svc.ReclaimExpiredLocalTasks(ctx)
	if err != nil {
		t.Fatalf("ReclaimExpiredLocalTasks: %v", err)
	}
	if n != 1 {
		t.Fatalf("reclaimed %d, want 1", n)
	}

	got, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ExecutionTarget != model.ExecutionTargetCloud {
		t.Fatalf("execution_target = %q, want cloud (empty)", got.ExecutionTarget)
	}
	if got.LocalClaimDeadline != nil {
		t.Fatalf("local_claim_deadline = %v, want nil after reclaim", got.LocalClaimDeadline)
	}
}

// TestReclaimExpiredLocalTasks_LeavesClaimedAlone confirms a task already
// claimed (status=running) is not swept by the fallback.
func TestReclaimExpiredLocalTasks_LeavesClaimedAlone(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	deadline := time.Now().Add(LocalClaimWindow)
	task := newLocalSeedTask(t, repo, userID, projectID, &deadline)

	// Claim it first → now running/local_claimed.
	if cfg, err := svc.ClaimLocalTask(ctx, userID, `{}`); err != nil || cfg == nil {
		t.Fatalf("claim failed: cfg=%v err=%v", cfg, err)
	}

	n, err := svc.ReclaimExpiredLocalTasks(ctx)
	if err != nil {
		t.Fatalf("ReclaimExpiredLocalTasks: %v", err)
	}
	if n != 0 {
		t.Fatalf("reclaimed %d, want 0 (task already claimed)", n)
	}
	got, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.TaskStatusRunning {
		t.Fatalf("status = %q, want running", got.Status)
	}
}

// claimOneLocal claims the seeded local task for userID and returns its id.
// Leaves the task status=running + execution_target=local_claimed, ready for
// CompleteLocalTask.
func claimOneLocal(t *testing.T, svc *TaskService, repo repository.Repository, userID, projectID string) string {
	t.Helper()
	deadline := time.Now().Add(LocalClaimWindow)
	task := newLocalSeedTask(t, repo, userID, projectID, &deadline)
	cfg, err := svc.ClaimLocalTask(context.Background(), userID, `{}`)
	if err != nil || cfg == nil {
		t.Fatalf("claim failed: cfg=%v err=%v", cfg, err)
	}
	return task.ID
}

func addLocalSeednoteDeliverables(t *testing.T, repo repository.Repository, taskID string) {
	t.Helper()
	var files []*model.TaskFile
	for _, name := range seednoteCompletionArtifactNamesForTest(true, false) {
		role := model.FileRoleOther
		switch name {
		case "cover.png":
			role = model.FileRoleCover
		case "image_01.png":
			role = model.FileRoleImage
		}
		files = append(files, &model.TaskFile{
			TaskID: taskID, Role: role, FileName: name, FilePath: "output/seednote/title/" + name,
		})
	}
	if err := repo.TaskFiles().BatchCreate(context.Background(), files); err != nil {
		t.Fatalf("create task files: %v", err)
	}
}

func claimOneLocalMontage(t *testing.T, svc *TaskService, repo repository.Repository, userID, projectID string) string {
	t.Helper()
	deadline := time.Now().Add(LocalClaimWindow)
	task := newLocalMontageTask(t, repo, userID, projectID, &deadline)
	cfg, err := svc.ClaimLocalTask(context.Background(), userID, `{}`)
	if err != nil || cfg == nil {
		t.Fatalf("claim failed: cfg=%v err=%v", cfg, err)
	}
	return task.ID
}

// TestCompleteLocalTask_Success verifies a claimed local task transitions to
// completed (terminal) and records completed_at — the core fix for the
// "local tasks never complete" gap.
func TestCompleteLocalTask_Success(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)
	addLocalSeednoteDeliverables(t, repo, taskID)

	if err := svc.CompleteLocalTask(ctx, taskID, &agent.ExecutionResult{Success: true, LogText: "done"}); err != nil {
		t.Fatalf("CompleteLocalTask: %v", err)
	}

	got, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.TaskStatusCompleted {
		t.Fatalf("status = %q, want completed", got.Status)
	}
	if got.CompletedAt == nil {
		t.Fatalf("completed_at not set")
	}
}

func TestCompleteLocalTask_SuccessWithoutDeliverablesFails(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)

	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{TaskID: taskID, Role: model.FileRoleOther, FileName: "CLAUDE.md", FilePath: "CLAUDE.md"}); err != nil {
		t.Fatalf("create runtime file: %v", err)
	}
	if err := svc.CompleteLocalTask(ctx, taskID, &agent.ExecutionResult{Success: true, LogText: "done"}); err != nil {
		t.Fatalf("CompleteLocalTask: %v", err)
	}

	got, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.ErrorMessage == "" {
		t.Fatalf("expected error message for missing deliverables")
	}
}

func TestCompleteLocalTask_MontageRejectsZeroByteDeliverables(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	taskID := claimOneLocalMontage(t, svc, repo, userID, projectID)

	files := []*model.TaskFile{
		{TaskID: taskID, Role: "final_video", FileName: "final.mp4", FilePath: "output/montage/final.mp4", FileSize: 0},
		{TaskID: taskID, Role: "delivery_manifest", FileName: "delivery-manifest.json", FilePath: "output/montage/delivery-manifest.json", FileSize: 0},
	}
	if err := repo.TaskFiles().BatchCreate(ctx, files); err != nil {
		t.Fatalf("create zero-byte montage files: %v", err)
	}
	if err := svc.CompleteLocalTask(ctx, taskID, &agent.ExecutionResult{Success: true, LogText: "done"}); err != nil {
		t.Fatalf("CompleteLocalTask: %v", err)
	}

	got, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.ErrorMessage == "" {
		t.Fatal("expected error message for zero-byte Montage deliverables")
	}
}

func TestCompleteLocalTask_NestedAgentOnlyFails(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)
	addLocalSeednoteDeliverables(t, repo, taskID)

	if err := svc.CompleteLocalTask(ctx, taskID, &agent.ExecutionResult{Success: true, ToolUseSummary: map[string]int{"Agent": 1}}); err != nil {
		t.Fatalf("CompleteLocalTask: %v", err)
	}

	got, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.ErrorMessage != agent.NestedAgentDelegationError {
		t.Fatalf("error = %q, want %q", got.ErrorMessage, agent.NestedAgentDelegationError)
	}
}

// TestCompleteLocalTask_Failure verifies a failed local result transitions to
// failed (terminal), not stuck running.
func TestCompleteLocalTask_Failure(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)

	if err := svc.CompleteLocalTask(ctx, taskID, &agent.ExecutionResult{Success: false, Error: "boom"}); err != nil {
		t.Fatalf("CompleteLocalTask: %v", err)
	}

	got, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.ErrorMessage == "" || got.CompletedAt == nil {
		t.Fatalf("error/completed_at not recorded: err=%q completed_at=%v", got.ErrorMessage, got.CompletedAt)
	}
}

func TestCompleteLocalTaskNilResultPersistsTerminalCostEvidence(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)

	if err := svc.CompleteLocalTask(ctx, taskID, nil); err != nil {
		t.Fatalf("CompleteLocalTask: %v", err)
	}

	got, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.TaskStatusFailed || got.Result == nil {
		t.Fatalf("local nil result = status %q result %v, want failed persisted result", got.Status, got.Result)
	}
	var persisted agent.ExecutionResult
	if err := json.Unmarshal([]byte(*got.Result), &persisted); err != nil {
		t.Fatalf("decode persisted result: %v", err)
	}
	if persisted.Success || persisted.Error != "agent returned no execution result" {
		t.Fatalf("persisted nil result = %+v, want stable terminal failure", persisted)
	}
	if persisted.CostStatus != agent.CostStatusUnreconciled || got.CostStatus != agent.CostStatusUnreconciled {
		t.Fatalf("cost status = result %q task %q, want unreconciled", persisted.CostStatus, got.CostStatus)
	}
	if len(persisted.ModelUsage) != 0 || len(got.TerminalModelUsage.Data()) != 0 {
		t.Fatalf("nil terminal fabricated usage: result=%+v task=%+v", persisted.ModelUsage, got.TerminalModelUsage.Data())
	}
}

// TestCompleteLocalTask_GuardedToNonLocal confirms a cloud (or already-terminal)
// task is a no-op — so the shared agent binary calling /complete in cloud mode
// cannot double-finalize. This is the safety property that lets one endpoint
// serve both execution modes.
func TestCompleteLocalTask_GuardedToNonLocal(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	// A running CLOUD task (not local_claimed): CompleteLocalTask must not touch it.
	cloudTask := &model.Task{
		ID:              uuid.New().String(),
		UserID:          userID,
		ProjectID:       projectID,
		Type:            model.PlatformSeednote,
		Status:          model.TaskStatusRunning,
		ExecutionTarget: model.ExecutionTargetCloud,
	}
	if err := repo.Tasks().Create(ctx, cloudTask); err != nil {
		t.Fatalf("create cloud task: %v", err)
	}

	if err := svc.CompleteLocalTask(ctx, cloudTask.ID, &agent.ExecutionResult{Success: true}); err != nil {
		t.Fatalf("CompleteLocalTask on cloud task: %v", err)
	}
	got, err := repo.Tasks().FindByID(ctx, cloudTask.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.TaskStatusRunning {
		t.Fatalf("cloud task status = %q, want unchanged running (guard)", got.Status)
	}
}

// TestCompleteLocalTask_Idempotent confirms a second complete call on an already
// terminal task is a no-op (no error, status stable).
func TestCompleteLocalTask_Idempotent(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := claimOneLocal(t, svc, repo, userID, projectID)
	addLocalSeednoteDeliverables(t, repo, taskID)

	if err := svc.CompleteLocalTask(ctx, taskID, &agent.ExecutionResult{Success: true}); err != nil {
		t.Fatalf("first complete: %v", err)
	}
	// Second call must not error and must not flip status.
	if err := svc.CompleteLocalTask(ctx, taskID, &agent.ExecutionResult{Success: false, Error: "late"}); err != nil {
		t.Fatalf("second complete: %v", err)
	}
	got, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.TaskStatusCompleted {
		t.Fatalf("status = %q, want completed (idempotent)", got.Status)
	}
}
