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
  "article": {
    "image": {
      "compress": true,
      "max_width": 2560,
      "max_size_mb": 5
    }
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
	if cfg.Article.Image.Compress != true {
		t.Errorf("Article.Image.Compress = %v, want true", cfg.Article.Image.Compress)
	}
	if cfg.Article.Image.MaxWidth != 2560 {
		t.Errorf("Article.Image.MaxWidth = %v, want 2560", cfg.Article.Image.MaxWidth)
	}
	if cfg.Article.Image.MaxSizeMB != 5 {
		t.Errorf("Article.Image.MaxSizeMB = %v, want 5MB", cfg.Article.Image.MaxSizeMB)
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
    "keywords": ["tea", "culture"]
  },
  "article": {
    "style": "dan-koe",
    "theme": "apple",
    "image": {
      "key": "test_article_key",
      "base_url": "https://test.api.com",
      "provider": "gemini",
      "model": "gemini-3-pro-image-preview",
      "size": "16:9",
      "compress": false,
      "max_width": 2560,
      "max_size_mb": 10
    }
  },
  "post": {
    "image": {
      "key": "test_post_key",
      "provider": "gemini",
      "model": "gemini-3-pro-image-preview",
      "size": "3:4"
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
	if cfg.Wechat.Name != "Test Account" {
		t.Errorf("WechatName = %v, want Test Account", cfg.Wechat.Name)
	}
	if len(cfg.Wechat.Keywords) != 2 || cfg.Wechat.Keywords[0] != "tea" || cfg.Wechat.Keywords[1] != "culture" {
		t.Errorf("WechatKeyWords = %v, want [tea culture]", cfg.Wechat.Keywords)
	}
	if cfg.Article.Style != "dan-koe" {
		t.Errorf("DefaultArticleStyle = %v, want dan-koe", cfg.Article.Style)
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
	if cfg.Article.Image.Compress != false {
		t.Errorf("Article.Image.Compress = %v, want false", cfg.Article.Image.Compress)
	}
	if cfg.Article.Image.MaxWidth != 2560 {
		t.Errorf("Article.Image.MaxWidth = %v, want 2560", cfg.Article.Image.MaxWidth)
	}
	if cfg.Article.Image.MaxSizeMB != 10 {
		t.Errorf("Article.Image.MaxSizeMB = %v, want 10MB", cfg.Article.Image.MaxSizeMB)
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
	cfg.Article.Image.MaxWidth = 1920
	cfg.Article.Image.MaxSizeMB = 5

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestConfig_Validate_MissingAppID(t *testing.T) {
	cfg := &Config{}
	cfg.Wechat.Secret = "secret123"
	cfg.Article.Image.MaxWidth = 1920
	cfg.Article.Image.MaxSizeMB = 5

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
	cfg.Article.Image.MaxWidth = 1920
	cfg.Article.Image.MaxSizeMB = 5

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
			cfg.Article.Image.MaxWidth = tt.width
			cfg.Article.Image.MaxSizeMB = 5

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
	cfg.Wechat.Keywords = []string{"tea", "culture"}
	cfg.Article.Style = "dan-koe"
	cfg.Article.Image.Key = "test_key"
	cfg.Article.Image.MaxWidth = 1920
	cfg.Article.Image.MaxSizeMB = 5

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

func TestConfig_PostImageCount_Default(t *testing.T) {
	cfg := &Config{}
	if got := cfg.PostImageCount(); got != 4 {
		t.Errorf("PostImageCount() = %d, want 4 (default)", got)
	}
}

func TestConfig_PostImageCount_Custom(t *testing.T) {
	tests := []struct {
		name  string
		count int
		want  int
	}{
		{"set to 3", 3, 3},
		{"set to 5", 5, 5},
		{"set to 1", 1, 1},
		{"set to 20", 20, 20},
		{"zero uses default", 0, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Post.Count = tt.count
			if got := cfg.PostImageCount(); got != tt.want {
				t.Errorf("PostImageCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestConfig_Validate_InvalidPostImageCount(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		wantErr bool
	}{
		{"zero (unset)", 0, false},
		{"valid min", 1, false},
		{"valid mid", 5, false},
		{"valid max", 20, false},
		{"too small", -1, true},
		{"too large", 21, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Wechat.AppID = "wx123456"
			cfg.Wechat.Secret = "secret123"
			cfg.Post.Count = tt.count

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				configErr, ok := err.(*ConfigError)
				if !ok {
					t.Fatalf("Error type = %T, want *ConfigError", err)
				}
				if configErr.Field != "PostImageCount" {
					t.Errorf("Error field = %v, want PostImageCount", configErr.Field)
				}
			}
		})
	}
}

func TestLoad_JSONConfig_WithPostImageCount(t *testing.T) {
	configContent := `{
  "wechat": {
    "appid": "wx123456",
    "secret": "secret123"
  },
  "post": {
    "count": 5,
    "image": {
      "key": "test_key",
      "size": "3:4"
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

	if cfg.Post.Count != 5 {
		t.Errorf("Post.Count = %d, want 5", cfg.Post.Count)
	}
	if cfg.PostImageCount() != 5 {
		t.Errorf("PostImageCount() = %d, want 5", cfg.PostImageCount())
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

func TestLoad_JSONConfig_WithStylePrompt(t *testing.T) {
	configContent := `{
  "wechat": {
    "appid": "wx123456",
    "secret": "secret123"
  },
  "post": {
    "image": {
      "key": "test_key",
      "style_prompt": "扁平插画风格，莫兰迪色系，圆角卡片"
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

	want := "扁平插画风格，莫兰迪色系，圆角卡片"
	if cfg.Post.Image.StylePrompt != want {
		t.Errorf("Post.Image.StylePrompt = %q, want %q", cfg.Post.Image.StylePrompt, want)
	}
}

func TestNewDefaultConfig(t *testing.T) {
	c := NewDefaultConfig()

	if c.Article.Style != DefaultArticleStyle {
		t.Errorf("Wechat.Style = %q, want %q", c.Article.Style, DefaultArticleStyle)
	}
	if c.Article.Theme != DefaultArticleTheme {
		t.Errorf("Article.Theme = %q, want %q", c.Article.Theme, DefaultArticleTheme)
	}
	if c.Article.Image.Provider != DefaultImageProvider {
		t.Errorf("Article.Image.Provider = %q, want %q", c.Article.Image.Provider, DefaultImageProvider)
	}
	if c.Article.Image.Size != DefaultArticleImageSize {
		t.Errorf("Article.Image.Size = %q, want %q", c.Article.Image.Size, DefaultArticleImageSize)
	}
	if c.Article.Image.Compress != true {
		t.Errorf("Article.Image.Compress = %v, want true", c.Article.Image.Compress)
	}
	if c.Article.Image.MaxWidth != DefaultImageMaxWidth {
		t.Errorf("Article.Image.MaxWidth = %d, want %d", c.Article.Image.MaxWidth, DefaultImageMaxWidth)
	}
	if c.Article.Image.MaxSizeMB != DefaultImageMaxSizeMB {
		t.Errorf("Article.Image.MaxSizeMB = %d, want %d", c.Article.Image.MaxSizeMB, DefaultImageMaxSizeMB)
	}
	if c.Post.Count != DefaultPostImageCount {
		t.Errorf("Post.Count = %d, want %d", c.Post.Count, DefaultPostImageCount)
	}
	if c.Post.Image.Provider != DefaultImageProvider {
		t.Errorf("Post.Image.Provider = %q, want %q", c.Post.Image.Provider, DefaultImageProvider)
	}
	if c.Post.Image.Size != DefaultPostImageSize {
		t.Errorf("Post.Image.Size = %q, want %q", c.Post.Image.Size, DefaultPostImageSize)
	}
	if c.Post.Image.Compress != true {
		t.Errorf("Post.Image.Compress = %v, want true", c.Post.Image.Compress)
	}
	if c.Post.Image.MaxWidth != DefaultImageMaxWidth {
		t.Errorf("Post.Image.MaxWidth = %d, want %d", c.Post.Image.MaxWidth, DefaultImageMaxWidth)
	}
	if c.Post.Image.MaxSizeMB != DefaultImageMaxSizeMB {
		t.Errorf("Post.Image.MaxSizeMB = %d, want %d", c.Post.Image.MaxSizeMB, DefaultImageMaxSizeMB)
	}
	// BaseURL should not be set for Gemini (uses SDK, not HTTP)
	if c.Article.Image.BaseURL != "" {
		t.Errorf("Article.Image.BaseURL = %q, want empty (Gemini uses SDK)", c.Article.Image.BaseURL)
	}
	if c.Post.Image.BaseURL != "" {
		t.Errorf("Post.Image.BaseURL = %q, want empty (Gemini uses SDK)", c.Post.Image.BaseURL)
	}
}

func TestNewDefaultConfig_ConsistentWithRuntimeDefaults(t *testing.T) {
	c := NewDefaultConfig()

	// ArticleImageSize() should return the value set in NewDefaultConfig
	if got := c.ArticleImageSize(); got != DefaultArticleImageSize {
		t.Errorf("ArticleImageSize() = %q, want %q (DefaultArticleImageSize)", got, DefaultArticleImageSize)
	}

	// PostImageSize() should return the value set in NewDefaultConfig
	if got := c.PostImageSize(); got != DefaultPostImageSize {
		t.Errorf("PostImageSize() = %q, want %q (DefaultPostImageSize)", got, DefaultPostImageSize)
	}

	// PostImageCount() should return the value set in NewDefaultConfig
	if got := c.PostImageCount(); got != DefaultPostImageCount {
		t.Errorf("PostImageCount() = %d, want %d (DefaultPostImageCount)", got, DefaultPostImageCount)
	}

	// Empty config should also use the same defaults
	empty := &Config{}
	if got := empty.ArticleImageSize(); got != DefaultArticleImageSize {
		t.Errorf("empty.ArticleImageSize() = %q, want %q", got, DefaultArticleImageSize)
	}
	if got := empty.PostImageSize(); got != DefaultPostImageSize {
		t.Errorf("empty.PostImageSize() = %q, want %q", got, DefaultPostImageSize)
	}
	if got := empty.PostImageCount(); got != DefaultPostImageCount {
		t.Errorf("empty.PostImageCount() = %d, want %d", got, DefaultPostImageCount)
	}
}

func TestWatermarkConfig_Validate(t *testing.T) {
	tests := []struct {
		name         string
		enable       bool
		margin       int
		wantErr      bool
		wantErrField string
	}{
		{"disabled no margin", false, 0, false, ""},
		{"disabled with margin", false, 20, false, ""},
		{"enabled with valid margin", true, 20, false, ""},
		{"enabled zero margin", true, 0, true, "WatermarkMargin"},
		{"margin too small", false, 0, false, ""},
		{"margin 1 (min)", false, 1, false, ""},
		{"margin 500 (max)", false, 500, false, ""},
		{"margin 501 (too large)", false, 501, true, "WatermarkMargin"},
		{"enabled margin 501", true, 501, true, "WatermarkMargin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := WatermarkConfig{Enable: tt.enable, Margin: tt.margin}
			err := w.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.wantErrField != "" {
				configErr, ok := err.(*ConfigError)
				if !ok {
					t.Fatalf("Error type = %T, want *ConfigError", err)
				}
				if configErr.Field != tt.wantErrField {
					t.Errorf("Error field = %q, want %q", configErr.Field, tt.wantErrField)
				}
			}
		})
	}
}

func TestLoad_JSONConfig_WithWatermark(t *testing.T) {
	configContent := `{
  "wechat": {
    "appid": "wx123456",
    "secret": "secret123"
  },
  "article": {
    "image": {
      "watermark": {
        "enable": true,
        "margin": 20
      }
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

	if !cfg.Article.Image.Watermark.Enable {
		t.Errorf("Article.Image.Watermark.Enable = false, want true")
	}
	if cfg.Article.Image.Watermark.Margin != 20 {
		t.Errorf("Article.Image.Watermark.Margin = %d, want 20", cfg.Article.Image.Watermark.Margin)
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
