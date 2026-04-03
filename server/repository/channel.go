package repository

import (
	"context"
	"time"

	"github.com/royalrick/anbanwriter/server/model"
	"gorm.io/gorm"
)

// ChannelStats holds computed statistics for a channel.
type ChannelStats struct {
	TotalTasks     int64      `json:"total_tasks"`
	CompletedTasks int64      `json:"completed_tasks"`
	FailedTasks    int64      `json:"failed_tasks"`
	RunningTasks   int64      `json:"running_tasks"`
	PendingTasks   int64      `json:"pending_tasks"`
	SuccessRate    float64    `json:"success_rate"`
	LastActivityAt *time.Time `json:"last_activity_at"`
}

// ChannelListOptions for filtering channel list queries.
type ChannelListOptions struct {
	Status   string // filter by status (active, archived)
	Platform string // filter by platform (article, xls, rednote)
}

// ChannelRepository defines the interface for channel data access.
type ChannelRepository interface {
	Create(ctx context.Context, channel *model.Channel) error
	FindByID(ctx context.Context, id string) (*model.Channel, error)
	ListByUserID(ctx context.Context, userID string, opts ChannelListOptions) ([]*model.Channel, error)
	FindByUserAndPlatform(ctx context.Context, userID, platform string) ([]*model.Channel, error)
	Update(ctx context.Context, channel *model.Channel) error
	UpdateStatus(ctx context.Context, id, status string) error
	Delete(ctx context.Context, id string) error
	GetStats(ctx context.Context, channelID string) (*ChannelStats, error)
}

// -----------------------------------------------------------------------------
// Implementation
// -----------------------------------------------------------------------------

type gormChannelRepository struct {
	db *gorm.DB
}

func newChannelRepository(db *gorm.DB) ChannelRepository {
	return &gormChannelRepository{db: db}
}

func (r *gormChannelRepository) Create(ctx context.Context, channel *model.Channel) error {
	return r.db.WithContext(ctx).Create(channel).Error
}

func (r *gormChannelRepository) FindByID(ctx context.Context, id string) (*model.Channel, error) {
	var channel model.Channel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&channel).Error; err != nil {
		return nil, err
	}
	return &channel, nil
}

func (r *gormChannelRepository) ListByUserID(ctx context.Context, userID string, opts ChannelListOptions) ([]*model.Channel, error) {
	q := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if opts.Status != "" {
		q = q.Where("status = ?", opts.Status)
	}
	if opts.Platform != "" {
		q = q.Where("platform = ?", opts.Platform)
	}

	var channels []*model.Channel
	if err := q.Order("created_at DESC").Find(&channels).Error; err != nil {
		return nil, err
	}
	return channels, nil
}

func (r *gormChannelRepository) FindByUserAndPlatform(ctx context.Context, userID, platform string) ([]*model.Channel, error) {
	var channels []*model.Channel
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND platform = ? AND status = ?", userID, platform, model.ChannelStatusActive).
		Find(&channels).Error; err != nil {
		return nil, err
	}
	return channels, nil
}

func (r *gormChannelRepository) Update(ctx context.Context, channel *model.Channel) error {
	return r.db.WithContext(ctx).Save(channel).Error
}

func (r *gormChannelRepository) UpdateStatus(ctx context.Context, id, status string) error {
	return r.db.WithContext(ctx).
		Model(&model.Channel{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now(),
		}).Error
}

func (r *gormChannelRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Channel{}).Error
}

func (r *gormChannelRepository) GetStats(ctx context.Context, channelID string) (*ChannelStats, error) {
	var stats struct {
		TotalTasks     int64
		CompletedTasks int64
		FailedTasks    int64
		RunningTasks   int64
		PendingTasks   int64
		LastActivityAt *time.Time
	}

	err := r.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) as total_tasks,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed_tasks,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed_tasks,
			SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END) as running_tasks,
			SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending_tasks,
			MAX(completed_at) as last_activity_at
		FROM tasks WHERE channel_id = ?
	`, channelID).Scan(&stats).Error

	if err != nil {
		return nil, err
	}

	result := &ChannelStats{
		TotalTasks:     stats.TotalTasks,
		CompletedTasks: stats.CompletedTasks,
		FailedTasks:    stats.FailedTasks,
		RunningTasks:   stats.RunningTasks,
		PendingTasks:   stats.PendingTasks,
		LastActivityAt: stats.LastActivityAt,
	}

	if stats.TotalTasks > 0 {
		result.SuccessRate = float64(stats.CompletedTasks) / float64(stats.TotalTasks)
	}

	return result, nil
}
