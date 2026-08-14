package service

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAgentFeedbackCreateAllowsLocalTaskID(t *testing.T) {
	repo := setupAgentFeedbackRepo(t)
	svc := NewAgentFeedbackService(repo, nil)

	feedback, err := svc.Create(context.Background(), "local-video-"+uuid.NewString(), "video", `{"quality":8}`, "", "", "local run")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if feedback.ID == "" || !strings.HasPrefix(feedback.TaskID, "local-video-") {
		t.Fatalf("unexpected feedback: %#v", feedback)
	}
}

func TestAgentFeedbackCreateIsIdempotentPerTaskAndAgent(t *testing.T) {
	repo := setupAgentFeedbackRepo(t)
	svc := NewAgentFeedbackService(repo, nil)
	ctx := context.Background()
	taskID := "local-idempotent-" + uuid.NewString()

	first, err := svc.Create(ctx, taskID, "seednote", `{"quality":8}`, "", "", "done")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	second, err := svc.Create(ctx, taskID, "seednote", `{"quality":8}`, "", "", "done")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("feedback IDs differ: first=%s second=%s", first.ID, second.ID)
	}
	rows, err := svc.FindByTaskID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("feedback row count = %d, want 1", len(rows))
	}
}

func TestAgentFeedbackCreateUpdatesLatestPayload(t *testing.T) {
	repo := setupAgentFeedbackRepo(t)
	svc := NewAgentFeedbackService(repo, nil)
	ctx := context.Background()
	taskID := "local-update-" + uuid.NewString()

	first, err := svc.Create(ctx, taskID, "montage", `{"quality":5}`, "first error", "first optimization", "first summary")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(ctx, taskID, "montage", `{"quality":9,"completeness":8,"efficiency":7}`, "", "latest optimization", "latest summary")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("feedback IDs differ: first=%s second=%s", first.ID, second.ID)
	}
	rows, err := svc.FindByTaskID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("feedback row count = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.Scores != second.Scores || got.Errors != "" || got.Optimizations != "latest optimization" || got.Summary != "latest summary" {
		t.Fatalf("stored payload = %#v, want latest complete payload %#v", got, second)
	}
}

func TestAgentFeedbackCreateUsesTrimmedBusinessKey(t *testing.T) {
	repo := setupAgentFeedbackRepo(t)
	svc := NewAgentFeedbackService(repo, nil)
	ctx := context.Background()
	taskID := "local-trim-" + uuid.NewString()

	first, err := svc.Create(ctx, "  "+taskID+"  ", " seednote ", `{"quality":8}`, "", "", "first")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if first.TaskID != taskID || first.AgentName != "seednote" {
		t.Fatalf("stored business key = (%q, %q), want (%q, %q)", first.TaskID, first.AgentName, taskID, "seednote")
	}
	second, err := svc.Create(ctx, taskID, "seednote", `{"quality":9}`, "", "", "latest")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("feedback IDs differ after trimming: first=%s second=%s", first.ID, second.ID)
	}
	rows, err := svc.FindByTaskID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("feedback row count = %d, want 1", len(rows))
	}
}

func TestAgentFeedbackDatabaseRejectsDuplicateBusinessKey(t *testing.T) {
	_, db := setupAgentFeedbackRepoAndDB(t)
	ctx := context.Background()
	taskID := "local-unique-" + uuid.NewString()
	first := &model.AgentFeedback{ID: uuid.NewString(), TaskID: taskID, AgentName: "moments"}
	second := &model.AgentFeedback{ID: uuid.NewString(), TaskID: taskID, AgentName: "moments"}
	if err := db.WithContext(ctx).Create(first).Error; err != nil {
		t.Fatalf("create first row: %v", err)
	}
	if err := db.WithContext(ctx).Create(second).Error; err == nil {
		t.Fatal("database accepted duplicate (task_id, agent_name)")
	}
}

func TestAgentFeedbackConcurrentUpsertReturnsCanonicalCompletePayload(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "feedback.db") + "?_busy_timeout=10000&_journal_mode=WAL"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { sqlDB.Close() })

	svc := NewAgentFeedbackService(repository.New(db), nil)
	const submissions = 12
	taskID := "local-concurrent-" + uuid.NewString()
	ids := make(chan string, submissions)
	errs := make(chan error, submissions)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < submissions; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			token := fmt.Sprintf("submission-%02d", i)
			scores := fmt.Sprintf(`{"quality":%d,"submission":%d}`, i%10+1, i)
			feedback, err := svc.Create(context.Background(), taskID, "seednote", scores, "errors-"+token, "optimizations-"+token, "summary-"+token)
			if err != nil {
				errs <- err
				return
			}
			ids <- feedback.ID
		}(i)
	}
	close(start)
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Create: %v", err)
	}
	var canonicalID string
	for id := range ids {
		if canonicalID == "" {
			canonicalID = id
		}
		if id != canonicalID {
			t.Fatalf("feedback ID = %s, want canonical %s", id, canonicalID)
		}
	}
	rows, err := svc.FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != canonicalID {
		t.Fatalf("rows = %#v, want one canonical row", rows)
	}
	var scores map[string]int
	if err := json.Unmarshal([]byte(rows[0].Scores), &scores); err != nil {
		t.Fatal(err)
	}
	token := fmt.Sprintf("submission-%02d", scores["submission"])
	if rows[0].Errors != "errors-"+token || rows[0].Optimizations != "optimizations-"+token || rows[0].Summary != "summary-"+token {
		t.Fatalf("final payload is torn: %#v", rows[0])
	}
}

func setupAgentFeedbackRepo(t *testing.T) repository.Repository {
	t.Helper()
	repo, _ := setupAgentFeedbackRepoAndDB(t)
	return repo
}

func setupAgentFeedbackRepoAndDB(t *testing.T) (repository.Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return repository.New(db), db
}
