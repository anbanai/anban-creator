package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
)

func TestInteractionSessionsReceiveFrozenProjectProfile(t *testing.T) {
	for _, mode := range []string{AssistantModeProfile, AssistantModeCreation, AssistantModeReview} {
		t.Run(mode, func(t *testing.T) {
			svc, _ := newAssistantInteractionServiceTest(t)
			factory := &assistantInteractionModeFactory{}
			svc.factory = factory
			project := createAssistantProject(t, svc.repo, "user-1", "Frozen project")
			profile := projectProfileFor(project)
			profile.Dimensions.Identity.Content["summary"] = "Confirmed account identity"
			project.Profile = datatypes.NewJSONType(profile)
			if err := svc.repo.Projects().Update(context.Background(), project); err != nil {
				t.Fatal(err)
			}
			snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: mode, ProjectID: project.ID}, "profile-context")
			if err != nil {
				t.Fatal(err)
			}
			commands := factory.commandSnapshot()
			initialContext, _ := commands[0].params["initialContext"].(string)
			var frozen struct {
				Project struct{ ID string }              `json:"project"`
				Files   []struct{ Path, Content string } `json:"files"`
			}
			if err := json.Unmarshal([]byte(initialContext), &frozen); err != nil {
				t.Fatalf("session received no project snapshot: %v", err)
			}
			if frozen.Project.ID != project.ID || !strings.Contains(initialContext, "Confirmed account identity") {
				t.Fatalf("wrong project snapshot: %s", initialContext)
			}
			for _, name := range []string{"identity", "style", "audience", "platforms", "preferences", "memory"} {
				found := false
				for _, file := range frozen.Files {
					found = found || file.Path == "profile/"+name+".md" && file.Content != ""
				}
				if !found {
					t.Errorf("snapshot omitted profile/%s.md", name)
				}
			}
			if factory.specs[0].InitialContext != "" {
				t.Fatal("session context must not be shared through runtime boot")
			}
			profile.Dimensions.Identity.Content["summary"] = "Later profile edit"
			project.Profile = datatypes.NewJSONType(profile)
			if err := svc.repo.Projects().Update(context.Background(), project); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.EnsureRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation); err != nil {
				t.Fatal(err)
			}
			if len(factory.commandSnapshot()) != 1 {
				t.Fatal("an active session must retain its frozen snapshot after a profile edit")
			}
			nextMode := AssistantModeCreation
			if mode == nextMode {
				nextMode = AssistantModeProfile
			}
			if _, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: nextMode, ProjectID: project.ID, ResumeInteractionID: snapshot.ID}, "next-mode"); err != nil {
				t.Fatal(err)
			}
			commands = factory.commandSnapshot()
			if len(commands) != 2 || len(factory.specs) != 1 {
				t.Fatal("mode switch must create a logical session in the shared runtime")
			}
			nextContext, _ := commands[1].params["initialContext"].(string)
			if !strings.Contains(nextContext, "Later profile edit") {
				t.Fatalf("new mode received no current project snapshot: %s", nextContext)
			}
		})
	}
}

func TestInteractionProjectChangeStartsSeparateLogicalSession(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionModeFactory{}
	svc.factory = factory
	firstProject := createAssistantProject(t, svc.repo, "user-1", "First project")
	secondProject := createAssistantProject(t, svc.repo, "user-1", "Second project")
	first, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: firstProject.ID}, "first-project")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: secondProject.ID, ResumeInteractionID: first.ID}, "second-project")
	if err != nil {
		t.Fatal(err)
	}
	commands := factory.commandSnapshot()
	if len(commands) != 2 || commands[0].params["sessionId"] == commands[1].params["sessionId"] {
		t.Fatalf("project switch reused a logical session: %#v", commands)
	}
	contextText, _ := commands[1].params["initialContext"].(string)
	if !strings.Contains(contextText, secondProject.ID) || strings.Contains(contextText, firstProject.ID) {
		t.Fatalf("project switch received wrong context: %s", contextText)
	}
	if second.Generation <= first.Generation || len(factory.specs) != 2 || factory.specs[1].ProjectID != secondProject.ID {
		t.Fatalf("project switch retained old runtime mount: %#v", factory.specs)
	}
	if second.Lifecycle != model.AssistantInteractionActive {
		t.Fatalf("project switch returned lifecycle %q after reconnecting", second.Lifecycle)
	}
	if _, _, _, _, _, _, err := svc.ResolveRuntimeCapability(factory.specs[0].RuntimeToken); err == nil {
		t.Fatal("old project runtime capability remains valid")
	}
	_, _, _, projectID, _, _, err := svc.ResolveRuntimeCapability(factory.specs[1].RuntimeToken)
	if err != nil || projectID != secondProject.ID {
		t.Fatalf("runtime capability project = %q, %v", projectID, err)
	}
}

func TestInteractionReviewReceivesSelectedWorkContext(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionModeFactory{}
	svc.factory = factory
	project := createAssistantProject(t, svc.repo, "user-1", "Review project")
	for _, title := range []string{"First delivered work", "Second delivered work"} {
		work := &model.Task{ID: uuid.NewString(), UserID: "user-1", ProjectID: project.ID, Type: model.PlatformWechat, Status: model.TaskStatusCompleted, Title: title}
		if err := svc.repo.Tasks().Create(context.Background(), work); err != nil {
			t.Fatal(err)
		}
		store := &streamArtifactStorage{objects: map[string][]byte{"content.md": []byte("Delivered body: " + title)}}
		if err := svc.repo.TaskFiles().Create(context.Background(), &model.TaskFile{ID: uuid.NewString(), TaskID: work.ID, State: model.TaskFileStateDelivered, Role: model.FileRoleMarkdown, FilePath: "output/content.md", FileName: "content.md", MimeType: "text/markdown", OSSKey: "content.md", StorageProvider: store.Name()}); err != nil {
			t.Fatal(err)
		}
		logger := zerolog.Nop()
		svc.SetTaskService(NewTaskService(svc.repo, nil, store, &logger, "", nil, nil))
		interactionID := ""
		if len(factory.specs) > 0 {
			interactionID = factory.specs[0].InteractionID
		}
		if _, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeReview, ProjectID: project.ID, ContentID: work.ID, ResumeInteractionID: interactionID}, "review-"+work.ID); err != nil {
			t.Fatal(err)
		}
		commands := factory.commandSnapshot()
		contextText, _ := commands[len(commands)-1].params["initialContext"].(string)
		if !strings.Contains(contextText, "Delivered body: "+title) || !strings.Contains(contextText, "[Server-owned read-only work analytics;") || len(contextText) > interactiveContextMaxBytes {
			t.Fatalf("review received incomplete/unbounded work context: %s", contextText)
		}
	}
	if len(factory.specs) != 1 || len(factory.commandSnapshot()) != 2 {
		t.Fatal("changing selected work must start a separate logical session within the runtime")
	}
}

func createInteractionContextFixture(t *testing.T, repo repository.Repository, projectID, contentID string) {
	t.Helper()
	project := &model.Project{ID: projectID, UserID: "user-1", Name: projectID, Platform: model.PlatformWechat, Profile: datatypes.NewJSONType(model.NewProjectProfile())}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	if contentID != "" {
		if err := repo.Tasks().Create(context.Background(), &model.Task{ID: contentID, UserID: "user-1", ProjectID: projectID, Status: model.TaskStatusCompleted}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInteractionRejectsForeignProjectBeforeStartingRuntime(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	project := createAssistantProject(t, svc.repo, "another-user", "Private project")
	_, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: project.ID}, "foreign-project")
	if !errors.Is(err, ErrInteractiveSessionOwner) {
		t.Fatalf("foreign project accepted: %v", err)
	}
	if factory.starts != 0 {
		t.Fatal("runtime started before project authorization")
	}
}

type interruptedContextRepository struct {
	repository.Repository
	interactions repository.AssistantInteractionRepository
}

func (r interruptedContextRepository) AssistantInteractions() repository.AssistantInteractionRepository {
	return r.interactions
}

type cancelAfterContextUpdate struct {
	repository.AssistantInteractionRepository
	cancel context.CancelFunc
}

func (r cancelAfterContextUpdate) UpdateContext(ctx context.Context, id, mode, project, content string, now time.Time) error {
	err := r.AssistantInteractionRepository.UpdateContext(ctx, id, mode, project, content, now)
	if err == nil {
		r.cancel()
	}
	return err
}

func TestInteractionRecoversProjectBindingAfterInterruptedSwitch(t *testing.T) {
	for _, recovery := range []string{"resume", "snapshot", "message"} {
		t.Run(recovery, func(t *testing.T) {
			svc, _ := newAssistantInteractionServiceTest(t)
			factory := &startupQuestionFactory{}
			svc.factory = factory
			firstProject := createAssistantProject(t, svc.repo, "user-1", "First project")
			secondProject := createAssistantProject(t, svc.repo, "user-1", "Second project")
			first, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: firstProject.ID}, "initial")
			if err != nil {
				t.Fatal(err)
			}
			baseRepo := svc.repo
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			svc.repo = interruptedContextRepository{Repository: baseRepo, interactions: cancelAfterContextUpdate{AssistantInteractionRepository: baseRepo.AssistantInteractions(), cancel: cancel}}
			in := AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: secondProject.ID, ResumeInteractionID: first.ID}
			if _, err := svc.CreateOrResume(ctx, "user-1", in, "interrupted"); err == nil {
				t.Fatal("expected cancellation after durable project update")
			}
			svc.repo = baseRepo
			if _, _, _, _, _, _, err := svc.ResolveRuntimeCapability(factory.specs[0].RuntimeToken); err == nil {
				t.Error("old project credential remains valid after durable project switch")
			}
			if svc.AcceptRuntimeEvent(first.ID, first.Generation, "question.opened", map[string]any{"call_id": "old-project-question"}) {
				t.Error("old project question was accepted after interrupted project switch")
			}
			factory.emit("question.opened", map[string]any{"call_id": "old-project-runtime-callback"})
			events, err := baseRepo.AssistantInteractions().ListEventsAfter(context.Background(), first.ID, first.Generation, 0, 10)
			if err != nil || len(events) != 2 {
				t.Fatalf("old runtime callback bypassed the project fence: count=%d err=%v", len(events), err)
			}
			switch recovery {
			case "resume":
				_, err = svc.CreateOrResume(context.Background(), "user-1", in, "retry")
			case "snapshot":
				_, err = svc.Snapshot(context.Background(), "user-1", first.ID)
			case "message":
				err = svc.SendMessage(context.Background(), "user-1", first.ID, InteractionMessageInput{Generation: first.Generation, ClientMessageID: "message-after-switch", Text: "Continue"})
				if err != nil && NormalizeInteractionError(err).Code == InteractionErrorGenerationChanged {
					err = nil
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			stored, err := baseRepo.AssistantInteractions().FindByID(context.Background(), first.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Generation <= first.Generation || len(factory.specs) != 2 || factory.specs[1].ProjectID != secondProject.ID {
				t.Fatalf("recovery reused the old project runtime: generation=%d specs=%#v", stored.Generation, factory.specs)
			}
			commands := factory.commandSnapshot()
			if len(commands) != 2 || commands[1].method != "session/start" {
				t.Fatalf("recovery sent a command to the old project runtime: %#v", commands)
			}
		})
	}
}

func TestInteractionResumeWaitsForRunningCommand(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeProfile}, "initial")
	if err != nil {
		t.Fatal(err)
	}
	lock := svc.lockFor(snapshot.ID)
	lock.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ResumeInteractionID: snapshot.ID}, "resume")
		done <- err
	}()
	select {
	case err := <-done:
		lock.Unlock()
		t.Fatalf("resume bypassed the command lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	lock.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resume did not proceed after command released the lock")
	}
}

func TestInteractionReplacesRuntimeWithoutItsBoundCapability(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionModeFactory{}
	svc.factory = factory
	project := createAssistantProject(t, svc.repo, "user-1", "Project")
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: project.ID}, "initial")
	if err != nil {
		t.Fatal(err)
	}
	current, err := svc.repo.AssistantInteractions().FindCurrentRuntime(context.Background(), snapshot.ID, snapshot.Generation)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtimeMu.Lock()
	capability := svc.capabilities[current.TokenHash]
	delete(svc.capabilities, current.TokenHash)
	svc.capabilities["unbound-start-attempt"] = capability
	svc.runtimeMu.Unlock()
	if svc.AcceptRuntimeEvent(snapshot.ID, snapshot.Generation, "question.opened", map[string]any{"call_id": "unbound-question"}) {
		t.Error("callback used an unrelated start attempt's credential")
	}
	binding, err := svc.EnsureRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Generation <= snapshot.Generation || len(factory.specs) != 2 {
		t.Fatalf("runtime without its own credential was reused: generation=%d starts=%d", binding.Generation, len(factory.specs))
	}
}

type switchProjectBeforeEventAppend struct {
	repository.AssistantInteractionRepository
	projectID string
}

func (r switchProjectBeforeEventAppend) AppendEvent(ctx context.Context, interactionID string, generation int64, expectedProjectID string, event *model.AssistantInteractionEvent) (bool, error) {
	if err := r.AssistantInteractionRepository.UpdateContext(ctx, interactionID, AssistantModeCreation, r.projectID, "", time.Now()); err != nil {
		return false, err
	}
	return r.AssistantInteractionRepository.AppendEvent(ctx, interactionID, generation, expectedProjectID, event)
}

func TestInteractionCallbackProjectFenceIsAtomicWithEventInsert(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	firstProject := createAssistantProject(t, svc.repo, "user-1", "First project")
	secondProject := createAssistantProject(t, svc.repo, "user-1", "Second project")
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: firstProject.ID}, "initial")
	if err != nil {
		t.Fatal(err)
	}
	baseRepo := svc.repo
	svc.repo = interruptedContextRepository{Repository: baseRepo, interactions: switchProjectBeforeEventAppend{AssistantInteractionRepository: baseRepo.AssistantInteractions(), projectID: secondProject.ID}}
	if svc.AcceptRuntimeEvent(snapshot.ID, snapshot.Generation, "question.opened", map[string]any{"call_id": "racing-project-question"}) {
		t.Error("callback slipped between the project check and event insertion")
	}
	events, err := baseRepo.AssistantInteractions().ListEventsAfter(context.Background(), snapshot.ID, snapshot.Generation, 0, 10)
	if err != nil || len(events) != 0 {
		t.Fatalf("rejected callback persisted events: count=%d err=%v", len(events), err)
	}
	stored, err := baseRepo.AssistantInteractions().FindByID(context.Background(), snapshot.ID)
	if err != nil || stored.CommandSequence != 0 || stored.Generation != snapshot.Generation {
		t.Fatalf("callback admission changed durable state: interaction=%#v err=%v", stored, err)
	}
}

type startupQuestionFactory struct {
	assistantInteractionModeFactory
	emit      func(string, any)
	failStart bool
}

type startupQuestionRuntime struct {
	Runtime
	emit func(string, any)
}

func (f *startupQuestionFactory) StartSession(ctx context.Context, spec InteractiveRuntimeSpec, emit func(string, any)) (Runtime, error) {
	f.emit = emit
	runtime, err := f.assistantInteractionModeFactory.StartSession(ctx, spec, emit)
	if err != nil {
		return nil, err
	}
	emit("question.opened", map[string]any{"call_id": "during-runtime-start"})
	if f.failStart {
		return nil, errors.New("runtime startup failed")
	}
	return startupQuestionRuntime{Runtime: runtime, emit: emit}, nil
}

func (r startupQuestionRuntime) Command(ctx context.Context, method string, params any) error {
	if method == "session/start" {
		r.emit("question.opened", map[string]any{"call_id": "during-session-start"})
	}
	return r.Runtime.Command(ctx, method, params)
}

func (r startupQuestionRuntime) Close() error {
	r.emit("question.opened", map[string]any{"call_id": "during-runtime-close"})
	return r.Runtime.Close()
}

func TestInteractionAcceptsSynchronousStartupQuestions(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &startupQuestionFactory{}
	svc.factory = factory
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "startup-question")
	if err != nil {
		t.Fatal(err)
	}
	events, err := svc.repo.AssistantInteractions().ListEventsAfter(context.Background(), snapshot.ID, snapshot.Generation, 0, 10)
	if err != nil || len(events) != 2 || events[0].Type != "question.opened" || events[1].Type != "question.opened" {
		t.Fatalf("startup questions were lost: count=%d err=%v", len(events), err)
	}
}

func TestInteractionRejectsLateCallbackAfterStartupTokenCleanup(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &startupQuestionFactory{failStart: true}
	svc.factory = factory
	if _, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "failed-startup"); err == nil {
		t.Fatal("expected runtime startup failure")
	}
	spec := factory.specs[0]
	before, err := svc.repo.AssistantInteractions().ListEventsAfter(context.Background(), spec.InteractionID, spec.Generation, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	factory.emit("question.opened", map[string]any{"call_id": "after-failed-startup"})
	after, err := svc.repo.AssistantInteractions().ListEventsAfter(context.Background(), spec.InteractionID, spec.Generation, 0, 10)
	if err != nil || len(after) != len(before) {
		t.Fatalf("cleaned-up startup token admitted a late callback: before=%d after=%d err=%v", len(before), len(after), err)
	}
}

func TestInteractionRejectsSynchronousCloseQuestions(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &startupQuestionFactory{}
	svc.factory = factory
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "startup-question")
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.repo.AssistantInteractions().ListEventsAfter(context.Background(), snapshot.ID, snapshot.Generation, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplaceRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation, "test-close"); err != nil {
		t.Fatal(err)
	}
	after, err := svc.repo.AssistantInteractions().ListEventsAfter(context.Background(), snapshot.ID, snapshot.Generation, 0, 10)
	if err != nil || len(after) != len(before) {
		t.Fatalf("closing runtime admitted a question: before=%d after=%d err=%v", len(before), len(after), err)
	}
}
