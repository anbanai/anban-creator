package storage

import (
	"time"

	"gorm.io/gorm/clause"
)

// UpsertHistories 批量 upsert 历史记录（source + item_id 为唯一键）
func (s *Store) UpsertHistories(items []History) error {
	if len(items) == 0 {
		return nil
	}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source"}, {Name: "item_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "digest", "url", "update_time", "synced_at"}),
	}).Create(&items).Error
}

// ListHistories 列出历史记录，按 update_time 降序
func (s *Store) ListHistories(limit int) ([]History, error) {
	var items []History
	err := s.db.Order("update_time DESC").Limit(limit).Find(&items).Error
	return items, err
}

// GetLastSyncTime 获取最近一次同步时间
func (s *Store) GetLastSyncTime() (time.Time, error) {
	var h History
	err := s.db.Order("synced_at DESC").First(&h).Error
	if err != nil {
		return time.Time{}, err
	}
	return h.SyncedAt, nil
}

// HasHistories 判断是否有历史记录
func (s *Store) HasHistories() (bool, error) {
	var count int64
	err := s.db.Model(&History{}).Count(&count).Error
	return count > 0, err
}
