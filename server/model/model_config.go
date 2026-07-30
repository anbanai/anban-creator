package model

import "time"

// ImageUserConfig holds per-user image generation model configuration.
// It is only used by MCP server-side image generation tools.
type ImageUserConfig struct {
	Provider string `json:"provider,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Model    string `json:"model,omitempty"`
	Proxy    string `json:"proxy,omitempty"`
}

// HasConfig returns true if any configuration field is set.
func (c *ImageUserConfig) HasConfig() bool {
	return c.Provider != "" || c.Endpoint != "" || c.APIKey != "" || c.Model != ""
}

func (c *ImageUserConfig) HasCompleteConfig() bool {
	return c.Provider != "" && c.Endpoint != "" && c.APIKey != "" && c.Model != ""
}

// UserModelConfig stores per-user image generation overrides (plaintext JSON).
type UserModelConfig struct {
	ID              string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID          string    `gorm:"type:char(36);uniqueIndex;not null" json:"user_id"`
	ImageConfigJSON string    `gorm:"type:text" json:"-"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// TableName returns the database table name for UserModelConfig.
func (UserModelConfig) TableName() string { return "user_model_configs" }
