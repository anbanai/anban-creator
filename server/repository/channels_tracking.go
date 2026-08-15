package repository

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type channelsTrackingRepository struct{ db *gorm.DB }

func newChannelsTrackingRepository(db *gorm.DB) ChannelsTrackingRepository {
	return &channelsTrackingRepository{db: db}
}

func (r *channelsTrackingRepository) Create(ctx context.Context, tracking *model.ChannelsVideoTracking) error {
	return r.db.WithContext(ctx).Create(tracking).Error
}

func (r *channelsTrackingRepository) FindByTaskID(ctx context.Context, taskID string) (*model.ChannelsVideoTracking, error) {
	var tracking model.ChannelsVideoTracking
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).First(&tracking).Error
	return &tracking, err
}

func (r *channelsTrackingRepository) FindByID(ctx context.Context, id string) (*model.ChannelsVideoTracking, error) {
	var tracking model.ChannelsVideoTracking
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&tracking).Error
	return &tracking, err
}

func (r *channelsTrackingRepository) FindDue(ctx context.Context, now time.Time, limit int) ([]*model.ChannelsVideoTracking, error) {
	var trackings []*model.ChannelsVideoTracking
	query := r.db.WithContext(ctx).
		Where("next_run_at IS NOT NULL AND next_run_at <= ?", now).
		Where("status = ?", model.ChannelsTrackingStatusTracking).
		Order("next_run_at ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&trackings).Error; err != nil {
		return nil, err
	}
	return trackings, nil
}

func (r *channelsTrackingRepository) Update(ctx context.Context, tracking *model.ChannelsVideoTracking) error {
	return r.db.WithContext(ctx).Save(tracking).Error
}

func (r *channelsTrackingRepository) TryClaimCapture(ctx context.Context, id, token string, now, until time.Time) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.ChannelsVideoTracking{}).
		Where("id = ? AND status = ?", id, model.ChannelsTrackingStatusTracking).
		Where("capture_claim_until IS NULL OR capture_claim_until <= ?", now).
		Updates(map[string]any{"capture_claim_until": until, "capture_claim_token": token})
	return result.RowsAffected == 1, result.Error
}

func (r *channelsTrackingRepository) ReleaseCaptureClaim(ctx context.Context, id, token string) error {
	return r.db.WithContext(ctx).
		Model(&model.ChannelsVideoTracking{}).
		Where("id = ? AND capture_claim_token = ?", id, token).
		Updates(map[string]any{"capture_claim_until": nil, "capture_claim_token": ""}).Error
}

type channelsMetricSnapshotRepository struct{ db *gorm.DB }

func newChannelsMetricSnapshotRepository(db *gorm.DB) ChannelsMetricSnapshotRepository {
	return &channelsMetricSnapshotRepository{db: db}
}

func (r *channelsMetricSnapshotRepository) UpsertByTrackingAndDate(ctx context.Context, snapshot *model.ChannelsMetricSnapshot) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tracking_id"}, {Name: "captured_date"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"captured_at", "like_count", "favorite_count", "comment_count", "forward_count", "raw_data",
		}),
	}).Create(snapshot).Error
}

func (r *channelsMetricSnapshotRepository) FindByTaskID(ctx context.Context, taskID string) ([]*model.ChannelsMetricSnapshot, error) {
	var snapshots []*model.ChannelsMetricSnapshot
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("captured_at ASC").Find(&snapshots).Error
	return snapshots, err
}

func (r *channelsMetricSnapshotRepository) DeleteByTrackingID(ctx context.Context, trackingID string) error {
	return r.db.WithContext(ctx).Where("tracking_id = ?", trackingID).Delete(&model.ChannelsMetricSnapshot{}).Error
}
