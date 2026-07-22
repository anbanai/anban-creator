package repository

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func TestTaskFileMutationMySQLLockOrderContract(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, Logger: logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Info})})
	if err != nil {
		t.Fatal(err)
	}
	var task model.Task
	_ = db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", "t1").First(&task).Error
	var execution model.TaskExecution
	_ = db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND task_id = ?", "e1", "t1").First(&execution).Error
	assertTaskFileMutationLockOrder(t, logs.String())
}

func assertTaskFileMutationLockOrder(t *testing.T, sql string) {
	t.Helper()
	taskAt, executionAt := strings.Index(sql, "FROM `tasks`"), strings.Index(sql, "FROM `task_executions`")
	if taskAt < 0 || executionAt <= taskAt {
		t.Fatalf("task-file mutation lock order SQL contract invalid:\n%s", sql)
	}
	taskLockOffset := strings.Index(sql[taskAt:], "FOR UPDATE")
	executionLockOffset := strings.Index(sql[executionAt:], "FOR UPDATE")
	if taskLockOffset < 0 || executionLockOffset < 0 || taskAt+taskLockOffset >= executionAt {
		t.Fatalf("task-file mutation lock order SQL contract invalid:\n%s", sql)
	}
}

func TestTaskFileRepositoryPublishesExecutionAtomicallyAndIdempotently(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	rows := []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePublished, Role: model.FileRoleOther, FilePath: "old.md", FileName: "old.md"},
		{TaskID: "t1", ExecutionID: "e2", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "new.md", FileName: "new.md"},
	}
	if err := repo.TaskFiles().BatchCreate(ctx, rows); err != nil {
		t.Fatal(err)
	}
	visible, err := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if err != nil || len(visible) != 1 || visible[0].ExecutionID != "e1" {
		t.Fatalf("visible before publish = %#v, %v", visible, err)
	}
	if err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e2"); err != nil {
		t.Fatal(err)
	}
	task, err := repo.Tasks().FindByID(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	task.Status = model.TaskStatusCompleted
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	if changed, err := repo.TaskExecutions().Transition(ctx, "e2", []string{model.TaskExecutionRunning}, model.TaskExecutionSucceeded, model.ExecutionTransition{}); err != nil || !changed {
		t.Fatalf("terminal transition = %v, %v", changed, err)
	}
	if err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e2"); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	visible, err = repo.TaskFiles().FindByTaskID(ctx, "t1")
	if err != nil || len(visible) != 1 || visible[0].ExecutionID != "e2" {
		t.Fatalf("visible after publish = %#v, %v", visible, err)
	}
	old, _ := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if len(old) != 1 || old[0].State != model.TaskFileStateSuperseded {
		t.Fatalf("old rows = %#v", old)
	}
}

func TestTaskFileRepositoryPublishWithNoTargetRowsPreservesPublishedSet(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePublished, Role: model.FileRoleOther, FilePath: "old.md", FileName: "old.md"}); err != nil {
		t.Fatal(err)
	}
	err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e2")
	if !errors.Is(err, ErrNoPendingExecutionArtifacts) {
		t.Fatalf("error = %v, want ErrNoPendingExecutionArtifacts", err)
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if len(visible) != 1 || visible[0].ExecutionID != "e1" {
		t.Fatalf("published set changed: %#v", visible)
	}
}

func TestTaskFileRepositoryRejectsStaleExecutionWithoutChangingPublishedSet(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "e2", State: model.TaskFileStatePublished, Role: model.FileRoleOther, FilePath: "current.md", FileName: "current.md"},
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "stale.md", FileName: "stale.md"},
	}); err != nil {
		t.Fatal(err)
	}
	err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e1")
	if !errors.Is(err, ErrTaskFileExecutionNotCurrent) {
		t.Fatalf("error = %v, want stale execution", err)
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if len(visible) != 1 || visible[0].ExecutionID != "e2" {
		t.Fatalf("published set changed: %#v", visible)
	}
}

func TestTaskFileRepositoryPublishRollbackPreservesOldSet(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePublished, Role: model.FileRoleOther, FilePath: "old.md", FileName: "old.md"},
		{TaskID: "t1", ExecutionID: "e2", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "new.md", FileName: "new.md"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_publish BEFORE UPDATE ON task_files WHEN OLD.execution_id = 'e2' BEGIN SELECT RAISE(ABORT, 'forced publish failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e2"); err == nil {
		t.Fatal("forced publication failure unexpectedly succeeded")
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if len(visible) != 1 || visible[0].ExecutionID != "e1" || visible[0].State != model.TaskFileStatePublished {
		t.Fatalf("rollback visible set = %#v", visible)
	}
}

func TestTaskFileRepositoryPublishRejectsStoppedTask(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	task, _ := repo.Tasks().FindByID(ctx, "t1")
	task.Status = model.TaskStatusFailed
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "a.md", FileName: "a.md"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e1"); !errors.Is(err, ErrTaskFileTaskNotRunning) {
		t.Fatalf("error = %v", err)
	}
}

func TestTaskFileRepositoryConcurrentAttemptsOnlyCurrentPublishes(t *testing.T) {
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// SQLite's :memory: database is connection-local. A single connection also
	// serializes transactions, which is its safe equivalent to MySQL FOR UPDATE.
	sqlDB.SetMaxOpenConns(1)
	repo := New(db)
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "stale.md", FileName: "stale.md"},
		{TaskID: "t1", ExecutionID: "e2", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "current.md", FileName: "current.md"},
	}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, executionID := range []string{"e1", "e2"} {
		wg.Add(1)
		go func(i int, executionID string) {
			defer wg.Done()
			<-start
			errs[i] = repo.TaskFiles().PublishCurrentExecution(ctx, "t1", executionID)
		}(i, executionID)
	}
	close(start)
	wg.Wait()
	if !errors.Is(errs[0], ErrTaskFileExecutionNotCurrent) || errs[1] != nil {
		t.Fatalf("publish errors = %#v", errs)
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if len(visible) != 1 || visible[0].ExecutionID != "e2" {
		t.Fatalf("visible = %#v", visible)
	}
}

func TestTaskFileRepositoryPublishRacingReplacementHasNoLatePendingRows(t *testing.T) {
	repo := newSerializedTaskFileTestRepo(t)
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "first.md", FileName: "first.md"}); err != nil {
		t.Fatal(err)
	}
	replacement := []*model.TaskFile{{Role: model.FileRoleOther, FilePath: "replacement.md", FileName: "replacement.md"}}
	publishErr, replaceErr := runArtifactMutationRace(
		func() error { return repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e1") },
		func() error { return repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", replacement) },
	)
	if publishErr != nil {
		t.Fatalf("publish error = %v", publishErr)
	}
	if replaceErr != nil && !errors.Is(replaceErr, ErrTaskFileManifestState) {
		t.Fatalf("replace error = %v", replaceErr)
	}
	rows, _ := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	visible, pending := 0, 0
	for _, row := range rows {
		if row.State == model.TaskFileStatePublished {
			visible++
		}
		if row.State == model.TaskFileStatePending {
			pending++
		}
	}
	if visible != 1 || pending != 0 {
		t.Fatalf("rows after race = %#v", rows)
	}
}

func TestTaskFileRepositoryPublishRacingDiscardPreservesVisibleSet(t *testing.T) {
	repo := newSerializedTaskFileTestRepo(t)
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "older", State: model.TaskFileStatePublished, Role: model.FileRoleOther, FilePath: "old.md", FileName: "old.md"},
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "new.md", FileName: "new.md"},
	}); err != nil {
		t.Fatal(err)
	}
	publishErr, discardErr := runArtifactMutationRace(
		func() error { return repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e1") },
		func() error { return repo.TaskFiles().DiscardCurrentExecution(ctx, "t1", "e1") },
	)
	if publishErr != nil && !errors.Is(publishErr, ErrTaskFileManifestState) {
		t.Fatalf("publish error = %v", publishErr)
	}
	if discardErr != nil && !errors.Is(discardErr, ErrTaskFileManifestState) {
		t.Fatalf("discard error = %v", discardErr)
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if len(visible) != 1 {
		t.Fatalf("visible set after race = %#v", visible)
	}
	rows, _ := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	for _, row := range rows {
		if row.State == model.TaskFileStatePending {
			t.Fatalf("late pending row = %#v", row)
		}
	}
}

func newSerializedTaskFileTestRepo(t *testing.T) Repository {
	t.Helper()
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	return New(db)
}

func runArtifactMutationRace(first, second func() error) (error, error) {
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, fn := range []func() error{first, second} {
		wg.Add(1)
		go func(i int, fn func() error) { defer wg.Done(); <-start; errs[i] = fn() }(i, fn)
	}
	close(start)
	wg.Wait()
	return errs[0], errs[1]
}

func seedCurrentTaskForArtifacts(t *testing.T, repo Repository, taskID, executionID string) {
	t.Helper()
	ctx := context.Background()
	userID, projectID := "u-"+taskID, "p-"+taskID
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: taskID + "@example.com", Password: "x", InviteCode: taskID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Name: "p", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning, CurrentExecutionID: &executionID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: taskID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true, ManifestStatus: model.TaskExecutionManifestPending}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskFileRepositoryDiscardExecutionHidesPendingRows(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "a.md", FileName: "a.md"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().DiscardCurrentExecution(ctx, "t1", "e1"); err != nil {
		t.Fatal(err)
	}
	rows, _ := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if len(rows) != 1 || rows[0].State != model.TaskFileStateSuperseded {
		t.Fatalf("rows = %#v", rows)
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if len(visible) != 0 {
		t.Fatalf("discarded rows leaked: %#v", visible)
	}
}

func TestTaskFileRepositoryCollectsFailedExecutionWithoutReplacingPublishedSet(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePublished, Role: model.FileRoleMarkdown, FilePath: "output/content.md", FileName: "content.md"},
		{TaskID: "t1", ExecutionID: "e2", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json"},
	}); err != nil {
		t.Fatal(err)
	}
	changed, err := repo.TaskExecutions().Transition(ctx, "e2", []string{model.TaskExecutionRunning}, model.TaskExecutionFailed, model.ExecutionTransition{})
	if err != nil || !changed {
		t.Fatalf("mark execution failed: changed=%v err=%v", changed, err)
	}
	if err := repo.TaskFiles().CollectCurrentExecution(ctx, "t1", "e2"); err != nil {
		t.Fatalf("collect failed execution: %v", err)
	}
	published, err := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if err != nil || len(published) != 1 || published[0].ExecutionID != "e1" {
		t.Fatalf("published files = %#v, err=%v", published, err)
	}
	failedFiles, err := repo.TaskFiles().FindByExecutionID(ctx, "e2")
	if err != nil || len(failedFiles) != 1 || failedFiles[0].State != "collected" {
		t.Fatalf("failed execution files = %#v, err=%v", failedFiles, err)
	}
	execution, err := repo.TaskExecutions().FindByID(ctx, "e2")
	if err != nil || execution.ManifestStatus != "collected" {
		t.Fatalf("execution = %#v, err=%v", execution, err)
	}
}

func TestTaskFileRepositoryCollectCurrentExecutionIsIdempotent(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending,
		Role: model.FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json",
	}); err != nil {
		t.Fatal(err)
	}
	changed, err := repo.TaskExecutions().Transition(ctx, "e1", []string{model.TaskExecutionRunning}, model.TaskExecutionFailed, model.ExecutionTransition{})
	if err != nil || !changed {
		t.Fatalf("mark execution failed: changed=%v err=%v", changed, err)
	}
	for range 2 {
		if err := repo.TaskFiles().CollectCurrentExecution(ctx, "t1", "e1"); err != nil {
			t.Fatalf("collect failed execution: %v", err)
		}
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(rows) != 1 || rows[0].State != "collected" {
		t.Fatalf("collected rows = %#v, err=%v", rows, err)
	}
}

func TestTaskFileRepositoryCollectRejectsStaleExecution(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: "e1", TaskID: "t1", Attempt: 2, Target: "kubernetes",
		Status: model.TaskExecutionFailed, Started: true, ManifestStatus: model.TaskExecutionManifestPending,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending,
		Role: model.FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().CollectCurrentExecution(ctx, "t1", "e1"); !errors.Is(err, ErrTaskFileExecutionNotCurrent) {
		t.Fatalf("collect stale execution error = %v", err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(rows) != 1 || rows[0].State != model.TaskFileStatePending {
		t.Fatalf("stale rows changed = %#v, err=%v", rows, err)
	}
}

func TestTaskFileRepositoryReplacePendingExecutionRollsBackOnFailure(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	original := &model.TaskFile{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "old.md", FileName: "old.md"}
	if err := repo.TaskFiles().Create(ctx, original); err != nil {
		t.Fatal(err)
	}
	bad := []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "new.md", FileName: "new.md"},
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "new.md", FileName: "new.md"},
	}
	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", bad); err == nil {
		t.Fatal("invalid replacement unexpectedly succeeded")
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(rows) != 1 || rows[0].FilePath != "old.md" {
		t.Fatalf("rollback rows = %#v, %v", rows, err)
	}
}

func TestTaskFileRepositoryReplacePendingExecutionForcesDeclaredScope(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	files := []*model.TaskFile{{TaskID: "foreign", ExecutionID: "foreign", State: model.TaskFileStatePublished, Role: model.FileRoleOther, FilePath: "output/a.md", FileName: "a.md"}}
	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", files); err != nil {
		t.Fatal(err)
	}
	rows, _ := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if len(rows) != 1 || rows[0].TaskID != "t1" || rows[0].State != model.TaskFileStatePending {
		t.Fatalf("scoped rows = %#v", rows)
	}
	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", []*model.TaskFile{{Role: model.FileRoleOther, FilePath: "../escape", FileName: "escape"}}); err == nil {
		t.Fatal("unsafe path accepted")
	}
}

func TestTaskFileRepositoryReplacePendingExecutionPreservesLogicalPathIdentity(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: "file-1", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending,
		Role: model.FileRoleOther, FilePath: "output/article.md", FileName: "old.md",
		MimeType: "text/plain", FileSize: 10, ContentHash: strings.Repeat("a", 64),
		OSSKey: "old-key", OSSURL: "https://old.example/file", StorageProvider: "local",
		MediaID: "old-media", WechatURL: "https://old.example/wechat",
	}); err != nil {
		t.Fatal(err)
	}
	before, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(before) != 1 {
		t.Fatalf("before rows = %#v, err=%v", before, err)
	}
	createdAt := before[0].CreatedAt

	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", []*model.TaskFile{{
		ID: "replacement-id", Role: model.FileRoleMarkdown, FilePath: "output/article.md", FileName: "article.md",
		MimeType: "text/markdown", FileSize: 20, ContentHash: strings.Repeat("b", 64),
		OSSKey: "new-key", OSSURL: "https://new.example/file", StorageProvider: "oss",
		MediaID: "new-media", WechatURL: "https://new.example/wechat",
	}}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %#v, err=%v", rows, err)
	}
	if rows[0].ID != "file-1" || !rows[0].CreatedAt.Equal(createdAt) {
		t.Fatalf("identity changed: %#v, createdAt=%v", rows[0], createdAt)
	}
	if rows[0].FileName != "article.md" || rows[0].MimeType != "text/markdown" || rows[0].FileSize != 20 ||
		rows[0].OSSKey != "new-key" || rows[0].OSSURL != "https://new.example/file" || rows[0].StorageProvider != "oss" ||
		rows[0].Role != model.FileRoleMarkdown || rows[0].ContentHash != strings.Repeat("b", 64) ||
		rows[0].MediaID != "new-media" || rows[0].WechatURL != "https://new.example/wechat" {
		t.Fatalf("manifest values = %#v", rows[0])
	}
}

func TestTaskFileRepositoryReplacePendingExecutionRemovesMissingPaths(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: "keep", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "output/keep.md", FileName: "keep.md"},
		{ID: "drop", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "output/drop.md", FileName: "drop.md"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", []*model.TaskFile{{
		Role: model.FileRoleOther, FilePath: "output/keep.md", FileName: "keep.md",
	}}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(rows) != 1 || rows[0].ID != "keep" || rows[0].FilePath != "output/keep.md" {
		t.Fatalf("rows = %#v, err=%v", rows, err)
	}
}

func TestTaskFileRepositoryReplacePendingExecutionIdenticalManifestIsStable(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: "file-1", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending,
		Role: model.FileRoleMarkdown, FilePath: "output/article.md", FileName: "article.md",
		MimeType: "text/markdown", FileSize: 42, ContentHash: strings.Repeat("a", 64),
		OSSKey: "known-key", OSSURL: "https://example.com/file", StorageProvider: "oss",
		MediaID: "known-media", WechatURL: "https://example.com/wechat",
	}); err != nil {
		t.Fatal(err)
	}
	before, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(before) != 1 {
		t.Fatalf("before rows = %#v, err=%v", before, err)
	}
	createdAt := before[0].CreatedAt

	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", []*model.TaskFile{{
		Role: model.FileRoleMarkdown, FilePath: "output/article.md", FileName: "article.md",
		MimeType: "text/markdown", FileSize: 42, ContentHash: strings.Repeat("a", 64),
		OSSKey: "known-key", OSSURL: "https://example.com/file", StorageProvider: "oss",
		MediaID: "known-media", WechatURL: "https://example.com/wechat",
	}}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %#v, err=%v", rows, err)
	}
	if rows[0].ID != "file-1" || !rows[0].CreatedAt.Equal(createdAt) ||
		rows[0].FileName != "article.md" || rows[0].MimeType != "text/markdown" || rows[0].FileSize != 42 ||
		rows[0].OSSKey != "known-key" || rows[0].OSSURL != "https://example.com/file" || rows[0].StorageProvider != "oss" ||
		rows[0].Role != model.FileRoleMarkdown || rows[0].ContentHash != strings.Repeat("a", 64) ||
		rows[0].MediaID != "known-media" || rows[0].WechatURL != "https://example.com/wechat" {
		t.Fatalf("stable manifest changed: %#v, createdAt=%v", rows[0], createdAt)
	}
}

func TestTaskFileRepositoryReplacePendingExecutionIgnoresCallerIDForNewPath(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")

	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", []*model.TaskFile{{
		ID: "caller-supplied", Role: model.FileRoleOther, FilePath: "output/new.md", FileName: "new.md",
	}}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %#v, err=%v", rows, err)
	}
	if rows[0].ID == "" || rows[0].ID == "caller-supplied" {
		t.Fatalf("new path retained caller ID: %#v", rows[0])
	}
	if _, err := uuid.Parse(rows[0].ID); err != nil {
		t.Fatalf("new path ID %q is not a generated UUID: %v", rows[0].ID, err)
	}
}

func TestTaskFileRepositoryReplacePendingExecutionCallerIDCollisionCannotMutateForeignRow(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	foreign := &model.TaskFile{
		ID: "foreign-id", TaskID: "foreign-task", ExecutionID: "foreign-execution", State: model.TaskFileStatePending,
		Role: model.FileRoleOther, FilePath: "output/foreign.md", FileName: "foreign.md", OSSKey: "foreign-key",
	}
	if err := repo.TaskFiles().Create(ctx, foreign); err != nil {
		t.Fatal(err)
	}

	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", []*model.TaskFile{{
		ID: "foreign-id", Role: model.FileRoleMarkdown, FilePath: "output/new.md", FileName: "new.md", OSSKey: "new-key",
	}}); err != nil {
		t.Fatal(err)
	}
	foreignRows, err := repo.TaskFiles().FindByExecutionID(ctx, "foreign-execution")
	if err != nil || len(foreignRows) != 1 || foreignRows[0].ID != "foreign-id" || foreignRows[0].OSSKey != "foreign-key" || foreignRows[0].FilePath != "output/foreign.md" {
		t.Fatalf("foreign row changed: %#v, err=%v", foreignRows, err)
	}
	currentRows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(currentRows) != 1 || currentRows[0].ID == "foreign-id" || currentRows[0].OSSKey != "new-key" {
		t.Fatalf("current rows = %#v, err=%v", currentRows, err)
	}
}

func TestTaskFileRepositoryReplacePendingExecutionEmptyManifestRemovesAllPendingRows(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: "first", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "output/first.md", FileName: "first.md"},
		{ID: "second", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "output/second.md", FileName: "second.md"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", nil); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows = %#v, err=%v", rows, err)
	}
}

func TestTaskFileRepositoryReplacePendingExecutionEmptyManifestPreservesLockedMCPRows(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	mcpPrefix := "uploads/users/u-t1/projects/p-t1/tasks/t1/executions/e1/artifacts/mcp/"
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{
			ID: "mcp", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending,
			Role: model.FileRoleImage, FilePath: "output/cover.png", FileName: "cover.png",
			OSSKey: mcpPrefix + "output/cover.png",
		},
		{
			ID: "workspace", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending,
			Role: model.FileRoleMarkdown, FilePath: "output/article.md", FileName: "article.md",
			OSSKey: "uploads/users/u-t1/projects/p-t1/tasks/t1/executions/e1/artifacts/output/article.md",
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := repo.TaskFiles().ReplacePendingCurrentExecutionPreservingMCPArtifacts(ctx, "t1", "e1", nil); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "mcp" || rows[0].OSSKey != mcpPrefix+"output/cover.png" {
		t.Fatalf("rows = %#v, want only locked MCP row", rows)
	}
}

// This behavioral test fixes the operation order with one SQLite connection.
// The MySQL contract tests below separately prove both methods take the same
// task-then-execution row locks used for production serialization.
func TestTaskFileRepositorySerializedConnectionPreservesMCPUpsertBeforeReplacement(t *testing.T) {
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	repo := New(db)
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")

	type upsertContextKey struct{}
	upsertReachedCreate := make(chan struct{})
	releaseUpsert := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseUpsert) }) }
	t.Cleanup(release)
	callbackName := "test:pause_mcp_upsert_after_execution_lock"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(upsertContextKey{}) == true && tx.Statement.Table == "task_files" {
			close(upsertReachedCreate)
			<-releaseUpsert
		}
	}); err != nil {
		t.Fatal(err)
	}

	mcpPrefix := "uploads/users/u-t1/projects/p-t1/tasks/t1/executions/e1/artifacts/mcp/"
	upsertDone := make(chan error, 1)
	go func() {
		ctx := context.WithValue(context.Background(), upsertContextKey{}, true)
		_, upsertErr := repo.TaskFiles().UpsertPendingCurrentExecution(ctx, "t1", "e1", &model.TaskFile{
			Role: model.FileRoleImage, FilePath: "output/cover.png", FileName: "cover.png",
			OSSKey: mcpPrefix + "output/cover.png",
		})
		upsertDone <- upsertErr
	}()
	select {
	case <-upsertReachedCreate:
	case <-time.After(2 * time.Second):
		t.Fatal("MCP upsert did not reach the locked create boundary")
	}

	replacementStarted := make(chan struct{})
	replacementDone := make(chan error, 1)
	go func() {
		close(replacementStarted)
		replacementDone <- repo.TaskFiles().ReplacePendingCurrentExecutionPreservingMCPArtifacts(context.Background(), "t1", "e1", nil)
	}()
	<-replacementStarted
	select {
	case err := <-replacementDone:
		t.Fatalf("replacement bypassed MCP upsert locks: %v", err)
	default:
	}

	release()
	if err := <-upsertDone; err != nil {
		t.Fatalf("MCP upsert: %v", err)
	}
	if err := <-replacementDone; err != nil {
		t.Fatalf("workspace replacement: %v", err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(context.Background(), "e1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].FilePath != "output/cover.png" || rows[0].OSSKey != mcpPrefix+"output/cover.png" {
		t.Fatalf("serialized rows = %#v, want committed MCP artifact preserved", rows)
	}
}

func TestTaskFileRepositorySerializedConnectionRejectsMetadataAfterWorkspaceReplacement(t *testing.T) {
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	repo := New(db)
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	original, err := repo.TaskFiles().UpsertPendingCurrentExecution(context.Background(), "t1", "e1", &model.TaskFile{
		Role: model.FileRoleImage, FilePath: "output/cover.png", FileName: "cover.png",
		OSSKey: "mcp/old.png", ContentHash: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}

	type replacementContextKey struct{}
	replacementReachedPendingRead := make(chan struct{})
	releaseReplacement := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseReplacement) }) }
	t.Cleanup(release)
	callbackName := "test:pause_replacement_after_execution_lock"
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(replacementContextKey{}) == true && tx.Statement.Table == "task_files" {
			close(replacementReachedPendingRead)
			<-releaseReplacement
		}
	}); err != nil {
		t.Fatal(err)
	}

	replacementDone := make(chan error, 1)
	go func() {
		ctx := context.WithValue(context.Background(), replacementContextKey{}, true)
		replacementDone <- repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", []*model.TaskFile{{
			Role: model.FileRoleImage, FilePath: original.FilePath, FileName: original.FileName,
			OSSKey: "workspace/new.png", ContentHash: strings.Repeat("b", 64),
		}})
	}()
	select {
	case <-replacementReachedPendingRead:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement did not reach the post-lock pending read")
	}

	metadataStarted := make(chan struct{})
	metadataDone := make(chan error, 1)
	go func() {
		close(metadataStarted)
		_, updateErr := repo.TaskFiles().UpdatePendingCurrentExecutionMetadata(context.Background(), original, model.FileRoleCover, "late-media", "https://late.example/image")
		metadataDone <- updateErr
	}()
	<-metadataStarted
	select {
	case err := <-metadataDone:
		t.Fatalf("metadata update bypassed replacement transaction: %v", err)
	default:
	}

	release()
	if err := <-replacementDone; err != nil {
		t.Fatalf("workspace replacement: %v", err)
	}
	if err := <-metadataDone; !errors.Is(err, ErrTaskFileDeliveryIdentityChanged) {
		t.Fatalf("late metadata error = %v, want ErrTaskFileDeliveryIdentityChanged", err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(context.Background(), "e1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("execution rows = %#v, err=%v", rows, err)
	}
	if got := rows[0]; got.ID != original.ID || got.OSSKey != "workspace/new.png" || got.ContentHash != strings.Repeat("b", 64) || got.MediaID != "" || got.WechatURL != "" {
		t.Fatalf("workspace row mutated by late metadata: %#v", got)
	}
}

func TestTaskFileRepositoryUpdatePendingExecutionMetadataChangesOnlyMetadata(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	original, err := repo.TaskFiles().UpsertPendingCurrentExecution(ctx, "t1", "e1", &model.TaskFile{
		Role: model.FileRoleImage, FilePath: "output/cover.png", FileName: "cover.png",
		MimeType: "image/png", FileSize: 42, OSSKey: "mcp/cover.png", OSSURL: "https://files.example/cover.png",
		StorageProvider: "oss", ContentHash: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repo.TaskFiles().UpdatePendingCurrentExecutionMetadata(ctx, original, model.FileRoleCover, "media-1", "https://wechat.example/cover.png")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != original.ID || updated.FilePath != original.FilePath || updated.OSSKey != original.OSSKey || updated.ContentHash != original.ContentHash ||
		updated.Role != model.FileRoleCover || updated.MediaID != "media-1" || updated.WechatURL != "https://wechat.example/cover.png" {
		t.Fatalf("updated row = %#v", updated)
	}
	if _, err := repo.TaskFiles().UpdatePendingCurrentExecutionMetadata(ctx, original, model.FileRoleCover, "media-1", "https://wechat.example/cover.png"); err != nil {
		t.Fatalf("identical metadata retry: %v", err)
	}
}

func TestTaskFileRepositoryPrefixReplacementScopesPreservationAndKeepsWorkspacePrecedence(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	seedCurrentTaskForArtifacts(t, repo, "t2", "e2")
	mcpPrefix := "uploads/users/u-t1/projects/p-t1/tasks/t1/executions/e1/artifacts/mcp/"
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: "keep", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleImage, FilePath: "output/keep.png", FileName: "keep.png", OSSKey: mcpPrefix + "output/keep.png"},
		{ID: "collision", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleImage, FilePath: "output/collision.png", FileName: "collision.png", OSSKey: mcpPrefix + "output/collision.png"},
		{ID: "workspace-stale", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleMarkdown, FilePath: "output/stale.md", FileName: "stale.md", OSSKey: "workspace/stale.md"},
		{ID: "wrong-execution-prefix", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleImage, FilePath: "output/wrong.png", FileName: "wrong.png", OSSKey: "uploads/users/u-t1/projects/p-t1/tasks/t1/executions/e2/artifacts/mcp/output/wrong.png"},
		{ID: "prefix-lookalike", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleImage, FilePath: "output/lookalike.png", FileName: "lookalike.png", OSSKey: strings.TrimSuffix(mcpPrefix, "/") + "-other/output/lookalike.png"},
		{ID: "unsafe-path", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleImage, FilePath: "../unsafe.png", FileName: "unsafe.png", OSSKey: mcpPrefix + "../unsafe.png"},
		{ID: "foreign", TaskID: "t2", ExecutionID: "e2", State: model.TaskFileStatePending, Role: model.FileRoleImage, FilePath: "output/foreign.png", FileName: "foreign.png", OSSKey: mcpPrefix + "output/foreign.png"},
	}); err != nil {
		t.Fatal(err)
	}
	workspaceKey := "uploads/users/u-t1/projects/p-t1/tasks/t1/executions/e1/artifacts/output/collision.png"
	if err := repo.TaskFiles().ReplacePendingCurrentExecutionPreservingMCPArtifacts(ctx, "t1", "e1", []*model.TaskFile{
		{Role: model.FileRoleImage, FilePath: "output/collision.png", FileName: "collision.png", OSSKey: workspaceKey},
		{Role: model.FileRoleMarkdown, FilePath: "output/new.md", FileName: "new.md", OSSKey: "workspace/new.md"},
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil {
		t.Fatal(err)
	}
	byPath := make(map[string]*model.TaskFile, len(rows))
	for _, row := range rows {
		byPath[row.FilePath] = row
	}
	if len(byPath) != 3 || byPath["output/keep.png"] == nil || byPath["output/new.md"] == nil {
		t.Fatalf("current rows = %#v, want preserved MCP plus workspace manifest", rows)
	}
	if collision := byPath["output/collision.png"]; collision == nil || collision.ID != "collision" || collision.OSSKey != workspaceKey {
		t.Fatalf("workspace collision row = %#v, want stable ID collision and workspace key %s", collision, workspaceKey)
	}
	foreign, err := repo.TaskFiles().FindByExecutionID(ctx, "e2")
	if err != nil || len(foreign) != 1 || foreign[0].ID != "foreign" {
		t.Fatalf("foreign rows changed or imported: %#v, err=%v", foreign, err)
	}
}

func TestTaskFileRepositoryMCPPreservationDoesNotMutateCallerSliceBackingArray(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	mcpPrefix := "uploads/users/u-t1/projects/p-t1/tasks/t1/executions/e1/artifacts/mcp/"
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: "mcp", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending,
		Role: model.FileRoleImage, FilePath: "output/cover.png", FileName: "cover.png", OSSKey: mcpPrefix + "output/cover.png",
	}); err != nil {
		t.Fatal(err)
	}
	sentinel := &model.TaskFile{ID: "caller-sentinel"}
	backing := []*model.TaskFile{
		{Role: model.FileRoleMarkdown, FilePath: "output/article.md", FileName: "article.md", OSSKey: "workspace/article.md"},
		sentinel,
	}
	manifest := backing[:1]
	if err := repo.TaskFiles().ReplacePendingCurrentExecutionPreservingMCPArtifacts(ctx, "t1", "e1", manifest); err != nil {
		t.Fatal(err)
	}
	if backing[1] != sentinel {
		t.Fatalf("caller backing array mutated: got %#v, want sentinel %#v", backing[1], sentinel)
	}
}

func TestTaskFileRepositoryReplacePendingExecutionRejectsNilAndDuplicateEntries(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", []*model.TaskFile{nil}); err == nil {
		t.Fatal("nil task file accepted")
	}
	duplicate := []*model.TaskFile{
		{Role: model.FileRoleOther, FilePath: "output/same.md", FileName: "same.md"},
		{Role: model.FileRoleOther, FilePath: "output/same.md", FileName: "same.md"},
	}
	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", duplicate); err == nil {
		t.Fatal("duplicate task file path accepted")
	}
}

func TestTaskFileRepositoryUpsertPendingExecutionMySQLLockOrderContract(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		Logger: logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `tasks` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "current_execution_id"}).AddRow("t1", model.TaskStatusRunning, "e1"))
	mock.ExpectQuery("SELECT .* FROM `task_executions` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "status", "manifest_status"}).AddRow("e1", "t1", model.TaskExecutionRunning, model.TaskExecutionManifestPending))
	mock.ExpectExec("INSERT INTO `task_files`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT .* FROM `task_files`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "execution_id", "state", "role", "file_path", "file_name", "oss_key", "content_hash"}).
			AddRow("mcp-id", "t1", "e1", model.TaskFileStatePending, model.FileRoleImage, "output/cover.png", "cover.png", "mcp/cover.png", strings.Repeat("a", 64)))
	mock.ExpectCommit()

	repo := New(db)
	_, err = repo.TaskFiles().UpsertPendingCurrentExecution(context.Background(), "t1", "e1", &model.TaskFile{
		ID: "mcp-id", Role: model.FileRoleImage, FilePath: "output/cover.png", FileName: "cover.png",
		OSSKey: "mcp/cover.png", ContentHash: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	assertTaskFileMutationLockOrder(t, logs.String())
}

func TestTaskFileRepositoryUpdatePendingExecutionMetadataMySQLLockOrderContract(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		Logger: logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `tasks` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "current_execution_id"}).AddRow("t1", model.TaskStatusRunning, "e1"))
	mock.ExpectQuery("SELECT .* FROM `task_executions` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "status", "manifest_status"}).AddRow("e1", "t1", model.TaskExecutionRunning, model.TaskExecutionManifestPending))
	mock.ExpectQuery("SELECT .* FROM `task_files`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "execution_id", "state", "role", "file_path", "file_name", "oss_key", "content_hash"}).
			AddRow("file-1", "t1", "e1", model.TaskFileStatePending, model.FileRoleImage, "output/cover.png", "cover.png", "mcp/cover.png", strings.Repeat("a", 64)))
	mock.ExpectExec("UPDATE `task_files` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := New(db)
	updated, err := repo.TaskFiles().UpdatePendingCurrentExecutionMetadata(context.Background(), &model.TaskFile{
		ID: "file-1", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending,
		Role: model.FileRoleImage, FilePath: "output/cover.png", FileName: "cover.png",
		OSSKey: "mcp/cover.png", ContentHash: strings.Repeat("a", 64),
	}, model.FileRoleCover, "media-1", "https://wechat.example/cover.png")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Role != model.FileRoleCover || updated.MediaID != "media-1" || updated.WechatURL != "https://wechat.example/cover.png" {
		t.Fatalf("updated metadata = %#v", updated)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	assertTaskFileMutationLockOrder(t, logs.String())
}

func TestTaskFileRepositoryReplacePendingExecutionMySQLSQLContract(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		Logger: logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `tasks` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "current_execution_id"}).AddRow("t1", model.TaskStatusRunning, "e1"))
	mock.ExpectQuery("SELECT .* FROM `task_executions` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "status", "manifest_status"}).AddRow("e1", "t1", model.TaskExecutionRunning, model.TaskExecutionManifestPending))
	mock.ExpectQuery("SELECT .* FROM `task_files` WHERE task_id = \\? AND execution_id = \\? AND state = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "execution_id", "state", "role", "file_path", "file_name", "oss_key"}).
			AddRow("persisted-id", "t1", "e1", model.TaskFileStatePending, model.FileRoleMarkdown, "output/article.md", "article.md", "old-key"))
	mock.ExpectExec("DELETE FROM `task_files`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE `task_files` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO `task_files`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	repo := New(db)
	err = repo.TaskFiles().ReplacePendingCurrentExecution(context.Background(), "t1", "e1", []*model.TaskFile{
		{ID: "ignored-existing-id", Role: model.FileRoleMarkdown, FilePath: "output/article.md", FileName: "article.md", OSSKey: "new-key"},
		{ID: "ignored-new-id", Role: model.FileRoleOther, FilePath: "output/new.md", FileName: "new.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	sql := logs.String()
	if strings.Contains(sql, "ON DUPLICATE KEY UPDATE") {
		t.Fatalf("replacement used dialect-sensitive upsert:\n%s", sql)
	}
	updateAt, insertAt := strings.Index(sql, "UPDATE `task_files`"), strings.Index(sql, "INSERT INTO `task_files`")
	if updateAt < 0 || insertAt <= updateAt {
		t.Fatalf("missing ordered task-file update/insert SQL:\n%s", sql)
	}
	updateSQL := sql[updateAt:insertAt]
	for _, predicate := range []string{"id = 'persisted-id'", "task_id = 't1'", "execution_id = 'e1'", "state = 'pending'"} {
		if !strings.Contains(updateSQL, predicate) {
			t.Fatalf("scoped update missing %q:\n%s", predicate, updateSQL)
		}
	}
	assertTaskFileMutationLockOrder(t, sql)
}

func TestTaskFileRepositoryReplacePendingExecutionMySQLIdenticalRetrySkipsNoOpUpdates(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		Logger: logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `tasks` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "current_execution_id"}).AddRow("t1", model.TaskStatusRunning, "e1"))
	mock.ExpectQuery("SELECT .* FROM `task_executions` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "status", "manifest_status"}).AddRow("e1", "t1", model.TaskExecutionRunning, model.TaskExecutionManifestPending))
	mock.ExpectQuery("SELECT .* FROM `task_files` WHERE task_id = \\? AND execution_id = \\? AND state = \\?").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "task_id", "execution_id", "state", "role", "file_path", "file_name", "mime_type", "file_size",
			"content_hash", "media_id", "wechat_url", "oss_key", "oss_url", "storage_provider",
		}).AddRow(
			"persisted-id", "t1", "e1", model.TaskFileStatePending, model.FileRoleMarkdown, "output/article.md", "article.md", "text/markdown", int64(42),
			strings.Repeat("a", 64), "known-media", "https://example.com/wechat", "known-key", "https://example.com/file", "oss",
		))
	mock.ExpectExec("DELETE FROM `task_files`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	repo := New(db)
	err = repo.TaskFiles().ReplacePendingCurrentExecution(context.Background(), "t1", "e1", []*model.TaskFile{{
		ID: "ignored-id", Role: model.FileRoleMarkdown, FilePath: "output/article.md", FileName: "article.md",
		MimeType: "text/markdown", FileSize: 42, ContentHash: strings.Repeat("a", 64),
		OSSKey: "known-key", OSSURL: "https://example.com/file", StorageProvider: "oss",
		MediaID: "known-media", WechatURL: "https://example.com/wechat",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	sql := logs.String()
	if strings.Contains(sql, "UPDATE `task_files`") || strings.Contains(sql, "UPDATE `task_executions`") {
		t.Fatalf("identical retry emitted no-op update:\n%s", sql)
	}
}
