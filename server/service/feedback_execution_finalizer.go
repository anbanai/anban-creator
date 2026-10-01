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

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

const (
	feedbackAnalysisArtifactPath  = "output/feedback-analysis.json"
	feedbackEvidenceArtifactPath  = "output/feedback-evidence.json"
	feedbackArtifactSchemaVersion = "1.0"
)

// feedbackAnalysisArtifact is the private handoff contract between a
// Feedback Agent and the Server finalizer. The Server owns all identity and
// persistence fields; agent supplied identity is checked, never trusted.
type feedbackAnalysisArtifact struct {
	SchemaVersion     string          `json:"schema_version"`
	Status            string          `json:"status"`
	Source            string          `json:"source"`
	DataAt            string          `json:"data_at"`
	Missing           []string        `json:"missing"`
	EvidencePaths     []string        `json:"evidence_paths"`
	JobID             string          `json:"job_id,omitempty"`
	ProjectID         string          `json:"project_id,omitempty"`
	Operation         string          `json:"operation,omitempty"`
	TargetContentID   string          `json:"target_content_id,omitempty"`
	BaselineScope     string          `json:"baseline_scope,omitempty"`
	Trigger           string          `json:"trigger,omitempty"`
	AnalyticsRevision int64           `json:"analytics_revision,omitempty"`
	Summary           string          `json:"summary,omitempty"`
	Confidence        string          `json:"confidence,omitempty"`
	Limitations       string          `json:"limitations,omitempty"`
	PossibleFactors   []string        `json:"possible_factors,omitempty"`
	Experiments       []string        `json:"experiments,omitempty"`
	Recommendations   json.RawMessage `json:"recommendations,omitempty"`
}

type feedbackEvidenceArtifact struct {
	SchemaVersion     string          `json:"schema_version"`
	Status            string          `json:"status"`
	Source            string          `json:"source"`
	DataAt            string          `json:"data_at"`
	Missing           []string        `json:"missing"`
	EvidencePaths     []string        `json:"evidence_paths"`
	JobID             string          `json:"job_id,omitempty"`
	AnalyticsRevision int64           `json:"analytics_revision,omitempty"`
	Evidence          json.RawMessage `json:"evidence,omitempty"`
}

func (s *TaskService) finalizeFeedbackExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult) error {
	if task == nil || execution == nil || task.TaskKind != model.TaskKindFeedbackAnalysis {
		return nil
	}
	input := task.AgentInput.Data()
	jobID, _ := input["feedback_job_id"].(string)
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return errors.New("feedback task has no feedback_job_id")
	}
	job, err := s.repo.FeedbackLoop().FindJobByIDOrFingerprint(ctx, jobID)
	if err != nil {
		return fmt.Errorf("find feedback job during finalization: %w", err)
	}
	if job.TaskID != task.ID || (job.ExecutionID != "" && job.ExecutionID != execution.ID) {
		return fmt.Errorf("feedback job %s execution identity mismatch", job.ID)
	}
	leaseScope := FeedbackAccountLeaseScope(job.AccountID)
	defer func() {
		if leaseScope != "" {
			_ = s.repo.FeedbackLoop().ReleaseFeedbackLease(context.Background(), leaseScope, job.ID)
		}
	}()
	if execution.Status != model.TaskExecutionSucceeded || result == nil || !result.Success {
		markFeedbackFailure(job, errors.New("feedback Agent execution failed"))
		now := time.Now().UTC()
		job.CompletedAt = &now
		if err := s.repo.FeedbackLoop().UpdateJob(ctx, job); err != nil {
			return fmt.Errorf("persist failed feedback job: %w", err)
		}
		return nil
	}

	analysisBody, found, err := s.readExecutionArtifact(ctx, execution.ID, feedbackAnalysisArtifactPath, maxTaskDeliveryJSONBytes)
	if err != nil {
		return fmt.Errorf("read feedback analysis artifact: %w", err)
	}
	if !found {
		return s.failFeedbackFinalization(ctx, job, "feedback_analysis_missing")
	}
	evidenceBody, found, err := s.readExecutionArtifact(ctx, execution.ID, feedbackEvidenceArtifactPath, maxTaskDeliveryJSONBytes)
	if err != nil {
		return fmt.Errorf("read feedback evidence artifact: %w", err)
	}
	if !found {
		return s.failFeedbackFinalization(ctx, job, "feedback_evidence_missing")
	}

	var analysis feedbackAnalysisArtifact
	if err := json.Unmarshal(analysisBody, &analysis); err != nil {
		return s.failFeedbackFinalization(ctx, job, "feedback_analysis_invalid")
	}
	var evidence feedbackEvidenceArtifact
	if err := json.Unmarshal(evidenceBody, &evidence); err != nil {
		return s.failFeedbackFinalization(ctx, job, "feedback_evidence_invalid")
	}
	if err := validateFeedbackArtifacts(job, &analysis, &evidence); err != nil {
		return s.failFeedbackFinalization(ctx, job, err.Error())
	}
	if analysis.Status == "skipped" || analysis.Status == "data_insufficient" {
		now := time.Now().UTC()
		job.Status = model.FeedbackJobSkipped
		job.SkipReason = analysis.Status
		job.CompletedAt = &now
		job.LastError = ""
		return s.repo.FeedbackLoop().UpdateJob(ctx, job)
	}

	combinedEvidence := map[string]any{}
	if err := json.Unmarshal(evidenceBody, &combinedEvidence); err != nil {
		return s.failFeedbackFinalization(ctx, job, "feedback_evidence_invalid")
	}
	combinedEvidence["analysis"] = analysis
	combined, err := json.Marshal(combinedEvidence)
	if err != nil {
		return fmt.Errorf("marshal feedback evidence: %w", err)
	}
	promotion := model.FeedbackPromotionNone
	if job.TargetContentID != "" && job.Operation == "content_postmortem" {
		promotion = model.FeedbackPromotionCandidate
	}
	confidence := strings.TrimSpace(analysis.Confidence)
	if confidence == "" {
		confidence = confidenceForSample(job.SampleCount)
	}
	limitations := strings.TrimSpace(analysis.Limitations)
	if limitations == "" {
		limitations = "Correlation does not establish causation; coverage depends on imported platform observations."
	}
	insight := &model.FeedbackInsight{
		ID: uuid.NewString(), JobID: job.ID, ExecutionID: execution.ID, ProjectID: job.ProjectID,
		AnalyticsRevision: job.AnalyticsRevision, TargetContentID: job.TargetContentID,
		BaselineScope: firstNonEmpty(analysis.BaselineScope, "account_platform"), Trigger: job.Trigger,
		Kind: job.Operation, EvidenceJSON: string(combined), Summary: strings.TrimSpace(analysis.Summary),
		Confidence: confidence, Limitations: limitations, PromotionStatus: promotion,
		AgentPackDigest: execution.AgentPackDigest, AnalysisArtifactPath: feedbackAnalysisArtifactPath,
		EvidenceArtifactPath: feedbackEvidenceArtifactPath, AnalysisArtifactHash: sha256Hex(analysisBody),
		EvidenceArtifactHash: sha256Hex(evidenceBody),
	}
	if insight.Summary == "" {
		insight.Summary = fmt.Sprintf("%s completed for analytics revision %d.", strings.ReplaceAll(job.Operation, "_", " "), job.AnalyticsRevision)
	}
	if err := s.repo.FeedbackLoop().CreateInsight(ctx, insight); err != nil {
		return s.failFeedbackFinalization(ctx, job, "feedback_insight_persist_failed")
	}
	// A warning artifact may support a human-readable insight, but it is not
	// sufficient evidence for a formal strategy. Strategy activation remains a
	// monthly, server-gated operation with the threshold enforced from the
	// frozen job sample count.
	if job.Operation == "strategy_advisor" && job.Cadence == model.FeedbackCadenceMonthly && job.SampleCount >= 10 && analysis.Status == "ready" {
		if err := s.persistFeedbackStrategy(ctx, job, execution, analysis, string(combined)); err != nil {
			return s.failFeedbackFinalization(ctx, job, "feedback_strategy_persist_failed")
		}
	}
	now := time.Now().UTC()
	job.Status = model.FeedbackJobSucceeded
	job.CompletedAt = &now
	job.LastError = ""
	return s.repo.FeedbackLoop().UpdateJob(ctx, job)
}

// FinalizeFeedbackExecution is exposed for focused service tests and recovery
// workers. Normal managed execution calls it from the durable finalizer.
func (s *TaskService) FinalizeFeedbackExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult) error {
	return s.finalizeFeedbackExecution(ctx, task, execution, result)
}

func validateFeedbackArtifacts(job *model.FeedbackJob, analysis *feedbackAnalysisArtifact, evidence *feedbackEvidenceArtifact) error {
	if job == nil || analysis == nil || evidence == nil {
		return errors.New("feedback_artifact_invalid")
	}
	if analysis.SchemaVersion != feedbackArtifactSchemaVersion || evidence.SchemaVersion != feedbackArtifactSchemaVersion {
		return errors.New("feedback_artifact_schema_invalid")
	}
	allowed := map[string]bool{"ready": true, "warning": true, "data_insufficient": true, "skipped": true}
	if !allowed[strings.TrimSpace(analysis.Status)] || !allowed[strings.TrimSpace(evidence.Status)] {
		return errors.New("feedback_artifact_status_invalid")
	}
	if strings.TrimSpace(analysis.Source) == "" || strings.TrimSpace(evidence.Source) == "" || strings.TrimSpace(analysis.DataAt) == "" || strings.TrimSpace(evidence.DataAt) == "" {
		return errors.New("feedback_artifact_metadata_missing")
	}
	if _, err := time.Parse(time.RFC3339, analysis.DataAt); err != nil {
		return errors.New("feedback_analysis_data_at_invalid")
	}
	if _, err := time.Parse(time.RFC3339, evidence.DataAt); err != nil {
		return errors.New("feedback_evidence_data_at_invalid")
	}
	if analysis.Status != "skipped" && analysis.Status != "data_insufficient" && (analysis.JobID != job.ID || analysis.ProjectID != job.ProjectID || analysis.Operation != job.Operation || analysis.TargetContentID != job.TargetContentID || analysis.AnalyticsRevision != job.AnalyticsRevision) {
		return errors.New("feedback_artifact_job_mismatch")
	}
	if evidence.Status != "skipped" && evidence.Status != "data_insufficient" && (evidence.JobID != job.ID || evidence.AnalyticsRevision != job.AnalyticsRevision) {
		return errors.New("feedback_artifact_revision_mismatch")
	}
	if analysis.Status != evidence.Status {
		return errors.New("feedback_artifact_status_mismatch")
	}
	if analysis.Status == "ready" && len(evidence.Evidence) == 0 {
		return errors.New("feedback_evidence_payload_missing")
	}
	if len(evidence.Evidence) > 0 && !json.Valid(evidence.Evidence) {
		return errors.New("feedback_evidence_payload_invalid")
	}
	return nil
}

func (s *TaskService) failFeedbackFinalization(ctx context.Context, job *model.FeedbackJob, reason string) error {
	markFeedbackFailure(job, errors.New(reason))
	if err := s.repo.FeedbackLoop().UpdateJob(ctx, job); err != nil {
		return fmt.Errorf("persist feedback finalization failure: %w", err)
	}
	return errors.New(reason)
}

func (s *TaskService) persistFeedbackStrategy(ctx context.Context, job *model.FeedbackJob, execution *model.TaskExecution, analysis feedbackAnalysisArtifact, evidence string) error {
	active, err := s.repo.FeedbackLoop().FindActiveStrategy(ctx, job.ProjectID, job.Platform)
	if err != nil {
		return err
	}
	if active != nil && active.SourceRevision == job.AnalyticsRevision {
		return nil
	}
	recommendations := analysis.Recommendations
	if len(recommendations) == 0 || !json.Valid(recommendations) {
		recommendations = json.RawMessage(`{}`)
	}
	digest := sha256Hex(recommendations)
	revision, err := s.repo.FeedbackLoop().NextStrategyRevision(ctx, job.ProjectID, job.Platform)
	if err != nil {
		return err
	}
	applicable := []string{model.TaskTypeWechatArticle, model.TaskTypeWechatPicture, model.PlatformSeednote}
	if job.Platform == model.PlatformSeednote {
		applicable = []string{model.PlatformSeednote}
	}
	applicableJSON, _ := json.Marshal(applicable)
	return s.repo.FeedbackLoop().CreateStrategy(ctx, &model.StrategySnapshot{
		ID: uuid.NewString(), ProjectID: job.ProjectID, Platform: job.Platform, Revision: revision,
		SourceRevision: job.AnalyticsRevision, Digest: digest, Status: "active", ApplicableTasks: string(applicableJSON),
		Recommendations: string(recommendations), Evidence: evidence, Confidence: analysis.Confidence,
		Limitations: firstNonEmpty(analysis.Limitations, "Advisory only; review sample coverage before applying."),
	})
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
