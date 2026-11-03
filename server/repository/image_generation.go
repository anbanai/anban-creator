package repository

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

type imageGenerationRepository struct{ db *gorm.DB }

func newImageGenerationRepository(db *gorm.DB) ImageGenerationRepository {
	return &imageGenerationRepository{db: db}
}

func (r *imageGenerationRepository) Create(ctx context.Context, generation *model.ImageGeneration) error {
	return r.db.WithContext(ctx).Create(generation).Error
}

func (r *imageGenerationRepository) FindByID(ctx context.Context, id string) (*model.ImageGeneration, error) {
	var generation model.ImageGeneration
	if err := r.db.WithContext(ctx).First(&generation, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &generation, nil
}

func (r *imageGenerationRepository) Claim(ctx context.Context, id string, startedAt, deadline time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.ImageGeneration{}).
		Where("id = ? AND status = ? AND started_at IS NULL", id, model.ImageGenerationStatusGenerating).
		Updates(map[string]any{"started_at": startedAt, "execution_deadline": deadline})
	return result.RowsAffected == 1, result.Error
}

func (r *imageGenerationRepository) MarkFailed(ctx context.Context, id, message string, completedAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.ImageGeneration{}).Where("id = ? AND status = ?", id, model.ImageGenerationStatusGenerating).Updates(map[string]any{
		"status": model.ImageGenerationStatusFailed, "error": message, "completed_at": completedAt,
	})
	return result.RowsAffected == 1, result.Error
}

func (r *imageGenerationRepository) Complete(ctx context.Context, id, revisedPrompt string, results []model.ImageGenerationResult, completedAt time.Time) (bool, error) {
	updated := r.db.WithContext(ctx).Model(&model.ImageGeneration{}).
		Where("id = ? AND status = ? AND started_at IS NOT NULL AND execution_deadline > ?", id, model.ImageGenerationStatusGenerating, completedAt).
		Updates(map[string]any{"status": model.ImageGenerationStatusCompleted, "revised_prompt": revisedPrompt, "completed_at": completedAt})
	if updated.Error != nil || updated.RowsAffected == 0 {
		return false, updated.Error
	}
	if len(results) == 0 {
		return false, gorm.ErrInvalidData
	}
	if err := r.db.WithContext(ctx).Create(&results).Error; err != nil {
		return false, err
	}
	return true, nil
}

func (r *imageGenerationRepository) ListGenerating(ctx context.Context, limit int) ([]model.ImageGeneration, error) {
	var generations []model.ImageGeneration
	err := r.db.WithContext(ctx).Where("status = ?", model.ImageGenerationStatusGenerating).Order("created_at ASC").Limit(limit).Find(&generations).Error
	return generations, err
}
