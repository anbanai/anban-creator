package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_DefaultConfig(t *testing.T) {
	// 使用临时配置文件，避免加载用户配置
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anbanwriter.json")

	// 创建一个最小的配置文件（只包含必需的微信配置）
	minimalConfig := `{
  "wechat": {
    "appid": "test_appid",
    "secret": "test_secret",
    "article": {
      "content": {
        "image": {
          "compress": true,
          "max_width": 2560,
          "max_size_mb": 5
        }
      }
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

	if cfg.Wechat.Article.Theme != "" {
		t.Errorf("Wechat.Article.Theme = %v, want empty (not set in config)", cfg.Wechat.Article.Theme)
	}
	if cfg.Wechat.Article.Content.Image.Compress != true {
		t.Errorf("Wechat.Article.Content.Image.Compress = %v, want true", cfg.Wechat.Article.Content.Image.Compress)
	}
	if cfg.Wechat.Article.Content.Image.MaxWidth != 2560 {
		t.Errorf("Wechat.Article.Content.Image.MaxWidth = %v, want 2560", cfg.Wechat.Article.Content.Image.MaxWidth)
	}
	if cfg.Wechat.Article.Content.Image.MaxSizeMB != 5 {
		t.Errorf("Wechat.Article.Content.Image.MaxSizeMB = %v, want 5MB", cfg.Wechat.Article.Content.Image.MaxSizeMB)
	}
	if cfg.Wechat.Article.Content.Image.Provider != "" {
		t.Errorf("Wechat.Article.Content.Image.Provider = %v, want empty (not set in config)", cfg.Wechat.Article.Content.Image.Provider)
	}
	if cfg.Wechat.Article.Content.Image.BaseURL != "" {
		t.Errorf("Wechat.Article.Content.Image.BaseURL = %v, want empty (not set in config)", cfg.Wechat.Article.Content.Image.BaseURL)
	}
	if cfg.Wechat.Article.Content.Image.Model != "" {
		t.Errorf("Wechat.Article.Content.Image.Model = %v, want empty (not set in config)", cfg.Wechat.Article.Content.Image.Model)
	}
	if cfg.Wechat.Article.Content.Image.Size != "" {
		t.Errorf("Wechat.Article.Content.Image.Size = %v, want empty (not set in config)", cfg.Wechat.Article.Content.Image.Size)
	}
}

func TestLoad_JSONConfig_Full(t *testing.T) {
	configContent := `{
  "name": "Test Account",
  "keywords": ["tea", "culture"],
  "wechat": {
    "appid": "wx123456",
    "secret": "secret123",
    "article": {
      "style": "dan-koe",
      "theme": "apple",
      "content": {
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
      }
    },
    "xls": {
      "content": {
        "image": {
          "key": "test_xls_key",
          "provider": "gemini",
          "model": "gemini-3-pro-image-preview",
          "size": "3:4"
        }
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

	if cfg.Wechat.AppID != "wx123456" {
		t.Errorf("WechatAppID = %v, want wx123456", cfg.Wechat.AppID)
	}
	if cfg.Wechat.Secret != "secret123" {
		t.Errorf("WechatSecret = %v, want secret123", cfg.Wechat.Secret)
	}
	if cfg.Name != "Test Account" {
		t.Errorf("Name = %v, want Test Account", cfg.Name)
	}
	if len(cfg.Keywords) != 2 || cfg.Keywords[0] != "tea" || cfg.Keywords[1] != "culture" {
		t.Errorf("Keywords = %v, want [tea culture]", cfg.Keywords)
	}
	if cfg.Wechat.Article.Style != "dan-koe" {
		t.Errorf("Wechat.Article.Style = %v, want dan-koe", cfg.Wechat.Article.Style)
	}
	if cfg.Wechat.Article.Theme != "apple" {
		t.Errorf("Wechat.Article.Theme = %v, want apple", cfg.Wechat.Article.Theme)
	}
	if cfg.Wechat.Article.Content.Image.Key != "test_article_key" {
		t.Errorf("Wechat.Article.Content.Image.Key = %v, want test_article_key", cfg.Wechat.Article.Content.Image.Key)
	}
	if cfg.Wechat.Article.Content.Image.BaseURL != "https://test.api.com" {
		t.Errorf("Wechat.Article.Content.Image.BaseURL = %v, want https://test.api.com", cfg.Wechat.Article.Content.Image.BaseURL)
	}
	if cfg.Wechat.Article.Content.Image.Provider != "gemini" {
		t.Errorf("Wechat.Article.Content.Image.Provider = %v, want gemini", cfg.Wechat.Article.Content.Image.Provider)
	}
	if cfg.Wechat.Article.Content.Image.Model != "gemini-3-pro-image-preview" {
		t.Errorf("Wechat.Article.Content.Image.Model = %v, want gemini-3-pro-image-preview", cfg.Wechat.Article.Content.Image.Model)
	}
	if cfg.Wechat.Article.Content.Image.Size != "16:9" {
		t.Errorf("Wechat.Article.Content.Image.Size = %v, want 16:9", cfg.Wechat.Article.Content.Image.Size)
	}
	if cfg.Wechat.Xls.Content.Image.Key != "test_xls_key" {
		t.Errorf("Wechat.Xls.Content.Image.Key = %v, want test_xls_key", cfg.Wechat.Xls.Content.Image.Key)
	}
	if cfg.Wechat.Xls.Content.Image.Size != "3:4" {
		t.Errorf("Wechat.Xls.Content.Image.Size = %v, want 3:4", cfg.Wechat.Xls.Content.Image.Size)
	}
	if cfg.Wechat.Article.Content.Image.Compress != false {
		t.Errorf("Wechat.Article.Content.Image.Compress = %v, want false", cfg.Wechat.Article.Content.Image.Compress)
	}
	if cfg.Wechat.Article.Content.Image.MaxWidth != 2560 {
		t.Errorf("Wechat.Article.Content.Image.MaxWidth = %v, want 2560", cfg.Wechat.Article.Content.Image.MaxWidth)
	}
	if cfg.Wechat.Article.Content.Image.MaxSizeMB != 10 {
		t.Errorf("Wechat.Article.Content.Image.MaxSizeMB = %v, want 10MB", cfg.Wechat.Article.Content.Image.MaxSizeMB)
	}
}

func TestLoad_JSONConfig(t *testing.T) {
	configContent := `{
  "wechat": {
    "appid": "wx123456",
    "secret": "secret123",
    "article": {
      "content": {
        "image": {
          "key": "test_image_key",
          "base_url": "https://test.api.com"
        }
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

	if cfg.Wechat.AppID != "wx123456" {
		t.Errorf("WechatAppID = %v, want wx123456", cfg.Wechat.AppID)
	}
	if cfg.Wechat.Secret != "secret123" {
		t.Errorf("WechatSecret = %v, want secret123", cfg.Wechat.Secret)
	}
	if cfg.Wechat.Article.Content.Image.Key != "test_image_key" {
		t.Errorf("Wechat.Article.Content.Image.Key = %v, want test_image_key", cfg.Wechat.Article.Content.Image.Key)
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
	want := filepath.Join(".anbanwriter", "settings.json")
	if got := DefaultConfigPath(); got != want {
		t.Errorf("DefaultConfigPath() = %q, want %q", got, want)
	}
}

func TestConfig_Validate_Success(t *testing.T) {
	cfg := &Config{}
	cfg.Wechat.AppID = "wx123456"
	cfg.Wechat.Secret = "secret123"
	cfg.Wechat.Article.Content.Image.MaxWidth = 1920
	cfg.Wechat.Article.Content.Image.MaxSizeMB = 5

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestConfig_Validate_MissingAppID(t *testing.T) {
	cfg := &Config{}
	cfg.Wechat.Secret = "secret123"
	cfg.Wechat.Article.Content.Image.MaxWidth = 1920
	cfg.Wechat.Article.Content.Image.MaxSizeMB = 5

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
	cfg.Wechat.Article.Content.Image.MaxWidth = 1920
	cfg.Wechat.Article.Content.Image.MaxSizeMB = 5

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
			cfg.Wechat.Article.Content.Image.MaxWidth = tt.width
			cfg.Wechat.Article.Content.Image.MaxSizeMB = 5

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSaveConfig_JSON(t *testing.T) {
	cfg := &Config{}
	cfg.Name = "Test Account"
	cfg.Wechat.AppID = "wx123456"
	cfg.Wechat.Secret = "secret123"
	cfg.Keywords = []string{"tea", "culture"}
	cfg.Wechat.Article.Style = "dan-koe"
	cfg.Wechat.Article.Content.Image.Key = "test_key"
	cfg.Wechat.Article.Content.Image.MaxWidth = 1920
	cfg.Wechat.Article.Content.Image.MaxSizeMB = 5

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

func TestConfig_XlsImageCount_Default(t *testing.T) {
	cfg := &Config{}
	if got := cfg.XlsImageCount(); got != 4 {
		t.Errorf("XlsImageCount() = %d, want 4 (default)", got)
	}
}

func TestConfig_XlsImageCount_Custom(t *testing.T) {
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
			cfg.Wechat.Xls.Content.Count = tt.count
			if got := cfg.XlsImageCount(); got != tt.want {
				t.Errorf("XlsImageCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestConfig_Validate_InvalidXlsImageCount(t *testing.T) {
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
			cfg.Wechat.Xls.Content.Count = tt.count

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				configErr, ok := err.(*ConfigError)
				if !ok {
					t.Fatalf("Error type = %T, want *ConfigError", err)
				}
				if configErr.Field != "XlsImageCount" {
					t.Errorf("Error field = %v, want XlsImageCount", configErr.Field)
				}
			}
		})
	}
}

func TestLoad_JSONConfig_WithXlsImageCount(t *testing.T) {
	configContent := `{
  "wechat": {
    "appid": "wx123456",
    "secret": "secret123",
    "xls": {
      "content": {
        "count": 5,
        "image": {
          "key": "test_key",
          "size": "3:4"
        }
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

	if cfg.Wechat.Xls.Content.Count != 5 {
		t.Errorf("Wechat.Xls.Content.Count = %d, want 5", cfg.Wechat.Xls.Content.Count)
	}
	if cfg.XlsImageCount() != 5 {
		t.Errorf("XlsImageCount() = %d, want 5", cfg.XlsImageCount())
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
    "secret": "secret123",
    "xls": {
      "content": {
        "image": {
          "key": "test_key",
          "style_prompt": "扁平插画风格，莫兰迪色系，圆角卡片"
        }
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

	want := "扁平插画风格，莫兰迪色系，圆角卡片"
	if cfg.Wechat.Xls.Content.Image.StylePrompt != want {
		t.Errorf("Wechat.Xls.Content.Image.StylePrompt = %q, want %q", cfg.Wechat.Xls.Content.Image.StylePrompt, want)
	}
}

func TestNewDefaultConfig(t *testing.T) {
	c := NewDefaultConfig()

	if c.Wechat.Article.Style != DefaultArticleStyle {
		t.Errorf("Wechat.Article.Style = %q, want %q", c.Wechat.Article.Style, DefaultArticleStyle)
	}
	if c.Wechat.Article.Theme != DefaultArticleTheme {
		t.Errorf("Wechat.Article.Theme = %q, want %q", c.Wechat.Article.Theme, DefaultArticleTheme)
	}
	if c.Wechat.Article.Content.Image.Provider != DefaultImageProvider {
		t.Errorf("Wechat.Article.Content.Image.Provider = %q, want %q", c.Wechat.Article.Content.Image.Provider, DefaultImageProvider)
	}
	if c.Wechat.Article.Content.Image.Size != DefaultArticleImageSize {
		t.Errorf("Wechat.Article.Content.Image.Size = %q, want %q", c.Wechat.Article.Content.Image.Size, DefaultArticleImageSize)
	}
	if c.Wechat.Article.Content.Image.Compress != true {
		t.Errorf("Wechat.Article.Content.Image.Compress = %v, want true", c.Wechat.Article.Content.Image.Compress)
	}
	if c.Wechat.Article.Content.Image.MaxWidth != DefaultImageMaxWidth {
		t.Errorf("Wechat.Article.Content.Image.MaxWidth = %d, want %d", c.Wechat.Article.Content.Image.MaxWidth, DefaultImageMaxWidth)
	}
	if c.Wechat.Article.Content.Image.MaxSizeMB != DefaultImageMaxSizeMB {
		t.Errorf("Wechat.Article.Content.Image.MaxSizeMB = %d, want %d", c.Wechat.Article.Content.Image.MaxSizeMB, DefaultImageMaxSizeMB)
	}
	if c.Wechat.Xls.Content.Count != DefaultXlsImageCount {
		t.Errorf("Wechat.Xls.Content.Count = %d, want %d", c.Wechat.Xls.Content.Count, DefaultXlsImageCount)
	}
	if c.Wechat.Xls.Content.Image.Provider != DefaultImageProvider {
		t.Errorf("Wechat.Xls.Content.Image.Provider = %q, want %q", c.Wechat.Xls.Content.Image.Provider, DefaultImageProvider)
	}
	if c.Wechat.Xls.Content.Image.Size != DefaultXlsImageSize {
		t.Errorf("Wechat.Xls.Content.Image.Size = %q, want %q", c.Wechat.Xls.Content.Image.Size, DefaultXlsImageSize)
	}
	if c.Wechat.Xls.Content.Image.Compress != true {
		t.Errorf("Wechat.Xls.Content.Image.Compress = %v, want true", c.Wechat.Xls.Content.Image.Compress)
	}
	if c.Wechat.Xls.Content.Image.MaxWidth != DefaultImageMaxWidth {
		t.Errorf("Wechat.Xls.Content.Image.MaxWidth = %d, want %d", c.Wechat.Xls.Content.Image.MaxWidth, DefaultImageMaxWidth)
	}
	if c.Wechat.Xls.Content.Image.MaxSizeMB != DefaultImageMaxSizeMB {
		t.Errorf("Wechat.Xls.Content.Image.MaxSizeMB = %d, want %d", c.Wechat.Xls.Content.Image.MaxSizeMB, DefaultImageMaxSizeMB)
	}
	// BaseURL should not be set for Gemini (uses SDK, not HTTP)
	if c.Wechat.Article.Content.Image.BaseURL != "" {
		t.Errorf("Wechat.Article.Content.Image.BaseURL = %q, want empty (Gemini uses SDK)", c.Wechat.Article.Content.Image.BaseURL)
	}
	if c.Wechat.Xls.Content.Image.BaseURL != "" {
		t.Errorf("Wechat.Xls.Content.Image.BaseURL = %q, want empty (Gemini uses SDK)", c.Wechat.Xls.Content.Image.BaseURL)
	}
}

func TestNewDefaultConfig_ConsistentWithRuntimeDefaults(t *testing.T) {
	c := NewDefaultConfig()

	// ArticleImageSize() should return the value set in NewDefaultConfig
	if got := c.ArticleImageSize(); got != DefaultArticleImageSize {
		t.Errorf("ArticleImageSize() = %q, want %q (DefaultArticleImageSize)", got, DefaultArticleImageSize)
	}

	// XlsImageSize() should return the value set in NewDefaultConfig
	if got := c.XlsImageSize(); got != DefaultXlsImageSize {
		t.Errorf("XlsImageSize() = %q, want %q (DefaultXlsImageSize)", got, DefaultXlsImageSize)
	}

	// XlsImageCount() should return the value set in NewDefaultConfig
	if got := c.XlsImageCount(); got != DefaultXlsImageCount {
		t.Errorf("XlsImageCount() = %d, want %d (DefaultXlsImageCount)", got, DefaultXlsImageCount)
	}

	// Empty config should also use the same defaults
	empty := &Config{}
	if got := empty.ArticleImageSize(); got != DefaultArticleImageSize {
		t.Errorf("empty.ArticleImageSize() = %q, want %q", got, DefaultArticleImageSize)
	}
	if got := empty.XlsImageSize(); got != DefaultXlsImageSize {
		t.Errorf("empty.XlsImageSize() = %q, want %q", got, DefaultXlsImageSize)
	}
	if got := empty.XlsImageCount(); got != DefaultXlsImageCount {
		t.Errorf("empty.XlsImageCount() = %d, want %d", got, DefaultXlsImageCount)
	}
}

func TestMergeImageAPI(t *testing.T) {
	t.Run("base fields take priority", func(t *testing.T) {
		base := ImageAPI{Key: "base-key", Provider: "openai", Model: "dall-e-3"}
		fallback := ImageAPI{Key: "fallback-key", Provider: "gemini", Model: "gemini-model", Size: "3:4"}
		result := mergeImageAPI(base, fallback)
		if result.Key != "base-key" {
			t.Errorf("Key = %q, want base-key", result.Key)
		}
		if result.Provider != "openai" {
			t.Errorf("Provider = %q, want openai", result.Provider)
		}
		if result.Model != "dall-e-3" {
			t.Errorf("Model = %q, want dall-e-3", result.Model)
		}
		// fallback fills missing size
		if result.Size != "3:4" {
			t.Errorf("Size = %q, want 3:4", result.Size)
		}
	})

	t.Run("empty base uses fallback", func(t *testing.T) {
		base := ImageAPI{}
		fallback := ImageAPI{
			Key:       "fb-key",
			Provider:  "gemini",
			Model:     "gemini-model",
			Size:      "3:4",
			MaxWidth:  1920,
			MaxSizeMB: 5,
			Compress:  true,
		}
		result := mergeImageAPI(base, fallback)
		if result.Key != "fb-key" {
			t.Errorf("Key = %q, want fb-key", result.Key)
		}
		if result.Provider != "gemini" {
			t.Errorf("Provider = %q, want gemini", result.Provider)
		}
		if result.Size != "3:4" {
			t.Errorf("Size = %q, want 3:4", result.Size)
		}
		if result.MaxWidth != 1920 {
			t.Errorf("MaxWidth = %d, want 1920", result.MaxWidth)
		}
		if !result.Compress {
			t.Errorf("Compress = false, want true")
		}
	})

	t.Run("partial merge - only missing fields filled", func(t *testing.T) {
		base := ImageAPI{Key: "my-key", Size: "16:9"}
		fallback := ImageAPI{Key: "other-key", Size: "3:4", Provider: "volcengine", MaxWidth: 1920}
		result := mergeImageAPI(base, fallback)
		if result.Key != "my-key" {
			t.Errorf("Key = %q, want my-key", result.Key)
		}
		if result.Size != "16:9" {
			t.Errorf("Size = %q, want 16:9", result.Size)
		}
		if result.Provider != "volcengine" {
			t.Errorf("Provider = %q, want volcengine (from fallback)", result.Provider)
		}
		if result.MaxWidth != 1920 {
			t.Errorf("MaxWidth = %d, want 1920 (from fallback)", result.MaxWidth)
		}
	})
}

func TestResolvedXlsContentImage(t *testing.T) {
	t.Run("no rednote config returns wechat.xls", func(t *testing.T) {
		cfg := &Config{}
		cfg.Wechat.Xls.Content.Image.Key = "xls-key"
		cfg.Wechat.Xls.Content.Image.Provider = "openai"
		result := cfg.ResolvedXlsContentImage()
		if result.Key != "xls-key" {
			t.Errorf("Key = %q, want xls-key", result.Key)
		}
		if result.Provider != "openai" {
			t.Errorf("Provider = %q, want openai", result.Provider)
		}
	})

	t.Run("rednote fills missing wechat.xls fields", func(t *testing.T) {
		cfg := &Config{}
		cfg.Wechat.Xls.Content.Image = ImageAPI{} // empty
		cfg.Rednote = &RednoteConfig{}
		cfg.Rednote.Content.Image = ImageAPI{
			Key:      "rednote-key",
			Provider: "gemini",
			Size:     "3:4:1K",
		}
		result := cfg.ResolvedXlsContentImage()
		if result.Key != "rednote-key" {
			t.Errorf("Key = %q, want rednote-key (from rednote)", result.Key)
		}
		if result.Provider != "gemini" {
			t.Errorf("Provider = %q, want gemini (from rednote)", result.Provider)
		}
		if result.Size != "3:4:1K" {
			t.Errorf("Size = %q, want 3:4:1K (from rednote)", result.Size)
		}
	})

	t.Run("wechat.xls takes priority over rednote", func(t *testing.T) {
		cfg := &Config{}
		cfg.Wechat.Xls.Content.Image = ImageAPI{
			Key:      "xls-key",
			Provider: "openrouter",
			Size:     "16:9",
		}
		cfg.Rednote = &RednoteConfig{}
		cfg.Rednote.Content.Image = ImageAPI{
			Key:      "rednote-key",
			Provider: "gemini",
			Size:     "3:4:1K",
		}
		result := cfg.ResolvedXlsContentImage()
		if result.Key != "xls-key" {
			t.Errorf("Key = %q, want xls-key (wechat.xls priority)", result.Key)
		}
		if result.Provider != "openrouter" {
			t.Errorf("Provider = %q, want openrouter (wechat.xls priority)", result.Provider)
		}
		if result.Size != "16:9" {
			t.Errorf("Size = %q, want 16:9 (wechat.xls priority)", result.Size)
		}
	})
}

func TestResolvedXlsCoverImage(t *testing.T) {
	t.Run("rednote cover fills missing wechat.xls.cover", func(t *testing.T) {
		cfg := &Config{}
		cfg.Rednote = &RednoteConfig{}
		cfg.Rednote.Cover.Image = ImageAPI{
			Key:      "rednote-cover-key",
			Provider: "volcengine",
			Size:     "3:4",
		}
		result := cfg.ResolvedXlsCoverImage()
		if result.Key != "rednote-cover-key" {
			t.Errorf("Key = %q, want rednote-cover-key", result.Key)
		}
		if result.Provider != "volcengine" {
			t.Errorf("Provider = %q, want volcengine", result.Provider)
		}
	})
}

func TestXlsImageSize_RednoteFallback(t *testing.T) {
	tests := []struct {
		name        string
		xlsSize     string
		rednoteSize string
		hasXhs      bool
		wantSize    string
	}{
		{"wechat.xls has size", "3:4", "3:4:1K", true, "3:4"},
		{"fallback to rednote", "", "3:4:1K", true, "3:4:1K"},
		{"no rednote", "", "", false, DefaultXlsImageSize},
		{"both empty", "", "", true, DefaultXlsImageSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Wechat.Xls.Content.Image.Size = tt.xlsSize
			if tt.hasXhs {
				cfg.Rednote = &RednoteConfig{}
				cfg.Rednote.Content.Image.Size = tt.rednoteSize
			}
			if got := cfg.XlsImageSize(); got != tt.wantSize {
				t.Errorf("XlsImageSize() = %q, want %q", got, tt.wantSize)
			}
		})
	}
}

func TestXlsImageCount_RednoteFallback(t *testing.T) {
	tests := []struct {
		name         string
		xlsCount     int
		rednoteCount int
		hasXhs       bool
		wantCount    int
	}{
		{"wechat.xls has count", 4, 6, true, 4},
		{"fallback to rednote", 0, 6, true, 6},
		{"no rednote", 0, 0, false, DefaultXlsImageCount},
		{"both zero", 0, 0, true, DefaultXlsImageCount},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Wechat.Xls.Content.Count = tt.xlsCount
			if tt.hasXhs {
				cfg.Rednote = &RednoteConfig{}
				cfg.Rednote.Content.Count = tt.rednoteCount
			}
			if got := cfg.XlsImageCount(); got != tt.wantCount {
				t.Errorf("XlsImageCount() = %d, want %d", got, tt.wantCount)
			}
		})
	}
}

func TestRednoteImageSize(t *testing.T) {
	tests := []struct {
		name        string
		hasRednote  bool
		rednoteSize string
		want        string
	}{
		{"nil rednote returns default", false, "", DefaultRednoteImageSize},
		{"rednote with size returns it", true, "3:4:2K", "3:4:2K"},
		{"rednote with empty size returns default", true, "", DefaultRednoteImageSize},
		{"rednote with 1K size", true, "3:4:1K", "3:4:1K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			if tt.hasRednote {
				cfg.Rednote = &RednoteConfig{}
				cfg.Rednote.Content.Image.Size = tt.rednoteSize
			}
			if got := cfg.RednoteImageSize(); got != tt.want {
				t.Errorf("RednoteImageSize() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRednoteImageCount(t *testing.T) {
	tests := []struct {
		name         string
		hasRednote   bool
		rednoteCount int
		want         int
	}{
		{"nil rednote returns default", false, 0, DefaultRednoteImageCount},
		{"rednote with count returns it", true, 8, 8},
		{"rednote with zero count returns default", true, 0, DefaultRednoteImageCount},
		{"rednote with 1 count", true, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			if tt.hasRednote {
				cfg.Rednote = &RednoteConfig{}
				cfg.Rednote.Content.Count = tt.rednoteCount
			}
			if got := cfg.RednoteImageCount(); got != tt.want {
				t.Errorf("RednoteImageCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestResolvedRednoteContentImage(t *testing.T) {
	t.Run("nil rednote returns wechat.xls", func(t *testing.T) {
		cfg := &Config{}
		cfg.Wechat.Xls.Content.Image.Key = "xls-key"
		cfg.Wechat.Xls.Content.Image.Provider = "openai"
		result := cfg.ResolvedRednoteContentImage()
		if result.Key != "xls-key" {
			t.Errorf("Key = %q, want xls-key", result.Key)
		}
		if result.Provider != "openai" {
			t.Errorf("Provider = %q, want openai", result.Provider)
		}
	})

	t.Run("rednote primary over wechat.xls", func(t *testing.T) {
		cfg := &Config{}
		cfg.Wechat.Xls.Content.Image = ImageAPI{Key: "xls-key", Provider: "openai", Size: "3:4"}
		cfg.Rednote = &RednoteConfig{}
		cfg.Rednote.Content.Image = ImageAPI{Key: "rednote-key", Provider: "gemini", Size: "3:4:1K"}
		result := cfg.ResolvedRednoteContentImage()
		if result.Key != "rednote-key" {
			t.Errorf("Key = %q, want rednote-key (rednote primary)", result.Key)
		}
		if result.Provider != "gemini" {
			t.Errorf("Provider = %q, want gemini (rednote primary)", result.Provider)
		}
		if result.Size != "3:4:1K" {
			t.Errorf("Size = %q, want 3:4:1K (rednote primary)", result.Size)
		}
	})

	t.Run("wechat.xls fills missing rednote fields", func(t *testing.T) {
		cfg := &Config{}
		cfg.Wechat.Xls.Content.Image = ImageAPI{Key: "xls-key", MaxWidth: 1920, MaxSizeMB: 5}
		cfg.Rednote = &RednoteConfig{}
		cfg.Rednote.Content.Image = ImageAPI{Provider: "volcengine", Size: "3:4:1K"}
		result := cfg.ResolvedRednoteContentImage()
		if result.Provider != "volcengine" {
			t.Errorf("Provider = %q, want volcengine (rednote primary)", result.Provider)
		}
		if result.Key != "xls-key" {
			t.Errorf("Key = %q, want xls-key (from wechat.xls fallback)", result.Key)
		}
		if result.MaxWidth != 1920 {
			t.Errorf("MaxWidth = %d, want 1920 (from wechat.xls fallback)", result.MaxWidth)
		}
	})

	t.Run("both empty rednote config", func(t *testing.T) {
		cfg := &Config{}
		cfg.Rednote = &RednoteConfig{}
		result := cfg.ResolvedRednoteContentImage()
		if result.Key != "" {
			t.Errorf("Key = %q, want empty", result.Key)
		}
	})
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
