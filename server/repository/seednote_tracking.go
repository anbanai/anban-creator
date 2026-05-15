package repository

import (
	"context"
	"time"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type seednoteTrackingRepository struct {
	db *gorm.DB
}

func newSeednoteTrackingRepository(db *gorm.DB) SeednoteTrackingRepository {
	return &seednoteTrackingRepository{db: db}
}

func (r *seednoteTrackingRepository) Create(ctx context.Context, tracking *model.SeednotePostTracking) error {
	return r.db.WithContext(ctx).Create(tracking).Error
}

func (r *seednoteTrackingRepository) FindByTaskID(ctx context.Context, taskID string) (*model.SeednotePostTracking, error) {
	var tracking model.SeednotePostTracking
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).First(&tracking).Error; err != nil {
		return nil, err
	}
	return &tracking, nil
}

func (r *seednoteTrackingRepository) FindByID(ctx context.Context, id string) (*model.SeednotePostTracking, error) {
	var tracking model.SeednotePostTracking
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&tracking).Error; err != nil {
		return nil, err
	}
	return &tracking, nil
}

func (r *seednoteTrackingRepository) FindDue(ctx context.Context, now time.Time, limit int) ([]*model.SeednotePostTracking, error) {
	var trackings []*model.SeednotePostTracking
	q := r.db.WithContext(ctx).
		Where("next_run_at IS NOT NULL AND next_run_at <= ?", now).
		Where("status IN ?", []string{
			model.SeednoteTrackingStatusWaitingDiscovery,
			model.SeednoteTrackingStatusTracking,
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

func (r *seednoteTrackingRepository) Update(ctx context.Context, tracking *model.SeednotePostTracking) error {
	return r.db.WithContext(ctx).Save(tracking).Error
}

func (r *seednoteTrackingRepository) UpdateStatus(ctx context.Context, id, status string) error {
	return r.db.WithContext(ctx).
		Model(&model.SeednotePostTracking{}).
		Where("id = ?", id).
		Update("status", status).Error
}

type seednoteMetricSnapshotRepository struct {
	db *gorm.DB
}

func newSeednoteMetricSnapshotRepository(db *gorm.DB) SeednoteMetricSnapshotRepository {
	return &seednoteMetricSnapshotRepository{db: db}
}

func (r *seednoteMetricSnapshotRepository) Create(ctx context.Context, snapshot *model.SeednoteMetricSnapshot) error {
	return r.db.WithContext(ctx).Create(snapshot).Error
}

func (r *seednoteMetricSnapshotRepository) UpsertByTrackingAndDate(ctx context.Context, snapshot *model.SeednoteMetricSnapshot) error {
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

func (r *seednoteMetricSnapshotRepository) FindByTaskID(ctx context.Context, taskID string) ([]*model.SeednoteMetricSnapshot, error) {
	var snapshots []*model.SeednoteMetricSnapshot
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("captured_at ASC").
		Find(&snapshots).Error
	return snapshots, err
}

func (r *seednoteMetricSnapshotRepository) FindLatestByTrackingID(ctx context.Context, trackingID string) (*model.SeednoteMetricSnapshot, error) {
	var snapshot model.SeednoteMetricSnapshot
	if err := r.db.WithContext(ctx).
		Where("tracking_id = ?", trackingID).
		Order("captured_at DESC").
		First(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *seednoteMetricSnapshotRepository) FindPreviousByTrackingID(ctx context.Context, trackingID string, capturedAt time.Time) (*model.SeednoteMetricSnapshot, error) {
	var snapshot model.SeednoteMetricSnapshot
	if err := r.db.WithContext(ctx).
		Where("tracking_id = ? AND captured_at < ?", trackingID, capturedAt).
		Order("captured_at DESC").
		First(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}
