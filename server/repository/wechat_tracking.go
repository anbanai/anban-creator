package repository

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type wechatTrackingRepository struct{ db *gorm.DB }

func newWechatTrackingRepository(db *gorm.DB) WechatTrackingRepository {
	return &wechatTrackingRepository{db: db}
}

func (r *wechatTrackingRepository) Create(ctx context.Context, tracking *model.WechatArticleTracking) error {
	return r.db.WithContext(ctx).Create(tracking).Error
}

func (r *wechatTrackingRepository) FindByTaskID(ctx context.Context, taskID string) (*model.WechatArticleTracking, error) {
	var tracking model.WechatArticleTracking
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).First(&tracking).Error
	return &tracking, err
}

func (r *wechatTrackingRepository) FindByID(ctx context.Context, id string) (*model.WechatArticleTracking, error) {
	var tracking model.WechatArticleTracking
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&tracking).Error
	return &tracking, err
}

func (r *wechatTrackingRepository) FindDue(ctx context.Context, now time.Time, limit int) ([]*model.WechatArticleTracking, error) {
	var trackings []*model.WechatArticleTracking
	query := r.db.WithContext(ctx).
		Where("next_run_at IS NOT NULL AND next_run_at <= ?", now).
		Where("status IN ?", []string{model.WechatTrackingStatusWaitingData, model.WechatTrackingStatusTracking}).
		Order("next_run_at ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	return trackings, query.Find(&trackings).Error
}

func (r *wechatTrackingRepository) Update(ctx context.Context, tracking *model.WechatArticleTracking) error {
	return r.db.WithContext(ctx).Save(tracking).Error
}

type wechatMetricSnapshotRepository struct{ db *gorm.DB }

func newWechatMetricSnapshotRepository(db *gorm.DB) WechatMetricSnapshotRepository {
	return &wechatMetricSnapshotRepository{db: db}
}

func (r *wechatMetricSnapshotRepository) Create(ctx context.Context, snapshot *model.WechatMetricSnapshot) error {
	return r.db.WithContext(ctx).Create(snapshot).Error
}

func (r *wechatMetricSnapshotRepository) UpsertByTrackingAndDate(ctx context.Context, snapshot *model.WechatMetricSnapshot) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tracking_id"}, {Name: "captured_date"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"captured_at", "stat_date", "target_user", "int_page_read_user", "int_page_read_count",
			"ori_page_read_user", "ori_page_read_count", "share_user", "share_count",
			"add_to_fav_user", "add_to_fav_count", "raw_data",
		}),
	}).Create(snapshot).Error
}

func (r *wechatMetricSnapshotRepository) FindByTaskID(ctx context.Context, taskID string) ([]*model.WechatMetricSnapshot, error) {
	var snapshots []*model.WechatMetricSnapshot
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("captured_at ASC").Find(&snapshots).Error
	return snapshots, err
}

func (r *wechatMetricSnapshotRepository) DeleteByTrackingID(ctx context.Context, trackingID string) error {
	return r.db.WithContext(ctx).Where("tracking_id = ?", trackingID).Delete(&model.WechatMetricSnapshot{}).Error
}
