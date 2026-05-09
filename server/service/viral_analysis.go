package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"

	"gorm.io/gorm"
)

// ViralAnalysisService handles viral content analysis business logic.
type ViralAnalysisService struct {
	repo   repository.Repository
	logger *zerolog.Logger
	// These will be set later when AI integration is added.
}

// NewViralAnalysisService creates a new ViralAnalysisService.
func NewViralAnalysisService(repo repository.Repository, logger *zerolog.Logger) *ViralAnalysisService {
	return &ViralAnalysisService{repo: repo, logger: logger}
}

// Create creates a new viral analysis record with pending status.
func (s *ViralAnalysisService) Create(ctx context.Context, userID, sourceType, sourceURL string) (*model.ViralAnalysis, error) {
	analysis := &model.ViralAnalysis{
		ID:         uuid.New().String(),
		UserID:     userID,
		SourceType: sourceType,
		SourceURL:  sourceURL,
		Status:     "pending",
	}

	if err := s.repo.ViralAnalyses().Create(ctx, analysis); err != nil {
		return nil, fmt.Errorf("create viral analysis: %w", err)
	}

	s.logger.Info().
		Str("analysis_id", analysis.ID).
		Str("user_id", userID).
		Str("source_type", sourceType).
		Msg("viral analysis created")

	return analysis, nil
}

// GetByID returns a viral analysis by ID, verifying ownership.
func (s *ViralAnalysisService) GetByID(ctx context.Context, id, userID string) (*model.ViralAnalysis, error) {
	analysis, err := s.repo.ViralAnalyses().FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("viral analysis not found: %s", id)
		}
		return nil, fmt.Errorf("find viral analysis by id: %w", err)
	}

	if analysis.UserID != userID {
		return nil, fmt.Errorf("viral analysis does not belong to user: %s", id)
	}

	return analysis, nil
}

// ListByUserID returns paginated viral analyses for a user.
func (s *ViralAnalysisService) ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.ViralAnalysis, int64, error) {
	analyses, err := s.repo.ViralAnalyses().FindByUserID(ctx, userID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list viral analyses: %w", err)
	}

	total, err := s.repo.ViralAnalyses().CountByUserID(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("count viral analyses: %w", err)
	}

	return analyses, total, nil
}

// StartAnalysis sets the viral analysis status to "analyzing".
func (s *ViralAnalysisService) StartAnalysis(ctx context.Context, id string) error {
	if err := s.repo.ViralAnalyses().UpdateStatus(ctx, id, "analyzing"); err != nil {
		return fmt.Errorf("start analysis: %w", err)
	}

	s.logger.Info().Str("analysis_id", id).Msg("viral analysis started")
	return nil
}

// CompleteAnalysis stores the analysis result and sets status to "completed".
func (s *ViralAnalysisService) CompleteAnalysis(ctx context.Context, id string, result json.RawMessage) error {
	if err := s.repo.ViralAnalyses().UpdateResult(ctx, id, result); err != nil {
		return fmt.Errorf("update analysis result: %w", err)
	}
	if err := s.repo.ViralAnalyses().UpdateStatus(ctx, id, "completed"); err != nil {
		return fmt.Errorf("complete analysis: %w", err)
	}

	s.logger.Info().Str("analysis_id", id).Msg("viral analysis completed")
	return nil
}

// FailAnalysis sets the viral analysis status to "failed" with an error message.
func (s *ViralAnalysisService) FailAnalysis(ctx context.Context, id, errMsg string) error {
	if err := s.repo.ViralAnalyses().UpdateStatusAndError(ctx, id, "failed", errMsg); err != nil {
		return fmt.Errorf("fail analysis: %w", err)
	}

	s.logger.Warn().Str("analysis_id", id).Str("error", errMsg).Msg("viral analysis failed")
	return nil
}

// CleanupOldCompleted deletes viral analyses completed more than 90 days ago.
func (s *ViralAnalysisService) CleanupOldCompleted(ctx context.Context) error {
	analyses, err := s.repo.ViralAnalyses().FindCompletedOlderThan(ctx, time.Now().AddDate(0, 0, -90))
	if err != nil {
		return fmt.Errorf("find old viral analyses: %w", err)
	}
	for _, a := range analyses {
		s.logger.Info().Str("analysis_id", a.ID).Msg("cleaning up old viral analysis")
		if err := s.repo.ViralAnalyses().Delete(ctx, a.ID); err != nil {
			s.logger.Warn().Err(err).Str("analysis_id", a.ID).Msg("failed to delete old viral analysis")
		}
	}
	return nil
}
