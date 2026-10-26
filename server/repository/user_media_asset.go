package repository

import (
	"context"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

type UserMediaAssetListFilter struct {
	UserID         string
	Query          string
	MediaType      string
	ProjectID      string
	Status         string
	IncludeRemoved bool
	Offset         int
	Limit          int
}

type UserMediaAssetRepository interface {
	Create(context.Context, *model.UserMediaAsset) error
	FindByIDForUser(context.Context, string, string, bool) (*model.UserMediaAsset, error)
	FindByAssetIDForUser(context.Context, string, string, bool) (*model.UserMediaAsset, error)
	Update(context.Context, *model.UserMediaAsset) error
	UpdateMetadata(context.Context, string, string, *string, *string) error
	MarkRemoved(context.Context, string, string, time.Time) error
	List(context.Context, UserMediaAssetListFilter) ([]*model.UserMediaAsset, int64, error)
	ListReadyCandidates(context.Context, string, string, string, string, int) ([]*model.UserMediaAsset, error)
}

type userMediaAssetRepository struct{ db *gorm.DB }

func newUserMediaAssetRepository(db *gorm.DB) UserMediaAssetRepository {
	return &userMediaAssetRepository{db: db}
}

func (r *userMediaAssetRepository) Create(ctx context.Context, row *model.UserMediaAsset) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *userMediaAssetRepository) FindByIDForUser(ctx context.Context, userID, id string, includeRemoved bool) (*model.UserMediaAsset, error) {
	q := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID)
	if !includeRemoved {
		q = q.Where("removed_at IS NULL")
	}
	var row model.UserMediaAsset
	if err := q.First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *userMediaAssetRepository) FindByAssetIDForUser(ctx context.Context, userID, assetID string, includeRemoved bool) (*model.UserMediaAsset, error) {
	q := r.db.WithContext(ctx).Where("asset_id = ? AND user_id = ?", assetID, userID)
	if !includeRemoved {
		q = q.Where("removed_at IS NULL")
	}
	var row model.UserMediaAsset
	if err := q.First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *userMediaAssetRepository) Update(ctx context.Context, row *model.UserMediaAsset) error {
	return r.db.WithContext(ctx).Save(row).Error
}

func (r *userMediaAssetRepository) UpdateMetadata(ctx context.Context, userID, id string, projectID *string, usageNotes *string) error {
	updates := map[string]any{"updated_at": time.Now().UTC()}
	if projectID != nil {
		updates["project_id"] = strings.TrimSpace(*projectID)
	}
	if usageNotes != nil {
		updates["usage_notes"] = strings.TrimSpace(*usageNotes)
	}
	if len(updates) == 1 {
		return nil
	}
	result := r.db.WithContext(ctx).Model(&model.UserMediaAsset{}).Where("id = ? AND user_id = ? AND removed_at IS NULL", id, userID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *userMediaAssetRepository) MarkRemoved(ctx context.Context, userID, id string, removedAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&model.UserMediaAsset{}).Where("id = ? AND user_id = ? AND removed_at IS NULL", id, userID).Updates(map[string]any{"removed_at": removedAt, "updated_at": removedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *userMediaAssetRepository) List(ctx context.Context, filter UserMediaAssetListFilter) ([]*model.UserMediaAsset, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.UserMediaAsset{}).Where("user_media_assets.user_id = ?", filter.UserID)
	if !filter.IncludeRemoved {
		q = q.Where("user_media_assets.removed_at IS NULL")
	}
	if filter.Query != "" {
		q = withUserMediaSearch(q, filter.Query)
	}
	if filter.MediaType != "" {
		q = q.Where("user_media_assets.media_type = ?", filter.MediaType)
	}
	if filter.ProjectID != "" {
		q = q.Where("user_media_assets.project_id = ?", filter.ProjectID)
	}
	if filter.Status != "" {
		q = q.Where("user_media_assets.status = ?", filter.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	var rows []*model.UserMediaAsset
	if err := q.Order("user_media_assets.updated_at DESC, user_media_assets.id DESC").Offset(maxInt(filter.Offset, 0)).Limit(filter.Limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *userMediaAssetRepository) ListReadyCandidates(ctx context.Context, userID, projectID, query, mediaType string, limit int) ([]*model.UserMediaAsset, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).Where("user_media_assets.user_id = ? AND user_media_assets.status = ? AND user_media_assets.removed_at IS NULL", userID, model.UserMediaAssetStatusAvailable)
		if query != "" {
			q = withUserMediaSearch(q, query)
		}
		if mediaType != "" {
			q = q.Where("user_media_assets.media_type = ?", mediaType)
		}
		return q
	}
	var preferred, fallback []*model.UserMediaAsset
	if projectID != "" {
		if err := base().Where("project_id = ?", projectID).Order("updated_at DESC, id DESC").Limit(limit).Find(&preferred).Error; err != nil {
			return nil, err
		}
	}
	remaining := limit - len(preferred)
	if remaining > 0 {
		q := base()
		if projectID != "" {
			q = q.Where("project_id IS NULL OR project_id <> ?", projectID)
		}
		if err := q.Order("updated_at DESC, id DESC").Limit(remaining).Find(&fallback).Error; err != nil {
			return nil, err
		}
	}
	return append(preferred, fallback...), nil
}

// withUserMediaSearch keeps the immutable asset table as the source of truth
// for file names while limiting the subquery to the current library row.
func withUserMediaSearch(q *gorm.DB, query string) *gorm.DB {
	like := "%" + query + "%"
	return q.Where("(user_media_assets.description LIKE ? OR user_media_assets.usage_notes LIKE ? OR EXISTS (SELECT 1 FROM assets WHERE assets.id = user_media_assets.asset_id AND assets.file_name LIKE ?))", like, like, like)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
