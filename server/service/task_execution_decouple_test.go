package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	gormlogger "gorm.io/gorm/logger"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/config"
	projectmemory "github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// cancelingExecutor cancels the parent execution ctx the moment Execute runs,
// then returns a successful result. This reproduces the production failure
// mode where the asynq deadline expires mid-pipeline: by the time the
// post-execution finalize phase runs, the execution ctx is dead. The finalize
// writes must use the decoupled persistCtx (not the expired ctx), otherwise
// the task stays stuck in "running" and all completed work is lost.
type cancelingExecutor struct {
	parentCancel context.CancelFunc
	result       *agent.ExecutionResult
	err          error
}

func (e *cancelingExecutor) Execute(ctx context.Context, opts *agent.ExecutionOptions) (*agent.ExecutionResult, error) {
	e.parentCancel()
	return e.result, e.err
}

func TestHandleExecutionNilResultFailsTaskWithoutPanic(t *testing.T) {
	tests := []struct {
		name         string
		execErr      error
		wantErrorMsg string
	}{
		{
			name:         "executor error",
			execErr:      errors.New("executor setup failed"),
			wantErrorMsg: "executor setup failed",
		},
		{
			name:         "missing result without error",
			wantErrorMsg: "agent returned no execution result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTaskTestDB(t)
			t.Cleanup(func() {
				sqlDB, _ := db.DB()
				if sqlDB != nil {
					sqlDB.Close()
				}
			})
			repo := repository.New(db)
			logger := zerolog.New(io.Discard)
			ctx := context.Background()
			userID := uuid.NewString()
			project := &model.Project{
				ID:       uuid.NewString(),
				UserID:   userID,
				Platform: model.PlatformArticle,
			}
			task := &model.Task{
				ID:     uuid.NewString(),
				UserID: userID,
				Type:   model.PlatformArticle,
				Status: model.TaskStatusRunning,
			}
			if err := repo.Tasks().Create(ctx, task); err != nil {
				t.Fatalf("create task: %v", err)
			}

			svc := NewTaskService(repo, &fakeTaskExecutor{err: tt.execErr}, &mockEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
			if err := svc.HandleExecution(ctx, task, project); err != nil {
				t.Fatalf("HandleExecution: %v", err)
			}

			found, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatalf("find task: %v", err)
			}
			if found.Status != model.TaskStatusFailed {
				t.Fatalf("status = %q, want %q", found.Status, model.TaskStatusFailed)
			}
			if found.CompletedAt == nil {
				t.Fatal("completed_at is nil")
			}
			if !strings.Contains(found.ErrorMessage, tt.wantErrorMsg) {
				t.Fatalf("error_message = %q, want it to contain %q", found.ErrorMessage, tt.wantErrorMsg)
			}
			if found.Result == nil {
				t.Fatal("nil executor result was not normalized and persisted")
			}
			var persisted agent.ExecutionResult
			if err := json.Unmarshal([]byte(*found.Result), &persisted); err != nil {
				t.Fatalf("decode persisted result: %v", err)
			}
			if persisted.Success || persisted.Error != "agent returned no execution result" {
				t.Fatalf("persisted nil result = %+v, want stable terminal failure", persisted)
			}
			if persisted.CostStatus != "" || found.CostStatus != agent.CostStatusUnreconciled {
				t.Fatalf("cost status = public result %q internal task %q", persisted.CostStatus, found.CostStatus)
			}
			if len(persisted.ModelUsage) != 0 || len(found.TerminalModelUsage.Data()) != 0 {
				t.Fatalf("nil terminal fabricated usage: result=%+v task=%+v", persisted.ModelUsage, found.TerminalModelUsage.Data())
			}
		})
	}
}

func TestHandleExecutionCancellationTakesPrecedenceOverNilResult(t *testing.T) {
	tests := []struct {
		name    string
		execErr error
	}{
		{name: "without executor error"},
		{name: "with executor error", execErr: errors.New("executor interrupted")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTaskTestDB(t)
			t.Cleanup(func() {
				sqlDB, _ := db.DB()
				if sqlDB != nil {
					sqlDB.Close()
				}
			})
			repo := repository.New(db)
			logger := zerolog.New(io.Discard)
			ctx, cancel := context.WithCancel(context.Background())
			userID := uuid.NewString()
			task := &model.Task{
				ID:     uuid.NewString(),
				UserID: userID,
				Type:   model.PlatformArticle,
				Status: model.TaskStatusRunning,
			}
			if err := repo.Tasks().Create(context.Background(), task); err != nil {
				t.Fatalf("create task: %v", err)
			}
			project := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle}
			exec := &cancelingExecutor{parentCancel: cancel, err: tt.execErr}
			svc := NewTaskService(repo, exec, &mockEnqueuer{}, nil, &logger, "", nil, "", nil, nil)

			if err := svc.HandleExecution(ctx, task, project); err != nil {
				t.Fatalf("HandleExecution: %v", err)
			}

			found, err := repo.Tasks().FindByID(context.Background(), task.ID)
			if err != nil {
				t.Fatalf("find task: %v", err)
			}
			if found.Status != model.TaskStatusCancelled {
				t.Fatalf("status = %q, want %q", found.Status, model.TaskStatusCancelled)
			}
			if found.CompletedAt == nil {
				t.Fatal("completed_at is nil")
			}
			if !strings.Contains(found.ErrorMessage, context.Canceled.Error()) {
				t.Fatalf("error_message = %q, want cancellation reason", found.ErrorMessage)
			}
			if strings.Contains(found.ErrorMessage, "executor interrupted") {
				t.Fatalf("error_message = %q, executor error overrode cancellation", found.ErrorMessage)
			}
		})
	}
}

func TestHandleExecutionPersistsPartialResultBeforeExecutorFailure(t *testing.T) {
	db := setupTaskTestDB(t)
	db.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	ctx := context.Background()
	userID := uuid.NewString()
	task := &model.Task{
		ID:     uuid.NewString(),
		UserID: userID,
		Type:   model.PlatformArticle,
		Status: model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	project := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle}
	workDir := t.TempDir()
	writeWorkspaceFile(t, workDir, "output/partial.md", "# partial result")
	result := &agent.ExecutionResult{WorkDir: workDir, Model: "partial-model"}
	store := &fakeAudioASRStorage{name: "oss", files: map[string][]byte{}}
	svc := NewTaskService(repo, &fakeTaskExecutor{
		result: result,
		err:    errors.New("executor failed after producing output"),
	}, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, project); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}

	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want %q", found.Status, model.TaskStatusFailed)
	}
	if found.Result == nil || strings.Contains(*found.Result, `"model":"partial-model"`) {
		t.Fatalf("result = %v, want persisted partial result without internal model identity", found.Result)
	}
	if !strings.Contains(found.ErrorMessage, "executor failed after producing output") {
		t.Fatalf("error_message = %q, want executor error", found.ErrorMessage)
	}
	files, err := repo.TaskFiles().FindByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task files: %v", err)
	}
	if len(files) != 1 || files[0].FilePath != "output/partial.md" {
		t.Fatalf("task files = %+v, want persisted partial output", files)
	}
}

// TestHandleExecution_PersistsOutcomeOnExpiredContext is the regression test for
// the "context deadline exceeded" cascade. Before the fix, the cancel-path DB
// writes ran on the (now-expired) asynq ctx and silently failed, leaving the
// task stuck in "running". With persistCtx, the task reaches a terminal state.
func TestHandleExecution_PersistsOutcomeOnExpiredContext(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	bgCtx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	// Workspace with a real output file so uploadMissingTaskFiles has work to do.
	workDir := t.TempDir()
	outputDir := filepath.Join(workDir, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "content.md"), []byte("# 标题\n\n正文"), 0644); err != nil {
		t.Fatalf("write content: %v", err)
	}

	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(bgCtx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	execCtx, cancel := context.WithCancel(context.Background())
	exec := &cancelingExecutor{
		parentCancel: cancel,
		result:       &agent.ExecutionResult{Success: true, WorkDir: workDir},
	}
	svc := NewTaskService(repo, exec, &mockEnqueuer{}, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(execCtx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}

	found, err := repo.Tasks().FindByID(bgCtx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	// execCtx was canceled, so the task lands in "cancelled" — the point is it
	// reached a TERMINAL state. Without persistCtx, the writes would run on the
	// dead ctx and the task would stay "running" (the original bug).
	if found.Status == model.TaskStatusRunning {
		t.Fatalf("task stuck in running — post-execution writes failed on expired ctx (decoupling bug); status=%q", found.Status)
	}
	if found.CompletedAt == nil {
		t.Fatalf("completed_at not set — cancel-path writes failed on expired ctx; status=%q", found.Status)
	}
}

func TestHandleExecutionRemoteArtifactsSkipsHostWorkspaceUpload(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	store := &fakeAudioASRStorage{name: "oss", files: map[string][]byte{}}
	createRemoteTaskFile(t, repo, store, task.ID, "output/article.md", "text/markdown", "# remote article")
	workDir := t.TempDir()
	writeWorkspaceFile(t, workDir, "output/local-only.md", "# should not be uploaded by server")
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success:         true,
		WorkDir:         workDir,
		RemoteArtifacts: true,
		ToolUseSummary:  map[string]int{"Bash": 1},
	}}, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	files, err := repo.TaskFiles().FindByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find files: %v", err)
	}
	var paths []string
	for _, file := range files {
		paths = append(paths, file.FilePath)
	}
	sort.Strings(paths)
	if got, want := strings.Join(paths, ","), "output/article.md"; got != want {
		t.Fatalf("task file paths = %q, want only remote manifest file %q", got, want)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusCompleted {
		t.Fatalf("status = %q, want completed; err=%q", found.Status, found.ErrorMessage)
	}
}

func TestHandleExecutionRemoteSeednoteValidatesTaskFilesWithoutWorkDir(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:              uuid.New().String(),
		UserID:          userID,
		ProjectID:       projectID,
		Type:            model.PlatformSeednote,
		Status:          model.TaskStatusRunning,
		HasContentImage: true,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	store := &fakeAudioASRStorage{name: "oss", files: map[string][]byte{}}
	for _, name := range seednoteCompletionArtifactNamesForTest(true, false) {
		mimeType, body := DetectTaskFileMIME(name), "fixture"
		if strings.HasSuffix(name, ".png") {
			body = "png"
		}
		createRemoteTaskFile(t, repo, store, task.ID, "output/"+name, mimeType, body)
	}
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success:         true,
		RemoteArtifacts: true,
		ToolUseSummary:  map[string]int{"generate_image": 2, "Bash": 1},
	}}, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusCompleted {
		t.Fatalf("status = %q, want completed; err=%q", found.Status, found.ErrorMessage)
	}
}

func TestHandleExecutionRemoteArticleApprovalReadsDraftFromTaskFiles(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Remote approval",
		Status:   model.ProjectStatusActive,
		Config: model.ProjectConfig{
			EnablePublishing:       true,
			RequirePublishApproval: true,
		},
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	store := &fakeAudioASRStorage{name: "oss", files: map[string][]byte{}}
	createRemoteTaskFile(t, repo, store, task.ID, "output/draft.json", "application/json", `{"articles":[{"title":"远端标题","content":"<p>远端正文</p>"}]}`)
	pubSvc := NewPublishingService(repo, &logger)
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success:         true,
		RemoteArtifacts: true,
		ToolUseSummary:  map[string]int{"Bash": 1},
	}}, &mockEnqueuer{}, store, &logger, "", nil, "", nil, pubSvc)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusCompleted {
		t.Fatalf("status = %q, want completed; err=%q", found.Status, found.ErrorMessage)
	}
	if found.PublishApprovalState != model.PublishApprovalStatePending {
		t.Fatalf("publish approval state = %q, want pending", found.PublishApprovalState)
	}
	var articles []DraftArticleInput
	if err := json.Unmarshal(found.PendingDraftArticles, &articles); err != nil {
		t.Fatalf("unmarshal pending articles: %v", err)
	}
	if len(articles) != 1 || articles[0].Title != "远端标题" || articles[0].Content != "<p>远端正文</p>" {
		t.Fatalf("pending articles = %+v, want remote draft data", articles)
	}
}

func TestHandleExecutionRemoteArtifactsMergesRemoteProjectMemory(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	store := &fakeAudioASRStorage{name: "oss", files: map[string][]byte{}}
	createRemoteTaskFile(t, repo, store, task.ID, "output/article.md", "text/markdown", "# remote article")
	memoryArchive := mustRemoteMemoryArchive(t, map[string]string{
		"MEMORY.md": "# Remote memory\n",
	})
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success:             true,
		RemoteArtifacts:     true,
		RemoteMemoryArchive: memoryArchive,
		ToolUseSummary:      map[string]int{"Bash": 1},
	}}, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)

	svc.SetProjectMemoryManager(projectmemory.NewProjectMemoryManager(store, config.MemoryConfig{
		Enabled:         true,
		OSSPrefix:       "claude-memory/projects",
		RuntimeDir:      ".claude/memory",
		MaxArchiveBytes: 262144,
	}, nil, logger))

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	if _, ok := store.files["claude-memory/projects/"+projectID+"/current.tar.gz"]; !ok {
		t.Fatal("remote project memory current archive was not uploaded")
	}
	manifest := string(store.files["claude-memory/projects/"+projectID+"/manifest.json"])
	if !strings.Contains(manifest, `"last_task_id":"`+task.ID+`"`) {
		t.Fatalf("memory manifest = %s, want task id %s", manifest, task.ID)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusCompleted {
		t.Fatalf("status = %q, want completed; err=%q", found.Status, found.ErrorMessage)
	}
}

func createRemoteTaskFile(t *testing.T, repo repository.Repository, store *fakeAudioASRStorage, taskID, relPath, mimeType, body string) {
	t.Helper()
	key := "uploads/test/" + taskID + "/" + relPath
	if store.files == nil {
		store.files = map[string][]byte{}
	}
	store.files[key] = []byte(body)
	sum := sha256.Sum256([]byte(body))
	if err := repo.TaskFiles().Create(context.Background(), &model.TaskFile{
		TaskID:          taskID,
		Role:            DetermineTaskFileRole(filepath.Base(relPath), mimeType),
		FilePath:        relPath,
		FileName:        filepath.Base(relPath),
		MimeType:        mimeType,
		FileSize:        int64(len(body)),
		ContentHash:     hex.EncodeToString(sum[:]),
		OSSKey:          key,
		OSSURL:          store.GetURL(key),
		StorageProvider: store.Name(),
	}); err != nil {
		t.Fatalf("create remote task file %s: %v", relPath, err)
	}
}

func mustRemoteMemoryArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		data := []byte(body)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
