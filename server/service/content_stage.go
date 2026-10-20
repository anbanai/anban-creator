package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ContentStageArtifact is immutable provenance for a stage output. Callers
// cannot mark a stage complete by supplying status or quality fields.
type ContentStageArtifact struct {
	TaskID        string `json:"task_id"`
	ExecutionID   string `json:"execution_id"`
	Path          string `json:"path"`
	ContentSHA256 string `json:"content_sha256"`
}

func contentStageDigest(value any) (string, []byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), body, nil
}

func contentStagePolicyRevision(raw []byte) int {
	var snapshot struct {
		Revision int `json:"revision"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &snapshot)
	}
	return snapshot.Revision
}

func researchContextHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func reuseStageTaskID(stage *model.ContentReuseStageExecution) string {
	if stage == nil || stage.TaskID == nil {
		return ""
	}
	return strings.TrimSpace(*stage.TaskID)
}

func contentPlanningKindForStage(stage string) string {
	switch stage {
	case model.ContentReuseStageResearch:
		return model.TaskTypeContentResearch
	case model.ContentReuseStageScoring, model.ContentReuseStageBrief:
		return model.TaskTypeContentValidation
	case model.ContentReuseStageProduce:
		return model.TaskTypeContentScript
	default:
		return ""
	}
}

func contentReuseStageTaskKind(stage string, run *model.ContentReuseRun) string {
	if kind := contentPlanningKindForStage(stage); kind != "" {
		return kind
	}
	if stage == model.ContentReuseStageRender && run != nil {
		_, _, kind, ok := contentReuseIdentity(run.TargetPlatform)
		if ok {
			return kind
		}
	}
	return ""
}

func nextContentReuseStage(stage string) string {
	switch stage {
	case model.ContentReuseStageProduce:
		return model.ContentReuseStageMaterials
	case model.ContentReuseStageMaterials:
		return model.ContentReuseStageRender
	case model.ContentReuseStageRender:
		return model.ContentReuseStageReview
	default:
		return ""
	}
}

// EnsureReuseStage creates one stage for a run, or returns the existing row
// when the same run/stage was already admitted. The frozen input is never
// replaced by a later request.
func (s *ContentAssetService) EnsureReuseStage(ctx context.Context, userID, runID, stage string, input any) (*model.ContentReuseStageExecution, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("content stage service unavailable")
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return nil, fmt.Errorf("stage is required")
	}
	run, err := s.repo.ContentAssets().FindReuseRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != model.ContentReuseRunStatusRunning {
		return nil, ErrContentReuseRunStopped
	}
	digest, frozen, err := contentStageDigest(input)
	if err != nil {
		return nil, err
	}
	if existing, findErr := s.repo.ContentAssets().FindReuseStage(ctx, runID, stage); findErr == nil {
		if existing.InputSHA256 != digest {
			return nil, fmt.Errorf("content stage input conflicts with accepted stage")
		}
		return existing, nil
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}
	row := model.NewContentReuseStageExecution(runID, stage)
	row.InputSnapshot = frozen
	row.InputSHA256 = digest
	if err := s.repo.ContentAssets().CreateReuseStage(ctx, row); err != nil {
		if existing, findErr := s.repo.ContentAssets().FindReuseStage(ctx, runID, stage); findErr == nil && existing.InputSHA256 == digest {
			return existing, nil
		}
		return nil, err
	}
	return row, nil
}

// EnsureResearchJob persists frozen research context before a candidate asset
// exists. Fingerprints are user-scoped at lookup; a collision from another
// owner is never returned as an existing job.
func (s *ContentAssetService) EnsureResearchJob(ctx context.Context, userID, projectID, candidateAssetID, fingerprint string, sourceRevision int64, policyRevision int, frozenContext any) (*model.ContentAssetResearchJob, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("content research service unavailable")
	}
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil || project.UserID != userID {
		return nil, fmt.Errorf("research project is not owned")
	}
	if candidateAssetID != "" {
		if _, err := s.repo.ContentAssets().FindByID(ctx, userID, candidateAssetID); err != nil {
			return nil, fmt.Errorf("research candidate is not owned")
		}
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return nil, fmt.Errorf("research fingerprint is required")
	}
	digest, frozen, err := contentStageDigest(frozenContext)
	if err != nil {
		return nil, err
	}
	// Include the frozen revisions in the compared bytes to prevent a caller
	// from reusing an idempotency key with a newer source or policy snapshot.
	var snapshot map[string]any
	if err := json.Unmarshal(frozen, &snapshot); err != nil {
		return nil, err
	}
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	snapshot["source_revision"], snapshot["policy_revision"] = sourceRevision, policyRevision
	frozen, err = json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	digest = researchContextHash(frozen)
	if existing, findErr := s.repo.ContentAssets().FindResearchJobByFingerprint(ctx, userID, fingerprint); findErr == nil {
		if existing.ProjectID != projectID || existing.CandidateAssetID != candidateAssetID || existing.SourceRevision != sourceRevision || existing.PolicyRevision != policyRevision || existing.FrozenContextSHA256 != digest {
			return nil, fmt.Errorf("research job fingerprint conflicts with frozen context")
		}
		return existing, nil
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}
	job := model.NewContentAssetResearchJob(userID, projectID, fingerprint, frozen)
	job.CandidateAssetID, job.SourceRevision, job.PolicyRevision, job.FrozenContextSHA256 = candidateAssetID, sourceRevision, policyRevision, digest
	if err := s.repo.ContentAssets().CreateResearchJob(ctx, job); err != nil {
		if existing, findErr := s.repo.ContentAssets().FindResearchJobByFingerprint(ctx, userID, fingerprint); findErr == nil && existing.ProjectID == projectID && existing.CandidateAssetID == candidateAssetID && existing.SourceRevision == sourceRevision && existing.PolicyRevision == policyRevision && existing.FrozenContextSHA256 == digest {
			return existing, nil
		}
		return nil, err
	}
	return job, nil
}

// FinalizeResearchJob is the Server-owned acceptance boundary for research
// evidence. Agent JSON cannot set accepted task/execution fields directly.
func (s *ContentAssetService) FinalizeResearchJob(ctx context.Context, userID, jobID, acceptedExecutionID string, artifacts []ContentStageArtifact, status, reason string) error {
	if s == nil || s.repo == nil || s.taskSvc == nil {
		return fmt.Errorf("content research finalizer unavailable")
	}
	job, err := s.repo.ContentAssets().FindResearchJobByID(ctx, userID, jobID)
	if err != nil {
		return err
	}
	if status != model.ContentStageCompleted && status != model.ContentStageFailed && status != model.ContentStagePaused {
		return fmt.Errorf("invalid research job status %q", status)
	}
	if job.TaskID == nil || strings.TrimSpace(*job.TaskID) == "" || strings.TrimSpace(acceptedExecutionID) == "" {
		return fmt.Errorf("research job task binding is missing")
	}
	task, err := s.repo.Tasks().FindByID(ctx, *job.TaskID)
	if err != nil || task.UserID != userID || task.ProjectID != job.ProjectID || task.Type != model.TaskTypeContentResearch || task.TaskKind != model.TaskTypeContentResearch || task.CurrentExecutionID == nil || *task.CurrentExecutionID != acceptedExecutionID {
		return fmt.Errorf("research task binding is invalid")
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, acceptedExecutionID)
	if err != nil || execution.TaskID != task.ID || !execution.ManifestSealed {
		return fmt.Errorf("research execution is not sealed")
	}
	if status == model.ContentStageCompleted && (execution.Status != model.TaskExecutionSucceeded || task.Status != model.TaskStatusCompleted) {
		return fmt.Errorf("research execution is not a successful task")
	}
	if status != model.ContentStageCompleted && execution.Status != model.TaskExecutionFailed && execution.Status != model.TaskExecutionCancelled {
		return fmt.Errorf("research failure is not terminal")
	}
	if status == model.ContentStageCompleted {
		if err := s.taskSvc.validateContentPlanningArtifacts(ctx, task, execution); err != nil {
			return fmt.Errorf("research evidence validation failed: %w", err)
		}
		if len(artifacts) != 2 {
			return fmt.Errorf("research finalization requires both research artifacts")
		}
		for _, required := range []string{"output/content-research.json", "output/research-evidence.json"} {
			found := false
			for _, ref := range artifacts {
				if ref.Path == required && ref.TaskID == task.ID && ref.ExecutionID == acceptedExecutionID {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("research artifact %s is missing", required)
			}
		}
	}
	files, err := s.repo.TaskFiles().FindByExecutionID(ctx, acceptedExecutionID)
	if err != nil {
		return err
	}
	fileByPath := make(map[string]*model.TaskFile, len(files))
	for _, file := range files {
		if file != nil {
			fileByPath[file.FilePath] = file
		}
	}
	for _, ref := range artifacts {
		file := fileByPath[ref.Path]
		if ref.TaskID != task.ID || ref.ExecutionID != acceptedExecutionID || file == nil || file.State != model.TaskFileStateDelivered || file.ContentHash != ref.ContentSHA256 || file.FileSize <= 0 {
			return fmt.Errorf("research artifact reference failed delivery validation")
		}
		if _, _, err := planningAcceptedArtifact(ctx, s.repo, s.taskSvc.store, userID, job.ProjectID, model.PlanningArtifactRef{TaskID: ref.TaskID, ExecutionID: ref.ExecutionID, Path: ref.Path, ContentSHA256: ref.ContentSHA256}); err != nil {
			return err
		}
	}
	previousStatus := job.Status
	job.TaskID, job.AcceptedExecutionID, job.Status = &task.ID, acceptedExecutionID, status
	job.ArtifactRefs, err = json.Marshal(artifacts)
	if err != nil {
		return err
	}
	job.ErrorMessage = ""
	if status != model.ContentStageCompleted {
		job.ErrorMessage = strings.TrimSpace(reason)
	}
	event := &model.ContentReuseAuditEvent{UserID: userID, RunID: job.ID, EntityKind: "research_job", EntityID: job.ID, FromState: previousStatus, ToState: status, TaskID: task.ID, ExecutionID: acceptedExecutionID, ResultSHA256: researchContextHash(job.ArtifactRefs), ReasonCode: strings.TrimSpace(reason)}
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if status == model.ContentStageCompleted {
			var research model.PlanningResearchResult
			var evidence model.PlanningResearchEvidence
			for _, ref := range artifacts {
				_, file, err := planningAcceptedArtifact(ctx, tx, s.taskSvc.store, userID, job.ProjectID, model.PlanningArtifactRef{TaskID: ref.TaskID, ExecutionID: ref.ExecutionID, Path: ref.Path, ContentSHA256: ref.ContentSHA256})
				if err != nil {
					return err
				}
				body, err := storage.ReadObject(ctx, s.taskSvc.store, file.OSSKey, 2<<20)
				if err != nil {
					return err
				}
				switch ref.Path {
				case "output/content-research.json":
					if err := json.Unmarshal(body, &research); err != nil {
						return fmt.Errorf("decode research result: %w", err)
					}
				case "output/research-evidence.json":
					if err := json.Unmarshal(body, &evidence); err != nil {
						return fmt.Errorf("decode research evidence: %w", err)
					}
				}
			}
			if err := applyResearchResult(ctx, tx, userID, strings.TrimSpace(job.CandidateAssetID), research, evidence); err != nil {
				return fmt.Errorf("apply research result: %w", err)
			}
		}
		return tx.ContentAssets().TransitionResearchJob(ctx, job, event)
	})
}

// applyResearchResult is the deterministic Server projection of a validated
// research artifact. It is idempotent so a finalizer retry cannot duplicate
// evidence or lose historical content links.
func applyResearchResult(ctx context.Context, repo repository.Repository, userID, primaryAssetID string, result model.PlanningResearchResult, evidence model.PlanningResearchEvidence) error {
	if result.Status != "ready" || len(result.Candidates) == 0 {
		return nil
	}
	for index, proposal := range result.Candidates {
		assetID := strings.TrimSpace(primaryAssetID)
		if assetID == "" || index > 0 {
			assetID = ""
		}
		if assetID == "" {
			asset := model.NewContentAsset(userID, strings.TrimSpace(proposal.Title))
			asset.CoreQuestion = strings.TrimSpace(proposal.Title)
			asset.SemanticKernel = strings.TrimSpace(proposal.SemanticKernel)
			if err := repo.ContentAssets().Create(ctx, asset); err != nil {
				if existing, findErr := repo.ContentAssets().FindByTitle(ctx, userID, asset.Title); findErr == nil {
					asset = existing
				} else {
					return err
				}
			}
			assetID = asset.ID
		}
		asset, err := repo.ContentAssets().FindByID(ctx, userID, assetID)
		if err != nil {
			return err
		}
		if strings.TrimSpace(proposal.Title) != "" {
			asset.Title = strings.TrimSpace(proposal.Title)
		}
		if strings.TrimSpace(proposal.SemanticKernel) != "" {
			asset.SemanticKernel = strings.TrimSpace(proposal.SemanticKernel)
		}
		if asset.Status == model.ContentAssetStatusCandidate {
			asset.Status = model.ContentAssetStatusResearched
		}
		if err := repo.ContentAssets().Update(ctx, asset); err != nil {
			return err
		}
		for _, mergeID := range proposal.MergeAssetIDs {
			if strings.TrimSpace(mergeID) == "" || mergeID == assetID {
				continue
			}
			if err := repo.ContentAssets().MergeAsset(ctx, userID, assetID, mergeID); err != nil {
				return err
			}
		}
		for _, source := range evidence.Sources {
			if source.ID == "" {
				continue
			}
			sourceType, projectID, sourceID := model.ContentAssetEvidenceExternal, "", source.ID
			if source.InternalReference != nil {
				sourceType, projectID, sourceID = model.ContentAssetEvidencePublished, source.InternalReference.ProjectID, source.InternalReference.ContentID
			}
			rows, err := repo.ContentAssets().ListEvidence(ctx, userID, assetID)
			if err != nil {
				return err
			}
			duplicate := false
			for _, row := range rows {
				if row != nil && row.SourceType == sourceType && row.SourceID == sourceID && row.URL == source.URL {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			var dataAsOf *time.Time
			if parsed, parseErr := time.Parse(time.RFC3339Nano, source.FetchedAt); parseErr == nil {
				parsed = parsed.UTC()
				dataAsOf = &parsed
			}
			facts, _ := json.Marshal(source.Facts)
			if err := repo.ContentAssets().CreateEvidence(ctx, &model.ContentAssetEvidence{ID: uuid.NewString(), AssetID: assetID, UserID: userID, SourceType: sourceType, ProjectID: projectID, SourceID: sourceID, Title: source.ID, URL: source.URL, Metrics: facts, DataAsOf: dataAsOf, DataSource: "content-research", Quality: "verified", Notes: strings.Join(source.Facts, "\n")}); err != nil {
				return err
			}
		}
	}
	return nil
}

// FinalizeResearchTask bridges the task execution finalizer to the research
// job acceptance boundary. It derives every artifact reference from durable
// Server rows; agent output or logs cannot mark a research job complete.
func (s *ContentAssetService) FinalizeResearchTask(ctx context.Context, task *model.Task) error {
	if s == nil || task == nil || strings.TrimSpace(task.ResearchJobID) == "" {
		return nil
	}
	if task.CurrentExecutionID == nil || strings.TrimSpace(*task.CurrentExecutionID) == "" {
		return fmt.Errorf("research task execution is missing")
	}
	executionID := strings.TrimSpace(*task.CurrentExecutionID)
	job, err := s.repo.ContentAssets().FindResearchJobByID(ctx, task.UserID, task.ResearchJobID)
	if err != nil {
		return err
	}
	if job.AcceptedExecutionID == executionID && (job.Status == model.ContentStageCompleted || job.Status == model.ContentStageFailed || job.Status == model.ContentStagePaused) {
		return nil
	}
	status := model.ContentStageFailed
	switch task.Status {
	case model.TaskStatusCompleted:
		status = model.ContentStageCompleted
	case model.TaskStatusFailed, model.TaskStatusCancelled:
		status = model.ContentStageFailed
	default:
		return nil
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		return err
	}
	if execution.TaskID != task.ID || !execution.ManifestSealed {
		return fmt.Errorf("research task execution is not sealed")
	}
	if status == model.ContentStageCompleted && execution.Status != model.TaskExecutionSucceeded {
		return fmt.Errorf("completed research task execution is not successful")
	}
	if status == model.ContentStageFailed && execution.Status != model.TaskExecutionFailed && execution.Status != model.TaskExecutionCancelled {
		return fmt.Errorf("failed research task execution is not terminal")
	}
	var artifacts []ContentStageArtifact
	if status == model.ContentStageCompleted {
		files, err := s.repo.TaskFiles().FindByExecutionID(ctx, executionID)
		if err != nil {
			return err
		}
		for _, file := range files {
			if file == nil || file.State != model.TaskFileStateDelivered || file.FileSize <= 0 || !strings.HasPrefix(file.FilePath, "output/") {
				continue
			}
			if file.FilePath == "output/content-research.json" || file.FilePath == "output/research-evidence.json" {
				artifacts = append(artifacts, ContentStageArtifact{TaskID: task.ID, ExecutionID: executionID, Path: file.FilePath, ContentSHA256: file.ContentHash})
			}
		}
	}
	reason := strings.TrimSpace(task.ErrorMessage)
	if reason == "" && status == model.ContentStageCompleted {
		reason = "research_execution_completed"
	}
	return s.FinalizeResearchJob(ctx, task.UserID, task.ResearchJobID, executionID, artifacts, status, reason)
}

// CompleteReuseStage atomically records accepted artifacts and the terminal
// stage transition. Artifact validity itself is established by the existing
// task execution finalizer; this method only stores its immutable references.
func (s *ContentAssetService) CompleteReuseStage(ctx context.Context, userID, stageID, acceptedExecutionID string, artifacts []ContentStageArtifact, status, reason string) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("content stage service unavailable")
	}
	stage, err := s.repo.ContentAssets().FindReuseStageByID(ctx, stageID)
	if err != nil {
		return err
	}
	run, err := s.repo.ContentAssets().FindReuseRunByID(ctx, stage.RunID)
	if err != nil || run.UserID != userID {
		if err == nil {
			err = gorm.ErrRecordNotFound
		}
		return err
	}
	if reuseStageTaskID(stage) == "" || strings.TrimSpace(acceptedExecutionID) == "" {
		return fmt.Errorf("stage task and execution are required")
	}
	task, err := s.repo.Tasks().FindByReuseStage(ctx, userID, stage.ID)
	if err != nil || task.ID != reuseStageTaskID(stage) || task.ReuseRunID != run.ID {
		return fmt.Errorf("stage task binding is invalid")
	}
	if task.CurrentExecutionID == nil || *task.CurrentExecutionID != acceptedExecutionID {
		return fmt.Errorf("stage execution is not current")
	}
	if expectedKind := contentReuseStageTaskKind(stage.Stage, run); expectedKind == "" || task.Type != expectedKind || task.TaskKind != expectedKind {
		return fmt.Errorf("stage task kind does not match frozen stage")
	}
	if status != model.ContentStageCompleted && status != model.ContentStageFailed && status != model.ContentStagePaused {
		return fmt.Errorf("invalid terminal stage status %q", status)
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, acceptedExecutionID)
	if err != nil || execution.TaskID != task.ID || !execution.ManifestSealed {
		return fmt.Errorf("stage execution is not sealed")
	}
	if status == model.ContentStageCompleted && execution.Status != model.TaskExecutionSucceeded {
		return fmt.Errorf("completed stage execution is not successful")
	}
	if status != model.ContentStageCompleted && execution.Status != model.TaskExecutionFailed && execution.Status != model.TaskExecutionCancelled {
		return fmt.Errorf("failed stage execution is not terminal")
	}
	if status == model.ContentStageCompleted && len(artifacts) == 0 {
		return fmt.Errorf("completed stage requires frozen artifacts")
	}
	files, err := s.repo.TaskFiles().FindByExecutionID(ctx, acceptedExecutionID)
	if err != nil {
		return err
	}
	byPath := make(map[string]*model.TaskFile, len(files))
	for _, file := range files {
		if file != nil {
			byPath[file.FilePath] = file
		}
	}
	seenPaths := make(map[string]bool, len(artifacts))
	for _, ref := range artifacts {
		file := byPath[ref.Path]
		if !strings.HasPrefix(ref.Path, "output/") || seenPaths[ref.Path] || ref.TaskID != task.ID || ref.ExecutionID != acceptedExecutionID || ref.ContentSHA256 == "" || file == nil || file.TaskID != task.ID || file.ExecutionID != acceptedExecutionID || file.State != model.TaskFileStateDelivered || file.ContentHash != ref.ContentSHA256 || file.FileSize <= 0 {
			return fmt.Errorf("stage artifact reference is not delivered by the accepted execution")
		}
		seenPaths[ref.Path] = true
	}
	if status == model.ContentStageCompleted && task.Status != model.TaskStatusCompleted {
		return fmt.Errorf("stage task has not completed")
	}
	if status != model.ContentStageCompleted && task.Status != model.TaskStatusFailed && task.Status != model.TaskStatusCancelled {
		return fmt.Errorf("failed stage task is not terminal")
	}
	if run.Status == model.ContentReuseRunStatusPaused || run.Status == model.ContentReuseRunStatusCancelled || run.Status == model.ContentReuseRunStatusCompleted {
		return nil
	}
	if stage.Status != model.ContentStageRunning || reuseStageTaskID(stage) != task.ID {
		return fmt.Errorf("stage is not bound to the current task")
	}
	body, err := json.Marshal(artifacts)
	if err != nil {
		return err
	}
	previous := stage.Status
	stage.Status = status
	stage.AcceptedExecutionID = strings.TrimSpace(acceptedExecutionID)
	stage.ArtifactRefs = body
	stage.ErrorMessage = ""
	if status != model.ContentStageCompleted {
		stage.ErrorMessage = strings.TrimSpace(reason)
	}
	event := &model.ContentReuseAuditEvent{ID: uuid.NewString(), UserID: userID, RunID: run.ID, StageExecutionID: stage.ID, EntityKind: "stage", EntityID: stage.ID, FromState: previous, ToState: status, TaskID: task.ID, ExecutionID: stage.AcceptedExecutionID, PolicyRevision: contentStagePolicyRevision(run.PolicySnapshot), ReasonCode: strings.TrimSpace(reason), CreatedAt: time.Now().UTC()}
	return s.repo.ContentAssets().TransitionReuseStage(ctx, stage, event)
}

// CreatePlanningStageTask admits a typed planning task bound to an existing
// stage. It deliberately uses CreateManual so billing, frozen ProjectSnapshot,
// and execution/bootstrap contracts remain centralized in TaskService.
func (s *ContentAssetService) CreatePlanningStageTask(ctx context.Context, userID, runID, stageID, taskKind string, input map[string]any) ([]*model.Task, error) {
	if s == nil || s.taskSvc == nil {
		return nil, fmt.Errorf("content planning task service unavailable")
	}
	run, err := s.repo.ContentAssets().FindReuseRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(stageID) == "" {
		return nil, fmt.Errorf("stage id is required")
	}
	stage, err := s.repo.ContentAssets().FindReuseStageByID(ctx, stageID)
	if err != nil {
		return nil, err
	}
	if stage.RunID != run.ID || reuseStageTaskID(stage) != "" || stage.Status != model.ContentStageQueued {
		return nil, fmt.Errorf("content stage is not available for task admission")
	}
	digest, _, err := contentStageDigest(input)
	if err != nil {
		return nil, err
	}
	if digest != stage.InputSHA256 {
		return nil, fmt.Errorf("planning task input differs from frozen stage input")
	}
	if taskKind != model.TaskTypeContentResearch && taskKind != model.TaskTypeContentValidation && taskKind != model.TaskTypeContentScript {
		return nil, fmt.Errorf("unsupported planning stage task kind %q", taskKind)
	}
	project, err := s.repo.Projects().FindByID(ctx, run.TargetProjectID)
	if err != nil || project.UserID != userID {
		return nil, fmt.Errorf("target project is not owned")
	}
	return s.taskSvc.CreateManual(ctx, CreateManualParams{UserID: userID, ProjectID: run.TargetProjectID, AgentID: model.AgentIDContentPlanning, Channel: model.ChannelContentPlanning, TaskKind: taskKind, RequestedTaskType: taskKind, ExecutionProfile: "effective", Prompt: fmt.Sprintf("content planning stage %s for reuse run %s", stage.Stage, run.ID), ContentAssetID: run.AssetID, ReuseRunID: run.ID, ReuseStageID: stage.ID, AgentInput: input})
}
