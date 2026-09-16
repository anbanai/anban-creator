package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	appwechat "github.com/anbanai/anban-creator/app/wechat"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/agent"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func setupCloudCompletionTest(t *testing.T, withArtifact bool, startedOverride ...bool) (*TaskService, repository.Repository, *model.Task, *model.TaskExecution) {
	t.Helper()
	svc, repo, _, task, execution := setupCloudCompletionTestWithDB(t, withArtifact, startedOverride...)
	return svc, repo, task, execution
}

func setupCloudCompletionTestWithDB(t *testing.T, withArtifact bool, startedOverride ...bool) (*TaskService, repository.Repository, *gorm.DB, *model.Task, *model.TaskExecution) {
	t.Helper()
	db := setupTaskTestDB(t)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &fakeTaskStorage{name: "oss", files: map[string][]byte{}}
	svc := newTestTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	profiles, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentProfileRegistry(profiles)
	profile, err := profiles.Resolve("effective")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Projects().Create(context.Background(), &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle,
		Name: "Cloud completion", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusRunning, ImageCapabilityKey: "standard", ExecutionProfile: profile.ID,
		AgentProfileSnapshot: snapshot, AgentProfileFingerprint: fingerprint,
	}
	visualsDisabled := false
	task.ArticleWithCover = &visualsDisabled
	task.ArticleWithContentImages = &visualsDisabled
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	started := true
	if len(startedOverride) > 0 {
		started = startedOverride[0]
	}
	executionStatus := model.TaskExecutionRunning
	if !started {
		executionStatus = model.TaskExecutionStarting
	}
	profiledExecution := model.NewTaskExecutionAgentProfile(snapshot, fingerprint)
	execution := &profiledExecution
	execution.ID, execution.TaskID, execution.Attempt = uuid.NewString(), task.ID, 1
	execution.Target, execution.Status, execution.Started = "docker", executionStatus, started
	execution.RuntimeProfile, execution.RuntimeImage = "article", "registry/content@sha256:test"
	if err := applyAgentPackIdentity(execution, task.Type); err != nil {
		t.Fatal(err)
	}
	if withArtifact {
		execution.ManifestStatus = model.TaskExecutionManifestPending
	}
	if err := repo.TaskExecutions().Create(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.Tasks().SetCurrentExecution(context.Background(), task.ID, execution.ID); err != nil || !won {
		t.Fatalf("set current execution: won=%v err=%v", won, err)
	}
	task.CurrentExecutionID = &execution.ID
	if withArtifact {
		required, err := resolveFrozenExecutionRequiredArtifactContract(execution)
		if err != nil {
			t.Fatal(err)
		}
		files := make([]*model.TaskFile, 0, len(required))
		for _, spec := range required {
			body := validTaskDeliveryFixtureBody(spec.Path, spec.MIMEType)
			key := buildTaskMCPArtifactStoragePrefix(task, execution.ID) + spec.Path
			store.files[key] = body
			files = append(files, &model.TaskFile{
				ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStatePending,
				Role: spec.Role, FilePath: spec.Path, FileName: filepath.Base(spec.Path),
				MimeType: spec.MIMEType, FileSize: int64(len(body)), OSSKey: key, OSSURL: store.GetURL(key), StorageProvider: store.Name(),
			})
		}
		if err := repo.TaskFiles().BatchCreate(context.Background(), files); err != nil {
			t.Fatal(err)
		}
		addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/marketing-scan.json", "application/json", validTaskDeliveryFixtureBody("output/marketing-scan.json", "application/json"))
		addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/final-review.md", "text/markdown", []byte("# Final review\n\nPASS\n"))
		addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/viral-audit.md", "text/markdown", []byte("# Viral audit\n\nPASS\n"))
		if err := repo.TaskFiles().ReplacePendingCurrentExecutionPreservingMCPArtifacts(context.Background(), task.ID, execution.ID, nil); err != nil {
			t.Fatalf("seal cloud execution manifest: %v", err)
		}
	}
	return svc, repo, db, task, execution
}

type replaySafeEnqueuer struct {
	calls    int
	accepted int
	seen     map[string]struct{}
}

type failingCloudCoreRepository struct {
	repository.Repository
	err error
}

type failingCloudCoreTaskRepository struct {
	repository.TaskRepository
	err error
}

func (r *failingCloudCoreRepository) Tasks() repository.TaskRepository {
	return &failingCloudCoreTaskRepository{TaskRepository: r.Repository.Tasks(), err: r.err}
}

func (r *failingCloudCoreRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&failingCloudCoreRepository{Repository: tx, err: r.err})
	})
}

func (r *failingCloudCoreTaskRepository) UpdateBillingTerminalReason(context.Context, string, string) error {
	return r.err
}

func (e *replaySafeEnqueuer) Enqueue(string, []byte) error { return nil }

func (e *replaySafeEnqueuer) EnqueueIn(string, []byte, time.Duration) error { return nil }

func (e *replaySafeEnqueuer) EnqueueUnique(_ string, _ []byte, uniqueKey string) (bool, error) {
	e.calls++
	if e.seen == nil {
		e.seen = make(map[string]struct{})
	}
	if _, exists := e.seen[uniqueKey]; exists {
		return false, nil
	}
	e.seen[uniqueKey] = struct{}{}
	e.accepted++
	return true, nil
}

func TestCompleteCloudExecutionCurrentAttemptAndDuplicate(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	old := &model.TaskExecution{ID: uuid.NewString(), TaskID: task.ID, Attempt: 0, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true}
	if err := repo.TaskExecutions().Create(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	if err := svc.CompleteCloudExecution(context.Background(), old.ID, result); !errors.Is(err, ErrStaleTaskExecution) {
		t.Fatalf("stale completion error = %v", err)
	}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatalf("duplicate completion: %v", err)
	}
	foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	files, _ := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if foundTask.Status != model.TaskStatusCompleted || foundExecution.FinalizationStatus != model.TaskExecutionFinalizationDone || len(files) != 6 {
		t.Fatalf("task=%s execution=%s files=%d", foundTask.Status, foundExecution.FinalizationStatus, len(files))
	}
}

func TestCompleteCloudExecutionUsesValidatedCoreArtifactsAfterProviderPolicyFailure(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	result := &agent.ExecutionResult{
		Success:         false,
		Error:           "供应商内容安全策略拒绝了本次请求。",
		TerminalReason:  model.TaskBillingTerminalProviderError,
		ErrorCode:       "provider_policy_rejection",
		RemoteArtifacts: true,
	}

	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatal(err)
	}
	foundTask, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundExecution, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundTask.Status != model.TaskStatusCompleted || foundExecution.Status != model.TaskExecutionSucceeded {
		t.Fatalf("task=%s execution=%s, want completed/succeeded", foundTask.Status, foundExecution.Status)
	}
	files, err := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 6 {
		t.Fatalf("delivered files=%d, want 6", len(files))
	}
	var stored agent.ExecutionResult
	if err := json.Unmarshal(foundExecution.Result, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Success || stored.ErrorCode != "provider_policy_rejection" {
		t.Fatalf("stored provider result = %#v, want failed SDK result with preserved policy code", stored)
	}
}

func TestCompleteCloudExecutionBuildsIndependentArticleOutcome(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	ctx := context.Background()
	withCover, withContentImages := true, true
	task.ArticleWithCover = &withCover
	task.ArticleWithContentImages = &withContentImages
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	contentHash := cloudArticleFixtureHash()
	scanBody, _ := json.Marshal(map[string]any{
		"version": "1.0", "status": "warning", "content_hash": contentHash,
		"findings": []map[string]string{{"rule_id": "absolute_claim", "severity": "warning"}},
	})
	draftBody, _ := json.Marshal(map[string]string{
		"status": "skipped", "reason": "marketing_scan_block_publish", "content_hash": contentHash,
	})
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/marketing-scan.json", "application/json", scanBody)
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/draft-result.json", "application/json", draftBody)

	result := &agent.ExecutionResult{
		Success: false, Error: "供应商内容安全策略拒绝了本次请求。",
		TerminalReason: model.TaskBillingTerminalProviderError,
		ErrorCode:      "provider_policy_rejection", PolicyDomain: "content_safety",
		ProviderCode: "content_exists_risk", HTTPStatus: 400,
		ContentDirection: "unknown", Recoverable: true, ResumeFrom: "provider_request",
		FailureStage: "writing", RequestID: "request-400", RemoteArtifacts: true,
		ArtifactUploadFailures: []agent.ArtifactUploadFailure{{Path: "output/img_01.png", Reason: "direct artifact upload returned HTTP 503"}},
	}
	if err := svc.CompleteCloudExecution(ctx, execution.ID, result); err != nil {
		t.Fatal(err)
	}

	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Outcome == nil {
		t.Fatal("task outcome was not persisted")
	}
	outcome := *found.Outcome
	if outcome.CoreDelivery.Status != model.TaskCoreDeliveryComplete ||
		outcome.Visual.Status != model.TaskVisualPartial ||
		outcome.Review.Status != model.TaskReviewWarning ||
		outcome.Publication.Status != model.TaskPublicationBlocked ||
		outcome.Publication.Code != "cover_media_missing" || outcome.Publication.Attempted {
		t.Fatalf("outcome dimensions = %#v", outcome)
	}
	if outcome.Diagnostic == nil || outcome.Diagnostic.Provider != execution.Provider ||
		outcome.Diagnostic.ProviderCode != "content_exists_risk" || outcome.Diagnostic.HTTPStatus != 400 ||
		outcome.Diagnostic.ContentDirection != "unknown" || outcome.Diagnostic.RequestID != "sha256:a73a5354c9cdfae22720ad37e5def080aef995ca12316c84db1238c587a9c9a3" {
		t.Fatalf("diagnostic = %#v", outcome.Diagnostic)
	}
	if strings.Contains(outcome.Diagnostic.Summary, result.Error) || !strings.Contains(outcome.Diagnostic.Summary, "未披露具体片段") {
		t.Fatalf("diagnostic summary is not sanitized: %q", outcome.Diagnostic.Summary)
	}
	warningCodes := make(map[string]bool, len(outcome.Warnings))
	for _, warning := range outcome.Warnings {
		warningCodes[warning.Code] = true
	}
	for _, code := range []string{"provider_policy_rejection", "visual_partial", "review_warning", "artifact_upload_failed"} {
		if !warningCodes[code] {
			t.Fatalf("missing warning %q in %#v", code, outcome.Warnings)
		}
	}
}

func TestCompleteCloudExecutionExposesRecordedDraftFailure(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	evidence := []byte(`{"source":"create_draft","status":"failed","code":"create_draft_invalid_payload"}`)
	won, err := repo.TaskExecutions().TransitionDraftDelivery(
		context.Background(), execution.ID, "", model.TaskExecutionDraftDeliveryFailed, evidence,
	)
	if err != nil || !won {
		t.Fatalf("record draft failure: won=%v err=%v", won, err)
	}

	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	found, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Outcome == nil {
		t.Fatal("task outcome was not persisted")
	}
	if found.Outcome.Publication.Status != model.TaskPublicationFailed ||
		found.Outcome.Publication.Code != "create_draft_invalid_payload" ||
		found.Outcome.Publication.Message != "草稿内容或图片不符合公众号投递要求。" {
		t.Fatalf("publication outcome = %#v", found.Outcome.Publication)
	}
}

func TestCompleteCloudExecutionIgnoresLegacyDraftResultArtifact(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	draftBody, _ := json.Marshal(map[string]string{
		"status": "failed", "code": "create_draft_execution_mismatch", "content_hash": cloudArticleFixtureHash(),
	})
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/draft-result.json", "application/json", draftBody)

	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	found, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Outcome == nil || found.Outcome.Publication.Status != model.TaskPublicationBlocked ||
		found.Outcome.Publication.Code != "publication_service_unavailable" || found.Outcome.Publication.Attempted {
		t.Fatalf("publication outcome = %#v", found.Outcome)
	}
}

func TestPublicationTaskOutcomeDropsUnknownArtifactCode(t *testing.T) {
	execution := &model.TaskExecution{
		DraftDeliveryStatus: model.TaskExecutionDraftDeliveryFailed,
		DraftDeliveryResult: []byte(`{"status":"failed","code":"<script>alert(1)</script>"}`),
	}
	outcome := publicationTaskOutcome(execution)
	if outcome.Code != "" || outcome.Message != "" {
		t.Fatalf("publication outcome = %#v, want unknown code omitted", outcome)
	}
}

func TestPublicationTaskOutcomeKeepsManagedImageProvenanceFailure(t *testing.T) {
	execution := &model.TaskExecution{
		DraftDeliveryStatus: model.TaskExecutionDraftDeliveryFailed,
		DraftDeliveryResult: []byte(`{"status":"failed","code":"content_image_source_invalid","attempted":false,"action":"review_content"}`),
	}
	outcome := publicationTaskOutcome(execution)
	if outcome.Code != "content_image_source_invalid" || outcome.Attempted || outcome.Action != "review_content" ||
		outcome.Message != "正文图片不属于当前任务执行，微信尚未收到请求。" {
		t.Fatalf("publication outcome = %#v", outcome)
	}
}

func TestCompleteCloudExecutionDoesNotTrustStaleArticleStatusArtifacts(t *testing.T) {
	for _, tt := range []struct {
		name            string
		path            string
		status          string
		contentHash     string
		wantDraft       string
		wantPublication model.TaskPublicationStatus
	}{
		{name: "marketing scan missing hash", path: "output/marketing-scan.json", status: "block_publish", wantDraft: model.TaskExecutionDraftDeliveryBlocked, wantPublication: model.TaskPublicationBlocked},
		{name: "marketing scan mismatched hash", path: "output/marketing-scan.json", status: "block_publish", contentHash: strings.Repeat("a", 64), wantDraft: model.TaskExecutionDraftDeliveryBlocked, wantPublication: model.TaskPublicationBlocked},
		{name: "legacy draft result is ignored", path: "output/draft-result.json", status: "skipped", contentHash: strings.Repeat("b", 64), wantDraft: model.TaskExecutionDraftDeliveryBlocked, wantPublication: model.TaskPublicationBlocked},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, task, execution := setupCloudCompletionTest(t, true)
			body, _ := json.Marshal(map[string]string{"status": tt.status, "content_hash": tt.contentHash})
			addCloudOutcomeArtifact(t, svc, repo, task, execution, tt.path, "application/json", body)

			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
				t.Fatal(err)
			}
			foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
			foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
			if foundExecution.DraftDeliveryStatus != tt.wantDraft || foundTask.Outcome == nil ||
				foundTask.Outcome.Publication.Status != tt.wantPublication {
				t.Fatalf("draft=%q outcome=%#v", foundExecution.DraftDeliveryStatus, foundTask.Outcome)
			}
		})
	}
}

func TestCompleteCloudExecutionBlocksArticlePublicationBeforeWechatWhenPackageMissing(t *testing.T) {
	svc, repo, db, task, execution := setupCloudCompletionTestWithDB(t, true)
	ctx := context.Background()
	if err := repo.TaskFiles().DeliverCurrentExecution(ctx, task.ID, execution.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("task_id = ? AND execution_id = ? AND file_path = ?", task.ID, execution.ID, "output/draft.json").
		Delete(&model.TaskFile{}).Error; err != nil {
		t.Fatal(err)
	}
	api := &fakeWechatPublicationAPI{}
	logger := zerolog.New(io.Discard)
	svc.SetWechatPublicationService(NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) {
		return api, nil
	}, &logger))

	status, evidence, err := svc.finalizeArticlePublication(ctx, task, execution)
	if err != nil {
		t.Fatal(err)
	}
	var result publicationDeliveryResult
	if err := json.Unmarshal(evidence, &result); err != nil {
		t.Fatal(err)
	}
	if status != model.TaskExecutionDraftDeliveryBlocked || result.Code != "publication_package_missing" || result.Attempted {
		t.Fatalf("status=%q evidence=%s", status, evidence)
	}
	if api.addCalls != 0 || api.draftListCalls != 0 {
		t.Fatalf("WeChat calls = add:%d list:%d, want none", api.addCalls, api.draftListCalls)
	}
	if _, err := repo.WechatPublications().FindByTaskID(ctx, task.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("publication intent unexpectedly exists: %v", err)
	}
}

func TestResolveCloudDraftDeliveryDoesNotPublishFailedExecution(t *testing.T) {
	svc, repo, db, task, execution := setupCloudCompletionTestWithDB(t, true)
	ctx := context.Background()
	if err := repo.TaskFiles().DeliverCurrentExecution(ctx, task.ID, execution.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).
		Update("status", model.TaskExecutionFailed).Error; err != nil {
		t.Fatal(err)
	}
	execution.Status = model.TaskExecutionFailed
	api := &fakeWechatPublicationAPI{
		draftListResponse: &appwechat.DraftBatchGetResponse{},
		addResponse:       &appwechat.DraftAddResponse{MediaID: "must-not-publish"},
	}
	logger := zerolog.New(io.Discard)
	svc.SetWechatPublicationService(NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) {
		return api, nil
	}, &logger))

	status, evidence, err := svc.resolveCloudDraftDelivery(ctx, task, execution)
	if err != nil {
		t.Fatal(err)
	}
	var result publicationDeliveryResult
	if err := json.Unmarshal(evidence, &result); err != nil {
		t.Fatal(err)
	}
	if status != model.TaskExecutionDraftDeliveryNotRequested || result.Code != "execution_not_succeeded" || result.Attempted {
		t.Fatalf("status=%q evidence=%s", status, evidence)
	}
	if api.addCalls != 0 || api.draftListCalls != 0 {
		t.Fatalf("WeChat calls = add:%d list:%d, want none", api.addCalls, api.draftListCalls)
	}
	if _, err := repo.WechatPublications().FindByTaskID(ctx, task.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("publication intent unexpectedly exists: %v", err)
	}
}

func TestCompleteCloudExecutionBlocksWhenRequestedContentImageIsNotInFinalHTML(t *testing.T) {
	svc, repo, db, task, execution := setupCloudCompletionTestWithDB(t, true)
	ctx := context.Background()
	withContentImages := true
	withoutCover := false
	task.ArticleWithContentImages = &withContentImages
	task.ArticleWithCover = &withoutCover
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/img_01.png", "image/png", tinyImagePNG(t))
	if err := db.Model(&model.TaskFile{}).
		Where("task_id = ? AND execution_id = ? AND file_path = ?", task.ID, execution.ID, "output/img_01.png").
		Updates(map[string]any{
			"role": model.FileRoleImage, "wechat_url": "https://mmbiz.qpic.cn/current-execution.png",
		}).Error; err != nil {
		t.Fatal(err)
	}
	api := &fakeWechatPublicationAPI{}
	logger := zerolog.New(io.Discard)
	svc.SetWechatPublicationService(NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) {
		return api, nil
	}, &logger))

	if err := svc.CompleteCloudExecution(ctx, execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Outcome == nil || found.Outcome.Publication.Status != model.TaskPublicationBlocked ||
		found.Outcome.Publication.Code != "content_images_missing" || found.Outcome.Publication.Attempted {
		t.Fatalf("publication outcome = %#v", found.Outcome)
	}
	if api.addCalls != 0 || api.draftListCalls != 0 {
		t.Fatalf("WeChat calls = add:%d list:%d, want none", api.addCalls, api.draftListCalls)
	}
	if _, err := repo.WechatPublications().FindByTaskID(ctx, task.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("publication intent unexpectedly exists: %v", err)
	}
}

func TestFinalizeArticlePublicationRejectsBodyImageOutsideCurrentExecution(t *testing.T) {
	svc, repo, db, task, execution := setupCloudCompletionTestWithDB(t, true)
	ctx := context.Background()
	withContentImages := true
	withoutCover := false
	task.ArticleWithContentImages = &withContentImages
	task.ArticleWithCover = &withoutCover
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	htmlBody := []byte(`<p>Final article</p><img src="https://mmbiz.qpic.cn/not-this-execution.png">`)
	htmlHash := fmt.Sprintf("%x", sha256.Sum256(htmlBody))
	packageBody, _ := json.Marshal(map[string]any{
		"schema_version": "1.0",
		"article": map[string]string{
			"title": "Server owned draft", "digest": "A deterministic package",
			"content_path": "output/05-article.html", "content_sha256": htmlHash,
		},
		"readiness": map[string]any{
			"status": "ready", "code": "",
			"evidence_paths": []string{"output/marketing-scan.json", "output/final-review.md", "output/viral-audit.md"},
		},
	})
	scanBody, _ := json.Marshal(map[string]any{
		"version": "1.0", "status": "passed", "content_hash": cloudArticleFixtureHash(), "findings": []any{},
	})
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/05-article.html", "text/html", htmlBody)
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/draft.json", "application/json", packageBody)
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/marketing-scan.json", "application/json", scanBody)
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/final-review.md", "text/markdown", []byte("# Final review\n\nPassed.\n"))
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/viral-audit.md", "text/markdown", []byte("# Viral audit\n\nPassed.\n"))
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/img_01.png", "image/png", []byte("content-image"))
	if err := db.Model(&model.TaskFile{}).
		Where("task_id = ? AND execution_id = ? AND file_path = ?", task.ID, execution.ID, "output/img_01.png").
		Updates(map[string]any{
			"role": model.FileRoleImage, "wechat_url": "https://mmbiz.qpic.cn/current-execution.png",
		}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().DeliverCurrentExecution(ctx, task.ID, execution.ID); err != nil {
		t.Fatal(err)
	}
	api := &fakeWechatPublicationAPI{}
	logger := zerolog.New(io.Discard)
	svc.SetWechatPublicationService(NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) {
		return api, nil
	}, &logger))

	status, evidence, err := svc.finalizeArticlePublication(ctx, task, execution)
	if err != nil {
		t.Fatal(err)
	}
	var result publicationDeliveryResult
	if err := json.Unmarshal(evidence, &result); err != nil {
		t.Fatal(err)
	}
	if status != model.TaskExecutionDraftDeliveryFailed || result.Code != "content_image_source_invalid" || result.Attempted {
		t.Fatalf("status=%q evidence=%s", status, evidence)
	}
	if api.addCalls != 0 || api.draftListCalls != 0 {
		t.Fatalf("WeChat calls = add:%d list:%d, want none", api.addCalls, api.draftListCalls)
	}
}

func TestFinalizeArticlePublicationRejectsContradictoryReadinessContract(t *testing.T) {
	tests := []struct {
		name          string
		code          string
		evidencePaths []string
	}{
		{
			name: "ready with blocking code", code: "cover_generation_failed",
			evidencePaths: []string{"output/marketing-scan.json", "output/final-review.md", "output/viral-audit.md"},
		},
		{
			name:          "duplicate evidence",
			evidencePaths: []string{"output/marketing-scan.json", "output/final-review.md", "output/viral-audit.md", "output/viral-audit.md"},
		},
		{
			name:          "extra evidence",
			evidencePaths: []string{"output/marketing-scan.json", "output/final-review.md", "output/viral-audit.md", "output/untrusted.json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, db, task, execution := setupCloudCompletionTestWithDB(t, true)
			ctx := context.Background()
			files, err := repo.TaskFiles().FindByExecutionID(ctx, execution.ID)
			if err != nil {
				t.Fatal(err)
			}
			store := svc.store.(*fakeTaskStorage)
			var htmlBody []byte
			for _, file := range files {
				if file.FilePath == "output/05-article.html" {
					htmlBody = store.files[file.OSSKey]
				}
				if err := db.Model(&model.TaskFile{}).Where("id = ?", file.ID).Update("state", model.TaskFileStateDelivered).Error; err != nil {
					t.Fatal(err)
				}
			}
			packageBody, _ := json.Marshal(map[string]any{
				"schema_version": "1.0",
				"article": map[string]string{
					"title": "Server owned draft", "digest": "A deterministic package",
					"content_path": "output/05-article.html", "content_sha256": fmt.Sprintf("%x", sha256.Sum256(htmlBody)),
				},
				"readiness": map[string]any{
					"status": "ready", "code": tt.code, "evidence_paths": tt.evidencePaths,
				},
			})
			for _, file := range files {
				if file.FilePath != "output/draft.json" {
					continue
				}
				store.files[file.OSSKey] = packageBody
				if err := db.Model(&model.TaskFile{}).Where("id = ?", file.ID).Update("file_size", len(packageBody)).Error; err != nil {
					t.Fatal(err)
				}
			}
			api := &fakeWechatPublicationAPI{addResponse: &appwechat.DraftAddResponse{MediaID: "must-not-publish"}}
			logger := zerolog.New(io.Discard)
			svc.SetWechatPublicationService(NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) { return api, nil }, &logger))

			status, evidence, err := svc.finalizeArticlePublication(ctx, task, execution)
			if err != nil {
				t.Fatal(err)
			}
			var result publicationDeliveryResult
			if err := json.Unmarshal(evidence, &result); err != nil {
				t.Fatal(err)
			}
			if status != model.TaskExecutionDraftDeliveryFailed || result.Code != "publication_package_invalid" || result.Attempted {
				t.Fatalf("status=%q evidence=%s", status, evidence)
			}
			if api.addCalls != 0 || api.draftListCalls != 0 {
				t.Fatalf("WeChat calls = add:%d list:%d, want none", api.addCalls, api.draftListCalls)
			}
		})
	}
}

func TestCompleteCloudExecutionPublishesReadyArticleExactlyOnce(t *testing.T) {
	svc, repo, db, task, execution := setupCloudCompletionTestWithDB(t, true)
	ctx := context.Background()
	withoutContentImages := false
	task.ArticleWithContentImages = &withoutContentImages
	task.SetProjectSnapshot(model.ProjectSnapshot{Author: "Frozen Author", Platform: model.PlatformArticle})
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	htmlBody := validTaskDeliveryFixtureBody("output/05-article.html", "text/html")
	htmlHash := fmt.Sprintf("%x", sha256.Sum256(htmlBody))
	packageBody, _ := json.Marshal(map[string]any{
		"schema_version": "1.0",
		"article": map[string]string{
			"title": "Server owned draft", "digest": "A deterministic package",
			"content_path": "output/05-article.html", "content_sha256": htmlHash,
		},
		"readiness": map[string]any{
			"status": "ready", "code": "",
			"evidence_paths": []string{"output/marketing-scan.json", "output/final-review.md", "output/viral-audit.md"},
		},
	})
	scanBody, _ := json.Marshal(map[string]any{
		"version": "1.0", "status": "passed", "content_hash": cloudArticleFixtureHash(), "findings": []any{},
	})
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/draft.json", "application/json", packageBody)
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/marketing-scan.json", "application/json", scanBody)
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/final-review.md", "text/markdown", []byte("# Final review\n\nPassed.\n"))
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/viral-audit.md", "text/markdown", []byte("# Viral audit\n\nPassed.\n"))
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/cover.png", "image/png", tinyImagePNG(t))
	files, err := repo.TaskFiles().FindByExecutionID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.FilePath == "output/cover.png" {
			file.Role = model.FileRoleCover
			file.MediaID = "cover-media-id"
			if err := db.Model(&model.TaskFile{}).Where("id = ?", file.ID).Updates(map[string]any{
				"role": model.FileRoleCover, "media_id": "cover-media-id",
			}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	api := &fakeWechatPublicationAPI{
		draftListResponse: &appwechat.DraftBatchGetResponse{},
		addResponse:       &appwechat.DraftAddResponse{MediaID: "draft-media-id"},
	}
	logger := zerolog.New(io.Discard)
	publicationSvc := NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) { return api, nil }, &logger)
	publicationSvc.now = func() time.Time { return time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC) }
	svc.SetWechatPublicationService(publicationSvc)

	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	if err := svc.CompleteCloudExecution(ctx, execution.ID, result); err != nil {
		t.Fatal(err)
	}
	found, _ := repo.Tasks().FindByID(ctx, task.ID)
	if found.Outcome == nil || found.Outcome.Publication.Status != model.TaskPublicationSucceeded ||
		!found.Outcome.Publication.Attempted {
		t.Fatalf("publication outcome = %#v", found.Outcome)
	}
	if api.addCalls != 1 {
		t.Fatalf("draft/add calls = %d, want 1", api.addCalls)
	}
	publication, err := repo.WechatPublications().FindByTaskID(ctx, task.ID)
	if err != nil || publication.DraftMediaID != "draft-media-id" || publication.DraftAuthor != "Frozen Author" || publication.DraftThumbMediaID != "cover-media-id" {
		t.Fatalf("publication = %#v err=%v", publication, err)
	}
}

func TestFinalizeCloudDraftDeliveryDoesNotMutateLiveInFlightAttempt(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	ctx := context.Background()
	evidence := encodedPublicationDeliveryEvidence("create_draft", model.TaskExecutionDraftDeliveryInFlight, "", true, "")
	if won, err := repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, "", model.TaskExecutionDraftDeliveryInFlight, evidence); err != nil || !won {
		t.Fatalf("mark in-flight delivery: won=%v err=%v", won, err)
	}
	attemptedAt := time.Now()
	if err := repo.WechatPublications().Create(ctx, &model.WechatPublication{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, UserID: task.UserID, ProjectID: task.ProjectID,
		Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafting,
		DraftAddAttemptedAt: &attemptedAt,
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.finalizeCloudDraftDelivery(ctx, task, execution, nil); err != nil {
		t.Fatalf("finalize concurrent in-flight delivery: %v", err)
	}
	stored, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DraftDeliveryStatus != model.TaskExecutionDraftDeliveryInFlight {
		t.Fatalf("delivery status = %q, want live in_flight ownership preserved", stored.DraftDeliveryStatus)
	}
}

func TestCompleteCloudExecutionSanitizesPublicExecutorDiagnostics(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	secret := "provider-secret-token"
	result := &agent.ExecutionResult{
		Success: false, Error: "raw " + secret,
		TerminalReason: model.TaskBillingTerminalProviderError,
		ErrorCode:      "provider_policy_rejection", PolicyDomain: "content_safety",
		ProviderCode: "content_exists_risk:" + secret, HTTPStatus: 999,
		ContentDirection: "output:" + secret, Recoverable: true,
		FailureStage: "writing:" + secret, ResumeFrom: "delivery:" + secret,
		RequestID: secret, RemoteArtifacts: true,
		ArtifactUploadFailures: []agent.ArtifactUploadFailure{{
			Path: "../../" + secret, Reason: "authorization failed: " + secret,
		}},
	}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatal(err)
	}
	found, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(found.Outcome)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("public outcome leaked executor-controlled diagnostic: %s", encoded)
	}
	if found.Outcome == nil || found.Outcome.Diagnostic == nil {
		t.Fatalf("sanitized diagnostic missing: %#v", found.Outcome)
	}
	diagnostic := found.Outcome.Diagnostic
	if diagnostic.ProviderCode != "" || diagnostic.HTTPStatus != 0 || diagnostic.Stage != "" ||
		diagnostic.ContentDirection != "unknown" || diagnostic.ResumePoint != "" ||
		diagnostic.RequestID != "sha256:f2939678ac70cc3c6223ed05ffd6ce4791ddec52b3e7b05389e527c0de22ef00" {
		t.Fatalf("unsafe diagnostic fields survived sanitization: %#v", diagnostic)
	}
}

func TestCompleteCloudExecutionDoesNotInferDraftSuccessFromLogs(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true, LogText: "Using tool: mcp__anban__create_draft"}

	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatal(err)
	}
	foundExecution, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundTask, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundExecution.DraftDeliveryStatus != model.TaskExecutionDraftDeliveryBlocked {
		t.Fatalf("draft delivery status = %q", foundExecution.DraftDeliveryStatus)
	}
	if foundTask.Outcome == nil || foundTask.Outcome.Publication.Status != model.TaskPublicationBlocked || foundTask.Outcome.Publication.Attempted {
		t.Fatalf("publication outcome = %#v", foundTask.Outcome)
	}
}

func TestCompleteCloudExecutionUsesDurableDraftPublication(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	ctx := context.Background()
	if err := repo.WechatPublications().Create(ctx, &model.WechatPublication{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, UserID: task.UserID, ProjectID: task.ProjectID,
		Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted,
		DraftMediaID:            "draft-media-id",
		DraftContentFingerprint: WechatContentFingerprint(string(validTaskDeliveryFixtureBody("output/05-article.html", "text/html"))),
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.CompleteCloudExecution(ctx, execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	foundTask, _ := repo.Tasks().FindByID(ctx, task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if foundExecution.DraftDeliveryStatus != model.TaskExecutionDraftDeliverySucceeded ||
		foundTask.Outcome == nil || foundTask.Outcome.Publication.Status != model.TaskPublicationSucceeded {
		t.Fatalf("draft=%q outcome=%#v", foundExecution.DraftDeliveryStatus, foundTask.Outcome)
	}
}

func TestCompleteCloudExecutionDoesNotTrustPublicationForChangedFinalArticle(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	ctx := context.Background()
	if err := repo.WechatPublications().Create(ctx, &model.WechatPublication{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, UserID: task.UserID, ProjectID: task.ProjectID,
		Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted,
		DraftMediaID: "draft-media-id", DraftContentFingerprint: WechatContentFingerprint("<p>different final article</p>"),
		DraftAddAttemptedAt: func() *time.Time { value := time.Now(); return &value }(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.CompleteCloudExecution(ctx, execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	foundTask, _ := repo.Tasks().FindByID(ctx, task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if foundExecution.DraftDeliveryStatus != model.TaskExecutionDraftDeliveryAmbiguous ||
		foundTask.Outcome == nil || foundTask.Outcome.Publication.Status != model.TaskPublicationAmbiguous {
		t.Fatalf("draft=%q outcome=%#v, want ambiguous publication", foundExecution.DraftDeliveryStatus, foundTask.Outcome)
	}
}

func TestCompleteCloudExecutionIgnoresDraftPublicationFromEarlierExecution(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	ctx := context.Background()
	if err := repo.WechatPublications().Create(ctx, &model.WechatPublication{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: uuid.NewString(), UserID: task.UserID, ProjectID: task.ProjectID,
		Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted,
		DraftMediaID: "old-draft-media-id",
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.CompleteCloudExecution(ctx, execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	foundTask, _ := repo.Tasks().FindByID(ctx, task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if foundExecution.DraftDeliveryStatus != model.TaskExecutionDraftDeliveryBlocked ||
		foundTask.Outcome == nil || foundTask.Outcome.Publication.Status != model.TaskPublicationBlocked || foundTask.Outcome.Publication.Attempted {
		t.Fatalf("draft=%q outcome=%#v", foundExecution.DraftDeliveryStatus, foundTask.Outcome)
	}
}

func TestCompleteCloudExecutionTreatsMalformedOptionalReportsAsWarnings(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/marketing-scan.json", "application/json", []byte(`{"status":`))
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/draft-result.json", "application/json", []byte(`{"status":`))

	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatalf("optional report must not block core delivery: %v", err)
	}
	found, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != model.TaskStatusCompleted || found.Outcome == nil ||
		found.Outcome.Review.Status != model.TaskReviewUnavailable ||
		found.Outcome.Publication.Status != model.TaskPublicationBlocked || found.Outcome.Publication.Attempted {
		t.Fatalf("task outcome = %#v", found)
	}
}

func addCloudOutcomeArtifact(t *testing.T, svc *TaskService, repo repository.Repository, task *model.Task, execution *model.TaskExecution, path, mimeType string, body []byte) {
	t.Helper()
	store, ok := svc.Storage().(*fakeTaskStorage)
	if !ok {
		t.Fatal("cloud completion test storage is unavailable")
	}
	key := buildTaskMCPArtifactStoragePrefix(task, execution.ID) + path
	store.files[key] = append([]byte(nil), body...)
	if _, err := repo.TaskFiles().Upsert(context.Background(), &model.TaskFile{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStatePending,
		Role: DetermineTaskFileRole(path, mimeType), FilePath: path, FileName: filepath.Base(path),
		MimeType: mimeType, FileSize: int64(len(body)), OSSKey: key, OSSURL: store.GetURL(key), StorageProvider: store.Name(),
	}); err != nil {
		t.Fatalf("create outcome artifact %s: %v", path, err)
	}
}

func cloudArticleFixtureHash() string {
	digest := sha256.Sum256(validTaskDeliveryFixtureBody("output/04-article-final.md", "text/markdown"))
	return fmt.Sprintf("%x", digest)
}

func TestCompleteCloudExecutionRollsBackArtifactsEvidenceAndTaskWhenBillingPersistenceFails(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	wantErr := errors.New("terminal billing unavailable")
	svc.repo = &failingCloudCoreRepository{Repository: repo, err: wantErr}
	result := &agent.ExecutionResult{
		Success: true, RemoteArtifacts: true,
		ModelUsage: []agent.ModelTokenUsage{{Provider: "provider", Model: "model", InputTokens: 11}},
		CostStatus: agent.CostStatusReconciled,
	}

	err := svc.CompleteCloudExecution(context.Background(), execution.ID, result)
	if !errors.Is(err, wantErr) {
		t.Fatalf("CompleteCloudExecution error = %v, want billing root cause", err)
	}
	persistedTask, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	persistedExecution, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	files, err := repo.TaskFiles().FindByExecutionID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedTask.Status != model.TaskStatusRunning || persistedTask.Result != nil ||
		persistedTask.CompletedAt != nil || persistedTask.BillingTerminalReason != "" ||
		persistedTask.WorkflowStatus != nil || len(persistedTask.TerminalModelUsage.Data()) != 0 ||
		persistedTask.CostStatus != "" {
		t.Fatalf("failed core transaction leaked task state: %#v", persistedTask)
	}
	if persistedExecution.FinalizationStatus != model.TaskExecutionFinalizationTerminal ||
		persistedExecution.ManifestStatus != model.TaskExecutionManifestPending {
		t.Fatalf("failed core transaction leaked execution state: %#v", persistedExecution)
	}
	for _, file := range files {
		if file.State != model.TaskFileStatePending {
			t.Fatalf("failed core transaction exposed artifact %#v", file)
		}
	}

	svc.repo = repo
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatalf("resume after atomic rollback: %v", err)
	}
	persistedTask, _ = repo.Tasks().FindByID(context.Background(), task.ID)
	persistedExecution, _ = repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if persistedTask.Status != model.TaskStatusCompleted || persistedTask.BillingTerminalReason != model.TaskBillingTerminalCompleted ||
		persistedExecution.FinalizationStatus != model.TaskExecutionFinalizationDone || persistedExecution.ManifestStatus != model.TaskExecutionManifestDelivered {
		t.Fatalf("resumed completion = task:%#v execution:%#v", persistedTask, persistedExecution)
	}
}

func TestCompleteCloudExecutionRejectsConflictingDuplicateResult(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	first := &agent.ExecutionResult{Success: true, RemoteArtifacts: true, LogText: "first"}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, first); err != nil {
		t.Fatal(err)
	}

	err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{
		Success: false, Error: "conflicting retry", RemoteArtifacts: true,
	})
	if !errors.Is(err, ErrTaskCompletionConflict) {
		t.Fatalf("conflicting duplicate completion = %v, want ErrTaskCompletionConflict", err)
	}

	foundTask, findErr := repo.Tasks().FindByID(context.Background(), task.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if foundTask.Status != model.TaskStatusCompleted || foundTask.Result == nil || !strings.Contains(*foundTask.Result, `"log_text":"first"`) {
		t.Fatalf("conflicting retry changed task evidence: status=%q result=%v", foundTask.Status, foundTask.Result)
	}
}

func TestSemanticJSONEqualRequiresExactlyOneJSONValue(t *testing.T) {
	tests := []struct {
		name      string
		left      string
		right     string
		wantEqual bool
		wantErr   bool
	}{
		{name: "whitespace and object key order", left: "  {\"large\":9007199254740993,\"nested\":{\"a\":1}}\n", right: "{\"nested\":{\"a\":1},\"large\":9007199254740993}", wantEqual: true},
		{name: "UseNumber preserves numeric spelling", left: "{\"value\":1}", right: "{\"value\":1.0}"},
		{name: "left trailing garbage", left: "{\"value\":1} trailing", right: "{\"value\":1}", wantErr: true},
		{name: "right trailing garbage", left: "{\"value\":1}", right: "{\"value\":1} trailing", wantErr: true},
		{name: "left second JSON value", left: "{\"value\":1} {\"second\":true}", right: "{\"value\":1}", wantErr: true},
		{name: "right second JSON value", left: "{\"value\":1}", right: "{\"value\":1} {\"second\":true}", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			equal, err := semanticJSONEqual([]byte(test.left), []byte(test.right))
			if equal != test.wantEqual || (err != nil) != test.wantErr {
				t.Fatalf("semanticJSONEqual = %v, %v; want equal=%v error=%v", equal, err, test.wantEqual, test.wantErr)
			}
		})
	}
}

func TestCompleteCloudExecutionRejectsInvalidStoredExecutionResultAsConflict(t *testing.T) {
	for _, suffix := range []string{" trailing", ` {"second":true}`} {
		t.Run(suffix, func(t *testing.T) {
			svc, repo, db, _, execution := setupCloudCompletionTestWithDB(t, false)
			ctx := context.Background()
			result := &agent.ExecutionResult{Success: false, Error: "provider unavailable", RemoteArtifacts: true}
			if err := svc.CompleteCloudExecution(ctx, execution.ID, result); err != nil {
				t.Fatalf("first completion: %v", err)
			}
			stored, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
			if err != nil {
				t.Fatal(err)
			}
			corrupt := append(append([]byte(nil), stored.Result...), suffix...)
			if err := db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).UpdateColumn("result", corrupt).Error; err != nil {
				t.Fatalf("corrupt stored result: %v", err)
			}
			if err := svc.CompleteCloudExecution(ctx, execution.ID, result); !errors.Is(err, ErrTaskCompletionConflict) {
				t.Fatalf("retry with corrupt stored result = %v, want ErrTaskCompletionConflict", err)
			}
		})
	}
}

func TestCompleteCloudExecutionAcceptsSameOriginalResultAfterServerNormalization(t *testing.T) {
	for _, test := range []struct {
		name          string
		withArtifact  bool
		result        func() *agent.ExecutionResult
		wantExecution string
	}{
		{
			name: "failure", result: func() *agent.ExecutionResult {
				return &agent.ExecutionResult{Success: false, Error: "provider unavailable", RemoteArtifacts: true}
			}, wantExecution: model.TaskExecutionFailed,
		},
		{
			name: "nested agent", result: func() *agent.ExecutionResult {
				return &agent.ExecutionResult{Success: true, RemoteArtifacts: true, ToolUseSummary: map[string]int{"TaskUpdate": 2, "Agent": 1}}
			}, withArtifact: true, wantExecution: model.TaskExecutionFailed,
		},
		{
			name: "artifact invalid", result: func() *agent.ExecutionResult {
				return &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
			}, wantExecution: model.TaskExecutionFailed,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, repo, _, execution := setupCloudCompletionTest(t, test.withArtifact)
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, test.result()); err != nil {
				t.Fatalf("first completion: %v", err)
			}
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, test.result()); err != nil {
				t.Fatalf("same original result retry: %v", err)
			}
			found, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
			if err != nil || found.Status != test.wantExecution || found.FinalizationStatus != model.TaskExecutionFinalizationDone {
				t.Fatalf("execution after retry = %#v, %v", found, err)
			}
		})
	}
}

func TestCompleteCloudExecutionDoesNotInferNilResultFromDiagnosticText(t *testing.T) {
	svc, repo, _, execution := setupCloudCompletionTest(t, false)
	result := &agent.ExecutionResult{Success: false, Error: missingExecutionResultDiagnostic}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatal(err)
	}
	found, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored agent.ExecutionResult
	if err := json.Unmarshal(found.Result, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.RemoteArtifacts {
		t.Fatal("explicit failure text was mistaken for a nil execution result")
	}
}

func TestCompleteCloudExecutionFencesEvidenceWhenAttemptBecomesStale(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	ctx := context.Background()
	next := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: 2, Target: "kubernetes",
		Status: model.TaskExecutionRunning, Started: true,
	}
	if err := repo.TaskExecutions().Create(ctx, next); err != nil {
		t.Fatal(err)
	}

	var switched bool
	svc.finalizationAfterStage = func(stage string) error {
		if stage != model.TaskExecutionFinalizationTerminal || switched {
			return nil
		}
		switched = true
		won, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, next.ID)
		if err != nil || !won {
			t.Fatalf("switch current execution: won=%v err=%v", won, err)
		}
		return nil
	}
	result := &agent.ExecutionResult{
		Success: true, RemoteArtifacts: true,
		ModelUsage: []agent.ModelTokenUsage{{Provider: "provider", Model: "stale-attempt", InputTokens: 31}},
		CostStatus: agent.CostStatusReconciled,
	}
	err := svc.CompleteCloudExecution(ctx, execution.ID, result)
	if !errors.Is(err, ErrStaleTaskExecution) {
		t.Fatalf("stale evidence finalization error = %v, want ErrStaleTaskExecution", err)
	}

	foundTask, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundTask.Result != nil || foundTask.CostStatus != "" || len(foundTask.TerminalModelUsage.Data()) != 0 {
		t.Fatalf("stale execution wrote task evidence: result=%v cost_status=%q usage=%+v", foundTask.Result, foundTask.CostStatus, foundTask.TerminalModelUsage.Data())
	}
	foundExecution, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundExecution.FinalizationStatus != model.TaskExecutionFinalizationTerminal {
		t.Fatalf("stale execution finalization advanced to %q, want %q", foundExecution.FinalizationStatus, model.TaskExecutionFinalizationTerminal)
	}
}

func TestStaleCloudFinalizerStopsBeforeTaskAndSettlementSideEffects(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	ctx := context.Background()
	if err := repo.Users().Create(ctx, &model.User{
		ID: task.UserID, Email: task.UserID + "@example.com", Password: "x", InviteCode: uuid.NewString()[:12],
	}); err != nil {
		t.Fatal(err)
	}
	terminalResult := &agent.ExecutionResult{Success: false, Error: "old attempt failed", RemoteArtifacts: true}
	encoded, err := json.Marshal(terminalResult)
	if err != nil {
		t.Fatal(err)
	}
	won, err := repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionFailed,
		model.ExecutionTransition{
			TerminalReason:     "old_attempt_failed",
			Result:             encoded,
			FinalizationStatus: model.TaskExecutionFinalizationResult,
		})
	if err != nil || !won {
		t.Fatalf("terminalize old execution: won=%v err=%v", won, err)
	}
	oldExecution, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: 2, Target: "kubernetes",
		Status: model.TaskExecutionRunning, Started: true,
	}
	if err := repo.TaskExecutions().Create(ctx, next); err != nil {
		t.Fatal(err)
	}
	if swapped, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, next.ID); err != nil || !swapped {
		t.Fatalf("switch current execution: swapped=%v err=%v", swapped, err)
	}

	err = svc.finalizeTaskFromExecution(ctx, task, oldExecution)
	if !errors.Is(err, ErrStaleTaskExecution) {
		t.Fatalf("stale finalizer error = %v, want ErrStaleTaskExecution", err)
	}
	foundTask, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundTask.Status != model.TaskStatusRunning || foundTask.WorkflowStatus != nil || foundTask.CurrentExecutionID == nil || *foundTask.CurrentExecutionID != next.ID {
		t.Fatalf("stale finalizer mutated current task: status=%q workflow=%v current=%v", foundTask.Status, foundTask.WorkflowStatus, foundTask.CurrentExecutionID)
	}
	foundOld, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundOld.FinalizationStatus != model.TaskExecutionFinalizationResult || foundOld.DraftDeliveryStatus != "" {
		t.Fatalf("stale finalizer advanced execution: stage=%q publishing=%q", foundOld.FinalizationStatus, foundOld.DraftDeliveryStatus)
	}
}

func TestCompleteCloudExecutionRejectsSuccessWithoutManifest(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if foundTask.Status != model.TaskStatusFailed || foundExecution.Status != model.TaskExecutionFailed || foundExecution.ManifestStatus != model.TaskExecutionManifestRetained {
		t.Fatalf("task=%s execution=%s manifest=%s", foundTask.Status, foundExecution.Status, foundExecution.ManifestStatus)
	}
}

func TestCompleteCloudExecutionCollectsArtifactsWhenValidationFails(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	task.Type = model.PlatformSeednote
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().DeleteByTaskID(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	failureFile := &model.TaskFile{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID,
		State: model.TaskFileStatePending, Role: model.FileRoleOther,
		FilePath: "output/failure-state.json", FileName: "failure-state.json", FileSize: 96,
	}
	if err := repo.TaskFiles().Create(context.Background(), failureFile); err != nil {
		t.Fatal(err)
	}

	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	foundExecution, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	files, err := repo.TaskFiles().FindByExecutionID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundExecution.Status != model.TaskExecutionFailed || foundExecution.ManifestStatus != model.TaskExecutionManifestRetained {
		t.Fatalf("execution status=%s manifest=%s", foundExecution.Status, foundExecution.ManifestStatus)
	}
	if len(files) != 1 || files[0].State != model.TaskFileStateRetained || files[0].FileName != "failure-state.json" {
		t.Fatalf("collected files = %#v", files)
	}
}

func TestCompleteCloudExecutionConcurrentDuplicate(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.CompleteCloudExecution(context.Background(), execution.ID, result)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatal(err)
	}
	found, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	files, _ := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if found.FinalizationStatus != model.TaskExecutionFinalizationDone || len(files) != 6 {
		t.Fatalf("finalization=%s files=%d", found.FinalizationStatus, len(files))
	}
}

func TestCompleteCloudExecutionResumesEveryDurableStage(t *testing.T) {
	stages := []string{
		model.TaskExecutionFinalizationTerminal,
		model.TaskExecutionFinalizationArtifacts,
		model.TaskExecutionFinalizationResult,
		model.TaskExecutionFinalizationWorkflow,
		model.TaskExecutionFinalizationDraftDelivery,
		model.TaskExecutionFinalizationSlot,
		model.TaskExecutionFinalizationDispatch,
		model.TaskExecutionFinalizationNotification,
	}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			svc, repo, task, execution := setupCloudCompletionTest(t, true)
			injected := false
			svc.finalizationAfterStage = func(got string) error {
				if got == stage && !injected {
					injected = true
					return errors.New("injected finalization failure")
				}
				return nil
			}
			result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err == nil {
				t.Fatal("expected injected failure")
			}
			svc.finalizationAfterStage = nil
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
				t.Fatalf("resume: %v", err)
			}
			foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
			foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
			if foundTask.Status != model.TaskStatusCompleted || foundExecution.FinalizationStatus != model.TaskExecutionFinalizationDone {
				t.Fatalf("task=%s finalization=%s", foundTask.Status, foundExecution.FinalizationStatus)
			}
		})
	}
	markerStages := append(append([]string(nil), stages[1:]...), model.TaskExecutionFinalizationDone)
	for _, stage := range markerStages {
		t.Run("after-marker-"+stage, func(t *testing.T) {
			svc, repo, task, execution := setupCloudCompletionTest(t, true)
			injected := false
			svc.finalizationAfterAdvance = func(got string) error {
				if got == stage && !injected {
					injected = true
					return errors.New("injected failure after durable marker")
				}
				return nil
			}
			result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err == nil {
				t.Fatal("expected injected failure")
			}
			svc.finalizationAfterAdvance = nil
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
				t.Fatalf("resume: %v", err)
			}
			foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
			foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
			if foundTask.Status != model.TaskStatusCompleted || foundExecution.FinalizationStatus != model.TaskExecutionFinalizationDone {
				t.Fatalf("task=%s finalization=%s", foundTask.Status, foundExecution.FinalizationStatus)
			}
		})
	}
}

func TestCompleteCloudExecutionResumesAfterCoreCommitBeforeStageMarker(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	injected := false
	svc.finalizationAfterStage = func(stage string) error {
		if stage == model.TaskExecutionFinalizationArtifacts && !injected {
			injected = true
			return errors.New("crash before core stage marker")
		}
		return nil
	}
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err == nil {
		t.Fatal("expected injected failure after core commit")
	}
	persistedTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
	persistedExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	files, _ := repo.TaskFiles().FindByExecutionID(context.Background(), execution.ID)
	if persistedTask.Status != model.TaskStatusCompleted || persistedTask.BillingTerminalReason != model.TaskBillingTerminalCompleted ||
		persistedExecution.FinalizationStatus != model.TaskExecutionFinalizationTerminal || persistedExecution.ManifestStatus != model.TaskExecutionManifestDelivered {
		t.Fatalf("core commit state = task:%#v execution:%#v", persistedTask, persistedExecution)
	}
	for _, file := range files {
		if file.State != model.TaskFileStateDelivered {
			t.Fatalf("core commit did not publish artifact %#v", file)
		}
	}

	svc.finalizationAfterStage = nil
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatalf("idempotent core resume: %v", err)
	}
	persistedExecution, _ = repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if persistedExecution.FinalizationStatus != model.TaskExecutionFinalizationDone {
		t.Fatalf("resumed finalization = %q, want done", persistedExecution.FinalizationStatus)
	}
}

func TestFinalizationDispatchReplayDoesNotDuplicateQueueOrInflateSlot(t *testing.T) {
	db := setupTaskTestDB(t)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	repo := repository.New(db)
	ctx := context.Background()
	userID, projectID := uuid.NewString(), uuid.NewString()
	project := &model.Project{ID: projectID, UserID: userID, Name: "dispatch", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	executionID := uuid.NewString()
	completedTask := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning, CurrentExecutionID: &executionID}
	if err := repo.Tasks().Create(ctx, completedTask); err != nil {
		t.Fatal(err)
	}
	pendingTask := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusPending}
	if err := repo.Tasks().Create(ctx, pendingTask); err != nil {
		t.Fatal(err)
	}
	execution := &model.TaskExecution{ID: executionID, TaskID: completedTask.ID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true, ManifestStatus: model.TaskExecutionManifestPending}
	if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{{
		ID: uuid.NewString(), TaskID: completedTask.ID, ExecutionID: executionID, State: model.TaskFileStatePending,
		Role: "content", FilePath: "output/content.md", FileName: "content.md", FileSize: 8,
	}}); err != nil {
		t.Fatal(err)
	}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	logger := zerolog.New(io.Discard)
	enqueuer := &replaySafeEnqueuer{}
	svc := newTestTaskService(repo, enqueuer, nil, &logger, "", NewRedisPubSub(rdb, &logger), nil)
	injected := false
	svc.finalizationAfterStage = func(stage string) error {
		if stage == model.TaskExecutionFinalizationDispatch && !injected {
			injected = true
			return errors.New("crash after pending dispatch")
		}
		return nil
	}
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	if err := svc.CompleteCloudExecution(ctx, executionID, result); err == nil {
		t.Fatal("expected injected crash after dispatch side effect")
	}
	found, _ := repo.TaskExecutions().FindByID(ctx, executionID)
	if found.FinalizationStatus != model.TaskExecutionFinalizationSlot || enqueuer.calls != 1 || enqueuer.accepted != 1 {
		t.Fatalf("interrupted stage=%s enqueue calls=%d accepted=%d", found.FinalizationStatus, enqueuer.calls, enqueuer.accepted)
	}
	if count, err := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int64(); err != nil || count != 1 {
		t.Fatalf("slot count after first dispatch=%d err=%v, want 1", count, err)
	}

	svc.finalizationAfterStage = nil
	if err := svc.ResumeExecutionFinalization(ctx, executionID); err != nil {
		t.Fatalf("resume finalization: %v", err)
	}
	found, _ = repo.TaskExecutions().FindByID(ctx, executionID)
	if found.FinalizationStatus != model.TaskExecutionFinalizationDone || enqueuer.calls != 2 || enqueuer.accepted != 1 {
		t.Fatalf("resumed stage=%s enqueue calls=%d accepted=%d", found.FinalizationStatus, enqueuer.calls, enqueuer.accepted)
	}
	if count, err := rdb.Get(ctx, projectRunningCountPrefix+projectID).Int64(); err != nil || count != 1 {
		t.Fatalf("slot count after replay=%d err=%v, want stable 1", count, err)
	}
}

func TestFinalizationLeaseRenewsAcrossLongStage(t *testing.T) {
	svc, repo, _, execution := setupCloudCompletionTest(t, true)
	svc.finalizationLease = 40 * time.Millisecond
	svc.finalizationRenewEvery = 5 * time.Millisecond
	entered, release := make(chan struct{}), make(chan struct{})
	svc.finalizationAfterStage = func(stage string) error {
		if stage == model.TaskExecutionFinalizationArtifacts {
			close(entered)
			<-release
		}
		return nil
	}
	done := make(chan error, 1)
	go func() {
		done <- svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true})
	}()
	<-entered
	time.Sleep(80 * time.Millisecond)
	won, err := repo.TaskExecutions().ClaimFinalization(context.Background(), execution.ID, uuid.NewString(), 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if won {
		t.Fatal("renewed finalization lease was stolen")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFinalizationRenewalLossPreventsStageAdvance(t *testing.T) {
	svc, repo, _, execution := setupCloudCompletionTest(t, true)
	svc.finalizationLease = 30 * time.Millisecond
	svc.finalizationRenewEvery = 2 * time.Millisecond
	stageFinished := make(chan struct{})
	svc.finalizationRenewClaim = func(context.Context, string, string) (bool, error) {
		<-stageFinished
		return false, nil
	}
	svc.finalizationAfterStage = func(stage string) error {
		if stage == model.TaskExecutionFinalizationArtifacts {
			close(stageFinished)
			time.Sleep(20 * time.Millisecond)
		}
		return nil
	}
	err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true})
	if !errors.Is(err, ErrFinalizationLeaseLost) {
		t.Fatalf("error=%v, want lease lost", err)
	}
	found, findErr := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if found.FinalizationStatus != model.TaskExecutionFinalizationTerminal {
		t.Fatalf("stage advanced after lease loss: %s", found.FinalizationStatus)
	}
}

type cancelOrderingDispatcher struct {
	repo            repository.Repository
	statusAtDelete  string
	deleteErr       error
	resolveErr      error
	resolveIdentity *model.RuntimeIdentity
	resolveCalls    int
	deletedIdentity model.RuntimeIdentity
	scope           string
}

type failingCleanupIdentityRepository struct {
	repository.Repository
	executions repository.TaskExecutionRepository
}

func (r *failingCleanupIdentityRepository) TaskExecutions() repository.TaskExecutionRepository {
	return r.executions
}

type failingCleanupIdentityExecutions struct {
	repository.TaskExecutionRepository
	err error
}

func (r *failingCleanupIdentityExecutions) SetCleanupRuntimeIdentity(context.Context, string, string, model.RuntimeIdentity) (bool, error) {
	return false, r.err
}

func (*cancelOrderingDispatcher) ResolveRuntime(string) srvconfig.RuntimeImageSelection {
	return srvconfig.RuntimeImageSelection{Profile: "article", Image: "registry/content@sha256:test"}
}

func (d *cancelOrderingDispatcher) Scope() string {
	if d.scope != "" {
		return d.scope
	}
	return "docker"
}

func (*cancelOrderingDispatcher) Prepare(_ context.Context, execution *model.TaskExecution, _ *model.Task) (*model.RuntimeIdentity, error) {
	return &model.RuntimeIdentity{Scope: "daemon-a", Workload: "container-" + execution.ID}, nil
}
func (d *cancelOrderingDispatcher) ResolvePrepared(_ context.Context, execution *model.TaskExecution, _ *model.Task) (*model.RuntimeIdentity, error) {
	d.resolveCalls++
	if d.resolveErr != nil {
		return nil, d.resolveErr
	}
	if d.resolveIdentity != nil {
		copy := *d.resolveIdentity
		return &copy, nil
	}
	return &model.RuntimeIdentity{Scope: "docker", Workload: "container-" + execution.ID, InstanceID: "instance-" + execution.ID}, nil
}
func (*cancelOrderingDispatcher) Activate(context.Context, *model.TaskExecution) error { return nil }
func (d *cancelOrderingDispatcher) Delete(_ context.Context, execution *model.TaskExecution) error {
	found, _ := d.repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	d.statusAtDelete = found.Status
	d.deletedIdentity = model.RuntimeIdentity{Scope: execution.RuntimeScope, Workload: execution.RuntimeWorkload, InstanceID: execution.RuntimeInstanceID}
	return d.deleteErr
}
func (*cancelOrderingDispatcher) Inspect(context.Context, *model.TaskExecution) (*agent.RuntimeExecutionState, error) {
	return nil, nil
}

func TestCancelCloudMarksAttemptBeforeDeleteAndDoesNotRollBack(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	dispatcher := &cancelOrderingDispatcher{repo: repo, deleteErr: errors.New("delete unavailable")}
	svc.SetRuntimeDispatcher(dispatcher)
	svc.cleanupRetryBackoff = time.Millisecond
	err := svc.CancelForUser(context.Background(), task.UserID, task.ID)
	if err == nil {
		t.Fatal("expected delete error")
	}
	foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if dispatcher.statusAtDelete != model.TaskExecutionCancelled || foundTask.Status != model.TaskStatusCancelled || foundExecution.Status != model.TaskExecutionCancelled {
		t.Fatalf("delete=%s task=%s execution=%s", dispatcher.statusAtDelete, foundTask.Status, foundExecution.Status)
	}
	if foundExecution.CompletedAt == nil || time.Since(*foundExecution.CompletedAt) > time.Minute {
		t.Fatalf("completed_at = %v", foundExecution.CompletedAt)
	}
	if foundExecution.CleanupStatus != model.TaskExecutionCleanupPending {
		t.Fatalf("cleanup status after delete error=%s", foundExecution.CleanupStatus)
	}
	dispatcher.deleteErr = nil
	time.Sleep(2 * time.Millisecond)
	if err := svc.CancelForUser(context.Background(), task.UserID, task.ID); err != nil {
		t.Fatalf("retry cancellation cleanup: %v", err)
	}
	foundExecution, _ = repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if foundExecution.CleanupStatus != model.TaskExecutionCleanupDone {
		t.Fatalf("cleanup status after retry=%s", foundExecution.CleanupStatus)
	}
}

func TestCancelCloudRecoversAndPersistsPreparedIdentityBeforeDelete(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	want := model.RuntimeIdentity{Scope: "docker", Workload: "container-" + execution.ID, InstanceID: "container-id-1"}
	dispatcher := &cancelOrderingDispatcher{repo: repo, resolveIdentity: &want}
	svc.SetRuntimeDispatcher(dispatcher)

	if err := svc.CancelForUser(context.Background(), task.UserID, task.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	found, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := model.RuntimeIdentity{Scope: found.RuntimeScope, Workload: found.RuntimeWorkload, InstanceID: found.RuntimeInstanceID}
	if dispatcher.resolveCalls != 1 || got != want || dispatcher.deletedIdentity != want || found.CleanupStatus != model.TaskExecutionCleanupDone {
		t.Fatalf("resolve=%d persisted=%#v deleted=%#v cleanup=%s", dispatcher.resolveCalls, got, dispatcher.deletedIdentity, found.CleanupStatus)
	}
}

func TestCancelCloudChecksActiveDispatchBarrierBeforePreparedLookup(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false, false)
	ctx := context.Background()
	if won, err := repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionStarting}, model.TaskExecutionDispatching, model.ExecutionTransition{}); err != nil || !won {
		t.Fatalf("move execution to dispatching: won=%v err=%v", won, err)
	}
	if won, err := repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, "in-flight-prepare", time.Minute); err != nil || !won {
		t.Fatalf("claim dispatch: won=%v err=%v", won, err)
	}
	svc.SetRuntimeDispatchLease(time.Minute)
	dispatcher := &cancelOrderingDispatcher{repo: repo, resolveErr: agent.ErrRuntimeWorkloadNotFound}
	svc.SetRuntimeDispatcher(dispatcher)

	err := svc.CancelForUser(ctx, task.UserID, task.ID)
	if !errors.Is(err, ErrRuntimePreparationInFlight) {
		t.Fatalf("cancel error = %v, want ErrRuntimePreparationInFlight", err)
	}
	found, findErr := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if found.CleanupStatus != model.TaskExecutionCleanupPending || dispatcher.resolveCalls != 0 || dispatcher.statusAtDelete != "" {
		t.Fatalf("cleanup=%s resolve=%d delete_status=%q", found.CleanupStatus, dispatcher.resolveCalls, dispatcher.statusAtDelete)
	}
}

func TestCancelCloudKeepsCleanupPendingWhenRuntimeRecoveryOrPersistenceFails(t *testing.T) {
	for _, tc := range []struct {
		name       string
		resolveErr error
		persistErr error
	}{
		{name: "recovery failure", resolveErr: errors.New("runtime lookup unavailable")},
		{name: "persistence failure", persistErr: errors.New("database unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, task, execution := setupCloudCompletionTest(t, true)
			if tc.persistErr != nil {
				svc.repo = &failingCleanupIdentityRepository{
					Repository: repo,
					executions: &failingCleanupIdentityExecutions{
						TaskExecutionRepository: repo.TaskExecutions(), err: tc.persistErr,
					},
				}
			}
			dispatcher := &cancelOrderingDispatcher{repo: repo, resolveErr: tc.resolveErr}
			svc.SetRuntimeDispatcher(dispatcher)
			svc.cleanupRetryBackoff = time.Millisecond

			if err := svc.CancelForUser(context.Background(), task.UserID, task.ID); err == nil {
				t.Fatal("expected cleanup recovery error")
			}
			found, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
			if err != nil {
				t.Fatal(err)
			}
			if found.CleanupStatus != model.TaskExecutionCleanupPending || dispatcher.statusAtDelete != "" {
				t.Fatalf("cleanup=%s delete_status=%q", found.CleanupStatus, dispatcher.statusAtDelete)
			}
		})
	}
}

func TestCancelCloudRejectsCleanupDispatcherTargetMismatch(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	dispatcher := &cancelOrderingDispatcher{repo: repo, scope: "other-provider"}
	svc.SetRuntimeDispatcher(dispatcher)
	svc.cleanupRetryBackoff = time.Millisecond

	err := svc.CancelForUser(context.Background(), task.UserID, task.ID)
	if !errors.Is(err, ErrRuntimeDispatcherTargetMismatch) {
		t.Fatalf("cancel error = %v, want ErrRuntimeDispatcherTargetMismatch", err)
	}
	found, findErr := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if found.CleanupStatus != model.TaskExecutionCleanupPending || dispatcher.resolveCalls != 0 || dispatcher.statusAtDelete != "" {
		t.Fatalf("cleanup=%s resolve=%d delete_status=%q", found.CleanupStatus, dispatcher.resolveCalls, dispatcher.statusAtDelete)
	}
}

func TestReconcileExecutionFailureAlwaysTerminalizesCurrentExecution(t *testing.T) {
	t.Run("pre-start failure does not create a replacement", func(t *testing.T) {
		svc, repo, task, execution := setupCloudCompletionTest(t, false, false)
		dispatcher := &dispatchTestDispatcher{}
		svc.SetRuntimeDispatcher(dispatcher)
		if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "image_pull_failed", nil); err != nil {
			t.Fatal(err)
		}
		current, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		old, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
		foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
		if current.ID != execution.ID || current.Attempt != 1 || current.Status != model.TaskExecutionFailed || old.Status != model.TaskExecutionFailed || foundTask.Status != model.TaskStatusFailed {
			t.Fatalf("current=%+v old=%s task=%s", current, old.Status, foundTask.Status)
		}
		if dispatcher.callCount() != 0 {
			t.Fatalf("provider dispatches=%d, want 0", dispatcher.callCount())
		}
	})

	t.Run("post-start enters terminal finalizer", func(t *testing.T) {
		svc, repo, task, execution := setupCloudCompletionTest(t, true, true)
		dispatcher := &dispatchTestDispatcher{}
		svc.SetRuntimeDispatcher(dispatcher)
		if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "job_failed", nil); err != nil {
			t.Fatal(err)
		}
		current, _ := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
		foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
		if current.ID != execution.ID || current.Status != model.TaskExecutionFailed || foundTask.Status != model.TaskStatusFailed || dispatcher.callCount() != 0 {
			t.Fatalf("current=%+v task=%s dispatches=%d", current, foundTask.Status, dispatcher.callCount())
		}
	})
}

func TestReconcileExecutionFailurePersistsMachineReadableRootCause(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false, false)
	if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "execution_identity_unavailable", []byte(`{"completion_report_failed":true}`)); err != nil {
		t.Fatal(err)
	}

	foundExecution, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	var result agent.ExecutionResult
	if err := json.Unmarshal(foundExecution.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.RootErrorCode != "execution_identity_unavailable" {
		t.Fatalf("result = %#v", result)
	}
	foundTask, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundTask.ErrorMessage != "execution_identity_unavailable" {
		t.Fatalf("error_message = %q", foundTask.ErrorMessage)
	}
}

func TestReconcilePreStartFailureKeepsOriginalExecution(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false, false)
	dispatcher := &dispatchTestDispatcher{}
	svc.SetRuntimeDispatcher(dispatcher)
	if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "image_pull_failed", nil); err != nil {
		t.Fatal(err)
	}
	current, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != execution.ID || current.Attempt != execution.Attempt || current.Status != model.TaskExecutionFailed {
		t.Fatalf("current execution = %#v, want original terminal execution", current)
	}
	foundTask, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil || foundTask.Status != model.TaskStatusFailed {
		t.Fatalf("task = %#v, err=%v", foundTask, err)
	}
	if dispatcher.callCount() != 0 {
		t.Fatalf("provider dispatches=%d, want 0", dispatcher.callCount())
	}
}

func TestReconcilePreStartFailureDoesNotResetProviderIdentity(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false, false)
	ctx := context.Background()
	oldIdentity := model.RuntimeIdentity{
		Scope:      "anban",
		Workload:   "job-" + execution.ID,
		InstanceID: "pod-old",
	}
	if err := repo.TaskExecutions().SetRuntimeIdentity(ctx, execution.ID, oldIdentity); err != nil {
		t.Fatalf("persist old Kubernetes identity: %v", err)
	}
	dispatcher := &dispatchTestDispatcher{}
	svc.SetRuntimeDispatcher(dispatcher)

	if err := svc.ReconcileExecutionFailure(ctx, execution.ID, model.TaskExecutionFailed, "image_pull_failed", nil); err != nil {
		t.Fatalf("reconcile failure: %v", err)
	}
	current, err := repo.TaskExecutions().FindCurrentByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != execution.ID || current.Attempt != execution.Attempt || current.Target != execution.Target || current.RuntimeScope != oldIdentity.Scope || current.RuntimeWorkload != oldIdentity.Workload {
		t.Fatalf("current target/identity = %#v, want original execution", current)
	}
	if current.RuntimeInstanceID != oldIdentity.InstanceID {
		t.Fatalf("current instance = %q, want %q", current.RuntimeInstanceID, oldIdentity.InstanceID)
	}
}

func TestReconcilePreStartFailureDoesNotResumeAfterTransientFailure(t *testing.T) {
	svc, repo, _, task, execution := setupCloudCompletionTestWithDB(t, false, false)
	dispatcher := &dispatchTestDispatcher{err: errors.New("temporary Kubernetes API failure")}
	svc.SetRuntimeDispatcher(dispatcher)
	if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "image_pull_failed", nil); err != nil {
		t.Fatalf("reconcile failure: %v", err)
	}
	current, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != execution.ID || current.Attempt != 1 || current.Status != model.TaskExecutionFailed {
		t.Fatalf("current=%+v", current)
	}
	if dispatcher.callCount() != 0 || dispatcher.createCount() != 0 {
		t.Fatalf("provider dispatches=%d creates=%d, want 0", dispatcher.callCount(), dispatcher.createCount())
	}
}

func TestConcurrentPreStartReconcileDoesNotCreateReplacement(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false, false)
	dispatcher := &dispatchTestDispatcher{}
	svc.SetRuntimeDispatcher(dispatcher)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "scheduling_failed", nil)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrStaleTaskExecution) {
			t.Fatal(err)
		}
	}
	current, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := repo.TaskExecutions().NextAttempt(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != execution.ID || current.Attempt != 1 || current.Status != model.TaskExecutionFailed || next != 2 || dispatcher.createCount() != 0 {
		t.Fatalf("current id=%s attempt=%d next=%d jobs=%d", current.ID, current.Attempt, next, dispatcher.createCount())
	}
}

func TestBootstrapStartedBoundaryPreventsPreStartReplacement(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false, false)
	dispatcher := &dispatchTestDispatcher{}
	svc.SetRuntimeDispatcher(dispatcher)
	if won, err := repo.TaskExecutions().Transition(context.Background(), execution.ID,
		[]string{model.TaskExecutionStarting}, model.TaskExecutionRunning,
		model.ExecutionTransition{Started: true, RuntimeInstanceID: "pod-1"}); err != nil || !won {
		t.Fatalf("mark bootstrap started: won=%v err=%v", won, err)
	}
	if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "job_failed", nil); err != nil {
		t.Fatal(err)
	}
	current, _ := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if current.ID != execution.ID || current.Attempt != 1 || !current.Started || current.Status != model.TaskExecutionFailed {
		t.Fatalf("current=%+v", current)
	}
	if dispatcher.createCount() != 0 {
		t.Fatalf("replacement jobs=%d", dispatcher.createCount())
	}
}
