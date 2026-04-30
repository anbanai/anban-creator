package model

import "time"

// TextUserConfig holds per-user text/LLM model configuration.
// It is only used by MCP server-side writing/LLM tools and must never be
// applied to Claude Code agent execution.
type TextUserConfig struct {
	Endpoint string `json:"endpoint,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Model    string `json:"model,omitempty"`
	Proxy    string `json:"proxy,omitempty"`
}

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
func (c *TextUserConfig) HasConfig() bool {
	return c.Endpoint != "" || c.APIKey != "" || c.Model != ""
}

// HasConfig returns true if any configuration field is set.
func (c *ImageUserConfig) HasConfig() bool {
	return c.Provider != "" || c.Endpoint != "" || c.APIKey != "" || c.Model != ""
}

// UserModelConfig stores per-user MCP tool model overrides (plaintext JSON).
type UserModelConfig struct {
	ID              string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID          string    `gorm:"type:char(36);uniqueIndex;not null" json:"user_id"`
	TextConfigJSON  string    `gorm:"type:text" json:"-"`
	ImageConfigJSON string    `gorm:"type:text" json:"-"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// TableName returns the database table name for UserModelConfig.
func (UserModelConfig) TableName() string { return "user_model_configs" }
