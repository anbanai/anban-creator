package service

import (
	"context"
	"fmt"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// ViralAnalysisHistoryService provides read-only access to legacy viral analysis records.
type ViralAnalysisHistoryService struct {
	repo repository.Repository
}

func NewViralAnalysisHistoryService(repo repository.Repository) *ViralAnalysisHistoryService {
	return &ViralAnalysisHistoryService{repo: repo}
}

// GetByID returns a historical viral analysis after verifying ownership.
func (s *ViralAnalysisHistoryService) GetByID(ctx context.Context, id, userID string) (*model.ViralAnalysis, error) {
	analysis, err := s.repo.ViralAnalyses().FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("viral analysis not found: %s", id)
	}
	if analysis.UserID != userID {
		return nil, fmt.Errorf("viral analysis does not belong to user: %s", id)
	}
	return analysis, nil
}

// ListByUserID returns paginated historical viral analyses for a user.
func (s *ViralAnalysisHistoryService) ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.ViralAnalysis, int64, error) {
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
