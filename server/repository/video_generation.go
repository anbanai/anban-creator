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

func (r *videoGenerationRepository) CreateSegment(ctx context.Context, segment *model.VideoGenerationSegment) error {
	if segment.ID == "" {
		segment.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(segment).Error
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

func (r *videoGenerationRepository) FindSegmentByArkTaskID(ctx context.Context, arkTaskID string) (*model.VideoGenerationSegment, error) {
	var segment model.VideoGenerationSegment
	if err := r.db.WithContext(ctx).Where("ark_task_id = ?", arkTaskID).First(&segment).Error; err != nil {
		return nil, err
	}
	return &segment, nil
}

func (r *videoGenerationRepository) FindLatestByTaskID(ctx context.Context, taskID string) (*model.VideoGeneration, error) {
	var gen model.VideoGeneration
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("created_at DESC").First(&gen).Error; err != nil {
		return nil, err
	}
	return &gen, nil
}

func (r *videoGenerationRepository) ListSegments(ctx context.Context, generationID string) ([]*model.VideoGenerationSegment, error) {
	var segments []*model.VideoGenerationSegment
	if err := r.db.WithContext(ctx).Where("video_generation_id = ?", generationID).Order("`index` ASC").Find(&segments).Error; err != nil {
		return nil, err
	}
	return segments, nil
}

func (r *videoGenerationRepository) Update(ctx context.Context, gen *model.VideoGeneration) error {
	return r.db.WithContext(ctx).Save(gen).Error
}

func (r *videoGenerationRepository) UpdateSegment(ctx context.Context, segment *model.VideoGenerationSegment) error {
	return r.db.WithContext(ctx).Save(segment).Error
}
