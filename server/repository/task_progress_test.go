package repository

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/model"
)

func createStructuredProgressTask(t *testing.T, repo Repository) *model.Task {
	t.Helper()
	executionID := uuid.NewString()
	task := &model.Task{
		ID: uuid.NewString(), UserID: uuid.NewString(), Type: model.PlatformMoments, Prompt: "progress",
		Status: model.TaskStatusRunning, CurrentExecutionID: &executionID,
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func TestTaskRepositoryAdvanceStructuredProgressRejectsLowerAndDuplicateEvents(t *testing.T) {
	repo := New(setupTestDB(t))
	task := createStructuredProgressTask(t, repo)
	ctx := context.Background()
	high := model.ProgressPayload{Stage: "quality_review", State: "complete", Title: "质量复盘", Description: "完成复盘", Percent: 88}
	advanced, persistedPayload, err := repo.Tasks().AdvanceStructuredProgress(ctx, task.ID, *task.CurrentExecutionID, 10, high)
	if err != nil || !advanced {
		t.Fatalf("advance high = %v, %v", advanced, err)
	}
	if persistedPayload != high {
		t.Fatalf("persisted payload = %#v, want %#v", persistedPayload, high)
	}
	for _, item := range []struct {
		sequence int
		payload  model.ProgressPayload
	}{
		{sequence: 6, payload: model.ProgressPayload{Stage: "writing", State: "complete", Title: "朋友圈正文", Description: "迟到事件", Percent: 55}},
		{sequence: 10, payload: high},
	} {
		advanced, _, err = repo.Tasks().AdvanceStructuredProgress(ctx, task.ID, *task.CurrentExecutionID, item.sequence, item.payload)
		if err != nil || advanced {
			t.Fatalf("advance stale %+v = %v, %v", item.payload, advanced, err)
		}
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ProgressSequence != 10 || persisted.Progress != 88 || persisted.LatestProgress.Data() != high {
		t.Fatalf("persisted high water = sequence:%d progress:%d payload:%#v", persisted.ProgressSequence, persisted.Progress, persisted.LatestProgress.Data())
	}
	if strings.Count(persisted.ProgressLog, "quality_review") != 1 || strings.Contains(persisted.ProgressLog, "迟到事件") {
		t.Fatalf("progress log = %q", persisted.ProgressLog)
	}
}

func TestTaskRepositoryAdvanceStructuredProgressIsAtomicHighWater(t *testing.T) {
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	repo := New(db)
	task := createStructuredProgressTask(t, repo)
	items := []struct {
		sequence int
		payload  model.ProgressPayload
	}{
		{sequence: 10, payload: model.ProgressPayload{Stage: "quality_review", State: "complete", Title: "质量复盘", Percent: 88}},
		{sequence: 6, payload: model.ProgressPayload{Stage: "writing", State: "complete", Title: "朋友圈正文", Percent: 55}},
	}
	start := make(chan struct{})
	errs := make(chan error, len(items))
	var wg sync.WaitGroup
	for _, item := range items {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := repo.Tasks().AdvanceStructuredProgress(context.Background(), task.ID, *task.CurrentExecutionID, item.sequence, item.payload)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ProgressSequence != 10 || persisted.Progress != 88 || persisted.LatestProgress.Data().Percent != 88 || persisted.LatestProgress.Data().Stage != "quality_review" {
		t.Fatalf("concurrent high water = sequence:%d progress:%d payload:%#v", persisted.ProgressSequence, persisted.Progress, persisted.LatestProgress.Data())
	}
}

func TestTaskRepositoryAdvanceStructuredProgressPersistsZeroPercent(t *testing.T) {
	repo := New(setupTestDB(t))
	task := createStructuredProgressTask(t, repo)
	payload := model.ProgressPayload{Stage: "first", State: "active", Title: "First", Percent: 0}
	advanced, persistedPayload, err := repo.Tasks().AdvanceStructuredProgress(context.Background(), task.ID, *task.CurrentExecutionID, 1, payload)
	if err != nil || !advanced || persistedPayload != payload {
		t.Fatalf("advance zero = %v/%#v/%v", advanced, persistedPayload, err)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ProgressSequence != 1 || persisted.Progress != 0 || persisted.LatestProgress.Data() != payload {
		t.Fatalf("zero progress = sequence:%d progress:%d payload:%#v", persisted.ProgressSequence, persisted.Progress, persisted.LatestProgress.Data())
	}
}

func TestTaskRepositoryAdvanceStructuredProgressRetainsMaximumPercentInLaterPayload(t *testing.T) {
	repo := New(setupTestDB(t))
	task := createStructuredProgressTask(t, repo)
	ctx := context.Background()

	first := model.ProgressPayload{Stage: "first", State: "complete", Title: "First", Percent: 88}
	if advanced, _, err := repo.Tasks().AdvanceStructuredProgress(ctx, task.ID, *task.CurrentExecutionID, 1, first); err != nil || !advanced {
		t.Fatalf("advance first = %v, %v", advanced, err)
	}
	later := model.ProgressPayload{Stage: "second", State: "active", Title: "Second", Percent: 55}
	advanced, persistedPayload, err := repo.Tasks().AdvanceStructuredProgress(ctx, task.ID, *task.CurrentExecutionID, 2, later)
	if err != nil || !advanced {
		t.Fatalf("advance later = %v, %v", advanced, err)
	}
	if persistedPayload.Stage != "second" || persistedPayload.Percent != 88 {
		t.Fatalf("persisted later payload = %#v", persistedPayload)
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ProgressSequence != 2 || persisted.Progress != 88 || persisted.LatestProgress.Data() != persistedPayload {
		t.Fatalf("retained maximum = sequence:%d progress:%d payload:%#v", persisted.ProgressSequence, persisted.Progress, persisted.LatestProgress.Data())
	}
	if !strings.Contains(persisted.ProgressLog, `"stage":"second","state":"active","title":"Second","percent":88`) {
		t.Fatalf("progress log = %q", persisted.ProgressLog)
	}
}

func TestTaskRepositoryAdvanceStructuredProgressRejectsReplacedExecutionWithoutMutation(t *testing.T) {
	repo := New(setupTestDB(t))
	task := createStructuredProgressTask(t, repo)
	ctx := context.Background()
	originalExecutionID := *task.CurrentExecutionID
	initial := model.ProgressPayload{Stage: "first", State: "complete", Title: "First", Percent: 25}
	if advanced, _, err := repo.Tasks().AdvanceStructuredProgress(ctx, task.ID, originalExecutionID, 1, initial); err != nil || !advanced {
		t.Fatalf("advance initial = %v, %v", advanced, err)
	}
	before, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if swapped, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, uuid.NewString()); err != nil || !swapped {
		t.Fatalf("replace current execution = %v, %v", swapped, err)
	}

	advanced, _, err := repo.Tasks().AdvanceStructuredProgress(ctx, task.ID, originalExecutionID, 2, model.ProgressPayload{
		Stage: "second", State: "complete", Title: "Second", Description: "stale execution", Percent: 75,
	})
	if err != nil || advanced {
		t.Fatalf("advance replaced execution = %v, %v", advanced, err)
	}
	assertStructuredProgressUnchanged(t, repo, before)
}

func TestTaskRepositoryAdvanceStructuredProgressRejectsTerminalTaskWithoutMutation(t *testing.T) {
	for _, status := range []string{model.TaskStatusCancelled, model.TaskStatusCompleted, model.TaskStatusFailed} {
		t.Run(status, func(t *testing.T) {
			repo := New(setupTestDB(t))
			task := createStructuredProgressTask(t, repo)
			ctx := context.Background()
			executionID := *task.CurrentExecutionID
			initial := model.ProgressPayload{Stage: "first", State: "complete", Title: "First", Percent: 25}
			if advanced, _, err := repo.Tasks().AdvanceStructuredProgress(ctx, task.ID, executionID, 1, initial); err != nil || !advanced {
				t.Fatalf("advance initial = %v, %v", advanced, err)
			}
			before, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Tasks().UpdateStatus(ctx, task.ID, status); err != nil {
				t.Fatal(err)
			}

			advanced, _, err := repo.Tasks().AdvanceStructuredProgress(ctx, task.ID, executionID, 2, model.ProgressPayload{
				Stage: "second", State: "complete", Title: "Second", Description: "terminal task", Percent: 75,
			})
			if err != nil || advanced {
				t.Fatalf("advance terminal task = %v, %v", advanced, err)
			}
			assertStructuredProgressUnchanged(t, repo, before)
		})
	}
}

func assertStructuredProgressUnchanged(t *testing.T, repo Repository, before *model.Task) {
	t.Helper()
	after, err := repo.Tasks().FindByID(context.Background(), before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ProgressSequence != before.ProgressSequence ||
		after.Progress != before.Progress ||
		after.LatestProgress.Data() != before.LatestProgress.Data() ||
		after.ProgressLog != before.ProgressLog {
		t.Fatalf("structured progress changed: before=%d/%d/%#v/%q after=%d/%d/%#v/%q",
			before.ProgressSequence, before.Progress, before.LatestProgress.Data(), before.ProgressLog,
			after.ProgressSequence, after.Progress, after.LatestProgress.Data(), after.ProgressLog)
	}
}
