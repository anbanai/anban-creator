package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type assistantInteractionTestRuntime struct{}

func (assistantInteractionTestRuntime) Command(context.Context, string, any) error { return nil }
func (assistantInteractionTestRuntime) Close() error                               { return nil }

type assistantInteractionTestFactory struct {
	mu        sync.Mutex
	starts    int
	callbacks map[string]func(string, any)
}

func (f *assistantInteractionTestFactory) Start(_ context.Context, sessionID, _ string, _ string, callback func(string, any)) (Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.callbacks == nil {
		f.callbacks = map[string]func(string, any){}
	}
	f.starts++
	f.callbacks[sessionID] = callback
	return assistantInteractionTestRuntime{}, nil
}

func (f *assistantInteractionTestFactory) callback(sessionID string) func(string, any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callbacks[sessionID]
}

func newAssistantInteractionServiceTest(t *testing.T) (*AssistantInteractionService, *assistantInteractionTestFactory) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	factory := &assistantInteractionTestFactory{}
	clock := func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	return NewAssistantInteractionService(repository.New(db), factory, clock), factory
}

func TestAssistantInteractionCreateOrResumeReusesScope(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	input := AssistantInteractionInput{ModeID: "creation", ProjectID: "project-1", ContentID: "content-1", AgentID: "project-creator", Intent: "create"}
	first, err := svc.CreateOrResume(context.Background(), "user-1", input, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateOrResume(context.Background(), "user-1", input, "attempt-2")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Generation != 1 || factory.starts != 1 {
		t.Fatalf("first=%#v second=%#v starts=%d", first, second, factory.starts)
	}
}

func TestAssistantInteractionGenerationRejectsStaleCommand(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	input := AssistantInteractionInput{ModeID: "creation", AgentID: "project-creator", Intent: "create"}
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", input, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplaceRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation, "runtime_closed"); err != nil {
		t.Fatal(err)
	}
	err = svc.RunCommand(context.Background(), "user-1", snapshot.ID, snapshot.Generation, InteractionOperationMessage, "attempt-message", func(RuntimeBindingView) error { return nil })
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorGenerationChanged {
		t.Fatalf("stale command error = %v", err)
	}
}

func TestAssistantInteractionCommandsAreSerialized(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: "creation", AgentID: "project-creator", Intent: "create"}, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	var active int32
	var maxActive int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_ = svc.RunCommand(context.Background(), "user-1", snapshot.ID, snapshot.Generation, InteractionOperationMessage, NewInteractionAttemptID(), func(RuntimeBindingView) error {
				current := atomic.AddInt32(&active, 1)
				for {
					previous := atomic.LoadInt32(&maxActive)
					if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				atomic.AddInt32(&active, -1)
				return nil
			})
		}(i)
	}
	wg.Wait()
	if maxActive != 1 {
		t.Fatalf("max concurrent commands = %d, want 1", maxActive)
	}
}

func TestAssistantInteractionReplacementDropsOldRuntimeCallbacks(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: "creation", AgentID: "project-creator", Intent: "create"}, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	oldRuntime, err := svc.RuntimeBindingForTest(context.Background(), "user-1", snapshot.ID, snapshot.Generation)
	if err != nil {
		t.Fatal(err)
	}
	oldCallback := factory.callback(oldRuntime.LogicalSessionID)
	if oldCallback == nil {
		t.Fatal("missing old runtime callback")
	}
	if _, err := svc.ReplaceRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation, "runtime_closed"); err != nil {
		t.Fatal(err)
	}
	if accepted := svc.AcceptRuntimeEvent(snapshot.ID, snapshot.Generation, "draft.ready", map[string]any{"ready": true}); accepted {
		t.Fatal("stale runtime event was accepted")
	}
	if accepted := svc.AcceptRuntimeEvent(snapshot.ID, snapshot.Generation+1, "runtime.ready", map[string]any{}); !accepted {
		t.Fatal("current runtime event was rejected")
	}
	oldCallback("draft.ready", map[string]any{"ready": true})
}
