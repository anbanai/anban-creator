package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	ImageGenerationStatusGenerating = "generating"
	ImageGenerationStatusCompleted  = "completed"
	ImageGenerationStatusFailed     = "failed"
)

// ImageGeneration 图片生成任务记录
type ImageGeneration struct {
	ID                     string                  `gorm:"type:char(36);primaryKey" json:"id"`
	UserID                 string                  `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID              string                  `gorm:"type:char(36);index;not null" json:"project_id"`
	Prompt                 string                  `gorm:"type:text;not null" json:"prompt"`
	RevisedPrompt          string                  `gorm:"type:text" json:"revised_prompt,omitempty"`
	Provider               string                  `gorm:"type:varchar(32);not null" json:"provider"`
	ProviderID             string                  `gorm:"type:varchar(64)" json:"provider_id,omitempty"`
	Model                  string                  `gorm:"type:varchar(64);not null" json:"model"`
	Quality                string                  `gorm:"type:varchar(16)" json:"quality,omitempty"`
	Size                   string                  `gorm:"type:varchar(32)" json:"size,omitempty"`
	N                      int                     `gorm:"default:1" json:"n"`
	OutputFormat           string                  `gorm:"type:varchar(16)" json:"output_format,omitempty"`
	OutputCompression      int                     `gorm:"default:0" json:"output_compression,omitempty"`
	Background             string                  `gorm:"type:varchar(16);default:''" json:"background,omitempty"`
	Watermark              bool                    `gorm:"default:false" json:"watermark,omitempty"`
	Status                 string                  `gorm:"type:varchar(16);index;not null;default:'generating'" json:"status"`
	Error                  string                  `gorm:"type:text" json:"error,omitempty"`
	InputTokens            int                     `json:"input_tokens,omitempty"`
	OutputTokens           int                     `json:"output_tokens,omitempty"`
	TextInputTokens        int64                   `json:"text_input_tokens,omitempty"`
	TextCachedInputTokens  int64                   `json:"text_cached_input_tokens,omitempty"`
	ImageInputTokens       int64                   `json:"image_input_tokens,omitempty"`
	ImageCachedInputTokens int64                   `json:"image_cached_input_tokens,omitempty"`
	ImageOutputTokens      int64                   `json:"image_output_tokens,omitempty"`
	TotalTokens            int64                   `json:"total_tokens,omitempty"`
	ReferenceFiles         string                  `gorm:"type:text" json:"reference_files,omitempty"`
	MaskFileID             string                  `gorm:"type:char(36)" json:"mask_file_id,omitempty"`
	Cost                   int                     `gorm:"default:0" json:"cost,omitempty"`
	EstimatedCost          int                     `gorm:"default:0" json:"estimated_cost,omitempty"`
	FinalCost              int                     `gorm:"default:0" json:"final_cost,omitempty"`
	BillingMode            string                  `gorm:"type:varchar(32)" json:"billing_mode,omitempty"`
	BillingStatus          string                  `gorm:"type:varchar(32)" json:"billing_status,omitempty"`
	PriceSnapshot          datatypes.JSON          `gorm:"type:json" json:"price_snapshot,omitempty"`
	StartedAt              *time.Time              `json:"started_at,omitempty"`
	CompletedAt            *time.Time              `json:"completed_at,omitempty"`
	CreatedAt              time.Time               `json:"created_at"`
	UpdatedAt              time.Time               `json:"updated_at"`
	Results                []ImageGenerationResult `gorm:"foreignKey:GenerationID" json:"results,omitempty"`
}

func (ImageGeneration) TableName() string { return "image_generations" }

// ImageGenerationResult 单张生成的图片结果
type ImageGenerationResult struct {
	ID           uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	GenerationID string `gorm:"type:char(36);index;not null" json:"generation_id"`
	ImageURL     string `gorm:"type:varchar(1024)" json:"image_url,omitempty"`
	ImagePath    string `gorm:"type:varchar(1024)" json:"image_path,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	Index        int    `json:"index"`
	FileID       string `gorm:"type:varchar(256)" json:"file_id,omitempty"`
}

func (ImageGenerationResult) TableName() string { return "image_generation_results" }
