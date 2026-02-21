package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// MinWeChatPixels 微信公众号封面图最小像素要求
	MinWeChatPixels = 3686400

	// ConfigDir 项目本地配置目录
	ConfigDir = ".wechatwriter"
	// ConfigFileName 配置文件名
	ConfigFileName = "settings.json"
)

// DefaultConfigPath 返回默认配置文件路径（项目本地）
func DefaultConfigPath() string {
	return filepath.Join(ConfigDir, ConfigFileName)
}

// ImageAPI 图片生成 API 配置（article 和 post 各自独立）
type ImageAPI struct {
	Key      string `json:"key,omitempty" yaml:"key,omitempty"`
	BaseURL  string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model    string `json:"model,omitempty" yaml:"model,omitempty"`
	Size     string `json:"size,omitempty" yaml:"size,omitempty"`
}

// Config 应用配置（嵌套结构，直接对应 JSON/YAML 文件）
type Config struct {
	Wechat struct {
		Name     string   `json:"name,omitempty" yaml:"name,omitempty"`
		Author   string   `json:"author,omitempty" yaml:"author,omitempty"`
		AppID    string   `json:"appid" yaml:"appid"`
		Secret   string   `json:"secret" yaml:"secret"`
		KeyWords []string `json:"key_words,omitempty" yaml:"key_words,omitempty"`
		Style    string   `json:"style,omitempty" yaml:"style,omitempty"`
	} `json:"wechat" yaml:"wechat"`

	Article struct {
		Theme string   `json:"theme,omitempty" yaml:"theme,omitempty"`
		Image ImageAPI `json:"image,omitempty" yaml:"image,omitempty"`
	} `json:"article,omitempty" yaml:"article,omitempty"`

	Post struct {
		Image ImageAPI `json:"image,omitempty" yaml:"image,omitempty"`
	} `json:"post,omitempty" yaml:"post,omitempty"`

	AI ImageAPI `json:"ai,omitempty" yaml:"ai,omitempty"`

	Image struct {
		Compress    bool `json:"compress" yaml:"compress"`
		MaxWidth    int  `json:"max_width,omitempty" yaml:"max_width,omitempty"`
		MaxSizeMB   int  `json:"max_size_mb,omitempty" yaml:"max_size_mb,omitempty"`
		HTTPTimeout int  `json:"http_timeout,omitempty" yaml:"http_timeout,omitempty"`
	} `json:"image" yaml:"image"`

	configPath string
}

// Load 从配置文件加载配置
func Load() (*Config, error) {
	return LoadWithDefaults("")
}

// LoadMinimal 加载配置但不验证微信账号
// 用于不需要微信 API 的命令（如 AI 提示词生成）
func LoadMinimal() (*Config, error) {
	return loadWithValidation("", false)
}

// LoadWithDefaults 使用指定配置文件路径加载配置
func LoadWithDefaults(configPath string) (*Config, error) {
	return loadWithValidation(configPath, true)
}

// loadWithValidation 加载配置并根据参数决定是否验证微信账号
func loadWithValidation(configPath string, validateWechat bool) (*Config, error) {
	cfg := new(Config)
	// 1. 尝试从配置文件加载
	if configPath == "" {
		configPath = findConfigFile()
	}
	if configPath != "" {
		if err := loadFromFile(cfg, configPath); err != nil {
			// 配置文件加载失败不是致命错误，继续使用默认值
			fmt.Fprintf(os.Stderr, "⚠️  警告: 配置文件加载失败 (%v)，将使用默认值\n", err)
		} else {
			cfg.configPath = configPath
			// 显示正在使用的配置文件
			relPath := getRelativePath(configPath)
			fmt.Fprintf(os.Stderr, "✅ 使用配置文件: %s\n", relPath)
		}
	}

	// 2. 验证配置
	if validateWechat {
		// 完整验证（包括微信账号）
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
	} else {
		// 只验证基础配置（跳过微信账号）
		if err := cfg.ValidateMinimal(); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// findConfigFile 查找配置文件
// 按优先级检查多个路径：CWD > CLAUDE_PLUGIN_ROOT > 用户目录 > 可执行文件目录
func findConfigFile() string {
	paths := []string{
		filepath.Join(ConfigDir, ConfigFileName), // CWD（最高优先级）
	}

	// Plugin root（作为 Claude Code 插件运行时）
	if pluginRoot := os.Getenv("CLAUDE_PLUGIN_ROOT"); pluginRoot != "" {
		paths = append(paths, filepath.Join(pluginRoot, ConfigDir, ConfigFileName))
	}

	// 用户目录（优先于可执行文件相对路径，避免开发目录污染）
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".config", "wechatwriter", ConfigFileName),
			filepath.Join(home, ConfigDir, ConfigFileName),
		)
	}

	// 可执行文件相对路径（已安装的二进制文件，最低优先级）
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if realExe, err := filepath.EvalSymlinks(exe); err == nil {
			exeDir = filepath.Dir(realExe)
		}
		paths = append(paths,
			filepath.Join(exeDir, ConfigDir, ConfigFileName),       // 同级: scripts/.wechatwriter/
			filepath.Join(exeDir, "..", ConfigDir, ConfigFileName), // 上级: .wechatwriter/（项目根目录）
		)
	}

	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// loadFromFile 从文件加载配置
func loadFromFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	return loadFromJSON(cfg, data)
}

// loadFromJSON 从 JSON 加载
func loadFromJSON(cfg *Config, data []byte) error {
	if err := json.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	return nil
}

// Validate 验证配置
func (c *Config) Validate() error {
	// 验证微信账号配置
	if c.Wechat.AppID == "" {
		return &ConfigError{
			Field:   "WechatAppID",
			Message: "微信公众号 AppID 未配置",
			HintMsg: "在配置文件中设置 wechat.appid",
		}
	}
	if c.Wechat.Secret == "" {
		return &ConfigError{
			Field:   "WechatSecret",
			Message: "微信公众号 Secret 未配置",
			HintMsg: "登录微信公众平台 > 设置与开发 > 基本配置 > 获取 Secret",
		}
	}

	// 验证数值范围
	if c.Image.MaxWidth != 0 && (c.Image.MaxWidth < 100 || c.Image.MaxWidth > 10000) {
		return &ConfigError{
			Field:   "MaxImageWidth",
			Message: "图片最大宽度必须在 100 到 10000 之间",
			HintMsg: "配置文件中设置 image.max_width: 2560",
		}
	}
	maxSizeBytes := int64(c.Image.MaxSizeMB) * 1024 * 1024
	if c.Image.MaxSizeMB != 0 && maxSizeBytes < 1024*100 { // 最小 100KB
		return &ConfigError{
			Field:   "MaxImageSize",
			Message: "图片最大大小不能小于 100KB",
			HintMsg: "配置文件中设置 image.max_size_mb: 5",
		}
	}
	if c.Image.HTTPTimeout != 0 && (c.Image.HTTPTimeout < 1 || c.Image.HTTPTimeout > 300) {
		return &ConfigError{
			Field:   "HTTPTimeout",
			Message: "超时时间必须在 1 到 300 秒之间",
			HintMsg: "配置文件中设置 image.http_timeout: 30",
		}
	}

	return nil
}

// ValidateMinimal 验证基础配置（跳过微信账号验证）
// 用于不需要微信 API 的命令
func (c *Config) ValidateMinimal() error {
	// 只验证数值范围
	if c.Image.MaxWidth != 0 && (c.Image.MaxWidth < 100 || c.Image.MaxWidth > 10000) {
		return &ConfigError{
			Field:   "MaxImageWidth",
			Message: "图片最大宽度必须在 100 到 10000 之间",
			HintMsg: "配置文件中设置 image.max_width: 2560",
		}
	}
	maxSizeBytes := int64(c.Image.MaxSizeMB) * 1024 * 1024
	if c.Image.MaxSizeMB != 0 && maxSizeBytes < 1024*100 { // 最小 100KB
		return &ConfigError{
			Field:   "MaxImageSize",
			Message: "图片最大大小不能小于 100KB",
			HintMsg: "配置文件中设置 image.max_size_mb: 5",
		}
	}
	if c.Image.HTTPTimeout != 0 && (c.Image.HTTPTimeout < 1 || c.Image.HTTPTimeout > 300) {
		return &ConfigError{
			Field:   "HTTPTimeout",
			Message: "超时时间必须在 1 到 300 秒之间",
			HintMsg: "配置文件中设置 image.http_timeout: 30",
		}
	}

	return nil
}

// ValidateForImageGeneration 验证图片生成配置
func ValidateForImageGeneration(apiCfg *ImageAPI) error {
	if apiCfg == nil || apiCfg.Key == "" {
		return &ConfigError{
			Field:   "ImageAPIKey",
			Message: "图片生成需要配置 API Key",
			HintMsg: "在配置文件中设置 article.image.key 或 post.image.key",
		}
	}
	return nil
}

// GetConfigFile 获取配置文件路径
func (c *Config) GetConfigFile() string {
	return c.configPath
}

// MaxImageSizeBytes 返回图片最大尺寸（字节）
func (c *Config) MaxImageSizeBytes() int64 {
	return int64(c.Image.MaxSizeMB) * 1024 * 1024
}

// PostImageSize 返回小绿书图片尺寸，默认 1728x2304（3:4 竖版 2K）
func (c *Config) PostImageSize() string {
	if c.Post.Image.Size != "" {
		return c.Post.Image.Size
	}
	return "1728x2304"
}

// ArticleImageSize 返回图文文章图片尺寸，默认 2560x1440（16:9 横版 2K）
func (c *Config) ArticleImageSize() string {
	if c.Article.Image.Size != "" {
		return c.Article.Image.Size
	}
	return "2560x1440"
}

// SaveConfig 保存配置到文件（JSON 格式）
func SaveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// 确保目录存在
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// ConfigError 配置错误
type ConfigError struct {
	Field   string
	Message string
	HintMsg string // 配置提示
}

func (e *ConfigError) Error() string {
	msg := fmt.Sprintf("配置错误 [%s]: %s", e.Field, e.Message)
	if e.HintMsg != "" {
		msg += fmt.Sprintf("\n💡 提示: %s", e.HintMsg)
	}
	return msg
}

func (e *ConfigError) Hint() string { return e.HintMsg }

// getRelativePath 获取相对路径（用于更友好的显示）
func getRelativePath(fullPath string) string {
	// 如果是用户目录，显示为 ~/.wechatwriter.yaml
	homeDir, _ := os.UserHomeDir()
	if homeDir != "" && strings.HasPrefix(fullPath, homeDir) {
		rel := strings.TrimPrefix(fullPath, homeDir)
		if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
			rel = rel[1:]
		}
		return "~/" + rel
	}

	// 如果是当前目录，直接显示文件名
	if cwd, err := os.Getwd(); err == nil {
		if strings.HasPrefix(fullPath, cwd) {
			rel := strings.TrimPrefix(fullPath, cwd)
			if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
				rel = rel[1:]
			}
			return "./" + rel
		}
	}

	// 其他情况返回完整路径
	return fullPath
}
