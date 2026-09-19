package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TemplateRepository provides access to the templates table.
type TemplateRepository interface {
	FindByID(ctx context.Context, id string) (*model.Template, error)
	FindByIDForUpdate(ctx context.Context, id string) (*model.Template, error)
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

func (r *templateRepository) FindByIDForUpdate(ctx context.Context, id string) (*model.Template, error) {
	var template model.Template
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&template).Error; err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *templateRepository) List(ctx context.Context, templateType string, category string, tag string, userID string, scope string, offset, limit int) ([]*model.Template, error) {
	var templates []*model.Template
	q := r.db.WithContext(ctx)
	if templateType != "" {
		q = q.Where("type = ?", templateType)
	}
	if templateType == model.TemplateTypeSeednote {
		q = q.Where("category IN ?", model.SeednoteTemplateCategories)
	}
	if category != "" {
		q = q.Where("category = ?", category)
	}
	if tag != "" {
		q = q.Where("tags LIKE ?", "%"+tag+"%")
	}
	q = applyVisibilityScope(q, userID, scope)
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
	q := r.db.WithContext(ctx).Model(&model.Template{})
	if templateType != "" {
		q = q.Where("type = ?", templateType)
	}
	if templateType == model.TemplateTypeSeednote {
		q = q.Where("category IN ?", model.SeednoteTemplateCategories)
	}
	if category != "" {
		q = q.Where("category = ?", category)
	}
	if tag != "" {
		q = q.Where("tags LIKE ?", "%"+tag+"%")
	}
	q = applyVisibilityScope(q, userID, scope)
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// applyVisibilityScope receives a service-authorized scope. Empty user IDs are
// always reduced to the public pool as a final defense for internal callers.
func applyVisibilityScope(q *gorm.DB, userID, scope string) *gorm.DB {
	if userID == "" {
		return q.Where("is_active = ? AND readiness_status = ? AND visibility = ?", true, model.TemplateReadinessReady, "public")
	}
	switch scope {
	case "public":
		return q.Where("is_active = ? AND readiness_status = ? AND visibility = ?", true, model.TemplateReadinessReady, "public")
	case "private":
		return q.Where("is_active = ? AND readiness_status = ? AND visibility = ?", true, model.TemplateReadinessReady, "private")
	case "inactive":
		return q.Where("is_active = ?", false)
	case "all":
		return q
	default:
		return q.Where("1 = 0")
	}
}

func (r *templateRepository) ListByIDs(ctx context.Context, ids []string) ([]*model.Template, error) {
	var templates []*model.Template
	if err := r.db.WithContext(ctx).
		Where("id IN ? AND is_active = ? AND readiness_status = ? AND visibility = ?", ids, true, model.TemplateReadinessReady, "public").
		Find(&templates).Error; err != nil {
		return nil, err
	}
	return templates, nil
}

func (r *templateRepository) ListActive(ctx context.Context, templateType string) ([]*model.Template, error) {
	var templates []*model.Template
	q := r.db.WithContext(ctx).Where("is_active = ? AND readiness_status = ?", true, model.TemplateReadinessReady)
	if templateType != "" {
		q = q.Where("type = ?", templateType)
	}
	if err := q.Order("sort_order DESC").Find(&templates).Error; err != nil {
		return nil, err
	}
	return templates, nil
}

func (r *templateRepository) Create(ctx context.Context, template *model.Template) error {
	isActive := template.IsActive
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(template).Error; err != nil {
			return err
		}
		if isActive {
			return nil
		}
		if err := tx.Model(&model.Template{}).Where("id = ?", template.ID).UpdateColumn("is_active", false).Error; err != nil {
			return err
		}
		template.IsActive = false
		return nil
	})
}

func (r *templateRepository) Update(ctx context.Context, template *model.Template) error {
	return r.db.WithContext(ctx).Save(template).Error
}

func (r *templateRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Template{}).Error
}
