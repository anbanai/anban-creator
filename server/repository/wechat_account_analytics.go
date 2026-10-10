package repository

import (
	"context"
	"errors"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

type wechatAccountAnalyticsRepository struct{ db *gorm.DB }

const wechatWinnerPrecedes = `((b2.status = ? AND b.status = ?) OR (b2.status = b.status AND (b2.effective_at > b.effective_at OR (b2.effective_at = b.effective_at AND b2.received_at > b.received_at) OR (b2.effective_at = b.effective_at AND b2.received_at = b.received_at AND b2.id > b.id) OR (b2.effective_at = b.effective_at AND b2.received_at = b.received_at AND b2.id = b.id AND d2.id > d.id))))`

func newWechatAccountAnalyticsRepository(db *gorm.DB) WechatAccountAnalyticsRepository {
	return &wechatAccountAnalyticsRepository{db: db}
}

func (r *wechatAccountAnalyticsRepository) CreateBatch(ctx context.Context, batch *model.WechatAccountAnalyticsBatch) error {
	return r.db.WithContext(ctx).Create(batch).Error
}

func (r *wechatAccountAnalyticsRepository) FindBatchByID(ctx context.Context, projectID, id string) (*model.WechatAccountAnalyticsBatch, error) {
	var batch model.WechatAccountAnalyticsBatch
	err := r.db.WithContext(ctx).Where("project_id = ? AND id = ?", projectID, id).First(&batch).Error
	return &batch, err
}

func (r *wechatAccountAnalyticsRepository) ListBatches(ctx context.Context, projectID string, offset, limit int) ([]*model.WechatAccountAnalyticsBatch, int64, error) {
	query := r.db.WithContext(ctx).Where("project_id = ?", projectID)
	var total int64
	if err := query.Model(&model.WechatAccountAnalyticsBatch{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var batches []*model.WechatAccountAnalyticsBatch
	if err := query.Order("created_at DESC, id DESC").Offset(offset).Limit(limit).Find(&batches).Error; err != nil {
		return nil, 0, err
	}
	return batches, total, nil
}

func (r *wechatAccountAnalyticsRepository) UpdateBatch(ctx context.Context, batch *model.WechatAccountAnalyticsBatch) error {
	return r.db.WithContext(ctx).Save(batch).Error
}

func (r *wechatAccountAnalyticsRepository) CreateDays(ctx context.Context, days []*model.WechatAccountAnalyticsDay) error {
	if len(days) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&days).Error
}

func (r *wechatAccountAnalyticsRepository) CreateSources(ctx context.Context, sources []*model.WechatTrafficSourceDay) error {
	if len(sources) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&sources).Error
}

func (r *wechatAccountAnalyticsRepository) FindBatchByIdempotency(ctx context.Context, projectID, key string) (*model.WechatAccountAnalyticsBatch, error) {
	var batch model.WechatAccountAnalyticsBatch
	err := r.db.WithContext(ctx).Where("project_id = ? AND idempotency_key = ?", projectID, key).First(&batch).Error
	return &batch, err
}

// ListWinningDays selects at most one active fact for each project/date. Batch
// precedence is explicit and deterministic: active, non-revoked batches;
// then completed batches; latest effective time; latest received time; then
// stable batch/fact IDs.
func (r *wechatAccountAnalyticsRepository) ListWinningDays(ctx context.Context, projectID, from, to string) ([]*model.WechatAccountAnalyticsDay, error) {
	var rows []*model.WechatAccountAnalyticsDay
	q := r.db.WithContext(ctx).Table("wechat_account_analytics_days AS d").
		Select("d.*").
		Joins("JOIN wechat_account_analytics_batches AS b ON b.id = d.batch_id AND b.project_id = d.project_id").
		Where("d.project_id = ? AND d.stat_date BETWEEN ? AND ? AND b.revoked_at IS NULL AND b.status IN ?", projectID, from, to, []string{model.WechatAccountAnalyticsBatchActive, model.WechatAccountAnalyticsBatchCompleted}).
		Where("NOT EXISTS (SELECT 1 FROM wechat_account_analytics_days d2 JOIN wechat_account_analytics_batches b2 ON b2.id = d2.batch_id AND b2.project_id = d2.project_id WHERE d2.project_id = d.project_id AND d2.stat_date = d.stat_date AND b2.revoked_at IS NULL AND b2.status IN ? AND "+wechatWinnerPrecedes+")", []string{model.WechatAccountAnalyticsBatchActive, model.WechatAccountAnalyticsBatchCompleted}, model.WechatAccountAnalyticsBatchActive, model.WechatAccountAnalyticsBatchCompleted).
		Order("d.stat_date ASC, d.id ASC")
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *wechatAccountAnalyticsRepository) ListWinningSources(ctx context.Context, projectID, from, to string) ([]*model.WechatTrafficSourceDay, error) {
	var rows []*model.WechatTrafficSourceDay
	q := r.db.WithContext(ctx).Table("wechat_traffic_source_days AS d").
		Select("d.*").
		Joins("JOIN wechat_account_analytics_batches AS b ON b.id = d.batch_id AND b.project_id = d.project_id").
		Where("d.project_id = ? AND d.stat_date BETWEEN ? AND ? AND b.revoked_at IS NULL AND b.status IN ?", projectID, from, to, []string{model.WechatAccountAnalyticsBatchActive, model.WechatAccountAnalyticsBatchCompleted}).
		Where("NOT EXISTS (SELECT 1 FROM wechat_traffic_source_days d2 JOIN wechat_account_analytics_batches b2 ON b2.id = d2.batch_id AND b2.project_id = d2.project_id WHERE d2.project_id = d.project_id AND d2.stat_date = d.stat_date AND d2.source = d.source AND b2.revoked_at IS NULL AND b2.status IN ? AND "+wechatWinnerPrecedes+")", []string{model.WechatAccountAnalyticsBatchActive, model.WechatAccountAnalyticsBatchCompleted}, model.WechatAccountAnalyticsBatchActive, model.WechatAccountAnalyticsBatchCompleted).
		Order("d.stat_date ASC, d.source ASC, d.id ASC")
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *wechatAccountAnalyticsRepository) RevokeBatch(ctx context.Context, projectID, batchID string, revokedAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&model.WechatAccountAnalyticsBatch{}).
		Where("project_id = ? AND id = ? AND revoked_at IS NULL", projectID, batchID).
		Updates(map[string]any{"revoked_at": revokedAt, "status": model.WechatAccountAnalyticsBatchRevoked, "updated_at": revokedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("wechat account analytics batch not found or already revoked")
	}
	return nil
}

// LockProject acquires the same project write lock used by other analytics
// mutations. SQLite obtains the lock from this no-op update as well.
func (r *wechatAccountAnalyticsRepository) LockProject(ctx context.Context, projectID string) error {
	return r.db.WithContext(ctx).Model(&model.Project{}).Where("id = ?", projectID).UpdateColumn("updated_at", gorm.Expr("updated_at")).Error
}

func (r *wechatAccountAnalyticsRepository) CurrentRevision(ctx context.Context, projectID string) (int64, error) {
	var state model.AnalyticsState
	err := r.db.WithContext(ctx).Where("project_id = ?", projectID).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	return state.Revision, err
}

func (r *wechatAccountAnalyticsRepository) BumpRevision(ctx context.Context, projectID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return bumpWechatAnalyticsRevision(ctx, tx, projectID)
	})
}

func (r *wechatAccountAnalyticsRepository) BumpRevisionInTx(ctx context.Context, projectID string) error {
	return bumpWechatAnalyticsRevision(ctx, r.db, projectID)
}

func bumpWechatAnalyticsRevision(ctx context.Context, db *gorm.DB, projectID string) error {
	var state model.AnalyticsState
	err := db.WithContext(ctx).Where("project_id = ?", projectID).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.WithContext(ctx).Create(&model.AnalyticsState{ProjectID: projectID, Revision: 1, ActiveGeneration: 1, Status: "ready", UpdatedAt: time.Now().UTC()}).Error
	}
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Model(&model.AnalyticsState{}).Where("project_id = ?", projectID).Updates(map[string]any{"revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC()}).Error
}
