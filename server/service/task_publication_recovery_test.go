package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	appwechat "github.com/anbanai/anban-creator/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
)

func makeCompletedPublicationRecoveryFixture(t *testing.T, action string) (*TaskService, repository.Repository, *model.Task, *model.TaskExecution, *mockEnqueuer) {
	t.Helper()
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	enqueuer := svc.enqueuer.(*mockEnqueuer)
	now := task.CreatedAt
	if _, err := repo.TaskExecutions().Transition(context.Background(), execution.ID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionSucceeded,
		model.ExecutionTransition{Result: datatypes.JSON([]byte(`{"success":true,"session_id":"source-session"}`)), FinalizationStatus: model.TaskExecutionFinalizationDone}); err != nil {
		t.Fatalf("terminalize fixture execution: %v", err)
	}
	task.Status = model.TaskStatusCompleted
	task.CompletedAt = &now
	task.Outcome = &model.TaskOutcome{Publication: model.TaskPublicationOutcome{
		Status: model.TaskPublicationBlocked, Action: action, Attempted: false,
	}}
	task.AgentInput = datatypes.NewJSONType(map[string]any{"original": "keep"})
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatalf("update recovery fixture task: %v", err)
	}
	latest, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("reload recovery fixture task: %v", err)
	}
	return svc, repo, latest, execution, enqueuer
}

func TestRecoverWechatPublicationConcurrentRetryVisualsEnqueuesOnce(t *testing.T) {
	svc, repo, task, _, enqueuer := makeCompletedPublicationRecoveryFixture(t, "retry_visuals")
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.RecoverWechatPublication(ctx, task.UserID, task.ID)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent recovery: %v", err)
		}
	}
	if len(enqueuer.enqueued) != 1 {
		t.Fatalf("enqueued task count = %d, want 1", len(enqueuer.enqueued))
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != model.TaskStatusPending {
		t.Fatalf("task status = %q, want pending", found.Status)
	}
}

func TestRecoverWechatPublicationVisualsExecutionUsesRecoveryPurpose(t *testing.T) {
	svc, repo, task, source, _ := makeCompletedPublicationRecoveryFixture(t, "retry_visuals")
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	if _, err := svc.RecoverWechatPublication(context.Background(), task.UserID, task.ID); err != nil {
		t.Fatalf("recover visuals: %v", err)
	}
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatalf("dispatch recovery execution: %v", err)
	}
	current, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Purpose != model.TaskExecutionPurposePublicationRecovery {
		t.Fatalf("execution purpose = %q, want %q", current.Purpose, model.TaskExecutionPurposePublicationRecovery)
	}
	if current.ParentExecutionID != source.ID {
		t.Fatalf("parent execution = %q, want publication recovery source %q", current.ParentExecutionID, source.ID)
	}
	if current.ResumeSessionID != "" {
		t.Fatalf("resume session = %q, want empty for publication recovery", current.ResumeSessionID)
	}
	storedTask, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := storedTask.AgentInput.Data()
	if input["original"] != "keep" || input["publication_recovery"] != nil || input["source_execution_id"] != nil || input["resume_from"] != nil {
		t.Fatalf("task agent input retained internal recovery controls: %#v", input)
	}
}

func TestImageGenerationResumeWithoutPublicationRecoveryMarkerUsesPrimaryPurpose(t *testing.T) {
	svc, repo, _, _, task := setupDispatchTest(t)
	task.AgentInput = datatypes.NewJSONType(map[string]any{"resume_from": "image_generation"})
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil {
		t.Fatalf("dispatch image resume: %v", err)
	}
	execution, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Purpose != model.TaskExecutionPurposePrimary {
		t.Fatalf("execution purpose = %q, want primary without publication_recovery marker", execution.Purpose)
	}
}

func TestRecoverWechatPublicationRetryDraftFinalizesWithoutAgentStart(t *testing.T) {
	svc, repo, task, execution, enqueuer := makeCompletedPublicationRecoveryFixture(t, "retry_draft")
	if _, err := svc.RecoverWechatPublication(context.Background(), task.UserID, task.ID); err != nil {
		t.Fatalf("recover draft: %v", err)
	}
	if len(enqueuer.enqueued) != 0 {
		t.Fatalf("agent enqueue count = %d, want 0", len(enqueuer.enqueued))
	}
	current, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != execution.ID {
		t.Fatalf("current execution changed from %q to %q", execution.ID, current.ID)
	}
	found, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != model.TaskStatusCompleted || found.Outcome == nil {
		t.Fatalf("task after draft recovery = %#v, want completed with outcome", found)
	}
	if found.Outcome.Publication.Action == "retry_draft" {
		t.Fatal("draft recovery left retry_draft action after server finalization")
	}
}

func TestRecoverWechatPublicationDoesNotProjectAnotherCallersInFlightState(t *testing.T) {
	svc, repo, task, execution, _ := makeCompletedPublicationRecoveryFixture(t, "retry_draft")
	ctx := context.Background()
	evidence := encodedPublicationDeliveryEvidence("create_draft", model.TaskExecutionDraftDeliveryInFlight, "", true, "")
	if won, err := repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, "", model.TaskExecutionDraftDeliveryInFlight, evidence); err != nil || !won {
		t.Fatalf("mark another caller in flight: won=%v err=%v", won, err)
	}

	result, err := svc.RecoverWechatPublication(ctx, task.UserID, task.ID)
	if err != nil {
		t.Fatalf("observe concurrent recovery: %v", err)
	}
	if result.Status != model.TaskPublicationAmbiguous || !result.Attempted {
		t.Fatalf("returned in-flight outcome = %#v", result)
	}
	stored, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Outcome == nil || stored.Outcome.Publication.Status != model.TaskPublicationBlocked || stored.Outcome.Publication.Action != "retry_draft" {
		t.Fatalf("concurrent observer overwrote durable task outcome = %#v", stored.Outcome)
	}
}

func TestRecoverWechatPublicationRetriesAuthorizedDefinitiveDraftRejection(t *testing.T) {
	svc, repo, _, task, execution := setupCloudCompletionTestWithDB(t, true)
	ctx := context.Background()
	if _, err := repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionSucceeded,
		model.ExecutionTransition{Result: datatypes.JSON([]byte(`{"success":true}`)), FinalizationStatus: model.TaskExecutionFinalizationDone}); err != nil {
		t.Fatalf("terminalize execution: %v", err)
	}
	if err := repo.TaskFiles().DeliverCurrentExecution(ctx, task.ID, execution.ID); err != nil {
		t.Fatalf("deliver completed execution artifacts: %v", err)
	}
	attemptedAt := time.Now().UTC().Add(-time.Minute)
	authorizedAt := attemptedAt.Add(time.Second)
	article := appwechat.DraftArticle{
		Title: "Valid article", Digest: "Durable delivery fixture",
		Content: string(validTaskDeliveryFixtureBody("output/05-article.html", "text/html")),
	}
	if err := repo.WechatPublications().Create(ctx, &model.WechatPublication{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, UserID: task.UserID, ProjectID: task.ProjectID,
		DraftTitle: article.Title, DraftDigest: article.Digest,
		DraftContentFingerprint: WechatContentFingerprint(article.Content), DraftRequestFingerprint: wechatDraftRequestFingerprint(article),
		Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusUnsupported,
		WechatStatusCode: 40164, DraftAddAttemptedAt: &attemptedAt, DraftAddAttempts: 1, DraftRetryAuthorizedAt: &authorizedAt,
	}); err != nil {
		t.Fatal(err)
	}
	evidence := encodedPublicationDeliveryEvidence("create_draft", model.TaskExecutionDraftDeliveryFailed, "create_draft_rejected", true, "retry_draft")
	if won, err := repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, "", model.TaskExecutionDraftDeliveryFailed, evidence); err != nil || !won {
		t.Fatalf("record rejected delivery: won=%v err=%v", won, err)
	}
	now := task.CreatedAt
	task.Status, task.CompletedAt = model.TaskStatusCompleted, &now
	task.Outcome = &model.TaskOutcome{Publication: model.TaskPublicationOutcome{
		Status: model.TaskPublicationFailed, Code: "create_draft_rejected", Attempted: true, Action: "retry_draft",
	}}
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	api := &fakeWechatPublicationAPI{
		draftListResponse: &appwechat.DraftBatchGetResponse{},
		addResponse:       &appwechat.DraftAddResponse{MediaID: "draft-after-repair"},
	}
	logger := zerolog.New(io.Discard)
	publicationSvc := NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) { return api, nil }, &logger)
	svc.SetWechatPublicationService(publicationSvc)

	result, err := svc.RecoverWechatPublication(ctx, task.UserID, task.ID)
	if err != nil {
		t.Fatalf("recover definitive rejection: %v", err)
	}
	if result.Status != model.TaskPublicationSucceeded || api.addCalls != 1 {
		t.Fatalf("recovery result=%#v addCalls=%d", result, api.addCalls)
	}
	stored, err := repo.WechatPublications().FindByTaskID(ctx, task.ID)
	if err != nil || stored.DraftMediaID != "draft-after-repair" || stored.DraftAddAttempts != 2 ||
		stored.DraftAddAttemptedAt == nil || !stored.DraftAddAttemptedAt.Equal(attemptedAt) {
		t.Fatalf("stored recovered publication=%#v err=%v", stored, err)
	}
}

func TestRecoverWechatPublicationVisualsEnqueueFailureRestoresCompletedTask(t *testing.T) {
	svc, repo, task, _, _ := makeCompletedPublicationRecoveryFixture(t, "retry_visuals")
	svc.enqueuer = &failOnceEnqueuer{err: errors.New("queue unavailable")}
	beforeInput, _ := json.Marshal(task.AgentInput.Data())
	_, err := svc.RecoverWechatPublication(context.Background(), task.UserID, task.ID)
	if err == nil {
		t.Fatal("recover visuals error = nil, want enqueue failure")
	}
	found, findErr := repo.Tasks().FindByID(context.Background(), task.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if found.Status != model.TaskStatusCompleted {
		t.Fatalf("task status = %q, want completed", found.Status)
	}
	gotInput, _ := json.Marshal(found.AgentInput.Data())
	if string(gotInput) != string(beforeInput) {
		t.Fatalf("agent input after rollback = %s, want %s", gotInput, beforeInput)
	}
}

func TestRecoverWechatPublicationDoesNotCreateAdditionalTaskCharge(t *testing.T) {
	ctx := context.Background()
	svc, billing, enqueuer := newFixedTaskBillingFixture(t, 1_000, 0)
	projectID := createTestProject(t, billing.repo, billingWalletUserID, model.PlatformArticle)
	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective", UserID: billingWalletUserID, ProjectID: projectID, Prompt: "recovery billing", Quantity: 1})
	if err != nil || len(tasks) != 1 {
		t.Fatalf("CreateManual = %#v, %v", tasks, err)
	}
	task := tasks[0]
	execution := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: 1, Target: "test",
		Status: model.TaskExecutionSucceeded, Purpose: model.TaskExecutionPurposePrimary,
		FinalizationStatus: model.TaskExecutionFinalizationDone,
	}
	if err := billing.repo.TaskExecutions().Create(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if won, err := billing.repo.Tasks().CompareAndSwapStatus(ctx, task.ID, model.TaskStatusPending, model.TaskStatusRunning); err != nil || !won {
		t.Fatalf("start recovery source task: won=%v err=%v", won, err)
	}
	if won, err := billing.repo.Tasks().SetCurrentExecution(ctx, task.ID, execution.ID); err != nil || !won {
		t.Fatalf("set recovery source execution: won=%v err=%v", won, err)
	}
	task.CurrentExecutionID = &execution.ID
	if won, err := billing.repo.Tasks().CompareAndSwapStatus(ctx, task.ID, model.TaskStatusRunning, model.TaskStatusCompleted); err != nil || !won {
		t.Fatalf("complete recovery source task: won=%v err=%v", won, err)
	}
	now := task.CreatedAt
	task.Status, task.CompletedAt = model.TaskStatusCompleted, &now
	task.Outcome = &model.TaskOutcome{Publication: model.TaskPublicationOutcome{Status: model.TaskPublicationBlocked, Action: "retry_visuals"}}
	task.AgentInput = datatypes.NewJSONType(map[string]any{})
	if err := billing.repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	_ = enqueuer
	if _, err := svc.RecoverWechatPublication(ctx, task.UserID, task.ID); err != nil {
		t.Fatal(err)
	}
	charges, err := billing.repo.Billing().ListChargesByTaskIDs(ctx, task.UserID, []string{task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(charges) != 1 || charges[0].Kind != model.BillingChargeKindTask {
		t.Fatalf("task charges after recovery = %#v, want one fixed task charge", charges)
	}
}
