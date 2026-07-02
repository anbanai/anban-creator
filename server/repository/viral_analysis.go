package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
)

// ViralAnalysisRepository provides access to the viral_analyses table.
type ViralAnalysisRepository interface {
	Create(ctx context.Context, analysis *model.ViralAnalysis) error
	FindByID(ctx context.Context, id string) (*model.ViralAnalysis, error)
	FindByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.ViralAnalysis, error)
	CountByUserID(ctx context.Context, userID string) (int64, error)
	UpdateStatus(ctx context.Context, id, status string) error
	UpdateStatusAndError(ctx context.Context, id, status, errorMsg string) error
	UpdateResult(ctx context.Context, id string, result json.RawMessage) error
	UpdateSourceData(ctx context.Context, id string, data json.RawMessage) error
	FindCompletedOlderThan(ctx context.Context, before time.Time) ([]*model.ViralAnalysis, error)
	Delete(ctx context.Context, id string) error
	Update(ctx context.Context, analysis *model.ViralAnalysis) error
}

type viralAnalysisRepository struct {
	db *gorm.DB
}

func newViralAnalysisRepository(db *gorm.DB) ViralAnalysisRepository {
	return &viralAnalysisRepository{db: db}
}

func (r *viralAnalysisRepository) Create(ctx context.Context, analysis *model.ViralAnalysis) error {
	return r.db.WithContext(ctx).Create(analysis).Error
}

func (r *viralAnalysisRepository) FindByID(ctx context.Context, id string) (*model.ViralAnalysis, error) {
	var analysis model.ViralAnalysis
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&analysis).Error; err != nil {
		return nil, err
	}
	return &analysis, nil
}

func (r *viralAnalysisRepository) FindByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.ViralAnalysis, error) {
	var analyses []*model.ViralAnalysis
	q := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC")
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Find(&analyses).Error; err != nil {
		return nil, err
	}
	return analyses, nil
}

func (r *viralAnalysisRepository) CountByUserID(ctx context.Context, userID string) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.ViralAnalysis{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *viralAnalysisRepository) UpdateStatus(ctx context.Context, id, status string) error {
	return r.db.WithContext(ctx).Model(&model.ViralAnalysis{}).Where("id = ?", id).Update("status", status).Error
}

func (r *viralAnalysisRepository) UpdateStatusAndError(ctx context.Context, id, status, errorMsg string) error {
	return r.db.WithContext(ctx).Model(&model.ViralAnalysis{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":        status,
			"error_message": errorMsg,
		}).Error
}

func (r *viralAnalysisRepository) UpdateResult(ctx context.Context, id string, result json.RawMessage) error {
	return r.db.WithContext(ctx).Model(&model.ViralAnalysis{}).Where("id = ?", id).Update("analysis_result", result).Error
}

func (r *viralAnalysisRepository) UpdateSourceData(ctx context.Context, id string, data json.RawMessage) error {
	return r.db.WithContext(ctx).Model(&model.ViralAnalysis{}).Where("id = ?", id).Update("source_data", data).Error
}

func (r *viralAnalysisRepository) FindCompletedOlderThan(ctx context.Context, before time.Time) ([]*model.ViralAnalysis, error) {
	var analyses []*model.ViralAnalysis
	err := r.db.WithContext(ctx).
		Where("status = ? AND created_at < ?", "completed", before).
		Find(&analyses).Error
	if err != nil {
		return nil, err
	}
	return analyses, nil
}

func (r *viralAnalysisRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.ViralAnalysis{}).Error
}

func (r *viralAnalysisRepository) Update(ctx context.Context, analysis *model.ViralAnalysis) error {
	return r.db.WithContext(ctx).Save(analysis).Error
}
