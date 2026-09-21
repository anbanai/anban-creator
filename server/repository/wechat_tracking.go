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
		Where("next_fetch_at IS NOT NULL AND next_fetch_at <= ?", now).
		Where("status IN ?", []string{model.WechatTrackingStatusWaitingData, model.WechatTrackingStatusTracking, model.WechatTrackingStatusError}).
		Order("next_fetch_at ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	return trackings, query.Find(&trackings).Error
}

func (r *wechatTrackingRepository) TryClaimDueDispatch(ctx context.Context, id string, expectedUpdatedAt, claimedAt, leaseUntil time.Time, token string) (bool, error) {
	rows, err := runWechatClaimWrite(ctx, r.db.Dialector.Name(), func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.WechatArticleTracking{}).
			Where("id = ? AND updated_at = ?", id, expectedUpdatedAt).
			Where("status IN ?", []string{model.WechatTrackingStatusWaitingData, model.WechatTrackingStatusTracking, model.WechatTrackingStatusError}).
			Where("next_fetch_at IS NOT NULL AND next_fetch_at <= ?", claimedAt).
			Updates(map[string]any{
				"recovery_claim_token": token,
				"recovery_claimed_at":  claimedAt,
				"next_fetch_at":        leaseUntil,
			})
	})
	return rows == 1, err
}

func (r *wechatTrackingRepository) ReleaseDueDispatch(ctx context.Context, id, token string, retryAt time.Time) (bool, error) {
	rows, err := runWechatClaimWrite(ctx, r.db.Dialector.Name(), func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.WechatArticleTracking{}).
			Where("id = ? AND recovery_claim_token = ?", id, token).
			Updates(map[string]any{
				"recovery_claim_token": "",
				"recovery_claimed_at":  nil,
				"next_fetch_at":        retryAt,
			})
	})
	return rows == 1, err
}

func (r *wechatTrackingRepository) TryClaimDailyFetch(ctx context.Context, id string, claimedAt, dayStart, nextFetchAt time.Time) (bool, error) {
	rows, err := runWechatClaimWrite(ctx, r.db.Dialector.Name(), func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.WechatArticleTracking{}).
			Where("id = ?", id).
			Where("status IN ?", []string{model.WechatTrackingStatusWaitingData, model.WechatTrackingStatusTracking, model.WechatTrackingStatusError}).
			Where("expires_at > ?", claimedAt).
			Where("last_fetch_at IS NULL OR last_fetch_at < ?", dayStart).
			Updates(map[string]any{
				"last_fetch_at":        claimedAt,
				"next_fetch_at":        nextFetchAt,
				"recovery_claim_token": "",
				"recovery_claimed_at":  nil,
			})
	})
	return rows == 1, err
}

func (r *wechatTrackingRepository) Update(ctx context.Context, tracking *model.WechatArticleTracking) error {
	// Capture/recovery updates must not restore an identity revoked while a
	// provider request was in flight. URL writes belong to publication/import binding.
	return r.db.WithContext(ctx).Omit("ArticleURL", "ArticleURLImportBatchID").Save(tracking).Error
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
		Columns: []clause.Column{{Name: "tracking_id"}, {Name: "stat_date"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"captured_at", "read_users", "share_users", "collection_users", "like_users",
			"zaikan_users", "comment_count", "read_finish_rate", "average_read_active_time",
			"read_to_subscribe_users", "raw_response", "updated_at",
		}),
	}).Create(snapshot).Error
}

func (r *wechatMetricSnapshotRepository) FindByTaskID(ctx context.Context, taskID string) ([]*model.WechatMetricSnapshot, error) {
	var snapshots []*model.WechatMetricSnapshot
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("stat_date ASC").Find(&snapshots).Error
	return snapshots, err
}

func (r *wechatMetricSnapshotRepository) DeleteByTrackingID(ctx context.Context, trackingID string) error {
	return r.db.WithContext(ctx).Where("tracking_id = ?", trackingID).Delete(&model.WechatMetricSnapshot{}).Error
}
