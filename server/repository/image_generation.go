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

func (r *imageGenerationRepository) MarkFailed(ctx context.Context, id, message string, completedAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&model.ImageGeneration{}).Where("id = ?", id).Updates(map[string]any{
		"status": model.ImageGenerationStatusFailed, "error": message, "completed_at": completedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
