package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

var ErrContentPlanningUnavailable = errors.New("content_planning_unavailable")
var ErrContentPlanningBillingUnavailable = errors.New("content_planning_billing_unavailable")

// ContentPlanningAvailable is a recoverable admission capability, not a Server
// startup requirement. The controller may surface this error without charging.
func (s *TaskService) ContentPlanningAvailable() error {
	if s == nil || s.runtimeDispatcher == nil {
		return ErrContentPlanningUnavailable
	}
	runtime := s.runtimeDispatcher.ResolveRuntime(model.TaskTypeContentResearch)
	if runtime.Profile != model.AgentIDContentPlanning || strings.TrimSpace(runtime.Image) == "" {
		return ErrContentPlanningUnavailable
	}
	return nil
}

func decodePlanningInput(taskKind string, input map[string]any) (model.ContentPlanningInput, error) {
	var value model.ContentPlanningInput
	b, err := json.Marshal(input)
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal(b, &value); err != nil {
		return value, err
	}
	if value.SchemaVersion != "1.0" || value.TaskKind != taskKind || len(value.Candidates) > 50 || len(value.AcceptedArtifacts) > 12 {
		return value, fmt.Errorf("%w: planning input identity or batch limit", ErrInvalidAgentInput)
	}
	return value, nil
}

func (s *TaskService) validatePlanningAdmission(ctx context.Context, p CreateManualParams, project *model.Project, taskKind string, input map[string]any) error {
	if err := s.ContentPlanningAvailable(); err != nil {
		return err
	}
	if s.billingCatalogSvc == nil || s.billingWalletSvc == nil {
		return ErrContentPlanningBillingUnavailable
	}
	value, err := decodePlanningInput(taskKind, input)
	if err != nil {
		return err
	}
	if value.TargetRequirements != nil && value.TargetRequirements.Platform != project.Platform {
		return fmt.Errorf("%w: planning target must match owned project platform", ErrInvalidAgentInput)
	}
	for _, candidate := range value.Candidates {
		if strings.TrimSpace(candidate.Title) == "" {
			return fmt.Errorf("%w: candidate title required", ErrInvalidAgentInput)
		}
		if candidate.AssetID != "" {
			if _, err := s.repo.ContentAssets().FindByID(ctx, p.UserID, candidate.AssetID); err != nil {
				return fmt.Errorf("%w: candidate is not owned", ErrInvalidAgentInput)
			}
		}
	}
	required := map[string]bool{}
	switch taskKind {
	case model.TaskTypeContentResearch:
		if len(value.AcceptedArtifacts) > 0 {
			return fmt.Errorf("%w: research cannot import arbitrary prior stages", ErrInvalidAgentInput)
		}
	case model.TaskTypeContentValidation:
		required["output/content-research.json"] = false
		required["output/research-evidence.json"] = false
	case model.TaskTypeContentScript:
		required["output/content-research.json"] = false
		required["output/research-evidence.json"] = false
		required["output/validation-article.md"] = false
		required["output/validation-review.json"] = false
	}
	executionByKind := map[string]string{}
	for _, ref := range value.AcceptedArtifacts {
		if found, ok := required[ref.Path]; !ok || found {
			return fmt.Errorf("%w: unexpected or duplicate accepted artifact", ErrInvalidAgentInput)
		}
		source, file, err := planningAcceptedArtifact(ctx, s.repo, s.store, p.UserID, p.ProjectID, ref)
		if err != nil {
			return err
		}
		if prior := executionByKind[source.Type]; prior != "" && prior != file.ExecutionID {
			return fmt.Errorf("%w: mixed accepted executions", ErrInvalidAgentInput)
		}
		executionByKind[source.Type] = file.ExecutionID
		required[ref.Path] = true
	}
	for name, found := range required {
		if !found {
			return fmt.Errorf("%w: missing accepted %s", ErrInvalidAgentInput, name)
		}
	}
	return nil
}

func planningAcceptedArtifact(ctx context.Context, repo repository.Repository, store storage.Provider, userID, projectID string, ref model.PlanningArtifactRef) (*model.Task, *model.TaskFile, error) {
	fail := func() (*model.Task, *model.TaskFile, error) {
		return nil, nil, fmt.Errorf("%w: accepted artifact unavailable or not owned", ErrInvalidAgentInput)
	}
	if !lowercaseSHA256.MatchString(ref.ContentSHA256) || store == nil {
		return fail()
	}
	source, err := repo.Tasks().FindByID(ctx, ref.TaskID)
	if err != nil || source.UserID != userID || source.Status != model.TaskStatusCompleted || !model.IsContentPlanningTask(source.Type) {
		return fail()
	}
	for _, ownedProjectID := range []string{projectID, source.ProjectID} {
		owned, err := repo.Projects().FindByID(ctx, ownedProjectID)
		if err != nil || owned.UserID != userID {
			return fail()
		}
	}
	pack, ok := agentpack.Default().ForTaskType(source.Type)
	if !ok {
		return fail()
	}
	allowed := false
	for _, spec := range pack.DeliveryForTaskType(source.Type) {
		if spec.Path == ref.Path {
			allowed = true
		}
	}
	if !allowed {
		return fail()
	}
	execution, err := repo.TaskExecutions().FindByID(ctx, ref.ExecutionID)
	if err != nil || execution.TaskID != source.ID || !execution.ManifestSealed || execution.Status != model.TaskExecutionSucceeded {
		return fail()
	}
	files, err := repo.TaskFiles().FindByExecutionID(ctx, ref.ExecutionID)
	if err != nil {
		return nil, nil, err
	}
	for _, file := range files {
		if file.FilePath != ref.Path {
			continue
		}
		if file.TaskID != source.ID || file.ExecutionID != ref.ExecutionID || file.State != model.TaskFileStateDelivered || file.ContentHash != ref.ContentSHA256 || file.StorageProvider != store.Name() || file.FileSize <= 0 || file.FileSize > 2<<20 {
			return fail()
		}
		if file.OSSKey != buildTaskArtifactFinalStorageKey(source, ref.ExecutionID, ref.ContentSHA256, ref.Path) && file.OSSKey != buildTaskMCPArtifactStoragePrefix(source, ref.ExecutionID)+ref.Path {
			return fail()
		}
		body, err := storage.ReadObject(ctx, store, file.OSSKey, 2<<20)
		if err != nil {
			return nil, nil, err
		}
		hash := sha256.Sum256(body)
		if hex.EncodeToString(hash[:]) != ref.ContentSHA256 || int64(len(body)) != file.FileSize {
			return fail()
		}
		if strings.HasSuffix(ref.Path, ".json") {
			var identity model.PlanningIdentity
			if json.Unmarshal(body, &identity) != nil || identity.SchemaVersion != "1.0" || identity.TaskID != source.ID || identity.ExecutionID != ref.ExecutionID || identity.TaskKind != source.Type || identity.Status != "ready" {
				return fail()
			}
		}
		return source, file, nil
	}
	return fail()
}

func (s *AgentBootstrapService) buildContentPlanningFiles(ctx context.Context, execution *model.TaskExecution, task *model.Task, deadline time.Time) ([]BootstrapFile, error) {
	if !model.IsContentPlanningTask(task.Type) {
		return nil, nil
	}
	input, err := decodePlanningInput(task.Type, task.AgentInput.Data())
	if err != nil {
		return nil, err
	}
	// Always read the admission snapshot, including Profile, never a live projection.
	snapshot := task.ProjectSnapshot.Data()
	accepted := make([]map[string]any, 0, len(input.AcceptedArtifacts))
	files := []BootstrapFile{}
	for i, ref := range input.AcceptedArtifacts {
		_, file, err := planningAcceptedArtifact(ctx, s.repo, s.cfg.Store, task.UserID, task.ProjectID, ref)
		if err != nil {
			return nil, err
		}
		signed, err := s.signedBootstrapObjectKey(ctx, file.OSSKey, deadline)
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf("input/accepted/%02d/%s", i, path.Base(ref.Path))
		files = append(files, BootstrapFile{Path: name, DownloadURL: signed, ContentSHA256: ref.ContentSHA256, ExpectedSize: file.FileSize, MaxBytes: 2 << 20, Mode: 0444})
		accepted = append(accepted, map[string]any{"reference": ref, "local_path": name})
	}
	body, err := json.Marshal(map[string]any{"schema_version": "1.0", "task_id": task.ID, "execution_id": execution.ID, "project_id": task.ProjectID, "task_kind": task.Type, "profile": snapshot.Profile, "input": input, "accepted_files": accepted})
	if err != nil {
		return nil, err
	}
	files = append(files, BootstrapFile{Path: "input/content-planning.json", Text: string(body), Mode: 0444})
	return files, nil
}

// ValidateAgentExecutionToolAccess is the fail-closed text-stage cost boundary.
// It is called after execution authentication and before MCP dispatch.
func (s *TaskService) ValidateAgentExecutionToolAccess(ctx context.Context, userID, projectID, taskID, executionID, tool string) error {
	if err := s.ValidateAgentExecutionAccess(ctx, userID, projectID, taskID, executionID); err != nil {
		return err
	}
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return err
	}
	if !model.IsContentPlanningTask(task.Type) {
		return nil
	}
	switch tool {
	case "get_task", "get_project_profile", "list_task_files", "set_task_progress_plan", "update_task_progress", "submit_completion_metadata", "get_completion_metadata_status", "list_resources", "get_resource", "score_article", "cancel_task":
		return nil
	case "search_web", "search_images", "fetch_web_page", "list_search_receipts":
		if task.Type == model.TaskTypeContentResearch {
			return nil
		}
	}
	return fmt.Errorf("%w: %s cannot invoke %s", ErrTaskCapabilityAccessDenied, task.Type, tool)
}
