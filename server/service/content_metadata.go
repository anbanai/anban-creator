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
	SchemaVersion   string                   `json:"schema_version,omitempty"`
	TaxonomyVersion string                   `json:"taxonomy_version,omitempty"`
	Subjects        []contentMetadataSubject `json:"subjects,omitempty"`
	TaskID          string                   `json:"task_id,omitempty"`
	ExecutionID     string                   `json:"execution_id,omitempty"`
	Tags            []contentMetadataTag     `json:"tags"`
	TaskType        string                   `json:"task_type,omitempty"`
	SourceDigest    string                   `json:"source_digest,omitempty"`
	TaggingStatus   string                   `json:"tagging_status,omitempty"`
	FeedbackStatus  string                   `json:"feedback_status,omitempty"`
	Feedback        *contentMetadataFeedback `json:"feedback,omitempty"`
}
type contentMetadataSubject struct {
	ArtifactRole      string               `json:"artifact_role"`
	Tags              []contentMetadataTag `json:"tags"`
	OmittedDimensions []string             `json:"omitted_dimensions,omitempty"`
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
	artifactRole   string
	source         string
	labelStatusSet bool
}

type contentTagEvidence struct {
	SourceFile   string `json:"source_file"`
	LineStart    int    `json:"line_start"`
	LineEnd      int    `json:"line_end"`
	Quote        string `json:"quote"`
	EvidenceKind string `json:"evidence_kind"`
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
	if payload.SchemaVersion != model.ContentTaxonomyVersion || payload.TaxonomyVersion != model.ContentTaxonomyVersion {
		return nil, fmt.Errorf("schema_version and taxonomy_version must be %s", model.ContentTaxonomyVersion)
	}
	payload.Tags = nil
	for _, subject := range payload.Subjects {
		if strings.TrimSpace(subject.ArtifactRole) == "" {
			return nil, fmt.Errorf("artifact_role is required")
		}
		for _, tag := range subject.Tags {
			tag.Dimension = strings.TrimSpace(tag.Dimension)
			if tag.Value == "" {
				tag.Value = tag.CanonicalValue
			}
			if tag.Dimension == "performance" || tag.Dimension == "content_type" || tag.Dimension == "platform" || tag.Dimension == "artifact_role" || tag.Dimension == "media_form" {
				return nil, fmt.Errorf("server-owned tag dimension %q cannot be submitted by Agent", tag.Dimension)
			}
			tag.artifactRole = strings.TrimSpace(subject.ArtifactRole)
			tag.labelStatusSet = strings.TrimSpace(tag.LabelStatus) != ""
			payload.Tags = append(payload.Tags, tag)
		}
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
		if len(tag.Evidence) == 0 {
			return nil, fmt.Errorf("semantic tag %q requires evidence", tag.Dimension)
		}
		var evidence []contentTagEvidence
		if err := json.Unmarshal(tag.Evidence, &evidence); err != nil || len(evidence) == 0 {
			return nil, fmt.Errorf("semantic tag %q requires structured evidence", tag.Dimension)
		}
		for _, item := range evidence {
			if strings.TrimSpace(item.SourceFile) == "" || item.LineStart < 1 || item.LineEnd < item.LineStart || strings.TrimSpace(item.Quote) == "" || strings.TrimSpace(item.EvidenceKind) == "" {
				return nil, fmt.Errorf("semantic tag %q has invalid evidence location", tag.Dimension)
			}
		}
		if tag.Confidence < 0 || tag.Confidence > 1 {
			return nil, fmt.Errorf("tag confidence must be between 0 and 1")
		}
		if tag.LabelStatus == "" {
			tag.LabelStatus = model.ContentTagCandidate
		}
		if tag.LabelStatus != model.ContentTagCanonical && tag.LabelStatus != model.ContentTagCandidate && tag.LabelStatus != model.ContentTagDiagnostic {
			return nil, fmt.Errorf("invalid tag label_status %q", tag.LabelStatus)
		}
	}
	version := strings.TrimSpace(input.TaxonomyVersion)
	if version == "" {
		version = payload.TaxonomyVersion
	}
	if version != model.ContentTaxonomyVersion {
		return nil, fmt.Errorf("unsupported taxonomy version %q", version)
	}
	if strings.TrimSpace(input.SourceDigest) == "" {
		input.SourceDigest = strings.TrimSpace(payload.SourceDigest)
	}
	task, err := s.repo.Tasks().FindByID(ctx, input.TaskID)
	if err != nil || task == nil {
		return nil, fmt.Errorf("task not found")
	}
	execution, executionErr := s.repo.TaskExecutions().FindByID(ctx, input.ExecutionID)
	if executionErr != nil || execution == nil || execution.TaskID != input.TaskID {
		return nil, fmt.Errorf("execution does not belong to task")
	}
	if !isContentTask(task, execution) {
		if isKnownContentTaskType(task.Type) {
			return nil, fmt.Errorf("content task type %q requires a valid frozen tagging contract", task.Type)
		}
		return nil, fmt.Errorf("task type %q is not a content subject", task.Type)
	}
	if userID := strings.TrimSpace(input.AuthenticatedUserID); userID != "" && task.UserID != userID {
		return nil, fmt.Errorf("task does not belong to authenticated user")
	}
	providedArtifactRoles := make([]string, 0, len(payload.Subjects))
	for _, subject := range payload.Subjects {
		if role := strings.TrimSpace(subject.ArtifactRole); role != "" {
			providedArtifactRoles = append(providedArtifactRoles, role)
		}
	}
	artifactRoles := append([]string(nil), providedArtifactRoles...)
	// A missing or failed content-tags.json is still a valid completion-metadata
	// handoff: Server-owned tags must be persisted while semantic tagging stays
	// visibly incomplete. Derive the subject role from the frozen Pack contract
	// instead of inventing the retired `final` role.
	if len(artifactRoles) == 0 && len(execution.AgentPackTaggingContract) > 0 && string(execution.AgentPackTaggingContract) != "null" {
		var contract struct {
			SubjectRoles []string `json:"subject_roles"`
		}
		if err := json.Unmarshal(execution.AgentPackTaggingContract, &contract); err != nil {
			return nil, fmt.Errorf("decode frozen tagging contract: %w", err)
		}
		for _, role := range contract.SubjectRoles {
			if role = strings.TrimSpace(role); role != "" {
				artifactRoles = append(artifactRoles, role)
			}
		}
	}
	artifactRole := "final"
	if len(artifactRoles) > 0 {
		artifactRole = artifactRoles[0]
	}
	if len(execution.AgentPackTaggingContract) > 0 && string(execution.AgentPackTaggingContract) != "null" {
		var contract struct {
			SubjectRoles []string `json:"subject_roles"`
		}
		if err := json.Unmarshal(execution.AgentPackTaggingContract, &contract); err != nil {
			return nil, fmt.Errorf("decode frozen tagging contract: %w", err)
		}
		if len(artifactRoles) == 0 {
			return nil, fmt.Errorf("frozen tagging contract has no subject role")
		}
		for _, artifactRole := range artifactRoles {
			allowed := false
			for _, role := range contract.SubjectRoles {
				if strings.TrimSpace(role) == artifactRole {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, fmt.Errorf("artifact_role %q is not declared by the frozen tagging contract", artifactRole)
			}
		}
	}
	if writeTags {
		payload.Tags = append(payload.Tags, serverDerivedTags(task, artifactRole)...)
	}
	if writeTags && task.ContentOrigin == model.ContentOriginHotSearch {
		payload.Tags = withServerContentOriginTag(payload.Tags)
	}
	if len(execution.AgentPackDeliveryContract) > 0 {
		var delivery []struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(execution.AgentPackDeliveryContract, &delivery); err != nil {
			return nil, fmt.Errorf("decode frozen delivery contract: %w", err)
		}
		if len(artifactRoles) == 0 {
			artifactRoles = []string{artifactRole}
		}
		for _, artifactRole := range artifactRoles {
			allowed := false
			for _, item := range delivery {
				if strings.TrimSpace(item.Role) == artifactRole {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, fmt.Errorf("artifact_role %q is not declared by the frozen delivery contract", artifactRole)
			}
		}
	}
	report := &model.ContentMetadataReport{ID: uuid.NewString(), TaskID: input.TaskID, ExecutionID: input.ExecutionID, Status: model.ContentMetadataPending, TaggingStatus: model.ContentMetadataPending, FeedbackStatus: model.ContentMetadataPending, TaxonomyVersion: model.ContentTaxonomyVersion, ContentSubject: input.TaskID, ArtifactRole: artifactRole, Platform: serverPlatform(task), ContentType: task.Type, SourceDigest: strings.TrimSpace(input.SourceDigest), RawMetadata: datatypes.JSON(input.RawMetadata), Attempts: 1}
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
	if writeTags && len(payload.Subjects) == 0 {
		// A content artifact cannot claim successful semantic tagging without a
		// subject entry. The completion hook uses an empty list for missing or
		// invalid tagging output, which must remain visibly failed.
		report.TaggingStatus = model.ContentMetadataFailed
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
			counts := make(map[string]int)
			for _, tag := range payload.Tags {
				dimension := strings.TrimSpace(tag.Dimension)
				canonical, labelStatus, displayName, err := normalizeTag(ctx, tx.ContentMetadata(), tag, version)
				if tag.labelStatusSet && tag.LabelStatus == model.ContentTagDiagnostic {
					labelStatus = model.ContentTagDiagnostic
				} else if tag.labelStatusSet && tag.LabelStatus == model.ContentTagCandidate && canonical != "" {
					labelStatus = model.ContentTagCandidate
				}
				if isServerOwnedDimension(dimension) && tag.source == "server" {
					canonical = normalizeTagValue(tag.Value)
					labelStatus = model.ContentTagCanonical
					displayName = serverTagDisplayName(dimension, canonical)
					err = nil
				}
				if err != nil {
					return err
				}
				evidence := datatypes.JSON(tag.Evidence)
				if len(evidence) == 0 {
					evidence = datatypes.JSON([]byte(`{}`))
				}
				counts[dimension]++
				if limit := model.ContentTagDimensionLimits[dimension]; limit > 0 && counts[dimension] > limit {
					return fmt.Errorf("too many tags for dimension %q", dimension)
				}
				for _, existing := range tags {
					if existing.Dimension == dimension && existing.CanonicalValue != "" && existing.CanonicalValue == canonical {
						return fmt.Errorf("duplicate tag %q for dimension %q", canonical, dimension)
					}
				}
				source := tag.source
				if source == "" {
					source = "agent"
				}
				if isServerOwnedDimension(dimension) {
					source = "server"
				}
				visible := labelStatus == model.ContentTagCanonical && (source == "server" || payload.TaggingStatus == model.ContentMetadataSucceeded)
				assignmentRole := report.ArtifactRole
				if strings.TrimSpace(tag.artifactRole) != "" {
					assignmentRole = strings.TrimSpace(tag.artifactRole)
				}
				tags = append(tags, &model.ContentTagAssignment{ID: uuid.NewString(), ReportID: report.ID, Dimension: dimension, Value: strings.TrimSpace(tag.Value), DisplayName: displayName, CanonicalValue: canonical, LabelStatus: labelStatus, Confidence: tag.Confidence, Primary: tag.Primary, Evidence: evidence, ContentSubject: report.ContentSubject, ArtifactRole: assignmentRole, TagGroup: model.ContentTagGroups[dimension], Source: source, AnalyticsVisible: visible, TaxonomyVersion: version})
			}
			if err := tx.ContentMetadata().ReplaceTags(ctx, report.ID, tags); err != nil {
				return err
			}
			if payload.TaggingStatus == model.ContentMetadataSucceeded {
				if taggingSubjectsComplete(execution.AgentPackTaggingContract, payload.Subjects) {
					report.TaggingStatus = model.ContentMetadataSucceeded
				} else {
					report.TaggingStatus = model.ContentMetadataFailed
				}
			} else if payload.TaggingStatus == model.ContentMetadataFailed || payload.TaggingStatus == "" {
				report.TaggingStatus = model.ContentMetadataFailed
			}
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

func taggingSubjectsComplete(rawContract datatypes.JSON, subjects []contentMetadataSubject) bool {
	if len(rawContract) == 0 || string(rawContract) == "null" {
		return true
	}
	var contract struct {
		SubjectRoles []string `json:"subject_roles"`
	}
	if err := json.Unmarshal(rawContract, &contract); err != nil {
		return false
	}
	provided := make(map[string]struct{}, len(subjects))
	for _, subject := range subjects {
		role := strings.TrimSpace(subject.ArtifactRole)
		if role == "" {
			return false
		}
		provided[role] = struct{}{}
		tagDimensions := make(map[string]struct{}, len(subject.Tags))
		for _, tag := range subject.Tags {
			tagDimensions[strings.TrimSpace(tag.Dimension)] = struct{}{}
		}
		omittedDimensions := make(map[string]struct{}, len(subject.OmittedDimensions))
		for _, omitted := range subject.OmittedDimensions {
			dimension := strings.TrimSpace(omitted)
			if _, ok := model.ContentTagDimensions[dimension]; !ok || isServerOwnedDimension(dimension) {
				return false
			}
			if _, duplicate := omittedDimensions[dimension]; duplicate {
				return false
			}
			if _, tagged := tagDimensions[dimension]; tagged {
				return false
			}
			omittedDimensions[dimension] = struct{}{}
		}
		if len(subject.Tags) == 0 && len(subject.OmittedDimensions) == 0 {
			return false
		}
	}
	for _, role := range contract.SubjectRoles {
		role = strings.TrimSpace(role)
		if role == "" {
			return false
		}
		if _, ok := provided[role]; !ok {
			return false
		}
	}
	return len(contract.SubjectRoles) > 0
}

func withServerContentOriginTag(tags []contentMetadataTag) []contentMetadataTag {
	const dimension = "source_relation"
	const value = "hot_search"
	filtered := make([]contentMetadataTag, 0, len(tags)+1)
	for _, tag := range tags {
		if strings.TrimSpace(tag.Dimension) == dimension {
			continue
		}
		filtered = append(filtered, tag)
	}
	return append(filtered, contentMetadataTag{
		Dimension:   dimension,
		Value:       value,
		LabelStatus: model.ContentTagCanonical,
		Confidence:  1,
		Primary:     true,
		Evidence:    json.RawMessage(`{"source":"server_task_content_origin"}`),
		source:      "server",
	})
}
func serverDerivedTags(task *model.Task, artifactRole string) []contentMetadataTag {
	p := task.Channel
	if strings.HasPrefix(p, "wechat") {
		p = model.PlatformWechat
	}
	if p == "" {
		p = model.PlatformForChannel(task.Type)
	}
	mf := serverMediaForm(task.Type)
	e := json.RawMessage(`{"source":"server"}`)
	return []contentMetadataTag{{Dimension: "content_type", Value: task.Type, LabelStatus: model.ContentTagCanonical, Confidence: 1, Evidence: e, source: "server"}, {Dimension: "platform", Value: p, LabelStatus: model.ContentTagCanonical, Confidence: 1, Evidence: e, source: "server"}, {Dimension: "artifact_role", Value: artifactRole, LabelStatus: model.ContentTagCanonical, Confidence: 1, Evidence: e, source: "server"}, {Dimension: "media_form", Value: mf, LabelStatus: model.ContentTagCanonical, Confidence: 1, Evidence: e, source: "server"}}
}

func isContentTask(task *model.Task, execution *model.TaskExecution) bool {
	if task == nil || execution == nil {
		return false
	}
	switch strings.TrimSpace(task.TaskKind) {
	case model.TaskKindViralAnalysis, model.TaskKindProfileAnalysis, model.TaskKindFeedbackAnalysis:
		return false
	}
	switch strings.TrimSpace(task.Type) {
	case model.TaskTypeViralAnalysis, model.TaskTypeProfileAnalysis, model.TaskKindFeedbackAnalysis:
		return false
	}
	// Future content Packs are identified by the frozen Server-owned tagging
	// contract rather than a hard-coded platform enum. Non-content Packs never
	// carry this contract.
	tagging, err := resolveFrozenExecutionTaggingContract(execution)
	return err == nil && tagging != nil
}

func isKnownContentTaskType(taskType string) bool {
	switch taskType {
	case model.TaskTypeWechatArticle, model.TaskTypeWechatPicture, model.PlatformSeednote,
		model.PlatformMoments, model.PlatformEcommerce, model.PlatformMontage,
		model.PlatformWhiteboardAnimation, model.PlatformHypit:
		return true
	default:
		return false
	}
}

func isServerOwnedDimension(dimension string) bool {
	switch dimension {
	case "content_type", "platform", "artifact_role", "media_form", "performance":
		return true
	default:
		return false
	}
}

func serverMediaForm(taskType string) string {
	switch taskType {
	case model.TaskTypeWechatPicture:
		return "image"
	case model.PlatformSeednote, model.PlatformMoments:
		return "graphic_text"
	case model.PlatformEcommerce:
		return "commerce_asset"
	case model.PlatformMontage, model.PlatformHypit, model.PlatformWhiteboardAnimation:
		return "video"
	default:
		return "article"
	}
}

func serverTagDisplayName(dimension, value string) string {
	for _, vocabulary := range model.ContentTagDefaultVocabularies[dimension] {
		if vocabulary.Value == value {
			return vocabulary.DisplayName
		}
	}
	return value
}

func serverPlatform(task *model.Task) string {
	if task == nil {
		return ""
	}
	if p := strings.TrimSpace(task.Channel); p != "" {
		if strings.HasPrefix(p, "wechat") {
			return model.PlatformWechat
		}
		return p
	}
	return model.PlatformForChannel(task.Type)
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
		// Defaults are a server-owned registry used for validation only. Unknown
		// or newly proposed values must be explicitly reviewed and inserted into
		// the vocabulary table; submission never mutates the formal vocabulary.
		return vocabulary.Value, model.ContentTagCanonical, vocabulary.DisplayName, nil
	}
	// Unknown values are retained as private diagnostics so an operator can
	// review and register them later. They never become canonical or visible to
	// analytics, and submission remains useful for the Server-owned tags.
	return "", model.ContentTagDiagnostic, strings.TrimSpace(tag.Value), nil
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
