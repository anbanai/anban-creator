package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type ContentMetadataService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

func NewContentMetadataService(repo repository.Repository, logger *zerolog.Logger) *ContentMetadataService {
	return &ContentMetadataService{repo: repo, logger: logger}
}

type ContentMetadataInput struct {
	AuthenticatedUserID string
	TaskID              string
	ExecutionID         string
	TaxonomyVersion     string
	SourceDigest        string
	RawMetadata         []byte
}

type contentMetadataPayload struct {
	TaskID         string                   `json:"task_id,omitempty"`
	ExecutionID    string                   `json:"execution_id,omitempty"`
	Tags           []contentMetadataTag     `json:"tags"`
	TaskType       string                   `json:"task_type,omitempty"`
	SourceDigest   string                   `json:"source_digest,omitempty"`
	TaggingStatus  string                   `json:"tagging_status,omitempty"`
	FeedbackStatus string                   `json:"feedback_status,omitempty"`
	Feedback       *contentMetadataFeedback `json:"feedback,omitempty"`
}
type contentMetadataFeedback struct {
	Scores        map[string]any `json:"scores,omitempty"`
	Errors        string         `json:"errors,omitempty"`
	Optimizations string         `json:"optimizations,omitempty"`
	Summary       string         `json:"summary,omitempty"`
}
type contentMetadataTag struct {
	Dimension      string          `json:"dimension"`
	Value          string          `json:"value"`
	CanonicalValue string          `json:"canonical_value,omitempty"`
	LabelStatus    string          `json:"label_status,omitempty"`
	DisplayName    string          `json:"display_name,omitempty"`
	Confidence     float64         `json:"confidence,omitempty"`
	Primary        bool            `json:"primary,omitempty"`
	Evidence       json.RawMessage `json:"evidence,omitempty"`
}

func (s *ContentMetadataService) Submit(ctx context.Context, input ContentMetadataInput) (*model.ContentMetadataReport, error) {
	return s.submit(ctx, input, true, true)
}

func (s *ContentMetadataService) submit(ctx context.Context, input ContentMetadataInput, writeTags, writeFeedback bool) (*model.ContentMetadataReport, error) {
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.ExecutionID = strings.TrimSpace(input.ExecutionID)
	if input.TaskID == "" || input.ExecutionID == "" {
		return nil, fmt.Errorf("task_id and execution_id are required")
	}
	if len(input.RawMetadata) == 0 || !json.Valid(input.RawMetadata) {
		return nil, fmt.Errorf("raw metadata must be valid JSON")
	}
	var payload contentMetadataPayload
	if err := json.Unmarshal(input.RawMetadata, &payload); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}
	if payload.TaskID != "" && strings.TrimSpace(payload.TaskID) != input.TaskID {
		return nil, fmt.Errorf("metadata task_id does not match execution scope")
	}
	if payload.ExecutionID != "" && strings.TrimSpace(payload.ExecutionID) != input.ExecutionID {
		return nil, fmt.Errorf("metadata execution_id does not match execution scope")
	}
	for i := range payload.Tags {
		tag := &payload.Tags[i]
		if _, ok := model.ContentTagDimensions[strings.TrimSpace(tag.Dimension)]; !ok {
			return nil, fmt.Errorf("unknown tag dimension %q", tag.Dimension)
		}
		if strings.TrimSpace(tag.Value) == "" {
			return nil, fmt.Errorf("tag value is required")
		}
		if tag.Confidence < 0 || tag.Confidence > 1 {
			return nil, fmt.Errorf("tag confidence must be between 0 and 1")
		}
		if tag.LabelStatus == "" {
			tag.LabelStatus = model.ContentTagCandidate
		}
		if tag.LabelStatus != model.ContentTagCanonical && tag.LabelStatus != model.ContentTagCandidate {
			return nil, fmt.Errorf("invalid tag label_status %q", tag.LabelStatus)
		}
	}
	version := strings.TrimSpace(input.TaxonomyVersion)
	if version == "" {
		version = model.ContentTaxonomyVersion
	}
	if strings.TrimSpace(input.SourceDigest) == "" {
		input.SourceDigest = strings.TrimSpace(payload.SourceDigest)
	}
	task, err := s.repo.Tasks().FindByID(ctx, input.TaskID)
	if err != nil || task == nil {
		return nil, fmt.Errorf("task not found")
	}
	if execution, err := s.repo.TaskExecutions().FindByID(ctx, input.ExecutionID); err != nil || execution.TaskID != input.TaskID {
		return nil, fmt.Errorf("execution does not belong to task")
	}
	if userID := strings.TrimSpace(input.AuthenticatedUserID); userID != "" && task.UserID != userID {
		return nil, fmt.Errorf("task does not belong to authenticated user")
	}
	report := &model.ContentMetadataReport{ID: uuid.NewString(), TaskID: input.TaskID, ExecutionID: input.ExecutionID, Status: model.ContentMetadataPending, TaggingStatus: model.ContentMetadataPending, FeedbackStatus: model.ContentMetadataPending, TaxonomyVersion: version, SourceDigest: strings.TrimSpace(input.SourceDigest), RawMetadata: datatypes.JSON(input.RawMetadata), Attempts: 1}
	if existing, findErr := s.repo.ContentMetadata().FindByTaskExecution(ctx, input.TaskID, input.ExecutionID); findErr == nil && existing != nil {
		report.ID = existing.ID
		report.Attempts = existing.Attempts + 1
		if !writeTags {
			report.TaggingStatus = existing.TaggingStatus
		}
		if !writeFeedback {
			report.FeedbackStatus = existing.FeedbackStatus
		}
	}
	if report.TaggingStatus == model.ContentMetadataPending && payload.TaggingStatus != "" {
		report.TaggingStatus = payload.TaggingStatus
	}
	if report.FeedbackStatus == model.ContentMetadataPending && payload.FeedbackStatus != "" {
		report.FeedbackStatus = payload.FeedbackStatus
	}
	if report.TaggingStatus != model.ContentMetadataPending && report.TaggingStatus != model.ContentMetadataSucceeded && report.TaggingStatus != model.ContentMetadataFailed {
		return nil, fmt.Errorf("invalid tagging_status %q", report.TaggingStatus)
	}
	if report.FeedbackStatus != model.ContentMetadataPending && report.FeedbackStatus != model.ContentMetadataSucceeded && report.FeedbackStatus != model.ContentMetadataFailed {
		return nil, fmt.Errorf("invalid feedback_status %q", report.FeedbackStatus)
	}
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.ContentMetadata().CreateOrUpdate(ctx, report); err != nil {
			return err
		}
		if writeTags {
			tags := make([]*model.ContentTagAssignment, 0, len(payload.Tags))
			for _, tag := range payload.Tags {
				canonical, labelStatus, displayName, err := normalizeTag(ctx, tx.ContentMetadata(), tag, version)
				if err != nil {
					return err
				}
				evidence := datatypes.JSON(tag.Evidence)
				if len(evidence) == 0 {
					evidence = datatypes.JSON([]byte(`{}`))
				}
				tags = append(tags, &model.ContentTagAssignment{ID: uuid.NewString(), ReportID: report.ID, Dimension: strings.TrimSpace(tag.Dimension), Value: strings.TrimSpace(tag.Value), DisplayName: displayName, CanonicalValue: canonical, LabelStatus: labelStatus, Confidence: tag.Confidence, Primary: tag.Primary, Evidence: evidence})
			}
			if err := tx.ContentMetadata().ReplaceTags(ctx, report.ID, tags); err != nil {
				return err
			}
			report.TaggingStatus = model.ContentMetadataSucceeded
		}
		if writeFeedback {
			if payload.Feedback != nil {
				scores, _ := json.Marshal(payload.Feedback.Scores)
				agentName := payload.TaskType
				if agentName == "" {
					agentName = "completion-hook"
				}
				feedback := &model.AgentFeedback{ID: uuid.NewString(), TaskID: input.TaskID, AgentName: agentName, ExecutionID: input.ExecutionID, Source: "hook", Scores: string(scores), Errors: payload.Feedback.Errors, Optimizations: payload.Feedback.Optimizations, Summary: payload.Feedback.Summary}
				if err := tx.AgentFeedbacks().Create(ctx, feedback); err != nil {
					return err
				}
				report.FeedbackStatus = model.ContentMetadataSucceeded
			}
		}
		if report.TaggingStatus == model.ContentMetadataSucceeded && report.FeedbackStatus == model.ContentMetadataSucceeded {
			report.Status = model.ContentMetadataSucceeded
		} else if report.TaggingStatus == model.ContentMetadataFailed || report.FeedbackStatus == model.ContentMetadataFailed {
			report.Status = model.ContentMetadataFailed
		} else {
			report.Status = model.ContentMetadataPending
		}
		if err := tx.ContentMetadata().CreateOrUpdate(ctx, report); err != nil {
			return err
		}
		return nil
	}); err != nil {
		report.Status = model.ContentMetadataFailed
		if writeTags {
			report.TaggingStatus = model.ContentMetadataFailed
		}
		if writeFeedback {
			report.FeedbackStatus = model.ContentMetadataFailed
		}
		report.ErrorMessage = err.Error()
		_ = s.repo.ContentMetadata().CreateOrUpdate(ctx, report)
		return nil, err
	}
	return report, nil
}

func normalizeTag(ctx context.Context, repo repository.ContentMetadataRepository, tag contentMetadataTag, version string) (string, string, string, error) {
	dimension := strings.TrimSpace(tag.Dimension)
	value := normalizeTagValue(tag.Value)
	if value == "" {
		return "", "", "", fmt.Errorf("tag value is required")
	}
	values, err := repo.ListVocabulary(ctx, dimension, version)
	if err != nil {
		return "", "", "", err
	}
	for _, vocabulary := range values {
		if normalizeTagValue(vocabulary.Value) == value {
			return vocabulary.Value, vocabulary.Status, vocabulary.DisplayName, nil
		}
		var aliases []string
		if len(vocabulary.Aliases) > 0 {
			_ = json.Unmarshal(vocabulary.Aliases, &aliases)
		}
		for _, alias := range aliases {
			if normalizeTagValue(alias) == value {
				return vocabulary.Value, vocabulary.Status, vocabulary.DisplayName, nil
			}
		}
	}
	for _, vocabulary := range model.ContentTagDefaultVocabularies[dimension] {
		matched := normalizeTagValue(vocabulary.Value) == value
		for _, alias := range vocabulary.Aliases {
			matched = matched || normalizeTagValue(alias) == value
		}
		if !matched {
			continue
		}
		aliases, _ := json.Marshal(vocabulary.Aliases)
		entry := &model.ContentTagVocabulary{ID: uuid.NewString(), Dimension: dimension, Value: vocabulary.Value, DisplayName: vocabulary.DisplayName, Status: model.ContentTagCanonical, Aliases: datatypes.JSON(aliases), TaxonomyVersion: version}
		if err := repo.UpsertVocabulary(ctx, entry); err != nil {
			return "", "", "", err
		}
		return vocabulary.Value, model.ContentTagCanonical, vocabulary.DisplayName, nil
	}
	canonical := normalizeTagValue(tag.CanonicalValue)
	if canonical == "" {
		canonical = value
	}
	status := model.ContentTagCandidate
	displayName := strings.TrimSpace(tag.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(tag.Value)
	}
	if err := repo.UpsertVocabulary(ctx, &model.ContentTagVocabulary{ID: uuid.NewString(), Dimension: dimension, Value: canonical, DisplayName: displayName, Status: status, Aliases: datatypes.JSON([]byte(`[]`)), TaxonomyVersion: version}); err != nil {
		return "", "", "", err
	}
	return canonical, status, displayName, nil
}

func normalizeTagValue(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func (s *ContentMetadataService) RecomputeTags(ctx context.Context, authenticatedUserID, taskID, executionID string) (*model.ContentMetadataReport, error) {
	report, err := s.Find(ctx, taskID, executionID)
	if err != nil {
		return nil, err
	}
	return s.submit(ctx, ContentMetadataInput{AuthenticatedUserID: authenticatedUserID, TaskID: report.TaskID, ExecutionID: report.ExecutionID, TaxonomyVersion: report.TaxonomyVersion, SourceDigest: report.SourceDigest, RawMetadata: report.RawMetadata}, true, false)
}

func (s *ContentMetadataService) RecomputeFeedback(ctx context.Context, authenticatedUserID, taskID, executionID string) (*model.ContentMetadataReport, error) {
	report, err := s.Find(ctx, taskID, executionID)
	if err != nil {
		return nil, err
	}
	return s.submit(ctx, ContentMetadataInput{AuthenticatedUserID: authenticatedUserID, TaskID: report.TaskID, ExecutionID: report.ExecutionID, TaxonomyVersion: report.TaxonomyVersion, SourceDigest: report.SourceDigest, RawMetadata: report.RawMetadata}, false, true)
}

func (s *ContentMetadataService) Find(ctx context.Context, taskID, executionID string) (*model.ContentMetadataReport, error) {
	return s.repo.ContentMetadata().FindByTaskExecution(ctx, strings.TrimSpace(taskID), strings.TrimSpace(executionID))
}

// Authorize verifies that a non-admin caller owns the task and that the
// execution belongs to it before allowing metadata reads or recomputation.
func (s *ContentMetadataService) Authorize(ctx context.Context, authenticatedUserID, taskID, executionID string) (*model.Task, error) {
	task, err := s.repo.Tasks().FindByID(ctx, strings.TrimSpace(taskID))
	if err != nil || task == nil {
		return nil, fmt.Errorf("task not found")
	}
	if userID := strings.TrimSpace(authenticatedUserID); userID != "" && task.UserID != userID {
		return nil, fmt.Errorf("task does not belong to authenticated user")
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, strings.TrimSpace(executionID))
	if err != nil || execution == nil || execution.TaskID != task.ID {
		return nil, fmt.Errorf("execution does not belong to task")
	}
	return task, nil
}

func (s *ContentMetadataService) FindAuthorized(ctx context.Context, authenticatedUserID, taskID, executionID string) (*model.ContentMetadataReport, error) {
	if _, err := s.Authorize(ctx, authenticatedUserID, taskID, executionID); err != nil {
		return nil, err
	}
	return s.Find(ctx, taskID, executionID)
}

func (s *ContentMetadataService) Recompute(ctx context.Context, taskID, executionID string) (*model.ContentMetadataReport, error) {
	report, err := s.Find(ctx, taskID, executionID)
	if err != nil {
		return nil, err
	}
	return s.Submit(ctx, ContentMetadataInput{TaskID: report.TaskID, ExecutionID: report.ExecutionID, TaxonomyVersion: report.TaxonomyVersion, SourceDigest: report.SourceDigest, RawMetadata: report.RawMetadata})
}
