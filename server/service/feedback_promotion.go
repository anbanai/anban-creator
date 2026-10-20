package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

var (
	ErrFeedbackInsightNotFound       = errors.New("feedback insight not found")
	ErrFeedbackInsightNotConfirmable = errors.New("feedback insight is not confirmable")
	ErrFeedbackInsightNotValidated   = errors.New("feedback insight has not been independently validated")
	ErrFeedbackMemoryCASUnavailable  = errors.New("feedback memory does not support complete-read CAS")
)

type FeedbackPromotionService struct {
	repo          repository.Repository
	memory        ProjectMemoryWriter
	profileMemory ProjectProfileMemory
}

type ProjectMemoryWriter interface {
	ProjectMemoryReader
	AppendMarkdownFileIfUnchanged(context.Context, string, string, string, string) error
}

type ProjectMemoryReader interface {
	ReadProject(context.Context, string) (memory.ProjectView, error)
}

func NewFeedbackPromotionService(repo repository.Repository, memory ProjectMemoryWriter) *FeedbackPromotionService {
	return &FeedbackPromotionService{repo: repo, memory: memory}
}

func (s *FeedbackPromotionService) SetProjectProfileMemory(memory ProjectProfileMemory) {
	if s != nil {
		s.profileMemory = memory
	}
}

func (s *FeedbackPromotionService) Confirm(ctx context.Context, insightID, userID string) (*model.FeedbackInsight, error) {
	insight, job, err := s.loadInsight(ctx, insightID)
	if err != nil {
		return nil, err
	}
	if userID != "" && !feedbackInsightOwnedBy(insight, job, userID) {
		return nil, ErrFeedbackInsightNotFound
	}
	if insight.PromotionStatus != model.FeedbackPromotionCandidate || insight.TargetContentID == "" || insight.Kind != "content_postmortem" {
		return nil, ErrFeedbackInsightNotConfirmable
	}
	now := time.Now().UTC()
	insight.PromotionStatus = model.FeedbackPromotionConfirmed
	insight.ConfirmedBy = userID
	insight.ConfirmedAt = &now
	if err := s.repo.FeedbackLoop().UpdateInsight(ctx, insight); err != nil {
		return nil, err
	}
	return insight, nil
}

func (s *FeedbackPromotionService) Validate(ctx context.Context, insightID, userID string) (*model.FeedbackInsight, error) {
	insight, job, err := s.loadInsight(ctx, insightID)
	if err != nil {
		return nil, err
	}
	if userID != "" && !feedbackInsightOwnedBy(insight, job, userID) {
		return nil, ErrFeedbackInsightNotFound
	}
	if insight.PromotionStatus != model.FeedbackPromotionConfirmed && insight.PromotionStatus != model.FeedbackPromotionValidated {
		return nil, ErrFeedbackInsightNotValidated
	}
	if insight.PromotionStatus == model.FeedbackPromotionConfirmed {
		insights, err := s.repo.FeedbackLoop().ListInsightsByTarget(ctx, insight.ProjectID, insight.TargetContentID)
		if err != nil {
			return nil, err
		}
		analytics, err := s.repo.Analytics().State(ctx, insight.ProjectID)
		if err != nil {
			return nil, err
		}
		independent := false
		for _, other := range insights {
			if other != nil && other.ID != insight.ID && other.TargetContentID == insight.TargetContentID && other.Kind == "content_postmortem" && other.AnalyticsRevision > insight.AnalyticsRevision && other.PromotionStatus != model.FeedbackPromotionRejected {
				otherJob, jobErr := s.repo.FeedbackLoop().FindJobByIDOrFingerprint(ctx, other.JobID)
				if jobErr != nil || otherJob == nil || otherJob.Status != model.FeedbackJobSucceeded ||
					otherJob.ProjectID != insight.ProjectID || otherJob.TargetContentID != insight.TargetContentID ||
					otherJob.AnalyticsRevision != other.AnalyticsRevision || analytics.Revision < other.AnalyticsRevision ||
					other.ExecutionID == "" || otherJob.ExecutionID != other.ExecutionID || other.ExecutionID == insight.ExecutionID {
					continue
				}
				independent = true
				break
			}
		}
		if !independent {
			return nil, ErrFeedbackInsightNotValidated
		}
		now := time.Now().UTC()
		if insight.SourceSessionID != "" {
			if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
				if err := s.applyProfileRecommendationsTx(ctx, tx, insight, nil); err != nil {
					return err
				}
				insight.PromotionStatus = model.FeedbackPromotionPromoted
				insight.ValidatedAt = &now
				return tx.FeedbackLoop().UpdateInsight(ctx, insight)
			}); err != nil {
				return nil, err
			}
			if project, err := s.repo.Projects().FindByID(ctx, insight.ProjectID); err == nil {
				s.reconcilePromotedProfile(ctx, insight.ProjectID, projectProfileFor(project))
			}
			return insight, nil
		}
		insight.PromotionStatus = model.FeedbackPromotionValidated
		insight.ValidatedAt = &now
		if err := s.repo.FeedbackLoop().UpdateInsight(ctx, insight); err != nil {
			return nil, err
		}
	}
	if insight.Trigger != FeedbackTriggerAutoAttribution {
		return insight, nil
	}
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := s.applyProfileRecommendationsTx(ctx, tx, insight, job); err != nil {
			return err
		}
		insight.PromotionStatus = model.FeedbackPromotionPromoted
		return tx.FeedbackLoop().UpdateInsight(ctx, insight)
	}); err != nil {
		return nil, err
	}
	if s.profileMemory != nil {
		var profile model.ProjectProfile
		if project, err := s.repo.Projects().FindByID(ctx, insight.ProjectID); err == nil {
			profile = projectProfileFor(project)
			s.reconcilePromotedProfile(ctx, insight.ProjectID, profile)
		}
	}
	return insight, nil
}

func (s *FeedbackPromotionService) applyProfileRecommendationsTx(ctx context.Context, tx repository.Repository, insight *model.FeedbackInsight, job *model.FeedbackJob) error {
	if insight == nil {
		return nil
	}
	expectedProfileRevision := insight.ProfileRevision
	sourceTaskID := ""
	sourceLabel := "[链接分析]"
	if insight.SourceSessionID != "" {
		if insight.Trigger != assistantReviewTrigger {
			return ErrFeedbackInsightNotValidated
		}
		sourceLabel = "[复盘模式]"
	} else {
		if insight.Trigger != FeedbackTriggerAutoAttribution || job == nil {
			return nil
		}
		expectedProfileRevision = job.ProfileRevision
		sourceTaskID = job.TaskID
	}
	project, err := tx.Projects().FindByIDForUpdate(ctx, insight.ProjectID)
	if err != nil {
		return err
	}
	profile := projectProfileFor(project)
	if profile.Version != expectedProfileRevision {
		return ErrProjectProfileVersionConflict
	}
	var envelope struct {
		Analysis json.RawMessage `json:"analysis"`
	}
	if err := json.Unmarshal([]byte(insight.EvidenceJSON), &envelope); err != nil {
		return err
	}
	analysisJSON := envelope.Analysis
	if len(analysisJSON) == 0 {
		analysisJSON = []byte(insight.EvidenceJSON)
	}
	var analysis feedbackAnalysisArtifact
	if err := json.Unmarshal(analysisJSON, &analysis); err != nil {
		return err
	}
	if err := validateFeedbackDimensionRecommendations(analysis.DimensionRecommendations); err != nil {
		return err
	}
	for name, recommendation := range analysis.DimensionRecommendations {
		dimension := getProfileDimension(profile, name)
		if dimension.Content == nil {
			dimension.Content = map[string]any{}
		}
		for key, value := range recommendation.Updates {
			dimension.Content[key] = value
		}
		dimension.Sources = appendUniqueProfileValue(dimension.Sources, sourceLabel)
		dimension.Evidence = appendUniqueProfileValue(dimension.Evidence, fmt.Sprintf("feedback insight %s, analytics revision %d: %s", insight.ID, insight.AnalyticsRevision, strings.Join(recommendation.Evidence, "; ")))
		setProfileDimension(&profile, name, dimension)
	}
	profile.Status = model.ProfileStatusConfirmed
	profile.InitializationStatus = model.ProfileInitializationReady
	profile.ApplicationSource = model.ProfileApplicationSourceSystemAnalysis
	profile.Version++
	if err := profile.Validate(); err != nil {
		return err
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	project.Profile = datatypes.NewJSONType(profile)
	if err := tx.Projects().Update(ctx, project); err != nil {
		return err
	}
	dimensions, err := json.Marshal(profile.Dimensions)
	if err != nil {
		return err
	}
	if err := tx.ProjectProfileRevisions().Create(ctx, &model.ProjectProfileRevision{ID: uuid.NewString(), ProjectID: insight.ProjectID, Revision: profile.Version, SixDimensions: dimensions, ProfileSnapshot: encoded, SourceTaskID: sourceTaskID}); err != nil {
		return err
	}
	return persistProfileState(ctx, tx, insight.ProjectID, profile)
}

func (s *FeedbackPromotionService) reconcilePromotedProfile(ctx context.Context, projectID string, profile model.ProjectProfile) {
	if s.profileMemory == nil {
		return
	}
	for _, name := range model.ProfileDimensions() {
		content, err := ProfileDimensionMarkdownAtVersion(name, getProfileDimension(profile, name), profile.Version)
		if err == nil {
			_ = s.profileMemory.WriteMarkdownFile(ctx, projectID, "profile/"+name+".md", content)
		}
	}
}

func getProfileDimension(profile model.ProjectProfile, name string) model.ProfileDimension {
	switch name {
	case "identity":
		return profile.Dimensions.Identity
	case "style":
		return profile.Dimensions.Style
	case "audience":
		return profile.Dimensions.Audience
	case "platforms":
		return profile.Dimensions.Platforms
	case "preferences":
		return profile.Dimensions.Preferences
	case "memory":
		return profile.Dimensions.Memory
	default:
		return model.ProfileDimension{Content: map[string]any{}, Sources: []string{}, Evidence: []string{}, MissingFields: []string{}}
	}
}

func setProfileDimension(profile *model.ProjectProfile, name string, value model.ProfileDimension) {
	switch name {
	case "identity":
		profile.Dimensions.Identity = value
	case "style":
		profile.Dimensions.Style = value
	case "audience":
		profile.Dimensions.Audience = value
	case "platforms":
		profile.Dimensions.Platforms = value
	case "preferences":
		profile.Dimensions.Preferences = value
	case "memory":
		profile.Dimensions.Memory = value
	}
}

func appendUniqueProfileValue(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func (s *FeedbackPromotionService) Promote(ctx context.Context, insightID, userID string) (*model.FeedbackInsight, error) {
	insight, job, err := s.loadInsight(ctx, insightID)
	if err != nil {
		return nil, err
	}
	if userID != "" && !feedbackInsightOwnedBy(insight, job, userID) {
		return nil, ErrFeedbackInsightNotFound
	}
	if insight.SourceSessionID != "" {
		if insight.Trigger != assistantReviewTrigger {
			return nil, ErrFeedbackInsightNotValidated
		}
		if insight.PromotionStatus == model.FeedbackPromotionPromoted {
			return insight, nil
		}
		return nil, ErrFeedbackInsightNotValidated
	}
	if insight.PromotionStatus == model.FeedbackPromotionPromoted {
		return insight, nil
	}
	if insight.PromotionStatus != model.FeedbackPromotionValidated {
		return nil, ErrFeedbackInsightNotValidated
	}
	if s.memory == nil {
		return nil, errors.New("project memory writer is unavailable")
	}
	marker := "<!-- feedback-insight:" + insight.ID + " -->"
	entry := fmt.Sprintf("%s\n- Content: %s\n- Insight: %s\n- Evidence revision: %d\n- Confidence: %s\n- Limitation: %s\n", marker, insight.TargetContentID, insight.Summary, insight.AnalyticsRevision, insight.Confidence, insight.Limitations)
	reader := s.memory
	view, readErr := reader.ReadProject(ctx, insight.ProjectID)
	if readErr != nil && !errors.Is(readErr, memory.ErrProjectMemoryMissing) {
		return nil, readErr
	}
	expected := ""
	content := entry
	alreadyMaterialized := false
	fileFound := false
	for _, file := range view.Files {
		if file.Path != ".claude/agent-memory/anban-feedback/MEMORY.md" {
			continue
		}
		fileFound = true
		// A bounded preview is not a valid CAS base. Refuse to append
		// against a partial snapshot rather than risking a lost update.
		if file.Truncated {
			return nil, memory.ErrProjectMemoryConflict
		}
		if strings.Contains(file.Content, marker) {
			alreadyMaterialized = true
		}
		expected = file.Content
		break
	}
	// A partial project view that omitted the target file cannot prove that
	// the file is absent. Treat it as a conflict instead of creating a new
	// file from an incomplete snapshot.
	if !fileFound && view.Partial {
		return nil, memory.ErrProjectMemoryConflict
	}
	if !alreadyMaterialized && expected == "" {
		content = "# Anban Feedback Memory\n\n" + entry
	} else if !alreadyMaterialized {
		content = strings.TrimRight(expected, "\n") + "\n\n" + entry
	}
	content += "\n"
	if alreadyMaterialized {
		// The durable insight row may have lost a race after the NAS write. The
		// marker makes this retry idempotent; only the DB promotion CAS remains.
	} else {
		appendContent := content
		if expected != "" {
			appendContent = "\n\n" + entry + "\n"
		}
		if err := s.memory.AppendMarkdownFileIfUnchanged(ctx, insight.ProjectID, ".claude/agent-memory/anban-feedback/MEMORY.md", expected, appendContent); err != nil {
			return nil, err
		}
	}
	nextMemoryRevision := insight.MemoryRevision + 1
	if nextMemoryRevision == 0 {
		nextMemoryRevision = 1
	}
	won, err := s.repo.FeedbackLoop().PromoteInsight(ctx, insight.ID, nextMemoryRevision)
	if err != nil {
		return nil, err
	}
	if !won {
		latest, findErr := s.repo.FeedbackLoop().FindInsightByID(ctx, insight.ID)
		if findErr == nil && latest != nil && latest.PromotionStatus == model.FeedbackPromotionPromoted {
			return latest, nil
		}
		return nil, ErrFeedbackInsightNotValidated
	}
	insight.PromotionStatus = model.FeedbackPromotionPromoted
	insight.MemoryRevision = nextMemoryRevision
	return insight, nil
}

func (s *FeedbackPromotionService) loadInsight(ctx context.Context, insightID string) (*model.FeedbackInsight, *model.FeedbackJob, error) {
	if s == nil || s.repo == nil || strings.TrimSpace(insightID) == "" {
		return nil, nil, ErrFeedbackInsightNotFound
	}
	insight, err := s.repo.FeedbackLoop().FindInsightByID(ctx, insightID)
	if err != nil || insight == nil {
		return nil, nil, ErrFeedbackInsightNotFound
	}
	if insight.SourceSessionID != "" {
		project, projectErr := s.repo.Projects().FindByID(ctx, insight.ProjectID)
		if projectErr != nil || project.UserID != insight.UserID || insight.UserID == "" {
			return nil, nil, ErrFeedbackInsightNotFound
		}
		return insight, nil, nil
	}
	job, err := s.repo.FeedbackLoop().FindJobByIDOrFingerprint(ctx, insight.JobID)
	if err != nil || job == nil {
		return nil, nil, ErrFeedbackInsightNotFound
	}
	return insight, job, nil
}

func feedbackInsightOwnedBy(insight *model.FeedbackInsight, job *model.FeedbackJob, userID string) bool {
	if userID == "" {
		return true
	}
	if insight == nil {
		return false
	}
	if insight.SourceSessionID != "" {
		return insight.UserID == userID
	}
	return job != nil && job.UserID == userID
}
