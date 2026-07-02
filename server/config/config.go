package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	appconfig "github.com/anbanai/anban-creator/app/config"
)

// Config holds all server configuration.
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Logging      LoggingConfig      `yaml:"logging"`
	Database     DatabaseConfig     `yaml:"database"`
	Redis        RedisConfig        `yaml:"redis"`
	JWT          JWTConfig          `yaml:"jwt"`
	WeChat       WeChatConfig       `yaml:"wechat"`
	Storage      StorageConfig      `yaml:"storage"`
	MCP          MCPConfig          `yaml:"mcp"`
	ImageAPI     ImageAPIConfig     `yaml:"image_api"`
	VideoAPI     VideoAPIConfig     `yaml:"video_api"`
	ImagePresets []ImageModelPreset `yaml:"image_presets"`
	Writing      WritingConfig      `yaml:"writing"`
	Vision       VisionConfig       `yaml:"vision"`
	TingWu       TingWuConfig       `yaml:"tingwu"`
	FunASR       FunASRConfig       `yaml:"funasr"`
	Claude       ClaudeConfig       `yaml:"claude"`
	Credits      CreditsConfig      `yaml:"credits"`
	CORS         CORSConfig         `yaml:"cors"`
	Asynq        AsynqConfig        `yaml:"asynq"`
	Email        EmailConfig        `yaml:"email"`
	Invitation   InvitationConfig   `yaml:"invitation"`
	Seednote     SeednoteConfig     `yaml:"seednote"`
	WCF          WCFConfig          `yaml:"wcf"`
}

// ImageModelPreset defines a system-managed image model that users can select
// when creating tasks or plans. Each preset has a minimum tier that gates access.
type ImageModelPreset struct {
	Key         string `yaml:"key"`          // unique identifier, e.g. "volcengine-standard"
	DisplayName string `yaml:"display_name"` // user-facing label
	Provider    string `yaml:"provider"`     // volcengine / gemini / openai
	Model       string `yaml:"model"`        // concrete model id
	Endpoint    string `yaml:"endpoint"`
	APIKey      string `yaml:"api_key"`
	MinTier     string `yaml:"min_tier"` // free / pro / enterprise
}

// maxImageModelKeyLen matches the varchar(50) column size on Task/Plan.ImageModelKey.
const maxImageModelKeyLen = 50

// ValidateImagePresets checks that preset keys fit the Task/Plan ImageModelKey
// column (varchar(50)) and are globally unique. Returns the first error found.
// Call this at startup so a malformed config fails fast instead of surfacing
// as a 500 on first task create.
func ValidateImagePresets(presets []ImageModelPreset) error {
	seen := make(map[string]bool, len(presets))
	for i, p := range presets {
		if p.Key == "" {
			return fmt.Errorf("image_presets[%d]: key is required", i)
		}
		if len(p.Key) > maxImageModelKeyLen {
			return fmt.Errorf("image_presets[%d]: key %q exceeds %d characters (DB column varchar(50))", i, p.Key, maxImageModelKeyLen)
		}
		if strings.ContainsAny(p.Key, " \t\n\r") {
			return fmt.Errorf("image_presets[%d]: key %q must not contain whitespace", i, p.Key)
		}
		if p.Key == "custom" {
			return fmt.Errorf("image_presets[%d]: key %q is reserved", i, p.Key)
		}
		if seen[p.Key] {
			return fmt.Errorf("image_presets[%d]: duplicate key %q", i, p.Key)
		}
		seen[p.Key] = true
	}
	return nil
}

// SeednoteConfig holds Seednote (种草笔记) sidecar configuration.
type SeednoteConfig struct {
	BaseURL string `yaml:"base_url"` // default "http://localhost:18060"
	Timeout int    `yaml:"timeout"`  // default 30 (seconds)
}

// WCFConfig holds the wcfLink WeChat-bot sidecar configuration.
// wcfLink is a local iLink WeChat channel service (NOT the Windows-only
// WeChatFerry PC hook) that the server drives over HTTP to send task
// notifications and receive WeChat commands. Opt-in: when disabled or the
// sidecar is unreachable, all WeChat features degrade to no-ops.
type WCFConfig struct {
	Enabled      bool   `yaml:"enabled"`       // master switch; default false
	BaseURL      string `yaml:"base_url"`      // default "http://localhost:18070"
	Timeout      int    `yaml:"timeout"`       // seconds; default 30
	PollInterval int    `yaml:"poll_interval"` // seconds between /api/events polls; default 2
}

// EmailConfig holds email/verification code configuration.
type EmailConfig struct {
	SMTPHost     string        `yaml:"smtp_host"`
	SMTPPort     int           `yaml:"smtp_port"`
	SMTPUsername string        `yaml:"smtp_username"`
	SMTPPassword string        `yaml:"smtp_password"`
	FromAddress  string        `yaml:"from_address"`
	FromName     string        `yaml:"from_name"`
	CodeTTL      time.Duration `yaml:"code_ttl"`    // default 5m
	CodeLength   int           `yaml:"code_length"` // default 6
}

type ServerConfig struct {
	Port int    `yaml:"port"` // default 8080
	Host string `yaml:"host"` // default "0.0.0.0"
}

type LoggingConfig struct {
	Level string `yaml:"level"` // "debug", "info" (default), "warn", "error", "trace"
}

type DatabaseConfig struct {
	DSN             string `yaml:"dsn"`
	MaxOpenConns    int    `yaml:"max_open_conns"`    // default 10
	MaxIdleConns    int    `yaml:"max_idle_conns"`    // default 5
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"` // default 3600 seconds
}

type RedisConfig struct {
	Addr     string `yaml:"addr"` // default "localhost:6379"
	Password string `yaml:"password"`
	DB       int    `yaml:"db"` // default 0
}

type JWTConfig struct {
	SecretKey     string `yaml:"secret_key"`
	AccessExpiry  string `yaml:"access_expiry"`  // default "24h"
	RefreshExpiry string `yaml:"refresh_expiry"` // default "168h"
}

type WeChatConfig struct {
	AppID      string `yaml:"app_id"`
	AppSecret  string `yaml:"app_secret"`
	QRCodePage string `yaml:"qrcode_page"` // mini program page for QR code scan, default "pages/login/index"
	EnvVersion string `yaml:"env_version"` // "develop", "trial", or "release", default "develop"
}

// MCPConfig holds Model Context Protocol endpoint configuration.
type MCPConfig struct {
	APIKey string `yaml:"api_key"` // API key for MCP endpoint authentication
}

const DefaultVideoAPIBaseURL = "https://ark.cn-beijing.volces.com/api/v3"

// VideoAPIConfig holds global Volcengine Ark content-generation settings.
// Business defaults live on Project.VideoDefaults/VideoModelPolicy so plans and
// tasks can snapshot them.
type VideoAPIConfig struct {
	Key              string                   `yaml:"key"`
	BaseURL          string                   `yaml:"base_url"`
	Timeout          time.Duration            `yaml:"timeout"`
	CreditMultiplier int                      `yaml:"credit_multiplier"`
	ModelCatalog     []VideoModelCatalogEntry `yaml:"model_catalog"`
}

type VideoModelCatalogEntry struct {
	Key                   string             `yaml:"key"`
	DisplayName           string             `yaml:"display_name"`
	ModelID               string             `yaml:"model_id"`
	SupportedResolutions  []string           `yaml:"supported_resolutions"`
	SupportedRatios       []string           `yaml:"supported_ratios"`
	MinDuration           int64              `yaml:"min_duration"`
	MaxDuration           int64              `yaml:"max_duration"`
	SupportsVideoInput    bool               `yaml:"supports_video_input"`
	Supports4K            bool               `yaml:"supports_4k"`
	NoInputPricePerSecond map[string]float64 `yaml:"no_input_price_per_second"`
	VideoInput5sMinPrice  map[string]float64 `yaml:"video_input_5s_min_price"`
	VideoInput5sMaxPrice  map[string]float64 `yaml:"video_input_5s_max_price"`
}

func (c VideoAPIConfig) CreditMultiplierOrDefault() int {
	if c.CreditMultiplier > 0 {
		return c.CreditMultiplier
	}
	return 1000
}

func (c VideoAPIConfig) ModelCatalogOrDefault() []VideoModelCatalogEntry {
	if len(c.ModelCatalog) > 0 {
		return c.ModelCatalog
	}
	return []VideoModelCatalogEntry{
		{
			Key:                  "seedance-2.0",
			DisplayName:          "Doubao Seedance 2.0",
			ModelID:              "doubao-seedance-2-0-260128",
			SupportedResolutions: []string{"480p", "720p", "1080p", "4k"},
			SupportedRatios:      []string{"16:9", "9:16", "1:1", "4:3", "3:4"},
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
			Supports4K:           true,
		},
		{
			Key:                  "seedance-2.0-fast",
			DisplayName:          "Doubao Seedance 2.0 Fast",
			ModelID:              "doubao-seedance-2-0-fast-260128",
			SupportedResolutions: []string{"480p", "720p"},
			SupportedRatios:      []string{"16:9", "9:16", "1:1", "4:3", "3:4"},
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
		},
		{
			Key:                  "seedance-2.0-mini",
			DisplayName:          "Doubao Seedance 2.0 Mini",
			ModelID:              "doubao-seedance-2-0-mini-260615",
			SupportedResolutions: []string{"480p", "720p"},
			SupportedRatios:      []string{"16:9", "9:16", "1:1", "4:3", "3:4"},
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
		},
	}
}

// StorageConfig holds file storage configuration.
// Supports "oss" (Alibaba Cloud OSS) or "local" (filesystem).
type StorageConfig struct {
	Provider        string `yaml:"provider"` // "oss" or "local", default "local"
	Endpoint        string `yaml:"endpoint"` // OSS endpoint, e.g. "oss-cn-hangzhou.aliyuncs.com"
	AccessKeyID     string `yaml:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret"`
	BucketName      string `yaml:"bucket_name"`
	Region          string `yaml:"region"`
	CustomDomain    string `yaml:"custom_domain"`  // Optional CDN domain for public file URLs
	LocalDataDir    string `yaml:"local_data_dir"` // Default "./data/files"
}

// SizesConfig holds per-platform image size defaults (ratio:tier format, e.g. "16:9", "3:4:4K").
type SizesConfig struct {
	ArticleCover    string `yaml:"article_cover"`    // default "16:9"
	ArticleContent  string `yaml:"article_content"`  // default "16:9"
	SeednoteCover   string `yaml:"seednote_cover"`   // default "3:4"
	SeednoteContent string `yaml:"seednote_content"` // default "3:4"
}

// ImageAPIConfig holds global image generation API configuration.
// All projects share this server-level config.
type ImageAPIConfig struct {
	Cover    *appconfig.ImageAPI            `yaml:"cover"`
	Content  *appconfig.ImageAPI            `yaml:"content"`
	Designer map[string]*appconfig.ImageAPI `yaml:"designer"`
	Sizes    SizesConfig                    `yaml:"sizes"`

	// designerOrder preserves the insertion order of Designer keys as written
	// in the YAML file. Populated by UnmarshalYAML; not serialized.
	designerOrder []string
}

// UnmarshalYAML decodes the YAML node and additionally captures the
// insertion order of the designer mapping so callers can iterate in a
// deterministic, file-order sequence.
func (c *ImageAPIConfig) UnmarshalYAML(value *yaml.Node) error {
	type plain ImageAPIConfig
	if err := value.Decode((*plain)(c)); err != nil {
		return err
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		if value.Content[i].Value != "designer" {
			continue
		}
		mapping := value.Content[i+1]
		if mapping == nil || mapping.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(mapping.Content); j += 2 {
			c.designerOrder = append(c.designerOrder, mapping.Content[j].Value)
		}
	}
	return nil
}

// DesignerOrder returns the designer keys in YAML insertion order.
func (c *ImageAPIConfig) DesignerOrder() []string {
	if c == nil {
		return nil
	}
	return c.designerOrder
}

// WritingConfig holds LLM API configuration for writing services
// (article writing, topic research, SEO, outlines).
//
// Markdown→WeChat-HTML conversion is now deterministic (no LLM), so it has no
// dedicated timeout — Timeout below covers only the LLM-using paths
// (write_article, outlines).
type WritingConfig struct {
	BaseURL string        `yaml:"base_url"` // LLM API endpoint
	Key     string        `yaml:"key"`      // API key
	Model   string        `yaml:"model"`    // Model name
	Timeout time.Duration `yaml:"timeout"`  // LLM request timeout (default 5m)
}

// VisionConfig holds LLM API configuration for vision/image analysis services.
// Uses OpenAI-compatible /chat/completions with image content blocks.
// Falls back to Writing config when not configured.
type VisionConfig struct {
	BaseURL string        `yaml:"base_url"` // Vision LLM API endpoint (OpenAI-compatible)
	Key     string        `yaml:"key"`      // API key
	Model   string        `yaml:"model"`    // Model name (must support vision/image input)
	Timeout time.Duration `yaml:"timeout"`  // Request timeout (default 60s)
}

// TingWuConfig holds Alibaba TingWu speech analysis configuration.
type TingWuConfig struct {
	Endpoint     string `yaml:"endpoint"`
	Region       string `yaml:"region"`
	AppKey       string `yaml:"app_key"`
	AccessKey    string `yaml:"access_key"`
	AccessSecret string `yaml:"access_secret"`
}

// Empty reports whether no TingWu settings are configured.
func (c TingWuConfig) Empty() bool {
	return strings.TrimSpace(c.Endpoint) == "" &&
		strings.TrimSpace(c.Region) == "" &&
		strings.TrimSpace(c.AppKey) == "" &&
		strings.TrimSpace(c.AccessKey) == "" &&
		strings.TrimSpace(c.AccessSecret) == ""
}

// Complete reports whether all settings required for direct TingWu calls exist.
func (c TingWuConfig) Complete() bool {
	return strings.TrimSpace(c.Endpoint) != "" &&
		strings.TrimSpace(c.Region) != "" &&
		strings.TrimSpace(c.AppKey) != "" &&
		strings.TrimSpace(c.AccessKey) != "" &&
		strings.TrimSpace(c.AccessSecret) != ""
}

// FunASRConfig holds OpenAI-compatible FunASR file ASR configuration.
type FunASRConfig struct {
	BaseURL string        `yaml:"base_url"`
	APIKey  string        `yaml:"api_key"`
	Model   string        `yaml:"model"`
	Timeout time.Duration `yaml:"timeout"`
}

// Empty reports whether no FunASR settings are configured.
func (c FunASRConfig) Empty() bool {
	return strings.TrimSpace(c.BaseURL) == "" &&
		strings.TrimSpace(c.APIKey) == "" &&
		strings.TrimSpace(c.Model) == ""
}

// Complete reports whether all settings required for OpenAI-compatible FunASR calls exist.
func (c FunASRConfig) Complete() bool {
	return strings.TrimSpace(c.BaseURL) != "" &&
		strings.TrimSpace(c.APIKey) != "" &&
		strings.TrimSpace(c.Model) != ""
}

// ClaudeConfig holds configuration for the Claude CLI subprocess.
// The Env map is passed as environment variables to the CLI process,
// supporting auth tokens, base URLs, model overrides, etc.
type ClaudeConfig struct {
	Model          string            `yaml:"model"`    // Model for agent execution (empty = use env vars like ANTHROPIC_MODEL)
	Executor       string            `yaml:"executor"` // "local" (default) or "docker"
	Env            map[string]string `yaml:"env"`
	PluginDir      string            `yaml:"plugin_dir"`       // Path to the abwriter plugin directory (contains agents/, skills/)
	Sandbox        bool              `yaml:"sandbox"`          // Enable sandbox isolation for agent execution (recommended in k8s)
	Docker         DockerConfig      `yaml:"docker"`           // Docker executor settings (used when executor=docker)
	MaxTurns       map[string]int    `yaml:"max_turns"`        // Per-task-type max turns, e.g. {"article": 300, "seednote": 150}
	TaskLogDir     string            `yaml:"task_log_dir"`     // Directory for per-task agent execution logs. Empty = disabled.
	AgentServerURL string            `yaml:"agent_server_url"` // Override server URL for agent MCP connections (e.g. k8s service URL). To env-control, write ${ANBAN_CLAUDE_AGENT_SERVER_URL} in config.yaml.
}

// DockerConfig holds Docker executor settings for container-based task execution.
type DockerConfig struct {
	Image         string `yaml:"image"`          // Docker image name (default: "abwriter-agent:latest")
	CPUCores      int64  `yaml:"cpu_cores"`      // CPU limit in cores (default: 2)
	MemoryMB      int64  `yaml:"memory_mb"`      // Memory limit in MB (default: 4096)
	TimeoutSec    int    `yaml:"timeout_sec"`    // Container execution timeout in seconds (default: 1800 = 30 min)
	ContainerName string `yaml:"container_name"` // Name of a persistent container to reuse via docker exec (empty = create+destroy per task)
	WorkspaceDir  string `yaml:"workspace_dir"`  // Host-side base directory for task workspaces (persistent container mode, must match volume mount source)
}

// CreditsConfig holds credits/points system configuration.
type CreditsConfig struct {
	DailySignIn   int                       `yaml:"daily_sign_in"`  // credits awarded per daily sign-in (default 1024)
	RegisterBonus int                       `yaml:"register_bonus"` // credits awarded on registration (default 4096)
	InviteReward  int                       `yaml:"invite_reward"`  // credits awarded to inviter when invitee registers (default 2048)
	TaskCosts     map[string]int            `yaml:"task_costs"`     // per-task-type costs, e.g. {"article": 4000, "seednote": 3200}
	ModelCosts    map[string]map[string]int `yaml:"model_costs"`    // per-model costs, key format: "provider/model"
	AdminAPIKey   string                    `yaml:"admin_api_key"`  // API key for admin credit grant endpoint

	// E-commerce deliverable module pricing. Each module (main_images, detail_page,
	// cover_banner, share_image, sku_images) maps to a per-unit credit price; an
	// e-commerce task's total cost is sum(unit_price × quantity) over the
	// user-selected modules (see CreditService.EcommercePackageCost).
	EcommerceModulePrices map[string]int `yaml:"ecommerce_module_prices"`

	// Goal mode pricing.
	// GoalModeMultiplier is the upfront credit multiplier applied when a task is
	// created with goal_mode=true (default 3). The goal loop runs entirely
	// inside Claude Code's /goal mechanism; the server cannot tell how many
	// turns were consumed, so the upfront charge is never refunded.
	GoalModeMultiplier int `yaml:"goal_mode_multiplier"`
}

// EffectiveGoalModeMultiplier returns the configured goal-mode credit multiplier,
// defaulting to 3 when unset or invalid.
func (c *CreditsConfig) EffectiveGoalModeMultiplier() int {
	if c == nil || c.GoalModeMultiplier <= 0 {
		return 3
	}
	return c.GoalModeMultiplier
}

// ModelCost returns the per-operation cost for a specific model.
// Returns (0, false) if no pricing is configured for this operation/model pair.
func (c *CreditsConfig) ModelCost(opType, provider, model string) (int, bool) {
	if c.ModelCosts == nil {
		return 0, false
	}
	models, ok := c.ModelCosts[opType]
	if !ok {
		return 0, false
	}
	// Try with provider prefix first: "provider/model"
	if provider != "" {
		if cost, ok := models[provider+"/"+model]; ok {
			return cost, true
		}
	}
	// Try with just model name
	if cost, ok := models[model]; ok {
		return cost, true
	}
	return 0, false
}

// CORSConfig holds Cross-Origin Resource Sharing configuration.
// When AllowedOrigins is empty, only localhost origins are permitted (development default).
type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
}

// AsynqConfig holds Asynq task queue configuration.
type AsynqConfig struct {
	Concurrency int `yaml:"concurrency"` // default 3

	// ContentGenerateTimeout bounds the whole content:generate task (research →
	// images → HTML → draft). It is set as the asynq task Timeout and as the
	// Redis-down fallback goroutine deadline. Must comfortably exceed the
	// realistic pipeline wall-clock time so the agent reaches 100% delivery
	// before the asynq ctx expires. Default 60m (the wechatarticle pipeline
	// with 8 vision-verified images runs ~35-40m at max_turns.article=300).
	ContentGenerateTimeout time.Duration `yaml:"content_generate_timeout"` // default 60m

	// PersistTimeout bounds the post-execution DB writes (result, workspace
	// files, workflow status, terminal status) which are decoupled from the
	// execution ctx so completed work is saved even when the pipeline overruns
	// ContentGenerateTimeout. Default 10m.
	PersistTimeout time.Duration `yaml:"persist_timeout"` // default 10m
}

// InvitationConfig holds invitation system configuration.
type InvitationConfig struct {
	Enabled    bool `yaml:"enabled"`      // master switch for invite-required registration
	MaxPerUser int  `yaml:"max_per_user"` // max invites per user (default 3)
}

// NewConfig loads configuration from a YAML file. Before parsing, ${VAR} and
// ${VAR:-default} placeholders in the file are expanded from the process
// environment — this is the ONLY way environment variables affect config.
// There is no hidden ANBAN_* override layer; every env dependency must be
// written explicitly in the YAML as ${...}, so the file stays the single
// visible source of truth.
func NewConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	data = expandEnvVars(data)

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	cfg.applyDefaults()
	cfg.resolvePaths()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// envVarRe matches ${VAR} and ${VAR:-default}. Only braced references are
// expanded, so a literal '$' in a value (e.g. a password) is never touched.
// Submatch 1 = variable name; submatch 2 = default text (nil when no :-group).
var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// expandEnvVars replaces ${VAR} / ${VAR:-default} occurrences in the raw config
// text using the process environment. A referenced variable that is unset (or
// empty) expands to its default when one is given, otherwise to an empty
// string. Unlike os.ExpandEnv, bare $VAR without braces is left untouched, so
// only placeholders the author wrote deliberately are expanded.
func expandEnvVars(data []byte) []byte {
	return envVarRe.ReplaceAllFunc(data, func(m []byte) []byte {
		sub := envVarRe.FindSubmatch(m)
		if v := os.Getenv(string(sub[1])); v != "" {
			return []byte(v)
		}
		if len(sub) > 2 && sub[2] != nil {
			return sub[2] // :-default provided
		}
		return nil // unset and no default → empty string
	})
}

// applyDefaults fills in zero-value fields with sensible defaults.
func (c *Config) applyDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Database.MaxOpenConns == 0 {
		c.Database.MaxOpenConns = 10
	}
	if c.Database.MaxIdleConns == 0 {
		c.Database.MaxIdleConns = 5
	}
	if c.Database.ConnMaxLifetime == 0 {
		c.Database.ConnMaxLifetime = 3600
	}
	if c.Redis.Addr == "" {
		c.Redis.Addr = "localhost:6379"
	}
	if c.JWT.AccessExpiry == "" {
		c.JWT.AccessExpiry = "24h"
	}
	if c.JWT.RefreshExpiry == "" {
		c.JWT.RefreshExpiry = "168h"
	}
	if c.Storage.Provider == "" {
		c.Storage.Provider = "local"
	}
	if c.Storage.LocalDataDir == "" {
		c.Storage.LocalDataDir = "./data/files"
	}
	if c.VideoAPI.BaseURL == "" {
		c.VideoAPI.BaseURL = DefaultVideoAPIBaseURL
	}
	if c.VideoAPI.Timeout == 0 {
		c.VideoAPI.Timeout = 10 * time.Minute
	}
	if c.VideoAPI.CreditMultiplier == 0 {
		c.VideoAPI.CreditMultiplier = 1000
	}

	// Per-platform image size defaults (ratio:tier format).
	if c.ImageAPI.Sizes.ArticleCover == "" {
		c.ImageAPI.Sizes.ArticleCover = "16:9"
	}
	if c.ImageAPI.Sizes.ArticleContent == "" {
		c.ImageAPI.Sizes.ArticleContent = "16:9"
	}
	if c.ImageAPI.Sizes.SeednoteCover == "" {
		c.ImageAPI.Sizes.SeednoteCover = "3:4"
	}
	if c.ImageAPI.Sizes.SeednoteContent == "" {
		c.ImageAPI.Sizes.SeednoteContent = "3:4"
	}

	// Credits defaults.
	if c.Credits.DailySignIn == 0 {
		c.Credits.DailySignIn = 1024
	}
	if c.Credits.RegisterBonus == 0 {
		c.Credits.RegisterBonus = 4096
	}
	if c.Credits.InviteReward == 0 {
		c.Credits.InviteReward = 2048
	}
	if c.Credits.TaskCosts == nil {
		c.Credits.TaskCosts = map[string]int{
			"article":        4000,
			"seednote":       3200,
			"viral_analysis": 800,
		}
	} else if _, ok := c.Credits.TaskCosts["viral_analysis"]; !ok {
		c.Credits.TaskCosts["viral_analysis"] = 800
	}
	if c.Credits.GoalModeMultiplier <= 0 {
		c.Credits.GoalModeMultiplier = 3
	}
	if c.Credits.EcommerceModulePrices == nil {
		c.Credits.EcommerceModulePrices = map[string]int{
			"main_images":  1500, // 主图套（默认5张，CTR之战）
			"detail_page":  3000, // 详情页商详（默认8-12节，FABE叙事）
			"cover_banner": 600,  // 封面/类目 banner，每张
			"share_image":  400,  // 分享图，每张
			"sku_images":   300,  // SKU 变体图，每张
		}
	}

	// Asynq defaults.
	if c.Asynq.Concurrency == 0 {
		c.Asynq.Concurrency = 3
	}
	if c.Asynq.ContentGenerateTimeout == 0 {
		c.Asynq.ContentGenerateTimeout = 60 * time.Minute
	}
	if c.Asynq.PersistTimeout == 0 {
		c.Asynq.PersistTimeout = 10 * time.Minute
	}

	// Invitation defaults.
	if c.Invitation.MaxPerUser == 0 {
		c.Invitation.MaxPerUser = 3
	}

	// Email defaults.
	if c.Email.CodeTTL == 0 {
		c.Email.CodeTTL = 5 * time.Minute
	}
	if c.Email.CodeLength == 0 {
		c.Email.CodeLength = 6
	}
	if c.Email.SMTPPort == 0 {
		c.Email.SMTPPort = 587
	}

	// Writing LLM defaults.
	if c.Writing.Timeout == 0 {
		c.Writing.Timeout = 10 * time.Minute
	}
	if c.FunASR.Timeout == 0 {
		c.FunASR.Timeout = 10 * time.Minute
	}

	// Seednote sidecar defaults.
	if c.Seednote.BaseURL == "" {
		c.Seednote.BaseURL = "http://localhost:18060"
	}
	if c.Seednote.Timeout == 0 {
		c.Seednote.Timeout = 30
	}

	// wcfLink WeChat-bot sidecar defaults.
	if c.WCF.BaseURL == "" {
		c.WCF.BaseURL = "http://localhost:18070"
	}
	if c.WCF.Timeout == 0 {
		c.WCF.Timeout = 30
	}
	if c.WCF.PollInterval == 0 {
		c.WCF.PollInterval = 2
	}

	// Claude executor defaults.
	if c.Claude.Executor == "" {
		c.Claude.Executor = "local"
	}
	if c.Claude.MaxTurns == nil {
		c.Claude.MaxTurns = map[string]int{
			"article":   100,
			"seednote":  60,
			"ecommerce": 120, // 多产品图 → 产品档案 → 主图/详情/封面/分享/SKU 批量 + 视觉自检循环，给足余量
		}
	}
	if c.Claude.Docker.Image == "" {
		c.Claude.Docker.Image = "abwriter-agent:latest"
	}
	if c.Claude.Docker.CPUCores == 0 {
		c.Claude.Docker.CPUCores = 2
	}
	if c.Claude.Docker.MemoryMB == 0 {
		c.Claude.Docker.MemoryMB = 4096
	}
	if c.Claude.Docker.TimeoutSec == 0 {
		// Default 3600s (60m) to stay >= asynq.content_generate_timeout (60m).
		// The container must outlive the task deadline, otherwise it is killed
		// before the agent finishes (silent partial failure).
		c.Claude.Docker.TimeoutSec = 3600
	}
	// Auto-detect plugin_dir by searching for agents/.
	if c.Claude.PluginDir == "" {
		c.Claude.PluginDir = detectPluginDir()
	}
}

// AgentServerURL returns the server URL as reachable from the agent's network
// perspective. If claude.agent_server_url is set, it takes precedence (useful
// for k8s where the service URL differs from localhost/host.docker.internal).
// Otherwise, Docker agents resolve the host via host.docker.internal; local
// agents use localhost.
func (c *Config) AgentServerURL() string {
	if c.Claude.AgentServerURL != "" {
		return strings.TrimRight(c.Claude.AgentServerURL, "/")
	}
	switch c.Claude.Executor {
	case "docker":
		return fmt.Sprintf("http://host.docker.internal:%d", c.Server.Port)
	default:
		return fmt.Sprintf("http://localhost:%d", c.Server.Port)
	}
}

// resolvePaths resolves relative paths to absolute. Must be called after
// applyDefaults (and after ${VAR} expansion in NewConfig) so that values
// supplied via the environment are also resolved.
func (c *Config) resolvePaths() {
	// Resolve plugin_dir to absolute path if relative.
	// The Claude Code CLI subprocess runs with CWD set to a temp directory,
	// so relative paths like ".." would resolve incorrectly at execution time.
	if c.Claude.PluginDir != "" && !filepath.IsAbs(c.Claude.PluginDir) {
		if abs, err := filepath.Abs(c.Claude.PluginDir); err == nil {
			c.Claude.PluginDir = abs
		}
	}
	// Resolve docker workspace_dir to absolute path if relative.
	if c.Claude.Docker.WorkspaceDir != "" && !filepath.IsAbs(c.Claude.Docker.WorkspaceDir) {
		if abs, err := filepath.Abs(c.Claude.Docker.WorkspaceDir); err == nil {
			c.Claude.Docker.WorkspaceDir = abs
		}
	}
	// Resolve task_log_dir to absolute path if relative.
	if c.Claude.TaskLogDir != "" && !filepath.IsAbs(c.Claude.TaskLogDir) {
		if abs, err := filepath.Abs(c.Claude.TaskLogDir); err == nil {
			c.Claude.TaskLogDir = abs
		}
	}
}

// detectPluginDir attempts to locate the abwriter plugin directory
// that contains agents/. It checks the claudecode submodule first,
// then the legacy plugin/ subdirectory, then searches upward from CWD.
func detectPluginDir() string {
	var candidates []string
	if wd, err := os.Getwd(); err == nil {
		// Check for claudecode submodule.
		if info, err := os.Stat(filepath.Join(wd, "claudecode", "agents")); err == nil && info.IsDir() {
			return filepath.Join(wd, "claudecode")
		}
		// Check for legacy plugin/ subdirectory.
		if info, err := os.Stat(filepath.Join(wd, "plugin", "agents")); err == nil && info.IsDir() {
			return filepath.Join(wd, "plugin")
		}
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if info, err := os.Stat(filepath.Join(exeDir, "agents")); err == nil && info.IsDir() {
			return exeDir
		}
	}
	for _, dir := range candidates {
		for range 5 {
			if info, err := os.Stat(filepath.Join(dir, "agents")); err == nil && info.IsDir() {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

// Validate checks that required configuration fields are set.
func (c *Config) Validate() error {
	var errs []string

	if strings.TrimSpace(c.Database.DSN) == "" {
		errs = append(errs, "database.dsn is required")
	}
	if strings.TrimSpace(c.JWT.SecretKey) == "" {
		errs = append(errs, "jwt.secret_key is required")
	}
	if _, err := time.ParseDuration(c.JWT.AccessExpiry); err != nil {
		errs = append(errs, fmt.Sprintf("jwt.access_expiry is not a valid duration: %s", c.JWT.AccessExpiry))
	}
	if _, err := time.ParseDuration(c.JWT.RefreshExpiry); err != nil {
		errs = append(errs, fmt.Sprintf("jwt.refresh_expiry is not a valid duration: %s", c.JWT.RefreshExpiry))
	}

	if c.Storage.Provider == "oss" {
		if strings.TrimSpace(c.Storage.Endpoint) == "" {
			errs = append(errs, "storage.endpoint is required when provider is \"oss\"")
		}
		if strings.TrimSpace(c.Storage.AccessKeyID) == "" {
			errs = append(errs, "storage.access_key_id is required when provider is \"oss\"")
		}
		if strings.TrimSpace(c.Storage.AccessKeySecret) == "" {
			errs = append(errs, "storage.access_key_secret is required when provider is \"oss\"")
		}
		if strings.TrimSpace(c.Storage.BucketName) == "" {
			errs = append(errs, "storage.bucket_name is required when provider is \"oss\"")
		}
	}

	if !c.TingWu.Empty() {
		if strings.TrimSpace(c.TingWu.Endpoint) == "" {
			errs = append(errs, "tingwu.endpoint is required when any TingWu setting is configured")
		}
		if strings.TrimSpace(c.TingWu.Region) == "" {
			errs = append(errs, "tingwu.region is required when any TingWu setting is configured")
		}
		if strings.TrimSpace(c.TingWu.AppKey) == "" {
			errs = append(errs, "tingwu.app_key is required when any TingWu setting is configured")
		}
		if strings.TrimSpace(c.TingWu.AccessKey) == "" {
			errs = append(errs, "tingwu.access_key is required when any TingWu setting is configured")
		}
		if strings.TrimSpace(c.TingWu.AccessSecret) == "" {
			errs = append(errs, "tingwu.access_secret is required when any TingWu setting is configured")
		}
	}

	if !c.FunASR.Empty() {
		if strings.TrimSpace(c.FunASR.BaseURL) == "" {
			errs = append(errs, "funasr.base_url is required when any FunASR setting is configured")
		}
		if strings.TrimSpace(c.FunASR.APIKey) == "" {
			errs = append(errs, "funasr.api_key is required when any FunASR setting is configured")
		}
		if strings.TrimSpace(c.FunASR.Model) == "" {
			errs = append(errs, "funasr.model is required when any FunASR setting is configured")
		}
	}

	if c.Claude.Executor != "local" && c.Claude.Executor != "docker" {
		errs = append(errs, fmt.Sprintf("claude.executor must be 'local' or 'docker', got %q", c.Claude.Executor))
	}

	// When using the Docker executor, the container timeout must be at least as
	// long as content_generate_timeout, otherwise the container is killed before
	// the asynq task deadline (the agent's work is lost mid-pipeline).
	if c.Claude.Executor == "docker" {
		if time.Duration(c.Claude.Docker.TimeoutSec)*time.Second < c.Asynq.ContentGenerateTimeout {
			errs = append(errs, fmt.Sprintf(
				"claude.docker.timeout_sec (%ds) must be >= asynq.content_generate_timeout (%s); otherwise the container is killed before the task deadline",
				c.Claude.Docker.TimeoutSec, c.Asynq.ContentGenerateTimeout))
		}
	}

	if c.Claude.AgentServerURL != "" {
		u := strings.TrimSpace(c.Claude.AgentServerURL)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			errs = append(errs, fmt.Sprintf("claude.agent_server_url must start with http:// or https://, got %q", u))
		}
	}

	if c.Credits.DailySignIn < 0 {
		errs = append(errs, "credits.daily_sign_in must not be negative")
	}
	if c.Credits.RegisterBonus < 0 {
		errs = append(errs, "credits.register_bonus must not be negative")
	}
	if c.Credits.InviteReward < 0 {
		errs = append(errs, "credits.invite_reward must not be negative")
	}
	if c.Claude.Executor == "local" {
		if strings.TrimSpace(c.Claude.PluginDir) == "" {
			errs = append(errs, "claude.plugin_dir is required for agent execution (set it in config.yaml, or ensure agents/ exists in a parent directory)")
		} else {
			agentsDir := filepath.Join(c.Claude.PluginDir, "agents")
			if info, err := os.Stat(agentsDir); err != nil || !info.IsDir() {
				errs = append(errs, fmt.Sprintf("claude.plugin_dir %q does not contain agents/ directory (checked %q)", c.Claude.PluginDir, agentsDir))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}
