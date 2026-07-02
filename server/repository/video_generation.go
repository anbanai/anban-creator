package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
)

type videoGenerationRepository struct {
	db *gorm.DB
}

func newVideoGenerationRepository(db *gorm.DB) VideoGenerationRepository {
	return &videoGenerationRepository{db: db}
}

func (r *videoGenerationRepository) Create(ctx context.Context, gen *model.VideoGeneration) error {
	if gen.ID == "" {
		gen.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(gen).Error
}

func (r *videoGenerationRepository) FindByID(ctx context.Context, id string) (*model.VideoGeneration, error) {
	var gen model.VideoGeneration
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&gen).Error; err != nil {
		return nil, err
	}
	return &gen, nil
}

func (r *videoGenerationRepository) FindByArkTaskID(ctx context.Context, arkTaskID string) (*model.VideoGeneration, error) {
	var gen model.VideoGeneration
	if err := r.db.WithContext(ctx).Where("ark_task_id = ?", arkTaskID).First(&gen).Error; err != nil {
		return nil, err
	}
	return &gen, nil
}

func (r *videoGenerationRepository) FindLatestByTaskID(ctx context.Context, taskID string) (*model.VideoGeneration, error) {
	var gen model.VideoGeneration
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("created_at DESC").First(&gen).Error; err != nil {
		return nil, err
	}
	return &gen, nil
}

func (r *videoGenerationRepository) Update(ctx context.Context, gen *model.VideoGeneration) error {
	return r.db.WithContext(ctx).Save(gen).Error
}
