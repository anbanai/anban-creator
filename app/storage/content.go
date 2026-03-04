package storage

import (
	"gorm.io/gorm/clause"
)

// UpsertContent 按 dir 做 upsert，仅覆盖非零字段
func (s *Store) UpsertContent(c *Content) error {
	// 先尝试找到现有记录
	var existing Content
	result := s.db.Where("dir = ?", c.Dir).First(&existing)

	if result.Error != nil {
		// 记录不存在，直接插入
		return s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "dir"}},
			DoUpdates: clause.AssignmentColumns([]string{"type", "status", "title", "digest", "topic", "style", "media_id", "updated_at"}),
		}).Create(c).Error
	}

	// 记录存在，仅更新非零字段
	updates := map[string]any{}
	if c.Status != "" {
		updates["status"] = c.Status
	}
	if c.Type != "" {
		updates["type"] = c.Type
	}
	if c.Title != "" {
		updates["title"] = c.Title
	}
	if c.Digest != "" {
		updates["digest"] = c.Digest
	}
	if c.Topic != "" {
		updates["topic"] = c.Topic
	}
	if c.Style != "" {
		updates["style"] = c.Style
	}
	if c.MediaID != "" {
		updates["media_id"] = c.MediaID
	}

	if len(updates) == 0 {
		return nil
	}
	return s.db.Model(&Content{}).Where("dir = ?", c.Dir).Updates(updates).Error
}

// UpdateContentStatus 快速更新内容状态
func (s *Store) UpdateContentStatus(dir, status string) error {
	return s.db.Model(&Content{}).Where("dir = ?", dir).Update("status", status).Error
}

// UpdateContentFields 批量更新指定字段
func (s *Store) UpdateContentFields(dir string, fields map[string]any) error {
	return s.db.Model(&Content{}).Where("dir = ?", dir).Updates(fields).Error
}

// FindContentByDir 按目录查找内容
func (s *Store) FindContentByDir(dir string) (*Content, error) {
	var c Content
	err := s.db.Where("dir = ?", dir).First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListContents 按 updated_at DESC 列出内容
func (s *Store) ListContents(limit int) ([]Content, error) {
	var items []Content
	err := s.db.Order("updated_at DESC").Limit(limit).Find(&items).Error
	return items, err
}

// ListUnpublishedContents 列出未发布的内容
func (s *Store) ListUnpublishedContents(limit int) ([]Content, error) {
	var items []Content
	err := s.db.Where("status != ?", "published").Order("updated_at DESC").Limit(limit).Find(&items).Error
	return items, err
}

// HasContents 判断是否有内容记录
func (s *Store) HasContents() (bool, error) {
	var count int64
	err := s.db.Model(&Content{}).Count(&count).Error
	return count > 0, err
}
