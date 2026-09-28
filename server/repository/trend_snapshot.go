package repository

import (
	"context"
	"errors"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TrendSnapshotRepository interface {
	FindByPlatform(ctx context.Context, platform string) (*model.TrendSnapshot, error)
	Upsert(ctx context.Context, snapshot *model.TrendSnapshot) error
	MarkAttempt(ctx context.Context, platform string, attemptedAt time.Time, lastError string) error
}

type trendSnapshotRepository struct{ db *gorm.DB }

func newTrendSnapshotRepository(db *gorm.DB) TrendSnapshotRepository {
	return &trendSnapshotRepository{db: db}
}

func (r *trendSnapshotRepository) FindByPlatform(ctx context.Context, platform string) (*model.TrendSnapshot, error) {
	var row model.TrendSnapshot
	if err := r.db.WithContext(ctx).Where("platform = ?", platform).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *trendSnapshotRepository) Upsert(ctx context.Context, snapshot *model.TrendSnapshot) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.TrendSnapshot
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("platform = ?", snapshot.Platform).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(snapshot).Error
		}
		if err != nil {
			return err
		}
		snapshot.ID = existing.ID
		return tx.Model(&existing).Updates(map[string]any{
			"items": snapshot.Items, "fetched_at": snapshot.FetchedAt,
			"last_attempted_at": snapshot.LastAttemptedAt, "last_error": snapshot.LastError,
			"source": snapshot.Source,
		}).Error
	})
}

func (r *trendSnapshotRepository) MarkAttempt(ctx context.Context, platform string, attemptedAt time.Time, lastError string) error {
	return r.db.WithContext(ctx).Model(&model.TrendSnapshot{}).Where("platform = ?", platform).Updates(map[string]any{"last_attempted_at": attemptedAt, "last_error": lastError}).Error
}
