package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

type DesignerReferenceRepository interface {
	Create(ctx context.Context, reference *model.DesignerReference) error
	FindByIDAndUserID(ctx context.Context, id, userID string) (*model.DesignerReference, error)
}

type designerReferenceRepository struct {
	db *gorm.DB
}

func NewDesignerReferenceRepository(db *gorm.DB) DesignerReferenceRepository {
	return &designerReferenceRepository{db: db}
}

func (r *designerReferenceRepository) Create(ctx context.Context, reference *model.DesignerReference) error {
	return r.db.WithContext(ctx).Create(reference).Error
}

func (r *designerReferenceRepository) FindByIDAndUserID(ctx context.Context, id, userID string) (*model.DesignerReference, error) {
	var reference model.DesignerReference
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&reference).Error; err != nil {
		return nil, err
	}
	return &reference, nil
}
