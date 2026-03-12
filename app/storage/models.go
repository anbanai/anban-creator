package storage

import "time"

// Image 图片操作记录
type Image struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Role        string    `gorm:"default:'standalone'" json:"role"` // cover / content / xls / standalone
	Prompt      string    `json:"prompt,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	LocalPath   string    `json:"local_path,omitempty"`
	MediaID     string    `gorm:"index" json:"media_id,omitempty"`
	WechatURL   string    `json:"wechat_url,omitempty"`
	Width       int       `json:"width,omitempty"`
	Height      int       `json:"height,omitempty"`
	SizeBytes   int64     `json:"size_bytes,omitempty"`
	StylePreset string    `json:"style_preset,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Draft 草稿操作记录
type Draft struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	MediaID   string    `gorm:"not null;index" json:"media_id"`
	DraftURL  string    `json:"draft_url,omitempty"`
	Title     string    `gorm:"default:''" json:"title"`
	Digest    string    `json:"digest,omitempty"`
	Type      string    `gorm:"default:'article'" json:"type"` // article / xls
	Dir       string    `json:"dir,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// History 微信历史记录（永久存储）
type History struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Source     string    `gorm:"not null;uniqueIndex:idx_source_item" json:"source"` // "draft" / "published"
	ItemID     string    `gorm:"not null;uniqueIndex:idx_source_item" json:"item_id"`
	Title      string    `gorm:"default:''" json:"title"`
	Digest     string    `json:"digest,omitempty"`
	URL        string    `json:"url,omitempty"`
	UpdateTime int64     `gorm:"not null" json:"update_time"`
	SyncedAt   time.Time `json:"synced_at"`
}

// Content 内容生命周期追踪
// Article 状态流转: created → outlined → drafted → polished → converted → published
// Post 状态流转:    created → planned → images_ready → published
type Content struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Type      string    `gorm:"not null;index" json:"type"`               // "article" / "xls"
	Dir       string    `gorm:"uniqueIndex" json:"dir"`                   // 项目目录路径（唯一标识）
	Status    string    `gorm:"not null;default:'created'" json:"status"` // 见状态流转
	Title     string    `json:"title,omitempty"`
	Digest    string    `json:"digest,omitempty"`
	Topic     string    `json:"topic,omitempty"`
	Style     string    `json:"style,omitempty"`
	MediaID   string    `gorm:"index" json:"media_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
