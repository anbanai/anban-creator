package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_DefaultConfig(t *testing.T) {
	// 使用临时配置文件，避免加载用户配置
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "wechatwriter.json")

	// 创建一个最小的配置文件（只包含必需的微信配置）
	minimalConfig := `{
  "wechat": {
    "appid": "test_appid",
    "secret": "test_secret"
  },
  "image": {
    "compress": true,
    "max_width": 2560,
    "max_size_mb": 5
  }
}`
	if err := os.WriteFile(configPath, []byte(minimalConfig), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	cfg, err := LoadWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Article.Theme != "" {
		t.Errorf("Article.Theme = %v, want empty (not set in config)", cfg.Article.Theme)
	}
	if cfg.Image.Compress != true {
		t.Errorf("CompressImages = %v, want true", cfg.Image.Compress)
	}
	if cfg.Image.MaxWidth != 2560 {
		t.Errorf("MaxImageWidth = %v, want 2560", cfg.Image.MaxWidth)
	}
	if cfg.Image.MaxSizeMB != 5 {
		t.Errorf("MaxImageSize = %v, want 5MB", cfg.Image.MaxSizeMB)
	}
	if cfg.Image.HTTPTimeout != 0 {
		t.Errorf("HTTPTimeout = %v, want 0 (not set in config)", cfg.Image.HTTPTimeout)
	}
	if cfg.Article.Image.Provider != "" {
		t.Errorf("Article.Image.Provider = %v, want empty (not set in config)", cfg.Article.Image.Provider)
	}
	if cfg.Article.Image.BaseURL != "" {
		t.Errorf("Article.Image.BaseURL = %v, want empty (not set in config)", cfg.Article.Image.BaseURL)
	}
	if cfg.Article.Image.Model != "" {
		t.Errorf("Article.Image.Model = %v, want empty (not set in config)", cfg.Article.Image.Model)
	}
	if cfg.Article.Image.Size != "" {
		t.Errorf("Article.Image.Size = %v, want empty (not set in config)", cfg.Article.Image.Size)
	}
}

func TestLoad_JSONConfig_Full(t *testing.T) {
	configContent := `{
  "wechat": {
    "name": "Test Account",
    "appid": "wx123456",
    "secret": "secret123",
    "key_words": ["tea", "culture"],
    "style": "dan-koe"
  },
  "article": {
    "theme": "apple",
    "image": {
      "key": "test_article_key",
      "base_url": "https://test.api.com",
      "provider": "gemini",
      "model": "gemini-3-pro-image-preview",
      "size": "16:9"
    }
  },
  "post": {
    "image": {
      "key": "test_post_key",
      "provider": "gemini",
      "model": "gemini-3-pro-image-preview",
      "size": "3:4"
    }
  },
  "image": {
    "http_timeout": 60,
    "compress": false,
    "max_width": 2560,
    "max_size_mb": 10
  }
}`

	tmpFile := filepath.Join(t.TempDir(), "test.json")
	if err := os.WriteFile(tmpFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}

	cfg, err := LoadWithDefaults(tmpFile)
	if err != nil {
		t.Fatalf("LoadWithDefaults() error = %v", err)
	}

	if cfg.Wechat.AppID != "wx123456" {
		t.Errorf("WechatAppID = %v, want wx123456", cfg.Wechat.AppID)
	}
	if cfg.Wechat.Secret != "secret123" {
		t.Errorf("WechatSecret = %v, want secret123", cfg.Wechat.Secret)
	}
	if cfg.Wechat.Name != "Test Account" {
		t.Errorf("WechatName = %v, want Test Account", cfg.Wechat.Name)
	}
	if len(cfg.Wechat.KeyWords) != 2 || cfg.Wechat.KeyWords[0] != "tea" || cfg.Wechat.KeyWords[1] != "culture" {
		t.Errorf("WechatKeyWords = %v, want [tea culture]", cfg.Wechat.KeyWords)
	}
	if cfg.Wechat.Style != "dan-koe" {
		t.Errorf("DefaultStyle = %v, want dan-koe", cfg.Wechat.Style)
	}
	if cfg.Article.Theme != "apple" {
		t.Errorf("Article.Theme = %v, want apple", cfg.Article.Theme)
	}
	if cfg.Article.Image.Key != "test_article_key" {
		t.Errorf("Article.Image.Key = %v, want test_article_key", cfg.Article.Image.Key)
	}
	if cfg.Article.Image.BaseURL != "https://test.api.com" {
		t.Errorf("Article.Image.BaseURL = %v, want https://test.api.com", cfg.Article.Image.BaseURL)
	}
	if cfg.Article.Image.Provider != "gemini" {
		t.Errorf("Article.Image.Provider = %v, want gemini", cfg.Article.Image.Provider)
	}
	if cfg.Article.Image.Model != "gemini-3-pro-image-preview" {
		t.Errorf("Article.Image.Model = %v, want gemini-3-pro-image-preview", cfg.Article.Image.Model)
	}
	if cfg.Article.Image.Size != "16:9" {
		t.Errorf("Article.Image.Size = %v, want 16:9", cfg.Article.Image.Size)
	}
	if cfg.Post.Image.Key != "test_post_key" {
		t.Errorf("Post.Image.Key = %v, want test_post_key", cfg.Post.Image.Key)
	}
	if cfg.Post.Image.Size != "3:4" {
		t.Errorf("Post.Image.Size = %v, want 3:4", cfg.Post.Image.Size)
	}
	if cfg.Image.HTTPTimeout != 60 {
		t.Errorf("HTTPTimeout = %v, want 60", cfg.Image.HTTPTimeout)
	}
	if cfg.Image.Compress != false {
		t.Errorf("CompressImages = %v, want false", cfg.Image.Compress)
	}
	if cfg.Image.MaxWidth != 2560 {
		t.Errorf("MaxImageWidth = %v, want 2560", cfg.Image.MaxWidth)
	}
	if cfg.Image.MaxSizeMB != 10 {
		t.Errorf("MaxImageSize = %v, want 10MB", cfg.Image.MaxSizeMB)
	}
}

func TestLoad_JSONConfig(t *testing.T) {
	configContent := `{
  "wechat": {
    "appid": "wx123456",
    "secret": "secret123"
  },
  "article": {
    "image": {
      "key": "test_image_key",
      "base_url": "https://test.api.com"
    }
  }
}`

	tmpFile := filepath.Join(t.TempDir(), "test.json")
	if err := os.WriteFile(tmpFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}

	cfg, err := LoadWithDefaults(tmpFile)
	if err != nil {
		t.Fatalf("LoadWithDefaults() error = %v", err)
	}

	if cfg.Wechat.AppID != "wx123456" {
		t.Errorf("WechatAppID = %v, want wx123456", cfg.Wechat.AppID)
	}
	if cfg.Wechat.Secret != "secret123" {
		t.Errorf("WechatSecret = %v, want secret123", cfg.Wechat.Secret)
	}
	if cfg.Article.Image.Key != "test_image_key" {
		t.Errorf("Article.Image.Key = %v, want test_image_key", cfg.Article.Image.Key)
	}
}

func TestFindConfigFile(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer os.Chdir(origDir)

	// No config dir -> returns empty
	if got := findConfigFile(); got != "" {
		t.Errorf("findConfigFile() = %q, want empty", got)
	}

	// Create settings.json -> should find it
	if err := os.MkdirAll(ConfigDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	configPath := filepath.Join(ConfigDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(`{}`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if got := findConfigFile(); got != configPath {
		t.Errorf("findConfigFile() = %q, want %q", got, configPath)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	want := filepath.Join(".wechatwriter", "settings.json")
	if got := DefaultConfigPath(); got != want {
		t.Errorf("DefaultConfigPath() = %q, want %q", got, want)
	}
}

func TestConfig_Validate_Success(t *testing.T) {
	cfg := &Config{}
	cfg.Wechat.AppID = "wx123456"
	cfg.Wechat.Secret = "secret123"
	cfg.Image.MaxWidth = 1920
	cfg.Image.MaxSizeMB = 5
	cfg.Image.HTTPTimeout = 30

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestConfig_Validate_MissingAppID(t *testing.T) {
	cfg := &Config{}
	cfg.Wechat.Secret = "secret123"
	cfg.Image.MaxWidth = 1920
	cfg.Image.MaxSizeMB = 5
	cfg.Image.HTTPTimeout = 30

	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should return error for missing AppID")
	}

	configErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("Error type = %T, want *ConfigError", err)
	}

	if configErr.Field != "WechatAppID" {
		t.Errorf("Error field = %v, want WechatAppID", configErr.Field)
	}
}

func TestConfig_Validate_MissingSecret(t *testing.T) {
	cfg := &Config{}
	cfg.Wechat.AppID = "wx123456"
	cfg.Image.MaxWidth = 1920
	cfg.Image.MaxSizeMB = 5
	cfg.Image.HTTPTimeout = 30

	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should return error for missing Secret")
	}

	configErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("Error type = %T, want *ConfigError", err)
	}

	if configErr.Field != "WechatSecret" {
		t.Errorf("Error field = %v, want WechatSecret", configErr.Field)
	}
}

func TestConfig_Validate_InvalidImageWidth(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		wantErr bool
	}{
		{"zero (unset)", 0, false},
		{"too small", 50, true},
		{"too large", 15000, true},
		{"valid", 1920, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Wechat.AppID = "wx123456"
			cfg.Wechat.Secret = "secret123"
			cfg.Image.MaxWidth = tt.width
			cfg.Image.MaxSizeMB = 5
			cfg.Image.HTTPTimeout = 30

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Validate_InvalidTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout int
		wantErr bool
	}{
		{"zero (unset)", 0, false},
		{"too large", 500, true},
		{"valid", 30, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Wechat.AppID = "wx123456"
			cfg.Wechat.Secret = "secret123"
			cfg.Image.MaxWidth = 1920
			cfg.Image.MaxSizeMB = 5
			cfg.Image.HTTPTimeout = tt.timeout

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSaveConfig_JSON(t *testing.T) {
	cfg := &Config{}
	cfg.Wechat.Name = "Test Account"
	cfg.Wechat.AppID = "wx123456"
	cfg.Wechat.Secret = "secret123"
	cfg.Wechat.KeyWords = []string{"tea", "culture"}
	cfg.Wechat.Style = "dan-koe"
	cfg.Article.Image.Key = "test_key"
	cfg.Image.MaxWidth = 1920
	cfg.Image.MaxSizeMB = 5
	cfg.Image.HTTPTimeout = 30

	tmpFile := filepath.Join(t.TempDir(), "test.json")
	err := SaveConfig(tmpFile, cfg)
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	content := string(data)
	if !contains(content, "wx123456") {
		t.Error("Saved config should contain appid")
	}
	if !contains(content, "test_key") {
		t.Error("Saved config should contain image API key")
	}
}

func TestConfig_ValidateForImageGeneration(t *testing.T) {
	apiCfg := &ImageAPI{}

	err := ValidateForImageGeneration(apiCfg)
	if err == nil {
		t.Error("ValidateForImageGeneration() should return error for missing API key")
	}

	apiCfg.Key = "test_key"
	err = ValidateForImageGeneration(apiCfg)
	if err != nil {
		t.Errorf("ValidateForImageGeneration() error = %v, want nil", err)
	}

	// nil apiCfg should also return error
	err = ValidateForImageGeneration(nil)
	if err == nil {
		t.Error("ValidateForImageGeneration() should return error for nil apiCfg")
	}
}

func TestFindConfigFile_PriorityOrder(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer os.Chdir(origDir)

	// Setup: CWD dir with config
	cwdDir := t.TempDir()
	if err := os.Chdir(cwdDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	if err := os.MkdirAll(ConfigDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	cwdConfig := filepath.Join(ConfigDir, ConfigFileName)
	if err := os.WriteFile(cwdConfig, []byte(`{}`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Setup: home dir with config
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	if err := os.MkdirAll(filepath.Join(homeDir, ConfigDir), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	homeConfig := filepath.Join(homeDir, ConfigDir, ConfigFileName)
	if err := os.WriteFile(homeConfig, []byte(`{}`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// CWD config takes priority over home config
	if got := findConfigFile(); got != cwdConfig {
		t.Errorf("findConfigFile() = %q, want CWD config %q", got, cwdConfig)
	}

	// Remove CWD config -> home config should be found (before exe-relative)
	if err := os.Remove(cwdConfig); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if got := findConfigFile(); got != homeConfig {
		t.Errorf("findConfigFile() = %q, want home config %q", got, homeConfig)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
