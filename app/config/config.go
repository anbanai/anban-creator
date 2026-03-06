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

	// DefaultArticleStyle 默认写作风格
	DefaultArticleStyle = "dan-koe"
	// DefaultArticleTheme 默认图文文章主题
	DefaultArticleTheme = "default"
	// DefaultImageProvider 默认图片生成服务商
	DefaultImageProvider = "gemini"
	// DefaultImageMaxWidth 默认图片最大宽度（像素）
	DefaultImageMaxWidth = 1920
	// DefaultImageMaxSizeMB 默认图片最大大小（MB）
	DefaultImageMaxSizeMB = 5
	// DefaultPostImageCount 默认小绿书图片数量
	DefaultPostImageCount = 4
	// DefaultArticleImageSize 默认图文文章图片尺寸（16:9 横版 2K）
	DefaultArticleImageSize = "16:9"
	// DefaultPostImageSize 默认小绿书图片尺寸（3:4 竖版 2K）
	DefaultPostImageSize = "3:4"
)

// DefaultConfigPath 返回默认配置文件路径（项目本地）
func DefaultConfigPath() string {
	return filepath.Join(ConfigDir, ConfigFileName)
}

// DefaultXiaohongshuImageSize 默认小红书图片尺寸（3:4 竖版 1K）
const DefaultXiaohongshuImageSize = "3:4:1K"

// DefaultXiaohongshuImageCount 默认小红书图片数量
const DefaultXiaohongshuImageCount = 6

// NewDefaultConfig 返回带有推荐默认值的 Config，用于生成配置文件模板
func NewDefaultConfig() *Config {
	c := &Config{}

	// 账号基本信息
	c.Name = "your_account_name"
	c.Keywords = []string{"keyword1", "keyword2"}
	c.Positioning = "your_account_positioning"

	// 微信公众号认证
	c.Wechat.AppID = "your_wechat_appid"
	c.Wechat.Secret = "your_wechat_secret"

	// 图文文章
	c.Wechat.Article.Author = "your_author_name"
	c.Wechat.Article.Style = DefaultArticleStyle
	c.Wechat.Article.Theme = DefaultArticleTheme
	// 文章封面图
	c.Wechat.Article.Cover.Image.Provider = DefaultImageProvider
	c.Wechat.Article.Cover.Image.Key = "your_image_api_key"
	c.Wechat.Article.Cover.Image.Model = "gemini-3-pro-image-preview"
	c.Wechat.Article.Cover.Image.Size = DefaultArticleImageSize
	c.Wechat.Article.Cover.Image.Compress = true
	c.Wechat.Article.Cover.Image.MaxWidth = DefaultImageMaxWidth
	c.Wechat.Article.Cover.Image.MaxSizeMB = DefaultImageMaxSizeMB
	// 文章内容配图
	c.Wechat.Article.Content.Image.Provider = DefaultImageProvider
	c.Wechat.Article.Content.Image.Key = "your_image_api_key"
	c.Wechat.Article.Content.Image.Model = "gemini-3-pro-image-preview"
	c.Wechat.Article.Content.Image.Size = DefaultArticleImageSize
	c.Wechat.Article.Content.Image.Compress = true
	c.Wechat.Article.Content.Image.MaxWidth = DefaultImageMaxWidth
	c.Wechat.Article.Content.Image.MaxSizeMB = DefaultImageMaxSizeMB

	// 小绿书
	c.Wechat.Post.Style = "morandi-flat"
	// 小绿书封面图
	c.Wechat.Post.Cover.Image.Provider = DefaultImageProvider
	c.Wechat.Post.Cover.Image.Key = "your_image_api_key"
	c.Wechat.Post.Cover.Image.Model = "gemini-3-pro-image-preview"
	c.Wechat.Post.Cover.Image.Size = DefaultPostImageSize
	c.Wechat.Post.Cover.Image.Refer = "path/to/refer.png"
	c.Wechat.Post.Cover.Image.Compress = true
	c.Wechat.Post.Cover.Image.MaxWidth = DefaultImageMaxWidth
	c.Wechat.Post.Cover.Image.MaxSizeMB = DefaultImageMaxSizeMB
	// 小绿书内容图
	c.Wechat.Post.Content.Count = DefaultPostImageCount
	c.Wechat.Post.Content.Image.Provider = DefaultImageProvider
	c.Wechat.Post.Content.Image.Key = "your_image_api_key"
	c.Wechat.Post.Content.Image.Model = "gemini-3-pro-image-preview"
	c.Wechat.Post.Content.Image.Size = DefaultPostImageSize
	c.Wechat.Post.Content.Image.Refer = "path/to/refer.png"
	c.Wechat.Post.Content.Image.StylePrompt = "扁平插画风格，莫兰迪色系，圆角卡片"
	c.Wechat.Post.Content.Image.Compress = true
	c.Wechat.Post.Content.Image.MaxWidth = DefaultImageMaxWidth
	c.Wechat.Post.Content.Image.MaxSizeMB = DefaultImageMaxSizeMB

	// 小红书（可选平台）
	c.Xiaohongshu = &XiaohongshuConfig{}
	c.Xiaohongshu.Style = "casual-warm"
	c.Xiaohongshu.Cover.Image.Provider = DefaultImageProvider
	c.Xiaohongshu.Cover.Image.Key = "your_image_api_key"
	c.Xiaohongshu.Cover.Image.Model = "gemini-3-pro-image-preview"
	c.Xiaohongshu.Cover.Image.Size = DefaultXiaohongshuImageSize
	c.Xiaohongshu.Cover.Image.Refer = "path/to/refer.png"
	c.Xiaohongshu.Cover.Image.Compress = true
	c.Xiaohongshu.Content.Image.Provider = DefaultImageProvider
	c.Xiaohongshu.Content.Image.Key = "your_image_api_key"
	c.Xiaohongshu.Content.Image.Model = "gemini-3-pro-image-preview"
	c.Xiaohongshu.Content.Image.Size = DefaultXiaohongshuImageSize
	c.Xiaohongshu.Content.Image.Refer = "path/to/refer.png"
	c.Xiaohongshu.Content.Image.Compress = true
	c.Xiaohongshu.Content.Count = DefaultXiaohongshuImageCount

	return c
}

// WatermarkConfig 水印裁剪配置
type WatermarkConfig struct {
	Enable bool `json:"enable,omitempty" yaml:"enable,omitempty"`
	Margin int  `json:"margin,omitempty" yaml:"margin,omitempty"`
}

// Validate 验证水印裁剪参数
func (w *WatermarkConfig) Validate() error {
	if w.Enable && w.Margin == 0 {
		return &ConfigError{
			Field:   "WatermarkMargin",
			Message: "启用去水印时必须设置裁剪边距 (watermark.margin)",
			HintMsg: "配置文件中设置 wechat.article.content.image.watermark.margin: 20（推荐 10-50 像素）",
		}
	}
	if w.Margin != 0 && (w.Margin < 1 || w.Margin > 500) {
		return &ConfigError{
			Field:   "WatermarkMargin",
			Message: "水印裁剪边距必须在 1 到 500 之间",
			HintMsg: "配置文件中设置 wechat.article.content.image.watermark.margin: 20",
		}
	}
	return nil
}

// VolcengineConfig 火山方舟 Seedream 高级选项（仅 settings.json 配置，不暴露到 agent/skill 层）
type VolcengineConfig struct {
	Watermark      *bool    `json:"watermark,omitempty" yaml:"watermark,omitempty"`
	Seed           *int64   `json:"seed,omitempty" yaml:"seed,omitempty"`
	GuidanceScale  *float64 `json:"guidance_scale,omitempty" yaml:"guidance_scale,omitempty"`
	OptimizePrompt *bool    `json:"optimize_prompt,omitempty" yaml:"optimize_prompt,omitempty"`
	OutputFormat   string   `json:"output_format,omitempty" yaml:"output_format,omitempty"` // "jpeg" or "png"
}

// ImageAPI 图片生成 API 配置（cover 和 content 各自独立）
type ImageAPI struct {
	Key         string            `json:"key,omitempty" yaml:"key,omitempty"`
	BaseURL     string            `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	Provider    string            `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model       string            `json:"model,omitempty" yaml:"model,omitempty"`
	Size        string            `json:"size,omitempty" yaml:"size,omitempty"`
	Refer       string            `json:"refer,omitempty" yaml:"refer,omitempty"`
	StylePrompt string            `json:"style_prompt,omitempty" yaml:"style_prompt,omitempty"`
	Compress    bool              `json:"compress,omitempty" yaml:"compress,omitempty"`
	MaxWidth    int               `json:"max_width,omitempty" yaml:"max_width,omitempty"`
	MaxSizeMB   int               `json:"max_size_mb,omitempty" yaml:"max_size_mb,omitempty"`
	Watermark   *WatermarkConfig  `json:"watermark,omitempty" yaml:"watermark,omitempty"`
	Volcengine  *VolcengineConfig `json:"volcengine,omitempty" yaml:"volcengine,omitempty"`
}

// MaxSizeBytes 返回图片最大尺寸（字节）
func (api *ImageAPI) MaxSizeBytes() int64 {
	return int64(api.MaxSizeMB) * 1024 * 1024
}

// Validate 验证图片处理参数范围
func (api *ImageAPI) Validate() error {
	if api.MaxWidth != 0 && (api.MaxWidth < 100 || api.MaxWidth > 10000) {
		return &ConfigError{
			Field:   "MaxImageWidth",
			Message: "图片最大宽度必须在 100 到 10000 之间",
			HintMsg: "配置文件中设置 wechat.article.content.image.max_width: 2560",
		}
	}
	maxSizeBytes := int64(api.MaxSizeMB) * 1024 * 1024
	if api.MaxSizeMB != 0 && maxSizeBytes < 1024*100 {
		return &ConfigError{
			Field:   "MaxImageSize",
			Message: "图片最大大小不能小于 100KB",
			HintMsg: "配置文件中设置 wechat.article.content.image.max_size_mb: 5",
		}
	}
	if api.Watermark != nil {
		return api.Watermark.Validate()
	}
	return nil
}

// ImageSection 单个图片用途配置（封面或内容图）
type ImageSection struct {
	Image ImageAPI `json:"image,omitempty" yaml:"image,omitempty"`
}

// PostContentSection 带数量的图片内容配置（用于小绿书内容图）
type PostContentSection struct {
	Image ImageAPI `json:"image,omitempty" yaml:"image,omitempty"`
	Count int      `json:"count,omitempty" yaml:"count,omitempty"`
}

// ArticleConfig 图文文章配置
type ArticleConfig struct {
	Style   string       `json:"style,omitempty" yaml:"style,omitempty"`
	Theme   string       `json:"theme,omitempty" yaml:"theme,omitempty"`
	Author  string       `json:"author,omitempty" yaml:"author,omitempty"`
	Cover   ImageSection `json:"cover,omitempty" yaml:"cover,omitempty"`
	Content ImageSection `json:"content,omitempty" yaml:"content,omitempty"`
}

// WechatPostConfig 微信小绿书配置
type WechatPostConfig struct {
	Style   string             `json:"style,omitempty" yaml:"style,omitempty"`
	Cover   ImageSection       `json:"cover,omitempty" yaml:"cover,omitempty"`
	Content PostContentSection `json:"content,omitempty" yaml:"content,omitempty"`
}

// WechatConfig 微信公众号配置
type WechatConfig struct {
	AppID   string           `json:"appid" yaml:"appid"`
	Secret  string           `json:"secret" yaml:"secret"`
	Article ArticleConfig    `json:"article,omitempty" yaml:"article,omitempty"`
	Post    WechatPostConfig `json:"post,omitempty" yaml:"post,omitempty"`
}

// XiaohongshuConfig 小红书配置
type XiaohongshuConfig struct {
	Style   string             `json:"style,omitempty" yaml:"style,omitempty"`
	Cover   ImageSection       `json:"cover,omitempty" yaml:"cover,omitempty"`
	Content PostContentSection `json:"content,omitempty" yaml:"content,omitempty"`
}

// Config 应用配置（嵌套结构，直接对应 JSON 文件）
type Config struct {
	Name        string   `json:"name,omitempty" yaml:"name,omitempty"`
	Keywords    []string `json:"keywords,omitempty" yaml:"keywords,omitempty"`
	Positioning string   `json:"positioning,omitempty" yaml:"positioning,omitempty"`

	Wechat      WechatConfig       `json:"wechat,omitempty" yaml:"wechat,omitempty"`
	Xiaohongshu *XiaohongshuConfig `json:"xiaohongshu,omitempty" yaml:"xiaohongshu,omitempty"`

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

	// 验证图片处理参数
	if err := c.Wechat.Article.Content.Image.Validate(); err != nil {
		return err
	}
	if err := c.Wechat.Post.Content.Image.Validate(); err != nil {
		return err
	}

	// 验证小绿书图片数量
	if c.Wechat.Post.Content.Count != 0 && (c.Wechat.Post.Content.Count < 1 || c.Wechat.Post.Content.Count > 20) {
		return &ConfigError{
			Field:   "PostImageCount",
			Message: "小绿书图片数量必须在 1 到 20 之间",
			HintMsg: "配置文件中设置 wechat.post.content.count: 4",
		}
	}

	return nil
}

// ValidateMinimal 验证基础配置（跳过微信账号验证）
// 用于不需要微信 API 的命令
func (c *Config) ValidateMinimal() error {
	// 验证图片处理参数
	if err := c.Wechat.Article.Content.Image.Validate(); err != nil {
		return err
	}
	if err := c.Wechat.Post.Content.Image.Validate(); err != nil {
		return err
	}

	// 验证小绿书图片数量
	if c.Wechat.Post.Content.Count != 0 && (c.Wechat.Post.Content.Count < 1 || c.Wechat.Post.Content.Count > 20) {
		return &ConfigError{
			Field:   "PostImageCount",
			Message: "小绿书图片数量必须在 1 到 20 之间",
			HintMsg: "配置文件中设置 wechat.post.content.count: 4",
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
			HintMsg: "在配置文件中设置 wechat.article.content.image.key、wechat.post.content.image.key 或 xiaohongshu.content.image.key",
		}
	}
	return nil
}

// GetConfigFile 获取配置文件路径
func (c *Config) GetConfigFile() string {
	return c.configPath
}

// PostImageSize 返回小绿书图片尺寸，默认 3:4 竖版
// 优先级: wechat.post.content.image.size > xiaohongshu.content.image.size > 默认值
func (c *Config) PostImageSize() string {
	if c.Wechat.Post.Content.Image.Size != "" {
		return c.Wechat.Post.Content.Image.Size
	}
	if c.Xiaohongshu != nil && c.Xiaohongshu.Content.Image.Size != "" {
		return c.Xiaohongshu.Content.Image.Size
	}
	return DefaultPostImageSize
}

// PostImageCount 返回小绿书图片数量，默认 4 张
// 优先级: wechat.post.content.count > xiaohongshu.content.count > 默认值
func (c *Config) PostImageCount() int {
	if c.Wechat.Post.Content.Count > 0 {
		return c.Wechat.Post.Content.Count
	}
	if c.Xiaohongshu != nil && c.Xiaohongshu.Content.Count > 0 {
		return c.Xiaohongshu.Content.Count
	}
	return DefaultPostImageCount
}

// mergeImageAPI 合并两个 ImageAPI 配置，base 字段非空时优先使用 base，否则使用 fallback
func mergeImageAPI(base, fallback ImageAPI) ImageAPI {
	result := base
	if result.Key == "" {
		result.Key = fallback.Key
	}
	if result.BaseURL == "" {
		result.BaseURL = fallback.BaseURL
	}
	if result.Provider == "" {
		result.Provider = fallback.Provider
	}
	if result.Model == "" {
		result.Model = fallback.Model
	}
	if result.Size == "" {
		result.Size = fallback.Size
	}
	if result.Refer == "" {
		result.Refer = fallback.Refer
	}
	if result.StylePrompt == "" {
		result.StylePrompt = fallback.StylePrompt
	}
	if !result.Compress && fallback.Compress {
		result.Compress = fallback.Compress
	}
	if result.MaxWidth == 0 {
		result.MaxWidth = fallback.MaxWidth
	}
	if result.MaxSizeMB == 0 {
		result.MaxSizeMB = fallback.MaxSizeMB
	}
	if result.Watermark == nil {
		result.Watermark = fallback.Watermark
	}
	if result.Volcengine == nil {
		result.Volcengine = fallback.Volcengine
	}
	return result
}

// ResolvedPostContentImage 返回合并后的小绿书内容图配置
// wechat.post.content.image 字段优先，xiaohongshu.content.image 作为 fallback
func (c *Config) ResolvedPostContentImage() ImageAPI {
	base := c.Wechat.Post.Content.Image
	if c.Xiaohongshu == nil {
		return base
	}
	return mergeImageAPI(base, c.Xiaohongshu.Content.Image)
}

// ResolvedPostCoverImage 返回合并后的小绿书封面图配置
// wechat.post.cover.image 字段优先，xiaohongshu.cover.image 作为 fallback
func (c *Config) ResolvedPostCoverImage() ImageAPI {
	base := c.Wechat.Post.Cover.Image
	if c.Xiaohongshu == nil {
		return base
	}
	return mergeImageAPI(base, c.Xiaohongshu.Cover.Image)
}

// ArticleImageSize 返回图文文章图片尺寸，默认 2560x1440（16:9 横版 2K）
func (c *Config) ArticleImageSize() string {
	if c.Wechat.Article.Content.Image.Size != "" {
		return c.Wechat.Article.Content.Image.Size
	}
	return DefaultArticleImageSize
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
