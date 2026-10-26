package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/billing"
	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContentPlanningResearchAdmission(t *testing.T) {
	svc, repo, _, existing, _ := planningFixture(t)
	userID := existing.UserID
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	tasks, err := svc.CreateManual(context.Background(), CreateManualParams{UserID: userID, ProjectID: projectID, AgentID: "content-planning", Channel: "content-planning", TaskKind: model.TaskKindContentResearch, RequestedTaskType: model.TaskTypeContentResearch, ExecutionProfile: "effective", Prompt: "Discover evidenced content", AgentInput: map[string]any{"schema_version": "1.0", "task_kind": model.TaskKindContentResearch, "candidates": []any{}, "accepted_artifacts": []any{}}})
	if err != nil {
		t.Fatalf("planning task admission: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Type != model.TaskTypeContentResearch || tasks[0].TaskKind != model.TaskKindContentResearch {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}
	charge, err := repo.Billing().FindChargeByTask(context.Background(), tasks[0].ID)
	if err != nil || charge.PriceCredits <= 0 || tasks[0].BillingSKUID != "task.content_research.effective" || tasks[0].BillingQuoteID == "" {
		t.Fatalf("planning admission billing: task=%+v charge=%+v err=%v", tasks[0], charge, err)
	}
}

type planningTestDispatcher struct{ dispatchTestDispatcher }

func (*planningTestDispatcher) ResolveRuntime(string) serverconfig.RuntimeImageSelection {
	return serverconfig.RuntimeImageSelection{Profile: "content-planning", Image: "creator-agent-content-planning:test"}
}

func TestContentPlanningMissingRuntimeAndForgedInputRejected(t *testing.T) {
	svc, repo, _, existing, _ := planningFixture(t)
	svc.SetRuntimeDispatcher(nil)
	userID := existing.UserID
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	params := CreateManualParams{UserID: userID, ProjectID: projectID, AgentID: "content-planning", Channel: "content-planning", TaskKind: "content_research", ExecutionProfile: "effective", AgentInput: planningInputMap(t, model.ContentPlanningInput{SchemaVersion: "1.0", TaskKind: "content_research", Candidates: []model.PlanningCandidate{}, AcceptedArtifacts: []model.PlanningArtifactRef{}})}
	if _, err := svc.CreateManual(context.Background(), params); !errors.Is(err, ErrContentPlanningUnavailable) {
		t.Fatalf("missing runtime: %v", err)
	}
	svc.SetRuntimeDispatcher(&planningTestDispatcher{})
	params.AgentInput["internal_evidence"] = []any{map[string]any{"fabricated": true}}
	if _, err := svc.CreateManual(context.Background(), params); !errors.Is(err, ErrInvalidAgentInput) {
		t.Fatalf("forged frozen evidence accepted: %v", err)
	}
	delete(params.AgentInput, "internal_evidence")
	params.AgentInput["target_requirements"] = map[string]any{"platform": "hypit"}
	if _, err := svc.CreateManual(context.Background(), params); !errors.Is(err, ErrInvalidAgentInput) {
		t.Fatalf("forged target: %v", err)
	}
}

func planningInputMap(t *testing.T, value model.ContentPlanningInput) map[string]any {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func planningFixture(t *testing.T) (*TaskService, repository.Repository, *gorm.DB, *model.Task, *model.TaskExecution) {
	t.Helper()
	svc, repo, db, task, execution := setupCloudCompletionTestWithDB(t, false)
	if err := db.AutoMigrate(&model.SearchOperation{}); err != nil {
		t.Fatal(err)
	}
	task.Type = model.TaskTypeContentResearch
	task.TaskKind = model.TaskKindContentResearch
	task.AgentID = "content-planning"
	task.Channel = task.AgentID
	task.AgentInput = datatypes.NewJSONType(planningInputMap(t, model.ContentPlanningInput{SchemaVersion: "1.0", TaskKind: task.TaskKind, Candidates: []model.PlanningCandidate{}, AcceptedArtifacts: []model.PlanningArtifactRef{}}))
	task.SetProjectSnapshot(model.ProjectSnapshot{Platform: model.PlatformWechat})
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	execution.ManifestSealed = true
	execution.TaskKind = task.TaskKind
	if err := applyAgentPackIdentity(execution, task.AgentID); err != nil {
		t.Fatal(err)
	}
	svc.SetRuntimeDispatcher(&planningTestDispatcher{})
	planningConfigureBilling(t, svc, repo, db, task.UserID)
	return svc, repo, db, task, execution
}

func planningPut(t *testing.T, svc *TaskService, repo repository.Repository, task *model.Task, execution *model.TaskExecution, name string, value any, delivered bool) model.PlanningArtifactRef {
	t.Helper()
	var body []byte
	mime := "application/json"
	if text, ok := value.(string); ok {
		body = []byte(text)
		mime = "text/markdown"
		if strings.HasSuffix(name, ".srt") {
			mime = "application/x-subrip"
		}
	} else {
		var err error
		body, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	hash := planningHash(body)
	key := buildTaskArtifactFinalStorageKey(task, execution.ID, hash, name)
	svc.store.(*fakeTaskStorage).files[key] = body
	state := model.TaskFileStatePending
	if delivered {
		state = model.TaskFileStateDelivered
	}
	_, err := repo.TaskFiles().Upsert(context.Background(), &model.TaskFile{ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, State: state, FilePath: name, FileName: filepath.Base(name), Role: DetermineTaskFileRole(name, mime), MimeType: mime, FileSize: int64(len(body)), ContentHash: hash, OSSKey: key, StorageProvider: svc.store.Name()})
	if err != nil {
		t.Fatal(err)
	}
	return model.PlanningArtifactRef{TaskID: task.ID, ExecutionID: execution.ID, Path: name, ContentSHA256: hash}
}
func planningResearchOutputs(task *model.Task, execution *model.TaskExecution, source model.PlanningSource) (model.PlanningResearchResult, model.PlanningResearchEvidence) {
	id := model.PlanningIdentity{SchemaVersion: "1.0", TaskID: task.ID, ExecutionID: execution.ID, TaskKind: task.TaskKind, Status: "ready", Missing: []string{}}
	assessment := model.PlanningAssessment{Score: 60, Rationale: "Evidence relates to the target audience", SourceRefs: []string{source.ID}}
	return model.PlanningResearchResult{PlanningIdentity: id, Candidates: []model.PlanningProposal{{ID: "candidate-1", Title: "Evidence-backed topic", SemanticKernel: "A durable question", SourceRefs: []string{source.ID}, MergeAssetIDs: []string{}, TargetFit: assessment, Transferability: assessment, Angles: []string{"Practical explanation"}, ProductionCost: "text stage only", MaterialRisk: "none", ComplianceRisk: "verify claims"}}}, model.PlanningResearchEvidence{PlanningIdentity: id, Sources: []model.PlanningSource{source}}
}
func TestContentPlanningResearchRejectsForgedReceiptAndIdentity(t *testing.T) {
	svc, repo, db, task, execution := planningFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	response, _ := json.Marshal(WebSearchResponse{FetchedAt: now, Results: []SearchResultItem{{URL: "https://example.org/evidence", Title: "Actual source fact", PublishedAt: "2026-10-19"}}})
	operation := &model.SearchOperation{ID: uuid.NewString(), Operation: "mcp.search_web", UserID: task.UserID, ProjectID: task.ProjectID, TaskID: task.ID, ExecutionID: execution.ID, Status: model.SearchOperationSucceeded, RequestFingerprint: strings.Repeat("a", 64), AttemptID: "attempt", ResponseSnapshot: response, UpdatedAt: now}
	if err := db.Create(operation).Error; err != nil {
		t.Fatal(err)
	}
	source := model.PlanningSource{ID: "source-1", OperationID: operation.ID, URL: "https://example.org/evidence", PublishedAt: "2026-10-19", FetchedAt: now.Format(time.RFC3339), Facts: []string{"Actual source fact"}}
	research, evidence := planningResearchOutputs(task, execution, source)
	planningPut(t, svc, repo, task, execution, "output/content-research.json", research, false)
	planningPut(t, svc, repo, task, execution, "output/research-evidence.json", evidence, false)
	if err := svc.validateContentPlanningArtifacts(context.Background(), task, execution); err != nil {
		t.Fatalf("valid evidence: %v", err)
	}
	status, _, result, err := svc.cloudTerminalOutcome(context.Background(), task, execution, &agent.ExecutionResult{Success: true})
	if err != nil || status != model.TaskExecutionSucceeded {
		t.Fatalf("actual research completion: status=%s result=%+v error=%v", status, result, err)
	}
	for _, mutation := range []string{"url", "execution", "facts", "merge"} {
		t.Run(mutation, func(t *testing.T) {
			r, e := planningResearchOutputs(task, execution, source)
			switch mutation {
			case "url":
				e.Sources[0].URL = "https://forged.example"
			case "execution":
				e.ExecutionID = "other"
			case "facts":
				e.Sources[0].Facts = []string{"Invented popularity 1000000"}
			case "merge":
				r.Candidates[0].MergeAssetIDs = []string{"foreign"}
			}
			planningPut(t, svc, repo, task, execution, "output/content-research.json", r, false)
			planningPut(t, svc, repo, task, execution, "output/research-evidence.json", e, false)
			if err := svc.validateContentPlanningArtifacts(context.Background(), task, execution); err == nil {
				t.Fatal("forged evidence accepted")
			}
		})
	}
}
func TestContentPlanningReceiptScopeAndCapabilities(t *testing.T) {
	svc, _, db, task, execution := planningFixture(t)
	ctx := context.Background()
	for i, scope := range []string{"current", "foreign_user", "foreign_execution", "failed"} {
		row := model.SearchOperation{ID: uuid.NewString(), Operation: "mcp.search_web", UserID: task.UserID, ProjectID: task.ProjectID, TaskID: task.ID, ExecutionID: execution.ID, Status: model.SearchOperationSucceeded, RequestFingerprint: fmt.Sprintf("%064d", i), AttemptID: "a", ResponseSnapshot: []byte(`{"results":[]}`)}
		if scope == "foreign_user" {
			row.UserID = "foreign"
		}
		if scope == "foreign_execution" {
			row.ExecutionID = "old"
		}
		if scope == "failed" {
			row.Status = model.SearchOperationFailed
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	receipts, err := svc.ListSearchReceipts(ctx, task.UserID, task.ProjectID, task.ID, execution.ID)
	if err != nil || len(receipts) != 1 {
		t.Fatalf("scoped receipts=%v err=%v", receipts, err)
	}
	for _, tool := range []string{"generate_image", "analyze_video", "upload_image", "create_draft", "submit_profile_result", "claim_topic"} {
		if err := svc.ValidateAgentExecutionToolAccess(ctx, task.UserID, task.ProjectID, task.ID, execution.ID, tool); !errors.Is(err, ErrTaskCapabilityAccessDenied) {
			t.Fatalf("paid/mutating tool %s: %v", tool, err)
		}
	}
	if err := svc.ValidateAgentExecutionToolAccess(ctx, task.UserID, task.ProjectID, task.ID, execution.ID, "search_web"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateAgentExecutionToolAccess(ctx, task.UserID, task.ProjectID, task.ID, execution.ID, "search_images"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateAgentExecutionToolAccess(ctx, task.UserID, task.ProjectID, task.ID, "stale", "search_web"); err == nil {
		t.Fatal("stale execution accepted")
	}
}
func TestContentPlanningAcceptedArtifactsCrossProjectAndBootstrap(t *testing.T) {
	svc, repo, db, source, execution := planningFixture(t)
	ctx := context.Background()
	source.Status = model.TaskStatusCompleted
	if err := repo.Tasks().Update(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).Updates(map[string]any{"status": model.TaskExecutionSucceeded, "manifest_sealed": true}).Error; err != nil {
		t.Fatal(err)
	}
	targetID := createTestProject(t, repo, source.UserID, model.PlatformMontage)
	refs := []model.PlanningArtifactRef{}
	research, evidence := planningResearchOutputs(source, execution, model.PlanningSource{ID: "s", Facts: []string{"fact"}})
	refs = append(refs, planningPut(t, svc, repo, source, execution, "output/content-research.json", research, true), planningPut(t, svc, repo, source, execution, "output/research-evidence.json", evidence, true))
	params := CreateManualParams{UserID: source.UserID, ProjectID: targetID, AgentID: "content-planning", Channel: "content-planning", TaskKind: "content_validation", ExecutionProfile: "effective", AgentInput: planningInputMap(t, model.ContentPlanningInput{SchemaVersion: "1.0", TaskKind: "content_validation", Candidates: []model.PlanningCandidate{}, AcceptedArtifacts: refs})}
	tasks, err := svc.CreateManual(ctx, params)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("same-owner cross-project admission: %v", err)
	}
	target := tasks[0]
	bootstrap := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: svc.store}}
	files, err := bootstrap.buildContentPlanningFiles(ctx, &model.TaskExecution{ID: "next"}, target, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files[0].ContentSHA256 != refs[0].ContentSHA256 || !strings.Contains(files[2].Text, `"execution_id":"next"`) {
		t.Fatalf("frozen bootstrap: %+v", files)
	}
	for _, change := range []string{"hash", "execution", "task", "foreign_project"} {
		t.Run(change, func(t *testing.T) {
			ref := refs[0]
			projectID := targetID
			switch change {
			case "hash":
				ref.ContentSHA256 = strings.Repeat("f", 64)
			case "execution":
				ref.ExecutionID = "other"
			case "task":
				ref.TaskID = "other"
			case "foreign_project":
				projectID = createTestProject(t, repo, uuid.NewString(), model.PlatformMontage)
			}
			if _, _, err := planningAcceptedArtifact(ctx, repo, svc.store, source.UserID, projectID, ref); err == nil {
				t.Fatal("unowned/stale artifact accepted")
			}
		})
	}
}

func TestContentPlanningQuoteFreezesOwnSKUAndTariffSource(t *testing.T) {
	repo := newBillingServiceRepository(t)
	bundle, err := billing.LoadBundle(filepath.Join("..", "billing"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := NewBillingCatalogService(repo, bundle, BillingCatalogOptions{})
	if _, err := catalog.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatal(err)
	}
	catalog.SetAgentProfileRegistry(registry)
	if err := repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{UserID: billingCatalogUserID, PaidCredits: 10000}); err != nil {
		t.Fatal(err)
	}
	for _, taskType := range []string{model.TaskTypeContentResearch, model.TaskTypeContentValidation, model.TaskTypeContentScript} {
		kind := model.TaskKindForTaskType(taskType)
		q, err := catalog.CreateTaskQuote(context.Background(), TaskQuoteRequest{UserID: billingCatalogUserID, TaskType: taskType, ExecutionProfile: "effective", RequestFingerprint: billingFingerprint(taskType), IdempotencyScope: "planning", IdempotencyKey: taskType})
		if err != nil {
			t.Fatal(err)
		}
		var sku billing.SKUConfig
		if err = json.Unmarshal(q.SKUSnapshot, &sku); err != nil {
			t.Fatal(err)
		}
		if sku.Operation != "task."+kind || sku.PriceSource != "task.viral-analysis.effective" || sku.PriceCredits != 1200 {
			t.Fatalf("frozen SKU: %+v", sku)
		}
	}
}

func TestContentPlanningValidationScriptAdmissionAndSemanticContracts(t *testing.T) {
	svc, repo, db, source, sourceExec := planningFixture(t)
	ctx := context.Background()
	source.Status = model.TaskStatusCompleted
	if err := repo.Tasks().Update(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TaskExecution{}).Where("id = ?", sourceExec.ID).Updates(map[string]any{"status": model.TaskExecutionSucceeded, "manifest_sealed": true}).Error; err != nil {
		t.Fatal(err)
	}
	r, e := planningResearchOutputs(source, sourceExec, model.PlanningSource{ID: "source-1", OperationID: "receipt", URL: "https://example.org", PublishedAt: "", FetchedAt: time.Now().UTC().Format(time.RFC3339), Facts: []string{"fact"}})
	refs := []model.PlanningArtifactRef{planningPut(t, svc, repo, source, sourceExec, "output/content-research.json", r, true), planningPut(t, svc, repo, source, sourceExec, "output/research-evidence.json", e, true)}
	targetProject := createTestProject(t, repo, source.UserID, model.PlatformMontage)
	input := model.ContentPlanningInput{SchemaVersion: "1.0", TaskKind: "content_validation", Candidates: []model.PlanningCandidate{}, AcceptedArtifacts: refs, TargetRequirements: &model.PlanningTargetRequirements{Platform: model.PlatformMontage, DurationSeconds: 10}}
	params := CreateManualParams{UserID: source.UserID, ProjectID: targetProject, AgentID: "content-planning", Channel: "content-planning", TaskKind: input.TaskKind, ExecutionProfile: "effective", AgentInput: planningInputMap(t, input)}
	tasks, err := svc.CreateManual(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	validation := tasks[0]
	validationExec := &model.TaskExecution{ID: uuid.NewString(), TaskID: validation.ID, TaskKind: validation.TaskKind, Status: model.TaskExecutionSucceeded, ManifestSealed: true}
	if err := applyAgentPackIdentity(validationExec, validation.AgentID); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, validationExec); err != nil {
		t.Fatal(err)
	}
	article := "# Validation article\n\n" + strings.Repeat("The cited material supports this practical claim with explicit limitations. ", 4)
	articleRef := planningPut(t, svc, repo, validation, validationExec, "output/validation-article.md", article, true)
	makeReview := func(task *model.Task, exec *model.TaskExecution, hash string, script bool) model.PlanningReview {
		checks := []model.PlanningCheck{}
		ids := []string{"facts", "evidence", "compliance", "target_fit"}
		if script {
			ids = append(ids, "timing", "materials")
		}
		for _, id := range ids {
			checks = append(checks, model.PlanningCheck{ID: id, Status: "pass", Rationale: "Reviewed against source-1", SourceRefs: []string{"source-1"}})
		}
		return model.PlanningReview{PlanningIdentity: model.PlanningIdentity{SchemaVersion: "1.0", TaskID: task.ID, ExecutionID: exec.ID, TaskKind: task.TaskKind, Status: "ready", Missing: []string{}}, ValidationKind: "semantic_only", ContentSHA256: hash, Checks: checks, SourceRefs: []string{"source-1"}}
	}
	review := makeReview(validation, validationExec, articleRef.ContentSHA256, false)
	reviewRef := planningPut(t, svc, repo, validation, validationExec, "output/validation-review.json", review, true)
	if err := svc.validateContentPlanningArtifacts(ctx, validation, validationExec); err != nil {
		t.Fatal(err)
	}
	review.ValidationKind = "audience_performance"
	planningPut(t, svc, repo, validation, validationExec, "output/validation-review.json", review, true)
	if err := svc.validateContentPlanningArtifacts(ctx, validation, validationExec); err == nil {
		t.Fatal("claimed market validation accepted")
	}
	review.ValidationKind = "semantic_only"
	reviewRef = planningPut(t, svc, repo, validation, validationExec, "output/validation-review.json", review, true)
	validation.Status = model.TaskStatusCompleted
	if err := repo.Tasks().Update(ctx, validation); err != nil {
		t.Fatal(err)
	}
	input.TaskKind = "content_script"
	input.AcceptedArtifacts = append(refs, articleRef, reviewRef)
	params.TaskKind = input.TaskKind
	params.AgentInput = planningInputMap(t, input)
	tasks, err = svc.CreateManual(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	scriptTask := tasks[0]
	scriptExec := &model.TaskExecution{ID: uuid.NewString(), TaskID: scriptTask.ID, TaskKind: scriptTask.TaskKind, Status: model.TaskExecutionRunning, ManifestSealed: true}
	if err := applyAgentPackIdentity(scriptExec, scriptTask.AgentID); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, scriptExec); err != nil {
		t.Fatal(err)
	}
	script := model.PlanningVideoScript{PlanningIdentity: model.PlanningIdentity{SchemaVersion: "1.0", TaskID: scriptTask.ID, ExecutionID: scriptExec.ID, TaskKind: scriptTask.TaskKind, Status: "ready", Missing: []string{}}, Platform: model.PlatformMontage, SpokenText: "Evidence explains the problem.\nThe second scene explains the response.", SourceRefs: []string{"source-1"}, Scenes: []model.PlanningScene{{Index: 1, StartSeconds: 0, EndSeconds: 5, SpokenText: "Evidence explains the problem.", Visuals: "Simple diagram", MaterialRequirements: []string{"diagram instructions; no generation"}, SourceRefs: []string{"source-1"}}, {Index: 2, StartSeconds: 5, EndSeconds: 10, SpokenText: "The second scene explains the response.", Visuals: "Response diagram", MaterialRequirements: []string{"diagram instructions; no generation"}, SourceRefs: []string{"source-1"}}}}
	scriptRef := planningPut(t, svc, repo, scriptTask, scriptExec, "output/video-script.json", script, false)
	planningPut(t, svc, repo, scriptTask, scriptExec, "output/video-script.md", "# Script\nEvidence explains the problem.\nThe second scene explains the response.", false)
	planningPut(t, svc, repo, scriptTask, scriptExec, "output/video-subtitles.srt", "1\n00:00:00,000 --> 00:00:05,000\nEvidence explains the problem.\n\n2\n00:00:05,000 --> 00:00:10,000\nThe second scene explains the response.\n", false)
	planningPut(t, svc, repo, scriptTask, scriptExec, "output/script-review.json", makeReview(scriptTask, scriptExec, scriptRef.ContentSHA256, true), false)
	if err := svc.validateContentPlanningArtifacts(ctx, scriptTask, scriptExec); err != nil {
		t.Fatalf("valid script: %v", err)
	}
	status, _, result, err := svc.cloudTerminalOutcome(ctx, scriptTask, scriptExec, &agent.ExecutionResult{Success: true})
	if err != nil || status != model.TaskExecutionSucceeded {
		t.Fatalf("actual script completion: status=%s result=%+v error=%v", status, result, err)
	}
	for _, name := range []string{"timing", "source", "target", "hash", "subtitles", "markdown"} {
		t.Run(name, func(t *testing.T) {
			copyScript := script
			copyScript.Scenes = append([]model.PlanningScene(nil), script.Scenes...)
			switch name {
			case "timing":
				copyScript.Scenes[0].StartSeconds = 2
			case "source":
				copyScript.SourceRefs = []string{"unknown"}
			case "target":
				copyScript.Platform = "hypit"
			}
			ref := planningPut(t, svc, repo, scriptTask, scriptExec, "output/video-script.json", copyScript, false)
			hash := ref.ContentSHA256
			if name == "hash" {
				hash = strings.Repeat("f", 64)
			}
			planningPut(t, svc, repo, scriptTask, scriptExec, "output/script-review.json", makeReview(scriptTask, scriptExec, hash, true), false)
			subtitles := "1\n00:00:00,000 --> 00:00:05,000\nEvidence explains the problem.\n\n2\n00:00:05,000 --> 00:00:10,000\nThe second scene explains the response.\n"
			if name == "subtitles" {
				subtitles = "1\n00:00:00,000 --> 00:00:09,000\nForged\n"
			}
			planningPut(t, svc, repo, scriptTask, scriptExec, "output/video-subtitles.srt", subtitles, false)
			if name == "markdown" {
				planningPut(t, svc, repo, scriptTask, scriptExec, "output/video-script.md", "# Script\nEvidence explains the problem.", false)
			}
			if err := svc.validateContentPlanningArtifacts(ctx, scriptTask, scriptExec); err == nil {
				t.Fatal("malformed script accepted")
			}
		})
	}
}

func planningConfigureBilling(t *testing.T, svc *TaskService, repo repository.Repository, db *gorm.DB, userID string) {
	t.Helper()
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	ensureTestUser(t, repo, userID)
	bundle, err := billing.LoadBundle(filepath.Join("..", "billing"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := NewBillingCatalogService(repo, bundle, BillingCatalogOptions{})
	catalog.SetAgentProfileRegistry(svc.agentProfiles)
	if _, err := catalog.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{UserID: userID, PaidCredits: 100000}); err != nil {
		t.Fatal(err)
	}
	createBillingLot(t, repo, model.BillingCreditLot{ID: uuid.NewString(), UserID: userID, Kind: model.BillingCreditLotKindPaid, SourceType: "fixture", SourceID: uuid.NewString(), CatalogID: bundle.Products.CatalogID, OriginalCredits: 100000, AvailableCredits: 100000, CreatedAt: time.Now().Add(-time.Hour)})
	svc.SetBillingCatalogService(catalog)
	svc.SetBillingWalletService(NewBillingWalletService(repo, bundle, BillingWalletOptions{}))
}

func TestContentPlanningInternalAnalyticsAreFrozenAndOwned(t *testing.T) {
	svc, repo, db, task, _ := planningFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	ref := model.PlanningAnalyticsRef{ProjectID: task.ProjectID, ContentID: "internal-content", AnalyticsRevision: 7}
	if err := db.Create(&model.AnalyticsState{ProjectID: task.ProjectID, Revision: 7, ActiveGeneration: 1, Status: "ready", UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	content := model.AnalyticsContent{ID: ref.ContentID, ProjectID: task.ProjectID, Channel: model.ChannelArticle, Title: "An actual internal title", Date: &now, URL: "https://example.org/internal"}
	if err := repo.Analytics().UpsertContent(ctx, &content); err != nil {
		t.Fatal(err)
	}
	input := planningInputMap(t, model.ContentPlanningInput{SchemaVersion: "1.0", TaskKind: "content_research", Candidates: []model.PlanningCandidate{}, AcceptedArtifacts: []model.PlanningArtifactRef{}, InternalSources: []model.PlanningAnalyticsRef{ref}})
	if err := svc.freezePlanningInternalSources(ctx, task.UserID, input); err != nil {
		t.Fatal(err)
	}
	frozen, err := decodePlanningInput("content_research", input)
	if err != nil || len(frozen.InternalEvidence) != 1 {
		t.Fatalf("frozen internal evidence: %+v %v", frozen, err)
	}
	source := model.PlanningSource{ID: "internal-1", InternalReference: &ref, URL: content.URL, PublishedAt: now.Format(time.RFC3339Nano), FetchedAt: now.Format(time.RFC3339Nano), Facts: []string{content.Title}}
	if !planningSourceMatchesInternal(source, frozen.InternalEvidence) {
		t.Fatal("actual internal title not accepted")
	}
	ref.AnalyticsRevision = 8
	if planningSourceMatchesInternal(source, frozen.InternalEvidence) {
		t.Fatal("forged analytics revision accepted")
	}
	if err := svc.freezePlanningInternalSources(ctx, "another-user", input); err == nil {
		t.Fatal("foreign internal evidence accepted")
	}
}

func TestContentPlanningNeverRequestsPublication(t *testing.T) {
	svc, _, _, task, execution := planningFixture(t)
	execution.Status = model.TaskExecutionSucceeded
	for _, kind := range []string{"content_research", "content_validation", "content_script"} {
		task.Type = kind
		task.TaskKind = kind
		status, _, err := svc.resolveCloudDraftDelivery(context.Background(), task, execution)
		if err != nil || status != model.TaskExecutionDraftDeliveryNotRequested {
			t.Fatalf("%s publication %s: %v", kind, status, err)
		}
	}
}

func TestContentPlanningResearchCanDeliverInsufficientEvidenceWithoutUnlockingValidation(t *testing.T) {
	svc, repo, db, task, execution := planningFixture(t)
	ctx := context.Background()
	id := model.PlanningIdentity{SchemaVersion: "1.0", TaskID: task.ID, ExecutionID: execution.ID, TaskKind: task.TaskKind, Status: "data_insufficient", Missing: []string{"no_relevant_sources"}}
	r := model.PlanningResearchResult{PlanningIdentity: id, Candidates: []model.PlanningProposal{}}
	e := model.PlanningResearchEvidence{PlanningIdentity: id, Sources: []model.PlanningSource{}}
	refs := []model.PlanningArtifactRef{planningPut(t, svc, repo, task, execution, "output/content-research.json", r, true), planningPut(t, svc, repo, task, execution, "output/research-evidence.json", e, true)}
	if err := svc.validateContentPlanningArtifacts(ctx, task, execution); err != nil {
		t.Fatalf("honest insufficient evidence must deliver: %v", err)
	}
	task.Status = model.TaskStatusCompleted
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).Updates(map[string]any{"status": model.TaskExecutionSucceeded, "manifest_sealed": true}).Error; err != nil {
		t.Fatal(err)
	}
	params := CreateManualParams{UserID: task.UserID, ProjectID: task.ProjectID, AgentID: "content-planning", Channel: "content-planning", TaskKind: "content_validation", ExecutionProfile: "effective", AgentInput: planningInputMap(t, model.ContentPlanningInput{SchemaVersion: "1.0", TaskKind: "content_validation", Candidates: []model.PlanningCandidate{}, AcceptedArtifacts: refs})}
	if _, err := svc.CreateManual(ctx, params); !errors.Is(err, ErrInvalidAgentInput) {
		t.Fatalf("insufficient research unlocked validation: %v", err)
	}
}
