package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

const assistantReviewTrigger = "assistant_review"

type AssistantCommitRequest struct {
	ConfirmationToken string `json:"confirmation_token,omitempty"`
	CheckpointDigest  string `json:"checkpoint_digest,omitempty"`
	CheckpointID      string `json:"checkpoint_id,omitempty"`
	IdempotencyKey    string `json:"idempotency_key"`
}

type AssistantTaskResult struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type AssistantCommitResult struct {
	ModeID  string                 `json:"mode_id"`
	Status  string                 `json:"status"`
	Project *model.Project         `json:"project,omitempty"`
	Profile *model.ProjectProfile  `json:"profile,omitempty"`
	Task    *AssistantTaskResult   `json:"task,omitempty"`
	Insight *model.FeedbackInsight `json:"insight,omitempty"`
}

// CommitAssistant commits only server-owned structured drafts. Runtime output
// is never written to project files directly.
func (s *InteractiveService) CommitAssistant(ctx context.Context, userID, sessionID string, req AssistantCommitRequest, tasks *TaskService, repo repository.Repository) (resultOut AssistantCommitResult, resultErr error) {
	startedAt := time.Now()
	defer func() {
		s.logRuntimeFields("info", sessionID, "assistant commit outcome", resultErr, map[string]any{"mode_id": resultOut.ModeID, "outcome": resultOut.Status, "duration_ms": time.Since(startedAt).Milliseconds()})
	}()
	// The supported deployment has one control-plane process. Serialize admission
	// and receipt checks so a reused key cannot create work before its receipt CAS.
	s.assistantCommitMu.Lock()
	defer s.assistantCommitMu.Unlock()
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" || len(key) > 200 || repo == nil {
		return AssistantCommitResult{}, ErrInteractiveDraftInvalid
	}
	sess, sessionErr := s.getOwned(userID, sessionID)
	if (sessionErr == nil && (sess.ModeID == "" || sess.ModeID == AssistantModeProfile)) || (sessionErr != nil && req.CheckpointID == "") {
		result, err := s.Commit(ctx, userID, sessionID, InteractiveCommitRequest{ConfirmationToken: req.ConfirmationToken, IdempotencyKey: key})
		if err != nil {
			return AssistantCommitResult{}, err
		}
		return AssistantCommitResult{ModeID: AssistantModeProfile, Status: "committed", Project: result.Project, Profile: &result.Profile}, nil
	}
	if sessionErr != nil && req.CheckpointID == "" {
		return AssistantCommitResult{}, sessionErr
	}
	if sessionErr == nil {
		sess.mu.Lock()
		if sess.committing {
			sess.mu.Unlock()
			return AssistantCommitResult{}, ErrInteractiveBusy
		}
		sess.committing = true
		sess.mu.Unlock()
		defer func() {
			sess.mu.Lock()
			sess.committing = false
			sess.mu.Unlock()
		}()
	}

	if repo.AssistantCheckpoints() == nil {
		return AssistantCommitResult{}, ErrInteractivePersistence
	}
	checkpointID := strings.TrimSpace(req.CheckpointID)
	if checkpointID == "" {
		return AssistantCommitResult{}, ErrAssistantCheckpointNotFound
	}
	checkpoint, err := repo.AssistantCheckpoints().FindByIDForUser(ctx, userID, checkpointID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AssistantCommitResult{}, ErrAssistantCheckpointNotFound
		}
		return AssistantCommitResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	if sessionErr == nil && (checkpoint.SourceSessionID != sessionID || checkpoint.ModeID != sess.ModeID || checkpoint.ProjectID != sess.ProjectID || checkpoint.ContentID != sess.ContentID) {
		return AssistantCommitResult{}, ErrAssistantCheckpointConflict
	}
	if req.CheckpointDigest == "" || req.CheckpointDigest != checkpoint.PayloadDigest {
		return AssistantCommitResult{}, ErrAssistantCheckpointConflict
	}
	if checkpoint.SourceSessionID != sessionID {
		return AssistantCommitResult{}, ErrAssistantCheckpointConflict
	}
	if checkpoint.ModeID == AssistantModeProfile || (checkpoint.ModeID != AssistantModeCreation && checkpoint.ModeID != AssistantModeReview) {
		return AssistantCommitResult{}, ErrAssistantCheckpointInvalid
	}
	if receipt, lookupErr := repo.AssistantCheckpoints().FindCommittedByKey(ctx, userID, key); lookupErr == nil {
		if receipt.ID != checkpoint.ID {
			return AssistantCommitResult{}, ErrInteractiveIdempotency
		}
	} else if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return AssistantCommitResult{}, errors.Join(ErrInteractivePersistence, lookupErr)
	}
	if checkpoint.Status == model.AssistantCheckpointCommitted {
		if checkpoint.CommitUserID != userID || checkpoint.CommitIdempotencyKey == nil || *checkpoint.CommitIdempotencyKey != key || len(checkpoint.CommitResultJSON) == 0 {
			return AssistantCommitResult{}, ErrInteractiveIdempotency
		}
		var result AssistantCommitResult
		if err := json.Unmarshal(checkpoint.CommitResultJSON, &result); err != nil {
			return AssistantCommitResult{}, errors.Join(ErrInteractivePersistence, err)
		}
		return result, nil
	}
	if checkpoint.Status != model.AssistantCheckpointReady || !checkpoint.ExpiresAt.After(time.Now().UTC()) {
		return AssistantCommitResult{}, ErrAssistantCheckpointConflict
	}
	if err := validateAssistantCheckpointPayload(checkpoint.Payload); err != nil {
		return AssistantCommitResult{}, err
	}
	if err := validateAssistantCommitScope(ctx, repo, userID, checkpoint); err != nil {
		return AssistantCommitResult{}, err
	}

	if checkpoint.ModeID == AssistantModeReview {
		var result AssistantCommitResult
		err := repo.WithTx(ctx, func(tx repository.Repository) error {
			current, err := tx.AssistantCheckpoints().FindByIDForUser(ctx, userID, checkpoint.ID)
			if err != nil {
				return errors.Join(ErrInteractivePersistence, err)
			}
			if current.PayloadDigest != req.CheckpointDigest || current.Status != model.AssistantCheckpointReady || !current.ExpiresAt.After(time.Now().UTC()) {
				return ErrAssistantCheckpointConflict
			}
			if err := validateAssistantCommitScope(ctx, tx, userID, current); err != nil {
				return err
			}
			result, err = commitAssistantReview(ctx, userID, current, tx)
			if err != nil {
				return err
			}
			raw, err := json.Marshal(result)
			if err != nil {
				return err
			}
			committed, err := tx.AssistantCheckpoints().CommitIfReady(ctx, userID, current.ID, key, raw, time.Now().UTC())
			if err != nil {
				return errors.Join(ErrInteractivePersistence, err)
			}
			if !committed {
				return ErrAssistantCheckpointConflict
			}
			return nil
		})
		if err != nil {
			return AssistantCommitResult{}, err
		}
		if sessionErr == nil {
			sess.mu.Lock()
			sess.committing = false
			sess.Status = "committed"
			sess.mu.Unlock()
			s.closeSession(sessionID, false)
		}
		return result, nil
	}
	var result AssistantCommitResult
	switch checkpoint.ModeID {
	case AssistantModeCreation:
		result, err = commitAssistantCreation(ctx, userID, checkpoint, tasks, repo)
	case AssistantModeReview:
		result, err = commitAssistantReview(ctx, userID, checkpoint, repo)
	}
	if err != nil {
		return AssistantCommitResult{}, err
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return AssistantCommitResult{}, err
	}
	committed, err := repo.AssistantCheckpoints().CommitIfReady(ctx, userID, checkpoint.ID, key, resultJSON, time.Now().UTC())
	if err != nil {
		return AssistantCommitResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	if !committed {
		current, findErr := repo.AssistantCheckpoints().FindByIDForUser(ctx, userID, checkpoint.ID)
		if findErr != nil {
			return AssistantCommitResult{}, errors.Join(ErrInteractivePersistence, findErr)
		}
		if current.Status == model.AssistantCheckpointCommitted {
			if current.CommitUserID != userID || current.CommitIdempotencyKey == nil || *current.CommitIdempotencyKey != key || len(current.CommitResultJSON) == 0 {
				return AssistantCommitResult{}, ErrInteractiveIdempotency
			}
			var replay AssistantCommitResult
			if err := json.Unmarshal(current.CommitResultJSON, &replay); err != nil {
				return AssistantCommitResult{}, errors.Join(ErrInteractivePersistence, err)
			}
			return replay, nil
		}
		return AssistantCommitResult{}, ErrAssistantCheckpointConflict
	}
	if sessionErr == nil {
		sess.mu.Lock()
		sess.committing = false
		sess.Status = "committed"
		sess.mu.Unlock()
		s.closeSession(sessionID, false)
	}
	return result, nil
}

func validateAssistantCommitScope(ctx context.Context, repo repository.Repository, userID string, checkpoint *model.AssistantCheckpoint) error {
	project, err := repo.Projects().FindByIDForUpdate(ctx, checkpoint.ProjectID)
	if err != nil {
		return errors.Join(ErrInteractivePersistence, err)
	}
	if project.UserID != userID {
		return ErrInteractiveSessionOwner
	}
	profile := projectProfileFor(project)
	if checkpoint.BaseProfileRevision != profile.Version {
		return ErrAssistantCheckpointConflict
	}
	if checkpoint.ModeID == AssistantModeReview {
		task, err := repo.Tasks().FindByID(ctx, checkpoint.ContentID)
		if err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		if task.UserID != userID || task.ProjectID != checkpoint.ProjectID || task.Status != model.TaskStatusCompleted || task.UpdatedAt.UTC().UnixNano() != checkpoint.BaseContentRevision {
			return ErrAssistantCheckpointConflict
		}
		analytics, err := repo.Analytics().State(ctx, checkpoint.ProjectID)
		if err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		if analytics.Revision != checkpoint.BaseAnalyticsRevision {
			return ErrAssistantCheckpointConflict
		}
	}
	return nil
}

type assistantCreationPlan struct {
	Goal               string   `json:"goal"`
	ContentType        string   `json:"content_type"`
	TargetAudience     string   `json:"target_audience"`
	Angle              string   `json:"angle"`
	Outline            []string `json:"outline"`
	StyleConstraints   string   `json:"style_constraints"`
	AssetPlan          string   `json:"asset_plan"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	CostEstimate       string   `json:"cost_estimate"`
	ExecutionOptions   []string `json:"execution_options"`
}

func decodeAssistantCreationPlan(payload []byte) (assistantCreationPlan, []byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return assistantCreationPlan{}, nil, ErrAssistantCheckpointInvalid
	}
	raw := json.RawMessage(payload)
	if nested, ok := envelope["plan"]; ok {
		raw = nested
	}
	var plan assistantCreationPlan
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return assistantCreationPlan{}, nil, ErrAssistantCheckpointInvalid
	}
	if strings.TrimSpace(plan.Goal) == "" || strings.TrimSpace(plan.ContentType) == "" || strings.TrimSpace(plan.TargetAudience) == "" || strings.TrimSpace(plan.Angle) == "" || len(plan.Outline) == 0 || strings.TrimSpace(plan.StyleConstraints) == "" || strings.TrimSpace(plan.AssetPlan) == "" || strings.TrimSpace(plan.AcceptanceCriteria) == "" || strings.TrimSpace(plan.CostEstimate) == "" || len(plan.ExecutionOptions) == 0 {
		return assistantCreationPlan{}, nil, ErrAssistantCheckpointInvalid
	}
	for _, point := range plan.Outline {
		if strings.TrimSpace(point) == "" {
			return assistantCreationPlan{}, nil, ErrAssistantCheckpointInvalid
		}
	}
	for _, option := range plan.ExecutionOptions {
		if strings.TrimSpace(option) == "" {
			return assistantCreationPlan{}, nil, ErrAssistantCheckpointInvalid
		}
	}
	canonical, err := json.Marshal(plan)
	if err != nil || len(canonical) > 3800 {
		return assistantCreationPlan{}, nil, ErrAssistantCheckpointInvalid
	}
	return plan, canonical, nil
}

type assistantReviewDraft struct {
	Summary                  string                                     `json:"summary"`
	DimensionRecommendations map[string]feedbackDimensionRecommendation `json:"dimension_recommendations"`
	Evidence                 json.RawMessage                            `json:"evidence,omitempty"`
	Confidence               string                                     `json:"confidence,omitempty"`
	Limitations              string                                     `json:"limitations,omitempty"`
}

func decodeAssistantReview(payload []byte) (assistantReviewDraft, []byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return assistantReviewDraft{}, nil, ErrAssistantCheckpointInvalid
	}
	raw := json.RawMessage(payload)
	if nested, ok := envelope["review"]; ok {
		raw = nested
	}
	var review assistantReviewDraft
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&review); err != nil || strings.TrimSpace(review.Summary) == "" || validateFeedbackDimensionRecommendations(review.DimensionRecommendations) != nil {
		return assistantReviewDraft{}, nil, ErrAssistantCheckpointInvalid
	}
	if len(review.Evidence) > 0 && !json.Valid(review.Evidence) {
		return assistantReviewDraft{}, nil, ErrAssistantCheckpointInvalid
	}
	canonical, err := json.Marshal(review)
	if err != nil || len(canonical) > 24<<10 {
		return assistantReviewDraft{}, nil, ErrAssistantCheckpointInvalid
	}
	return review, canonical, nil
}

func commitAssistantCreation(ctx context.Context, userID string, checkpoint *model.AssistantCheckpoint, tasks *TaskService, repo repository.Repository) (AssistantCommitResult, error) {
	if tasks == nil {
		return AssistantCommitResult{}, ErrInteractivePersistence
	}
	_, planJSON, err := decodeAssistantCreationPlan(checkpoint.Payload)
	if err != nil {
		return AssistantCommitResult{}, err
	}
	prompt := "请严格按照以下经用户确认的创作计划完成内容创作。计划是本任务的冻结输入；遇到缺失信息时先按计划约束处理。\n\n" + string(planJSON)
	project, err := tasks.ResolveTaskCreationProject(ctx, userID, checkpoint.ProjectID)
	if err != nil {
		return AssistantCommitResult{}, err
	}
	if existing, lookupErr := repo.Tasks().FindByAssistantCheckpointID(ctx, userID, checkpoint.ID); lookupErr == nil {
		if existing.ProjectID != checkpoint.ProjectID || existing.Prompt != prompt {
			return AssistantCommitResult{}, ErrAssistantCheckpointConflict
		}
		return assistantTaskCommitResult(existing), nil
	} else if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return AssistantCommitResult{}, errors.Join(ErrInteractivePersistence, lookupErr)
	}
	var selected agentpack.Manifest
	for _, agentID := range project.AgentIDs {
		pack, ok := agentpack.Default().ForAgent(agentID)
		if ok && pack.SupportsPlan() && model.ProjectAllowsAgent(project, agentID) {
			selected = pack
			break
		}
	}
	if selected.ID == "" {
		for _, agentID := range model.DefaultProjectAgentIDs(project.Platform) {
			pack, ok := agentpack.Default().ForAgent(agentID)
			if ok && pack.SupportsPlan() && model.ProjectAllowsAgent(project, agentID) {
				selected = pack
				break
			}
		}
	}
	if selected.ID == "" {
		return AssistantCommitResult{}, ErrAssistantModeUnavailable
	}
	if len(planJSON)+128 > 5000 || !utf8.Valid(planJSON) {
		return AssistantCommitResult{}, ErrAssistantCheckpointInvalid
	}

	created, err := tasks.CreateManual(ctx, CreateManualParams{
		UserID: userID, ProjectID: checkpoint.ProjectID, ExecutionProfile: "effective",
		AgentID: selected.Agent.Name, Channel: selected.Channel, TaskKind: selected.PlanTaskKind,
		RequestedTaskType:     taskTypeForIdentity(selected.Agent.Name, selected.Channel, selected.PlanTaskKind),
		AssistantCheckpointID: checkpoint.ID, AssistantProfileRevision: &checkpoint.BaseProfileRevision, Prompt: prompt, Quantity: 1,
	})
	if err != nil {
		if existing, lookupErr := repo.Tasks().FindByAssistantCheckpointID(ctx, userID, checkpoint.ID); lookupErr == nil {
			if existing.ProjectID != checkpoint.ProjectID || existing.Prompt != prompt {
				return AssistantCommitResult{}, ErrAssistantCheckpointConflict
			}
			return assistantTaskCommitResult(existing), nil
		}
		return AssistantCommitResult{}, err
	}
	if len(created) != 1 || created[0] == nil {
		return AssistantCommitResult{}, ErrInteractivePersistence
	}
	return assistantTaskCommitResult(created[0]), nil
}

func assistantTaskCommitResult(task *model.Task) AssistantCommitResult {
	return AssistantCommitResult{ModeID: AssistantModeCreation, Status: "committed", Task: &AssistantTaskResult{ID: task.ID, ProjectID: task.ProjectID, Status: task.Status, CreatedAt: task.CreatedAt}}
}

func commitAssistantReview(ctx context.Context, userID string, checkpoint *model.AssistantCheckpoint, repo repository.Repository) (AssistantCommitResult, error) {
	review, _, err := decodeAssistantReview(checkpoint.Payload)
	if err != nil {
		return AssistantCommitResult{}, err
	}
	analysis, err := json.Marshal(feedbackAnalysisArtifact{
		SchemaVersion: feedbackArtifactSchemaVersion, Status: "ready", Source: "assistant_postmortem_review",
		ProjectID: checkpoint.ProjectID, TargetContentID: checkpoint.ContentID, AnalyticsRevision: checkpoint.BaseAnalyticsRevision,
		Summary: strings.TrimSpace(review.Summary), Confidence: firstNonEmptyAssistant(review.Confidence, "medium"),
		Limitations:              firstNonEmptyAssistant(review.Limitations, "用户确认的复盘假设需经后续独立数据验证。"),
		DimensionRecommendations: review.DimensionRecommendations,
	})
	if err != nil {
		return AssistantCommitResult{}, err
	}
	evidence := map[string]json.RawMessage{"analysis": analysis}
	if len(review.Evidence) > 0 {
		evidence["user_evidence"] = review.Evidence
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return AssistantCommitResult{}, err
	}
	expected := &model.FeedbackInsight{
		ID: checkpoint.ID, UserID: userID, SourceSessionID: checkpoint.SourceSessionID,
		JobID: checkpoint.SourceSessionID, ExecutionID: checkpoint.SourceSessionID,
		ProjectID: checkpoint.ProjectID, AnalyticsRevision: checkpoint.BaseAnalyticsRevision,
		ProfileRevision: checkpoint.BaseProfileRevision, TargetContentID: checkpoint.ContentID,
		BaselineScope: "assistant_review", Trigger: assistantReviewTrigger, Kind: "content_postmortem",
		EvidenceJSON: string(evidenceJSON), Summary: strings.TrimSpace(review.Summary),
		Confidence:      firstNonEmptyAssistant(review.Confidence, "medium"),
		Limitations:     firstNonEmptyAssistant(review.Limitations, "用户确认的复盘假设需经后续独立数据验证。"),
		PromotionStatus: model.FeedbackPromotionConfirmed, ConfirmedBy: userID,
	}
	now := time.Now().UTC()
	expected.ConfirmedAt = &now
	insight, err := repo.FeedbackLoop().FindInsightByID(ctx, expected.ID)
	if err != nil {
		return AssistantCommitResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	if insight == nil {
		if err := repo.FeedbackLoop().CreateInsight(ctx, expected); err != nil {
			return AssistantCommitResult{}, errors.Join(ErrInteractivePersistence, err)
		}
		insight, err = repo.FeedbackLoop().FindInsightByID(ctx, expected.ID)
		if err != nil {
			return AssistantCommitResult{}, errors.Join(ErrInteractivePersistence, err)
		}
	}
	if insight == nil {
		return AssistantCommitResult{}, errors.Join(ErrInteractivePersistence, gorm.ErrRecordNotFound)
	}
	if !assistantReviewInsightMatches(insight, expected) {
		return AssistantCommitResult{}, ErrInteractiveIdempotency
	}
	return AssistantCommitResult{ModeID: AssistantModeReview, Status: "confirmed_waiting_for_analytics_validation", Insight: insight}, nil
}

func assistantReviewInsightMatches(actual, expected *model.FeedbackInsight) bool {
	if actual == nil || expected == nil {
		return false
	}
	return actual.ID == expected.ID &&
		actual.UserID == expected.UserID &&
		actual.SourceSessionID == expected.SourceSessionID &&
		actual.JobID == expected.JobID &&
		actual.ExecutionID == expected.ExecutionID &&
		actual.ProjectID == expected.ProjectID &&
		actual.AnalyticsRevision == expected.AnalyticsRevision &&
		actual.ProfileRevision == expected.ProfileRevision &&
		actual.TargetContentID == expected.TargetContentID &&
		actual.BaselineScope == expected.BaselineScope &&
		actual.Trigger == expected.Trigger &&
		actual.Kind == expected.Kind &&
		actual.EvidenceJSON == expected.EvidenceJSON &&
		actual.Summary == expected.Summary &&
		actual.Confidence == expected.Confidence &&
		actual.Limitations == expected.Limitations &&
		actual.PromotionStatus == expected.PromotionStatus &&
		actual.ConfirmedBy == expected.ConfirmedBy
}
