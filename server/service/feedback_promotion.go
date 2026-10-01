package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var (
	ErrFeedbackInsightNotFound       = errors.New("feedback insight not found")
	ErrFeedbackInsightNotConfirmable = errors.New("feedback insight is not confirmable")
	ErrFeedbackInsightNotValidated   = errors.New("feedback insight has not been independently validated")
)

type FeedbackPromotionService struct {
	repo   repository.Repository
	memory ProjectMemoryWriter
}

type ProjectMemoryWriter interface {
	WriteMarkdownFile(context.Context, string, string, string) error
}

type ProjectMemoryReader interface {
	ReadProject(context.Context, string) (memory.ProjectView, error)
}

func NewFeedbackPromotionService(repo repository.Repository, memory ProjectMemoryWriter) *FeedbackPromotionService {
	return &FeedbackPromotionService{repo: repo, memory: memory}
}

func (s *FeedbackPromotionService) Confirm(ctx context.Context, insightID, userID string) (*model.FeedbackInsight, error) {
	insight, job, err := s.loadInsight(ctx, insightID)
	if err != nil {
		return nil, err
	}
	if userID != "" && job.UserID != userID {
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
	if userID != "" && job.UserID != userID {
		return nil, ErrFeedbackInsightNotFound
	}
	if insight.PromotionStatus != model.FeedbackPromotionConfirmed {
		return nil, ErrFeedbackInsightNotValidated
	}
	insights, err := s.repo.FeedbackLoop().ListInsightsByTarget(ctx, insight.ProjectID, insight.TargetContentID)
	if err != nil {
		return nil, err
	}
	independent := false
	for _, other := range insights {
		if other != nil && other.ID != insight.ID && other.TargetContentID == insight.TargetContentID && other.Kind == "content_postmortem" && other.AnalyticsRevision > insight.AnalyticsRevision && other.PromotionStatus != model.FeedbackPromotionRejected {
			otherJob, jobErr := s.repo.FeedbackLoop().FindJobByIDOrFingerprint(ctx, other.JobID)
			if jobErr != nil || otherJob == nil || otherJob.Status != model.FeedbackJobSucceeded || other.ExecutionID == insight.ExecutionID {
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
	insight.PromotionStatus = model.FeedbackPromotionValidated
	insight.ValidatedAt = &now
	if err := s.repo.FeedbackLoop().UpdateInsight(ctx, insight); err != nil {
		return nil, err
	}
	return insight, nil
}

func (s *FeedbackPromotionService) Promote(ctx context.Context, insightID, userID string) (*model.FeedbackInsight, error) {
	insight, job, err := s.loadInsight(ctx, insightID)
	if err != nil {
		return nil, err
	}
	if userID != "" && job.UserID != userID {
		return nil, ErrFeedbackInsightNotFound
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
	content := fmt.Sprintf("%s\n- Content: %s\n- Insight: %s\n- Evidence revision: %d\n- Confidence: %s\n- Limitation: %s\n", marker, insight.TargetContentID, insight.Summary, insight.AnalyticsRevision, insight.Confidence, insight.Limitations)
	if reader, ok := s.memory.(ProjectMemoryReader); ok {
		if view, readErr := reader.ReadProject(ctx, insight.ProjectID); readErr == nil {
			for _, file := range view.Files {
				if file.Path == ".claude/agent-memory/anban-feedback/MEMORY.md" {
					if strings.Contains(file.Content, marker) {
						content = file.Content
						break
					}
					content = strings.TrimSpace(file.Content) + "\n\n" + content
					break
				}
			}
		}
	}
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "# Anban Feedback Memory") {
		content = "# Anban Feedback Memory\n\n" + content
	}
	content += "\n"
	if err := s.memory.WriteMarkdownFile(ctx, insight.ProjectID, ".claude/agent-memory/anban-feedback/MEMORY.md", content); err != nil {
		return nil, err
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
	job, err := s.repo.FeedbackLoop().FindJobByIDOrFingerprint(ctx, insight.JobID)
	if err != nil || job == nil {
		return nil, nil, ErrFeedbackInsightNotFound
	}
	return insight, job, nil
}
