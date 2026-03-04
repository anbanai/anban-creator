package storage

// CreateDraft 记录草稿操作
func (s *Store) CreateDraft(d *Draft) error {
	return s.db.Create(d).Error
}

// ListDrafts 列出最近的草稿记录
func (s *Store) ListDrafts(limit int) ([]Draft, error) {
	var drafts []Draft
	err := s.db.Order("created_at DESC").Limit(limit).Find(&drafts).Error
	return drafts, err
}
