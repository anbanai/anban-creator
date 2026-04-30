package repository

import (
	"context"
	"time"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type rednoteTrackingRepository struct {
	db *gorm.DB
}

func newRednoteTrackingRepository(db *gorm.DB) RednoteTrackingRepository {
	return &rednoteTrackingRepository{db: db}
}

func (r *rednoteTrackingRepository) Create(ctx context.Context, tracking *model.RednotePostTracking) error {
	return r.db.WithContext(ctx).Create(tracking).Error
}

func (r *rednoteTrackingRepository) FindByTaskID(ctx context.Context, taskID string) (*model.RednotePostTracking, error) {
	var tracking model.RednotePostTracking
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).First(&tracking).Error; err != nil {
		return nil, err
	}
	return &tracking, nil
}

func (r *rednoteTrackingRepository) FindByID(ctx context.Context, id string) (*model.RednotePostTracking, error) {
	var tracking model.RednotePostTracking
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&tracking).Error; err != nil {
		return nil, err
	}
	return &tracking, nil
}

func (r *rednoteTrackingRepository) FindDue(ctx context.Context, now time.Time, limit int) ([]*model.RednotePostTracking, error) {
	var trackings []*model.RednotePostTracking
	q := r.db.WithContext(ctx).
		Where("next_run_at IS NOT NULL AND next_run_at <= ?", now).
		Where("status IN ?", []string{
			model.RednoteTrackingStatusWaitingDiscovery,
			model.RednoteTrackingStatusTracking,
		}).
		Order("next_run_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&trackings).Error; err != nil {
		return nil, err
	}
	return trackings, nil
}

func (r *rednoteTrackingRepository) Update(ctx context.Context, tracking *model.RednotePostTracking) error {
	return r.db.WithContext(ctx).Save(tracking).Error
}

func (r *rednoteTrackingRepository) UpdateStatus(ctx context.Context, id, status string) error {
	return r.db.WithContext(ctx).
		Model(&model.RednotePostTracking{}).
		Where("id = ?", id).
		Update("status", status).Error
}

type rednoteMetricSnapshotRepository struct {
	db *gorm.DB
}

func newRednoteMetricSnapshotRepository(db *gorm.DB) RednoteMetricSnapshotRepository {
	return &rednoteMetricSnapshotRepository{db: db}
}

func (r *rednoteMetricSnapshotRepository) Create(ctx context.Context, snapshot *model.RednoteMetricSnapshot) error {
	return r.db.WithContext(ctx).Create(snapshot).Error
}

func (r *rednoteMetricSnapshotRepository) UpsertByTrackingAndDate(ctx context.Context, snapshot *model.RednoteMetricSnapshot) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tracking_id"},
			{Name: "captured_date"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"captured_at",
			"like_count",
			"collect_count",
			"comment_count",
			"share_count",
			"view_count",
			"raw_data",
		}),
	}).Create(snapshot).Error
}

func (r *rednoteMetricSnapshotRepository) FindByTaskID(ctx context.Context, taskID string) ([]*model.RednoteMetricSnapshot, error) {
	var snapshots []*model.RednoteMetricSnapshot
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("captured_at ASC").
		Find(&snapshots).Error
	return snapshots, err
}

func (r *rednoteMetricSnapshotRepository) FindLatestByTrackingID(ctx context.Context, trackingID string) (*model.RednoteMetricSnapshot, error) {
	var snapshot model.RednoteMetricSnapshot
	if err := r.db.WithContext(ctx).
		Where("tracking_id = ?", trackingID).
		Order("captured_at DESC").
		First(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *rednoteMetricSnapshotRepository) FindPreviousByTrackingID(ctx context.Context, trackingID string, capturedAt time.Time) (*model.RednoteMetricSnapshot, error) {
	var snapshot model.RednoteMetricSnapshot
	if err := r.db.WithContext(ctx).
		Where("tracking_id = ? AND captured_at < ?", trackingID, capturedAt).
		Order("captured_at DESC").
		First(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}
