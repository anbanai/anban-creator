package model

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

var ErrAssetNotFound = errors.New("asset not found")

const AssetPurposeArticleImage = "article_image"

func IsPublicArticleImageURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return false
	}
	const marker = "/api/v1/article-assets/"
	index := strings.LastIndex(u.Path, marker)
	if index < 0 {
		return false
	}
	id := strings.TrimPrefix(u.Path[index:], marker)
	return id != "" && !strings.Contains(id, "/") && u.RawQuery == "" && u.Fragment == ""
}

// Asset is an immutable storage object owned by a user.
type Asset struct {
	ID          string    `gorm:"type:char(36);primaryKey" json:"asset_id"`
	UserID      string    `gorm:"type:char(36);index;not null" json:"-"`
	Purpose     string    `gorm:"type:varchar(50);index;not null" json:"-"`
	StorageKey  string    `gorm:"type:varchar(500);uniqueIndex;not null" json:"-"`
	FileName    string    `gorm:"type:varchar(255);not null" json:"file_name"`
	ContentType string    `gorm:"type:varchar(120);not null" json:"content_type"`
	Size        int64     `gorm:"not null" json:"size"`
	ETag        string    `gorm:"column:etag;type:varchar(255);not null" json:"-"`
	CreatedAt   time.Time `json:"created_at"`
}

func (Asset) TableName() string { return "assets" }

type AssetView struct {
	AssetID           string    `json:"asset_id"`
	FileName          string    `json:"file_name"`
	ContentType       string    `json:"content_type"`
	Size              int64     `json:"size"`
	DownloadURL       string    `json:"download_url"`
	DownloadExpiresAt time.Time `json:"download_expires_at"`
}
