package repository

import (
	"context"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
)

// TemplateRepository provides access to the templates table.
type TemplateRepository interface {
	FindByID(ctx context.Context, id string) (*model.Template, error)
	List(ctx context.Context, templateType string, category string, tag string, userID string, scope string, offset, limit int) ([]*model.Template, error)
	Count(ctx context.Context, templateType string, category string, tag string, userID string, scope string) (int64, error)
	ListByIDs(ctx context.Context, ids []string) ([]*model.Template, error)
	ListActive(ctx context.Context, templateType string) ([]*model.Template, error)
	Create(ctx context.Context, template *model.Template) error
	Update(ctx context.Context, template *model.Template) error
	Delete(ctx context.Context, id string) error
}

type templateRepository struct {
	db *gorm.DB
}

func newTemplateRepository(db *gorm.DB) TemplateRepository {
	return &templateRepository{db: db}
}

func (r *templateRepository) FindByID(ctx context.Context, id string) (*model.Template, error) {
	var template model.Template
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&template).Error; err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *templateRepository) List(ctx context.Context, templateType string, category string, tag string, userID string, scope string, offset, limit int) ([]*model.Template, error) {
	var templates []*model.Template
	q := r.db.WithContext(ctx).Where("is_active = ?", true)
	if templateType != "" {
		q = q.Where("type = ?", templateType)
	}
	if category != "" {
		q = q.Where("category = ?", category)
	}
	if tag != "" {
		q = q.Where("tags LIKE ?", "%"+tag+"%")
	}
	// Visibility scoping (only meaningful when userID is provided).
	//   scope=mine    → only templates owned by userID
	//   scope=public  → only publicly visible templates
	//   scope=all     → templates owned by userID OR publicly visible
	// Unauthenticated callers (userID empty) only ever see public templates.
	if userID == "" {
		q = q.Where("visibility = ?", "public")
	} else {
		switch scope {
		case "mine":
			q = q.Where("user_id = ?", userID)
		case "public":
			q = q.Where("visibility = ?", "public")
		default: // "all" or unspecified
			q = q.Where("user_id = ? OR visibility = ?", userID, "public")
		}
	}
	q = q.Order("sort_order DESC, created_at DESC")
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Find(&templates).Error; err != nil {
		return nil, err
	}
	return templates, nil
}

func (r *templateRepository) Count(ctx context.Context, templateType string, category string, tag string, userID string, scope string) (int64, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&model.Template{}).Where("is_active = ?", true)
	if templateType != "" {
		q = q.Where("type = ?", templateType)
	}
	if category != "" {
		q = q.Where("category = ?", category)
	}
	if tag != "" {
		q = q.Where("tags LIKE ?", "%"+tag+"%")
	}
	if userID == "" {
		q = q.Where("visibility = ?", "public")
	} else {
		switch scope {
		case "mine":
			q = q.Where("user_id = ?", userID)
		case "public":
			q = q.Where("visibility = ?", "public")
		default:
			q = q.Where("user_id = ? OR visibility = ?", userID, "public")
		}
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *templateRepository) ListByIDs(ctx context.Context, ids []string) ([]*model.Template, error) {
	var templates []*model.Template
	if err := r.db.WithContext(ctx).
		Where("id IN ? AND is_active = ?", ids, true).
		Find(&templates).Error; err != nil {
		return nil, err
	}
	return templates, nil
}

func (r *templateRepository) ListActive(ctx context.Context, templateType string) ([]*model.Template, error) {
	var templates []*model.Template
	q := r.db.WithContext(ctx).Where("is_active = ?", true)
	if templateType != "" {
		q = q.Where("type = ?", templateType)
	}
	if err := q.Order("sort_order DESC").Find(&templates).Error; err != nil {
		return nil, err
	}
	return templates, nil
}

func (r *templateRepository) Create(ctx context.Context, template *model.Template) error {
	return r.db.WithContext(ctx).Create(template).Error
}

func (r *templateRepository) Update(ctx context.Context, template *model.Template) error {
	return r.db.WithContext(ctx).Save(template).Error
}

func (r *templateRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Template{}).Error
}
