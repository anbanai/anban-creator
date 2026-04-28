package model

import "time"

// TextUserConfig holds per-user text/LLM model configuration.
// Used for both Agent (Claude Code) execution and Writing/LLM service.
type TextUserConfig struct {
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model,omitempty"`
	Proxy   string `json:"proxy,omitempty"`
}

// ImageUserConfig holds per-user image generation model configuration.
type ImageUserConfig struct {
	Provider string `json:"provider,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Model    string `json:"model,omitempty"`
	Proxy    string `json:"proxy,omitempty"`
}

// HasConfig returns true if any configuration field is set.
func (c *TextUserConfig) HasConfig() bool {
	return c.BaseURL != "" || c.APIKey != "" || c.Model != ""
}

// HasConfig returns true if any configuration field is set.
func (c *ImageUserConfig) HasConfig() bool {
	return c.Provider != "" || c.BaseURL != "" || c.APIKey != "" || c.Model != ""
}

// UserModelConfig stores per-user AI model overrides.
// API keys are encrypted at rest (AES-256-GCM).
type UserModelConfig struct {
	ID                   string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID               string    `gorm:"type:char(36);uniqueIndex;not null" json:"user_id"`
	TextConfigEncrypted  []byte    `gorm:"type:blob" json:"-"`
	ImageConfigEncrypted []byte    `gorm:"type:blob" json:"-"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// TableName returns the database table name for UserModelConfig.
func (UserModelConfig) TableName() string { return "user_model_configs" }
