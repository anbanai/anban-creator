package storage

import "time"

// CreateImage 记录图片操作
func (s *Store) CreateImage(img *Image) error {
	return s.db.Create(img).Error
}

// FindImageByPath 按本地路径查找图片记录
func (s *Store) FindImageByPath(localPath string) (*Image, error) {
	var img Image
	err := s.db.Where("local_path = ?", localPath).First(&img).Error
	if err != nil {
		return nil, err
	}
	return &img, nil
}

// UpdateImageUpload 更新图片的 media_id 和 wechat_url
func (s *Store) UpdateImageUpload(localPath, mediaID, wechatURL string) error {
	result := s.db.Model(&Image{}).
		Where("local_path = ?", localPath).
		Updates(map[string]any{
			"media_id":   mediaID,
			"wechat_url": wechatURL,
		})
	return result.Error
}

// UpsertImageUpload 上传后更新已有记录，如果不存在则创建新记录
func (s *Store) UpsertImageUpload(localPath, mediaID, wechatURL string) error {
	_, err := s.FindImageByPath(localPath)
	if err != nil {
		// 记录不存在，创建新记录
		return s.CreateImage(&Image{
			LocalPath: localPath,
			MediaID:   mediaID,
			WechatURL: wechatURL,
			CreatedAt: time.Now(),
		})
	}
	return s.UpdateImageUpload(localPath, mediaID, wechatURL)
}

// ListImages 列出最近的图片记录
func (s *Store) ListImages(limit int) ([]Image, error) {
	var imgs []Image
	err := s.db.Order("created_at DESC").Limit(limit).Find(&imgs).Error
	return imgs, err
}
