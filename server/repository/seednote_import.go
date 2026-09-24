package repository

import (
	"context"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

type seednoteImportRepository struct{ db *gorm.DB }

func newSeednoteImportRepository(db *gorm.DB) SeednoteImportRepository {
	return &seednoteImportRepository{db: db}
}
func (r *seednoteImportRepository) CreateBatch(ctx context.Context, batch *model.SeednoteImportBatch) error {
	return r.db.WithContext(ctx).Create(batch).Error
}
func (r *seednoteImportRepository) FindBatchByID(ctx context.Context, projectID, id string) (*model.SeednoteImportBatch, error) {
	var batch model.SeednoteImportBatch
	err := r.db.WithContext(ctx).Where("project_id = ? AND id = ?", projectID, id).First(&batch).Error
	return &batch, err
}
func (r *seednoteImportRepository) ListBatches(ctx context.Context, projectID string, offset, limit int) ([]*model.SeednoteImportBatch, int64, error) {
	q := r.db.WithContext(ctx).Where("project_id = ?", projectID)
	var total int64
	if err := q.Model(&model.SeednoteImportBatch{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var batches []*model.SeednoteImportBatch
	if err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&batches).Error; err != nil {
		return nil, 0, err
	}
	return batches, total, nil
}
func (r *seednoteImportRepository) UpdateBatch(ctx context.Context, batch *model.SeednoteImportBatch) error {
	return r.db.WithContext(ctx).Save(batch).Error
}
func (r *seednoteImportRepository) CreateRows(ctx context.Context, rows []*model.SeednoteImportRow) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&rows).Error
}
func (r *seednoteImportRepository) FindRowsByBatchID(ctx context.Context, projectID, batchID string) ([]*model.SeednoteImportRow, error) {
	var rows []*model.SeednoteImportRow
	err := r.db.WithContext(ctx).Where("project_id = ? AND batch_id = ?", projectID, batchID).Order("source_row ASC").Find(&rows).Error
	return rows, err
}
func (r *seednoteImportRepository) FindRowByID(ctx context.Context, projectID, batchID, rowID string) (*model.SeednoteImportRow, error) {
	var row model.SeednoteImportRow
	err := r.db.WithContext(ctx).Where("project_id = ? AND batch_id = ? AND id = ?", projectID, batchID, rowID).First(&row).Error
	return &row, err
}
func (r *seednoteImportRepository) UpdateRow(ctx context.Context, row *model.SeednoteImportRow) error {
	return r.db.WithContext(ctx).Save(row).Error
}

type seednotePostRepository struct{ db *gorm.DB }

func newSeednotePostRepository(db *gorm.DB) SeednotePostRepository {
	return &seednotePostRepository{db: db}
}
func (r *seednotePostRepository) Create(ctx context.Context, post *model.SeednotePost) error {
	return r.db.WithContext(ctx).Create(post).Error
}
func (r *seednotePostRepository) FindByID(ctx context.Context, projectID, id string) (*model.SeednotePost, error) {
	var post model.SeednotePost
	err := r.db.WithContext(ctx).Where("project_id = ? AND id = ?", projectID, id).First(&post).Error
	return &post, err
}
func (r *seednotePostRepository) ListByProject(ctx context.Context, projectID, search string, offset, limit int) ([]*model.SeednotePost, int64, error) {
	q := r.db.WithContext(ctx).Where("project_id = ?", projectID)
	if strings.TrimSpace(search) != "" {
		q = q.Where("title LIKE ?", "%"+strings.TrimSpace(search)+"%")
	}
	var total int64
	if err := q.Model(&model.SeednotePost{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var posts []*model.SeednotePost
	if err := q.Order("first_published_at DESC, created_at DESC").Offset(offset).Limit(limit).Find(&posts).Error; err != nil {
		return nil, 0, err
	}
	return posts, total, nil
}
func (r *seednotePostRepository) FindBySignature(ctx context.Context, projectID, normalizedTitle string, publishedAt *time.Time) ([]*model.SeednotePost, error) {
	q := r.db.WithContext(ctx).Where("project_id = ? AND normalized_title = ?", projectID, normalizedTitle)
	if publishedAt != nil {
		q = q.Where("first_published_at = ? OR first_published_at IS NULL", *publishedAt)
	}
	var posts []*model.SeednotePost
	err := q.Find(&posts).Error
	return posts, err
}
func (r *seednotePostRepository) Update(ctx context.Context, post *model.SeednotePost) error {
	return r.db.WithContext(ctx).Save(post).Error
}

type seednotePostAliasRepository struct{ db *gorm.DB }

func newSeednotePostAliasRepository(db *gorm.DB) SeednotePostAliasRepository {
	return &seednotePostAliasRepository{db: db}
}
func (r *seednotePostAliasRepository) Create(ctx context.Context, alias *model.SeednotePostAlias) error {
	return r.db.WithContext(ctx).Create(alias).Error
}
func (r *seednotePostAliasRepository) FindBySignature(ctx context.Context, normalizedTitle string, publishedAt *time.Time) ([]*model.SeednotePostAlias, error) {
	q := r.db.WithContext(ctx).Where("normalized_title = ?", normalizedTitle)
	// Only active imports may teach future matching; revoked or unproven
	// associations remain historical records, not identity evidence.
	q = q.Where("batch_id IN (?)", r.db.Model(&model.SeednoteImportBatch{}).Select("id").Where("revoked_at IS NULL"))
	if publishedAt != nil {
		q = q.Where("first_published_at = ? OR first_published_at IS NULL", *publishedAt)
	}
	var aliases []*model.SeednotePostAlias
	err := q.Find(&aliases).Error
	return aliases, err
}

type seednoteMetricVersionRepository struct{ db *gorm.DB }

func newSeednoteMetricVersionRepository(db *gorm.DB) SeednoteMetricVersionRepository {
	return &seednoteMetricVersionRepository{db: db}
}
func (r *seednoteMetricVersionRepository) Create(ctx context.Context, version *model.SeednoteMetricVersion) error {
	return r.db.WithContext(ctx).Create(version).Error
}
func (r *seednoteMetricVersionRepository) UpdatePostID(ctx context.Context, id, postID string) error {
	return r.db.WithContext(ctx).Model(&model.SeednoteMetricVersion{}).Where("id = ?", id).Update("post_id", postID).Error
}
func (r *seednoteMetricVersionRepository) FindByPostID(ctx context.Context, postID string, from, to *time.Time) ([]*model.SeednoteMetricVersion, error) {
	q := r.db.WithContext(ctx).Where("post_id = ?", postID).Where("batch_id NOT IN (?)", r.db.Model(&model.SeednoteImportBatch{}).Select("id").Where("revoked_at IS NOT NULL"))
	if from != nil {
		q = q.Where("data_as_of_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("data_as_of_at < ?", *to)
	}
	var versions []*model.SeednoteMetricVersion
	err := q.Order("data_as_of_at ASC, imported_at ASC, id ASC").Find(&versions).Error
	return versions, err
}
func (r *seednoteMetricVersionRepository) FindByProject(ctx context.Context, projectID string, from, to *time.Time) ([]*model.SeednoteMetricVersion, error) {
	q := r.db.WithContext(ctx).Table("seednote_metric_versions AS v").Joins("JOIN seednote_posts AS p ON p.id = v.post_id").Where("p.project_id = ?", projectID).Where("v.batch_id NOT IN (?)", r.db.Model(&model.SeednoteImportBatch{}).Select("id").Where("revoked_at IS NOT NULL"))
	if from != nil {
		q = q.Where("v.data_as_of_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("v.data_as_of_at < ?", *to)
	}
	var versions []*model.SeednoteMetricVersion
	err := q.Select("v.*").Order("v.data_as_of_at ASC, v.imported_at ASC, v.id ASC").Scan(&versions).Error
	return versions, err
}
func (r *seednoteMetricVersionRepository) FindByImportRowID(ctx context.Context, rowID string) (*model.SeednoteMetricVersion, error) {
	var version model.SeednoteMetricVersion
	err := r.db.WithContext(ctx).Where("import_row_id = ?", rowID).Order("imported_at DESC").First(&version).Error
	return &version, err
}

// LockBatch serializes resolution and revocation. A no-op write also acquires
// the write lock in SQLite, where SELECT FOR UPDATE is unsupported.
func (r *seednoteImportRepository) LockBatch(ctx context.Context, projectID, id string) error {
	return r.db.WithContext(ctx).Model(&model.SeednoteImportBatch{}).Where("project_id = ? AND id = ?", projectID, id).UpdateColumn("updated_at", gorm.Expr("updated_at")).Error
}
