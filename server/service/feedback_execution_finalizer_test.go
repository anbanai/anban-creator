package service

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestFinalizeFeedbackExecutionPersistsOnlyValidatedPrivateArtifacts(t *testing.T) {
	fixture := newFeedbackFinalizerFixture(t)
	db := fixture.svc.repo.Analytics().DB()
	if err := db.AutoMigrate(&model.FeedbackJob{}, &model.FeedbackInsight{}, &model.FeedbackLease{}, &model.StrategySnapshot{}); err != nil {
		t.Fatal(err)
	}
	job := &model.FeedbackJob{
		ID: uuid.NewString(), UserID: fixture.task.UserID, ProjectID: fixture.task.ProjectID,
		Platform: model.PlatformWechat, AccountID: "wechat:wx-feedback", Operation: "content_postmortem", Cadence: FeedbackCadenceWeekly,
		PeriodStart: "2026-09-01", PeriodEnd: "2026-09-30", AnalyticsRevision: 7,
		TargetContentID: "content-1", Trigger: "user_attribution", Fingerprint: uuid.NewString(),
		Status: model.FeedbackJobRunning, TaskID: fixture.task.ID, ExecutionID: fixture.execution.ID,
	}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	if acquired, err := fixture.repo.FeedbackLoop().AcquireFeedbackLease(context.Background(), FeedbackAccountLeaseScope(job.AccountID), job.ID, time.Now().UTC(), feedbackLeaseTTL); err != nil || !acquired {
		t.Fatalf("acquire feedback lease = %v, %v", acquired, err)
	}
	input := map[string]any{"feedback_job_id": job.ID, "operation": job.Operation, "target_content_id": job.TargetContentID}
	fixture.task.SetAgentInput(input)
	if err := fixture.repo.Tasks().Update(context.Background(), fixture.task); err != nil {
		t.Fatal(err)
	}
	analysis := feedbackAnalysisArtifact{
		SchemaVersion: feedbackArtifactSchemaVersion, Status: "ready", Source: "get_feedback_context",
		DataAt: "2026-10-01T00:00:00Z", Missing: []string{}, EvidencePaths: []string{feedbackEvidenceArtifactPath},
		JobID: job.ID, ProjectID: job.ProjectID, Operation: job.Operation, TargetContentID: job.TargetContentID,
		AnalyticsRevision: job.AnalyticsRevision, BaselineScope: "account_platform", Summary: "Observed above account baseline.",
		Confidence: "medium", Limitations: "Correlation does not establish causation.",
	}
	evidence := feedbackEvidenceArtifact{SchemaVersion: feedbackArtifactSchemaVersion, Status: "ready", Source: "get_feedback_context", DataAt: "2026-10-01T00:00:00Z", Missing: []string{}, EvidencePaths: []string{}, JobID: job.ID, AnalyticsRevision: job.AnalyticsRevision, Evidence: json.RawMessage(`{"sample_count":5,"target":{"likes":20},"baseline":{"median_likes":4}}`)}
	analysisBytes, _ := json.Marshal(analysis)
	evidenceBytes, _ := json.Marshal(evidence)
	for path, body := range map[string][]byte{feedbackAnalysisArtifactPath: analysisBytes, feedbackEvidenceArtifactPath: evidenceBytes} {
		key := buildTaskMCPArtifactStoragePrefix(fixture.task, fixture.execution.ID) + path
		fixture.store.files[key] = body
		if err := fixture.repo.TaskFiles().Create(context.Background(), &model.TaskFile{ID: uuid.NewString(), TaskID: fixture.task.ID, ExecutionID: fixture.execution.ID, State: model.TaskFileStateDelivered,
			Role: "analysis", FilePath: path, FileName: path[stringsLastIndex(path, "/")+1:], MimeType: "application/json", FileSize: int64(len(body)), OSSKey: key, StorageProvider: fixture.store.Name(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	fixture.execution.Status = model.TaskExecutionSucceeded
	if err := fixture.svc.FinalizeFeedbackExecution(context.Background(), fixture.task, fixture.execution, &agent.ExecutionResult{Success: true}); err != nil {
		t.Fatalf("FinalizeFeedbackExecution: %v", err)
	}
	storedJob, err := fixture.repo.FeedbackLoop().FindJobByIDOrFingerprint(context.Background(), job.ID)
	if err != nil || storedJob.Status != model.FeedbackJobSucceeded {
		t.Fatalf("job status = %#v, err=%v", storedJob, err)
	}
	insight, err := fixture.repo.FeedbackLoop().FindInsightByID(context.Background(), "")
	if err != nil || insight != nil {
		t.Fatalf("unexpected lookup by empty id: insight=%#v err=%v", insight, err)
	}
	var persisted model.FeedbackInsight
	if err := db.Where("job_id = ?", job.ID).First(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.TargetContentID != job.TargetContentID || persisted.AnalyticsRevision != job.AnalyticsRevision || persisted.PromotionStatus != model.FeedbackPromotionCandidate || persisted.ExecutionID != fixture.execution.ID {
		t.Fatalf("insight = %#v, expected candidate tied to content, revision, execution", persisted)
	}
	if len(persisted.AnalysisArtifactHash) != 64 || len(persisted.EvidenceArtifactHash) != 64 || persisted.AgentPackDigest != fixture.execution.AgentPackDigest {
		t.Fatalf("artifact provenance missing: %#v", persisted)
	}
	var leases int64
	if err := db.Model(&model.FeedbackLease{}).Where("scope = ?", FeedbackAccountLeaseScope(job.AccountID)).Count(&leases).Error; err != nil {
		t.Fatal(err)
	}
	if leases != 0 {
		t.Fatalf("feedback lease count = %d, want released after finalization", leases)
	}
}

func TestFinalizeFeedbackExecutionRejectsArtifactRevisionMismatch(t *testing.T) {
	fixture := newFeedbackFinalizerFixture(t)
	db := fixture.svc.repo.Analytics().DB()
	if err := db.AutoMigrate(&model.FeedbackJob{}, &model.FeedbackInsight{}, &model.FeedbackLease{}, &model.StrategySnapshot{}); err != nil {
		t.Fatal(err)
	}
	job := &model.FeedbackJob{ID: uuid.NewString(), UserID: fixture.task.UserID, ProjectID: fixture.task.ProjectID, Platform: model.PlatformWechat, Operation: "content_postmortem", Cadence: FeedbackCadenceWeekly, PeriodStart: "2026-09-01", PeriodEnd: "2026-09-30", AnalyticsRevision: 9, TargetContentID: "content-1", Trigger: "user_attribution", Fingerprint: uuid.NewString(), Status: model.FeedbackJobRunning, TaskID: fixture.task.ID, ExecutionID: fixture.execution.ID}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	fixture.task.SetAgentInput(map[string]any{"feedback_job_id": job.ID})
	if err := fixture.repo.Tasks().Update(context.Background(), fixture.task); err != nil {
		t.Fatal(err)
	}
	analysis := feedbackAnalysisArtifact{SchemaVersion: feedbackArtifactSchemaVersion, Status: "ready", Source: "get_feedback_context", DataAt: "2026-10-01T00:00:00Z", Missing: []string{}, EvidencePaths: []string{}, JobID: job.ID, AnalyticsRevision: 8}
	evidence := feedbackEvidenceArtifact{SchemaVersion: feedbackArtifactSchemaVersion, Status: "ready", Source: "get_feedback_context", DataAt: "2026-10-01T00:00:00Z", Missing: []string{}, EvidencePaths: []string{}, JobID: job.ID, AnalyticsRevision: 9, Evidence: json.RawMessage(`{"sample_count":1}`)}
	analysisBytes, _ := json.Marshal(analysis)
	evidenceBytes, _ := json.Marshal(evidence)
	for path, body := range map[string][]byte{feedbackAnalysisArtifactPath: analysisBytes, feedbackEvidenceArtifactPath: evidenceBytes} {
		key := buildTaskMCPArtifactStoragePrefix(fixture.task, fixture.execution.ID) + path
		fixture.store.files[key] = body
		if err := fixture.repo.TaskFiles().Create(context.Background(), &model.TaskFile{ID: uuid.NewString(), TaskID: fixture.task.ID, ExecutionID: fixture.execution.ID, State: model.TaskFileStateDelivered, Role: "analysis", FilePath: path, FileName: path[stringsLastIndex(path, "/")+1:], MimeType: "application/json", FileSize: int64(len(body)), OSSKey: key, StorageProvider: fixture.store.Name()}); err != nil {
			t.Fatal(err)
		}
	}
	fixture.execution.Status = model.TaskExecutionSucceeded
	if err := fixture.svc.FinalizeFeedbackExecution(context.Background(), fixture.task, fixture.execution, &agent.ExecutionResult{Success: true}); err == nil {
		t.Fatal("expected mismatched revision rejection")
	}
	var count int64
	if err := db.Model(&model.FeedbackInsight{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("insight count=%d err=%v, want 0", count, err)
	}
	stored, err := fixture.repo.FeedbackLoop().FindJobByIDOrFingerprint(context.Background(), job.ID)
	if err != nil || stored.Status != model.FeedbackJobFailed {
		t.Fatalf("job=%#v err=%v, want failed", stored, err)
	}
}

func TestFinalizeFeedbackExecutionDoesNotActivateStrategyFromWarning(t *testing.T) {
	fixture := newFeedbackFinalizerFixture(t)
	db := fixture.svc.repo.Analytics().DB()
	if err := db.AutoMigrate(&model.FeedbackJob{}, &model.FeedbackInsight{}, &model.FeedbackLease{}, &model.StrategySnapshot{}); err != nil {
		t.Fatal(err)
	}
	job := &model.FeedbackJob{
		ID: uuid.NewString(), UserID: fixture.task.UserID, ProjectID: fixture.task.ProjectID,
		Platform: model.PlatformWechat, AccountID: "wechat:wx-feedback", Operation: "strategy_advisor", Cadence: model.FeedbackCadenceMonthly,
		PeriodStart: "2026-09-01", PeriodEnd: "2026-09-30", AnalyticsRevision: 12, SampleCount: 10,
		Trigger: "monthly", Fingerprint: uuid.NewString(), Status: model.FeedbackJobRunning, TaskID: fixture.task.ID, ExecutionID: fixture.execution.ID,
	}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	fixture.task.SetAgentInput(map[string]any{"feedback_job_id": job.ID})
	if err := fixture.repo.Tasks().Update(context.Background(), fixture.task); err != nil {
		t.Fatal(err)
	}
	analysis := feedbackAnalysisArtifact{
		SchemaVersion: feedbackArtifactSchemaVersion, Status: "warning", Source: "get_feedback_context",
		DataAt: "2026-10-01T00:00:00Z", Missing: []string{"baseline.coverage"}, EvidencePaths: []string{feedbackEvidenceArtifactPath},
		JobID: job.ID, ProjectID: job.ProjectID, Operation: job.Operation, AnalyticsRevision: job.AnalyticsRevision,
		Summary: "Monthly evidence is incomplete.", Confidence: "low", Limitations: "Coverage is incomplete.",
	}
	evidence := feedbackEvidenceArtifact{
		SchemaVersion: feedbackArtifactSchemaVersion, Status: "warning", Source: "get_feedback_context",
		DataAt: "2026-10-01T00:00:00Z", Missing: []string{"baseline.coverage"}, EvidencePaths: []string{},
		JobID: job.ID, AnalyticsRevision: job.AnalyticsRevision, Evidence: json.RawMessage(`{"sample_count":10}`),
	}
	for path, body := range map[string][]byte{feedbackAnalysisArtifactPath: feedbackMustJSON(t, analysis), feedbackEvidenceArtifactPath: feedbackMustJSON(t, evidence)} {
		key := buildTaskMCPArtifactStoragePrefix(fixture.task, fixture.execution.ID) + path
		fixture.store.files[key] = body
		if err := fixture.repo.TaskFiles().Create(context.Background(), &model.TaskFile{ID: uuid.NewString(), TaskID: fixture.task.ID, ExecutionID: fixture.execution.ID, State: model.TaskFileStateDelivered, Role: "analysis", FilePath: path, FileName: path[stringsLastIndex(path, "/")+1:], MimeType: "application/json", FileSize: int64(len(body)), OSSKey: key, StorageProvider: fixture.store.Name()}); err != nil {
			t.Fatal(err)
		}
	}
	fixture.execution.Status = model.TaskExecutionSucceeded
	if err := fixture.svc.FinalizeFeedbackExecution(context.Background(), fixture.task, fixture.execution, &agent.ExecutionResult{Success: true}); err != nil {
		t.Fatalf("FinalizeFeedbackExecution: %v", err)
	}
	var strategies int64
	if err := db.Model(&model.StrategySnapshot{}).Count(&strategies).Error; err != nil {
		t.Fatal(err)
	}
	if strategies != 0 {
		t.Fatalf("strategy snapshot count = %d, want 0 for warning evidence", strategies)
	}
}

func feedbackMustJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func newFeedbackFinalizerFixture(t *testing.T) *managedCompletionFixture {
	t.Helper()
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	userID, projectID := uuid.NewString(), uuid.NewString()
	if err := repo.Projects().Create(context.Background(), &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformWechat, Name: "Feedback", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, AgentID: model.AgentIDFeedback, Channel: model.ChannelFeedback, TaskKind: model.TaskKindFeedbackAnalysis, Type: model.TaskKindFeedbackAnalysis, Status: model.TaskStatusRunning}
	task.SetAgentInput(map[string]any{})
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	execution := &model.TaskExecution{ID: uuid.NewString(), TaskID: task.ID, Attempt: 1, Target: "managed-test", Status: model.TaskExecutionRunning, Started: true, StartedAt: &now, RuntimeProfile: "feedback", RuntimeImage: "registry/feedback@sha256:test"}
	execution.AgentID = model.AgentIDFeedback
	execution.Channel = model.ChannelFeedback
	execution.TaskKind = model.TaskKindFeedbackAnalysis
	execution.AgentPackID = "feedback"
	execution.AgentPackVersion = "1.1.0"
	execution.AgentPackDigest = "test-feedback-pack"
	execution.AgentPackDeliveryContract = []byte(`[{"role":"analysis_result","path":"output/feedback-analysis.json","mime_type":"application/json","internal_only":true},{"role":"evidence","path":"output/feedback-evidence.json","mime_type":"application/json","internal_only":true}]`)
	execution.AgentPackRequiredArtifactContract = []byte(`[{"role":"analysis_result","path":"output/feedback-analysis.json","mime_type":"application/json","required":true},{"role":"evidence","path":"output/feedback-evidence.json","mime_type":"application/json","required":true}]`)
	execution.RuntimeAdapter = "standard"
	if err := repo.TaskExecutions().Create(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.Tasks().SetCurrentExecution(context.Background(), task.ID, execution.ID); err != nil || !won {
		t.Fatalf("set execution: %v %v", won, err)
	}
	if err := repo.Tasks().UpdateStatus(context.Background(), task.ID, model.TaskStatusRunning); err != nil {
		t.Fatal(err)
	}
	store := &fakeTaskStorage{name: "oss", files: map[string][]byte{}}
	logger := zerolog.New(io.Discard)
	return &managedCompletionFixture{svc: newTestTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil), repo: repo, store: store, task: task, execution: execution}
}

func stringsLastIndex(value, sep string) int {
	for i := len(value) - len(sep); i >= 0; i-- {
		if value[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}
