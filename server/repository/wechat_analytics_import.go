package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type wechatAnalyticsImportRepository struct{ db *gorm.DB }

func newWechatAnalyticsImportRepository(db *gorm.DB) WechatAnalyticsImportRepository {
	return &wechatAnalyticsImportRepository{db: db}
}

func (r *wechatAnalyticsImportRepository) CreateBatch(ctx context.Context, batch *model.WechatAnalyticsImportBatch) error {
	return r.db.WithContext(ctx).Create(batch).Error
}

func (r *wechatAnalyticsImportRepository) FindBatchByID(ctx context.Context, projectID, id string) (*model.WechatAnalyticsImportBatch, error) {
	var batch model.WechatAnalyticsImportBatch
	err := r.db.WithContext(ctx).Where("project_id = ? AND id = ?", projectID, id).First(&batch).Error
	return &batch, err
}

func (r *wechatAnalyticsImportRepository) FindBatchByIDAnyProject(ctx context.Context, id string) (*model.WechatAnalyticsImportBatch, error) {
	var batch model.WechatAnalyticsImportBatch
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&batch).Error
	return &batch, err
}

func (r *wechatAnalyticsImportRepository) ListBatches(ctx context.Context, projectID string, offset, limit int) ([]*model.WechatAnalyticsImportBatch, int64, error) {
	query := r.db.WithContext(ctx).Where("project_id = ?", projectID)
	var total int64
	if err := query.Model(&model.WechatAnalyticsImportBatch{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var batches []*model.WechatAnalyticsImportBatch
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&batches).Error; err != nil {
		return nil, 0, err
	}
	return batches, total, nil
}

func (r *wechatAnalyticsImportRepository) UpdateBatch(ctx context.Context, batch *model.WechatAnalyticsImportBatch) error {
	return r.db.WithContext(ctx).Save(batch).Error
}

func (r *wechatAnalyticsImportRepository) CreateRows(ctx context.Context, rows []*model.WechatAnalyticsImportRow) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&rows).Error
}

func (r *wechatAnalyticsImportRepository) FindRowsByBatchID(ctx context.Context, projectID, batchID string) ([]*model.WechatAnalyticsImportRow, error) {
	var rows []*model.WechatAnalyticsImportRow
	err := r.db.WithContext(ctx).Where("project_id = ? AND batch_id = ?", projectID, batchID).Order("source_row ASC").Find(&rows).Error
	return rows, err
}

func (r *wechatAnalyticsImportRepository) FindRowByID(ctx context.Context, projectID, batchID, rowID string) (*model.WechatAnalyticsImportRow, error) {
	var row model.WechatAnalyticsImportRow
	err := r.db.WithContext(ctx).Where("project_id = ? AND batch_id = ? AND id = ?", projectID, batchID, rowID).First(&row).Error
	return &row, err
}

func (r *wechatAnalyticsImportRepository) UpdateRow(ctx context.Context, row *model.WechatAnalyticsImportRow) error {
	return r.db.WithContext(ctx).Save(row).Error
}

func (r *wechatAnalyticsImportRepository) CreateSnapshot(ctx context.Context, snapshot *model.WechatAnalyticsSnapshot) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(snapshot).Error
}

func (r *wechatAnalyticsImportRepository) FindSnapshotsByPublicationID(ctx context.Context, projectID, publicationID string) ([]*model.WechatAnalyticsSnapshot, error) {
	var snapshots []*model.WechatAnalyticsSnapshot
	err := r.db.WithContext(ctx).Where("project_id = ? AND publication_id = ?", projectID, publicationID).
		Where("batch_id NOT IN (?)", r.db.Model(&model.WechatAnalyticsImportBatch{}).Select("id").Where("revoked_at IS NOT NULL")).Order("data_as_of_at DESC, imported_at DESC").Find(&snapshots).Error
	return snapshots, err
}

// LockBatch serializes resolution and revocation. A no-op write also acquires
// the write lock in SQLite, where SELECT FOR UPDATE is unsupported.
func (r *wechatAnalyticsImportRepository) LockBatch(ctx context.Context, projectID, id string) error {
	return r.db.WithContext(ctx).Model(&model.WechatAnalyticsImportBatch{}).Where("project_id = ? AND id = ?", projectID, id).UpdateColumn("updated_at", gorm.Expr("updated_at")).Error
}

// LockProject serializes link ownership changes across different import batches.
func (r *wechatAnalyticsImportRepository) LockProject(ctx context.Context, projectID string) error {
	return r.db.WithContext(ctx).Model(&model.Project{}).Where("id = ?", projectID).UpdateColumn("updated_at", gorm.Expr("updated_at")).Error
}

func (r *wechatAnalyticsImportRepository) FindSnapshotsByTaskID(ctx context.Context, projectID, taskID string) ([]*model.WechatAnalyticsSnapshot, error) {
	var snapshots []*model.WechatAnalyticsSnapshot
	err := r.db.WithContext(ctx).Where("project_id = ? AND task_id = ?", projectID, taskID).Where("batch_id NOT IN (?)", r.db.Model(&model.WechatAnalyticsImportBatch{}).Select("id").Where("revoked_at IS NOT NULL")).Order("data_as_of_at DESC, imported_at DESC").Find(&snapshots).Error
	return snapshots, err
}
