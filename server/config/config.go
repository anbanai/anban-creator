package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/resource"

	appconfig "github.com/anbanai/anban-creator/app/config"
	serverbilling "github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
)

// Config holds all server configuration.
type Config struct {
	Server             ServerConfig                    `yaml:"server"`
	Logging            LoggingConfig                   `yaml:"logging"`
	Database           DatabaseConfig                  `yaml:"database"`
	Redis              RedisConfig                     `yaml:"redis"`
	JWT                JWTConfig                       `yaml:"jwt"`
	WeChat             WeChatConfig                    `yaml:"wechat"`
	Storage            StorageConfig                   `yaml:"storage"`
	MCP                MCPConfig                       `yaml:"mcp"`
	ImageAPI           ImageAPIConfig                  `yaml:"image_api"`
	Montage            MontageConfig                   `yaml:"montage"`
	ImagePresets       []ImageModelPreset              `yaml:"image_presets"`
	Writing            WritingConfig                   `yaml:"writing"`
	Vision             VisionConfig                    `yaml:"vision"`
	ModelProviders     map[string]ModelProviderConfig  `yaml:"model_providers"`
	ModelRoutes        ModelRoutesConfig               `yaml:"model_routes"`
	BillingRuntime     BillingRuntimeConfig            `yaml:"billing_runtime" json:"billing_runtime"`
	BillingBundle      *serverbilling.Bundle           `yaml:"-" json:"-"`
	ImageUnderstanding UnderstandingRuntimeConfig      `yaml:"-"`
	VideoUnderstanding VideoUnderstandingRuntimeConfig `yaml:"-"`
	TingWu             TingWuConfig                    `yaml:"tingwu"`
	Claude             ClaudeConfig                    `yaml:"claude"`
	CORS               CORSConfig                      `yaml:"cors"`
	Asynq              AsynqConfig                     `yaml:"asynq"`
	Email              EmailConfig                     `yaml:"email"`
	Invitation         InvitationConfig                `yaml:"invitation"`
	Seednote           SeednoteConfig                  `yaml:"seednote"`
	Ilink              IlinkConfig                     `yaml:"ilink"`
	Memory             MemoryConfig                    `yaml:"memory"`
}

func validateKubernetesResourceConfig(configPath string, cfg KubernetesResourceConfig) []string {
	var errs []string
	parsedRequests := make(map[string]resource.Quantity, len(cfg.Requests))
	parsedLimits := make(map[string]resource.Quantity, len(cfg.Limits))
	for group, values := range map[string]map[string]string{"requests": cfg.Requests, "limits": cfg.Limits} {
		for name, raw := range values {
			quantity, err := resource.ParseQuantity(strings.TrimSpace(raw))
			if strings.TrimSpace(name) == "" || err != nil || quantity.Sign() <= 0 {
				errs = append(errs, fmt.Sprintf("%s.%s.%s must be a positive Kubernetes quantity", configPath, group, name))
				continue
			}
			if group == "requests" {
				parsedRequests[name] = quantity
			} else {
				parsedLimits[name] = quantity
			}
		}
	}
	for name, request := range parsedRequests {
		if limit, ok := parsedLimits[name]; ok && request.Cmp(limit) > 0 {
			errs = append(errs, fmt.Sprintf("%s.requests.%s must not exceed its limit", configPath, name))
		}
	}
	return errs
}

// ImageModelPreset defines a system-managed image model that users can select
// when creating tasks or plans. Each preset has a minimum tier that gates access.
type ImageModelPreset struct {
	Key           string                       `yaml:"key"`            // unique identifier, e.g. "volcengine-standard"
	DisplayName   string                       `yaml:"display_name"`   // user-facing label
	ProviderRoute string                       `yaml:"provider_route"` // semantic route, e.g. image_generation.designer.seedream
	Provider      string                       `yaml:"provider"`       // derived provider kind or legacy direct provider
	Model         string                       `yaml:"model"`          // concrete model id
	Endpoint      string                       `yaml:"endpoint"`
	APIKey        string                       `yaml:"api_key"`
	Timeout       time.Duration                `yaml:"timeout"`
	MinTier       string                       `yaml:"min_tier"` // free / pro / enterprise
	QualityRank   int                          `yaml:"quality_rank"`
	Capabilities  DesignerProviderCapabilities `yaml:"capabilities"`
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

// IlinkConfig holds the ilink WeChat assistant channel configuration. wcflink is
// the current transport implementation behind this platform channel.
type IlinkConfig struct {
	Enabled              bool   `yaml:"enabled"`
	BaseURL              string `yaml:"base_url"`
	Timeout              int    `yaml:"timeout"`
	PollInterval         int    `yaml:"poll_interval"`
	NotificationRetryMax int    `yaml:"notification_retry_max"`
	AssistantAccountID   string `yaml:"assistant_account_id"`
	AssistantName        string `yaml:"assistant_name"`
	AssistantWechatID    string `yaml:"assistant_wechat_id"`
	AssistantQRCodeURL   string `yaml:"assistant_qrcode_url"`
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
	Port        int    `yaml:"port"` // default 8080
	Host        string `yaml:"host"` // default "0.0.0.0"
	TLSCertFile string `yaml:"tls_cert_file"`
	TLSKeyFile  string `yaml:"tls_key_file"`
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
	APIKey       string                `yaml:"api_key"` // API key for MCP endpoint authentication
	ToolTimeouts MCPToolTimeoutsConfig `yaml:"tool_timeouts"`
}

type MCPToolTimeoutsConfig struct {
	GenerateImage time.Duration `yaml:"generate_image"`
}

type MontageConfig struct {
	Enabled                bool                                   `yaml:"enabled"`
	enabledSet             bool                                   `yaml:"-"`
	SubmodulePath          string                                 `yaml:"submodule_path"`
	DefaultPipeline        string                                 `yaml:"default_pipeline"`
	AllowedPipelines       []string                               `yaml:"allowed_pipelines"`
	MaxDurationSeconds     int64                                  `yaml:"max_duration_seconds"`
	MaxAssets              int                                    `yaml:"max_assets"`
	TimeoutMinutes         int                                    `yaml:"timeout_minutes"`
	ExecutionTargets       []string                               `yaml:"execution_targets"`
	DefaultExecutionTarget string                                 `yaml:"default_execution_target"`
	Env                    map[string]string                      `yaml:"env"`
	ToolPolicy             map[string]MontageToolCapabilityPolicy `yaml:"tool_policy"`
	PipelineDefaults       map[string]map[string]any              `yaml:"pipeline_defaults"`
}

type MontageToolCapabilityPolicy struct {
	Preferred []string `yaml:"preferred" json:"preferred,omitempty"`
	Allowed   []string `yaml:"allowed" json:"allowed,omitempty"`
	Disabled  []string `yaml:"disabled" json:"disabled,omitempty"`
	Notes     string   `yaml:"notes" json:"notes,omitempty"`
}

func (c *MontageConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawMontageConfig MontageConfig
	var raw rawMontageConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		if value.Content[i].Value == "enabled" {
			raw.enabledSet = true
			break
		}
	}
	*c = MontageConfig(raw)
	return nil
}

func (c *MontageConfig) ApplyDefaults() {
	if !c.enabledSet {
		c.Enabled = true
	}
	if c.SubmodulePath == "" {
		c.SubmodulePath = "third_party/OpenMontage"
	}
	if c.DefaultPipeline == "" {
		c.DefaultPipeline = "cinematic"
	}
	if len(c.AllowedPipelines) == 0 {
		c.AllowedPipelines = []string{"cinematic", "talking-head", "screen-demo", "clip-factory"}
	}
	if c.MaxDurationSeconds <= 0 {
		c.MaxDurationSeconds = 600
	}
	if c.MaxAssets <= 0 {
		c.MaxAssets = 20
	}
	if c.TimeoutMinutes <= 0 {
		c.TimeoutMinutes = 90
	}
	if len(c.ExecutionTargets) == 0 {
		c.ExecutionTargets = []string{"cloud", "local"}
	}
	if c.DefaultExecutionTarget == "" {
		c.DefaultExecutionTarget = "cloud"
	}
	if c.Env == nil {
		c.Env = map[string]string{}
	}
	if c.ToolPolicy == nil {
		c.ToolPolicy = map[string]MontageToolCapabilityPolicy{}
	}
	if c.PipelineDefaults == nil {
		c.PipelineDefaults = map[string]map[string]any{}
	}
}

func (c MontageConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.SubmodulePath) == "" {
		return fmt.Errorf("montage.submodule_path is required")
	}
	if strings.TrimSpace(c.DefaultPipeline) == "" {
		return fmt.Errorf("montage.default_pipeline is required")
	}
	if c.MaxDurationSeconds <= 0 {
		return fmt.Errorf("montage.max_duration_seconds must be positive")
	}
	if c.MaxAssets <= 0 {
		return fmt.Errorf("montage.max_assets must be positive")
	}
	if c.TimeoutMinutes <= 0 {
		return fmt.Errorf("montage.timeout_minutes must be positive")
	}
	if !montageStringSliceContains(c.AllowedPipelines, c.DefaultPipeline) {
		return fmt.Errorf("montage.default_pipeline must be in montage.allowed_pipelines")
	}
	if !validMontageTarget(c.DefaultExecutionTarget) {
		return fmt.Errorf("montage.default_execution_target must be cloud or local")
	}
	if !montageStringSliceContains(c.ExecutionTargets, c.DefaultExecutionTarget) {
		return fmt.Errorf("montage.default_execution_target must be in montage.execution_targets")
	}
	for _, target := range c.ExecutionTargets {
		if !validMontageTarget(target) {
			return fmt.Errorf("montage.execution_targets contains invalid target %q", target)
		}
	}
	for key, value := range c.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") {
			return fmt.Errorf("montage.env contains invalid key %q", key)
		}
		if strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("montage.env value for %q contains a NUL byte", key)
		}
	}
	return nil
}

func (c MontageConfig) RedactedEnv() map[string]bool {
	redacted := make(map[string]bool, len(c.Env))
	for key, value := range c.Env {
		redacted[key] = strings.TrimSpace(value) != ""
	}
	return redacted
}

func validMontageTarget(target string) bool {
	return target == "cloud" || target == "local"
}

func montageStringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// ModelProviderConfig describes a reusable provider endpoint/key.
type ModelProviderConfig struct {
	Protocol string `yaml:"protocol"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
}

// RouteConfig binds a business route to a provider/model.
type RouteConfig struct {
	Provider string        `yaml:"provider"`
	Model    string        `yaml:"model"`
	Timeout  time.Duration `yaml:"timeout"`
}

type UnderstandingRouteConfig struct {
	RouteConfig  `yaml:",inline"`
	RequireUsage bool `yaml:"require_usage"`
}

type VideoUnderstandingRouteConfig struct {
	UnderstandingRouteConfig `yaml:",inline"`
	RequireNativeVideo       bool   `yaml:"require_native_video"`
	MaxRecommendedResolution string `yaml:"max_recommended_resolution"`
}

type ImageGenerationRouteConfig struct {
	Provider       string                       `yaml:"provider"`
	Model          string                       `yaml:"model"`
	Timeout        time.Duration                `yaml:"timeout"`
	Alias          string                       `yaml:"alias"`
	Enabled        bool                         `yaml:"enabled"`
	QualityRank    int                          `yaml:"quality_rank" json:"quality_rank"`
	ResponseFormat string                       `yaml:"response_format"`
	Capabilities   DesignerProviderCapabilities `yaml:"capabilities" json:"capabilities"`
}

type ImageGenerationRoutesConfig struct {
	Cover    ImageGenerationRouteConfig            `yaml:"cover"`
	Content  ImageGenerationRouteConfig            `yaml:"content"`
	Designer map[string]ImageGenerationRouteConfig `yaml:"designer"`
}

type DesignerProviderCapabilities struct {
	QualityLevels      []string `yaml:"quality_levels" json:"quality_levels"`
	SizePresets        []string `yaml:"size_presets" json:"size_presets"`
	DefaultSize        string   `yaml:"default_size" json:"default_size"`
	MaxBatch           int      `yaml:"max_batch" json:"max_batch"`
	MaxReferenceImages int      `yaml:"max_reference_images" json:"max_reference_images"`
	SupportsReference  bool     `yaml:"supports_reference" json:"supports_reference"`
	SupportsMask       bool     `yaml:"supports_mask" json:"supports_mask"`
	OutputFormats      []string `yaml:"output_formats" json:"output_formats"`
	HasBackground      bool     `yaml:"has_background" json:"has_background"`
	HasCompression     bool     `yaml:"has_compression" json:"has_compression"`
	Watermark          bool     `yaml:"watermark" json:"watermark"`
}

type ModelRoutesConfig struct {
	Writing            RouteConfig                   `yaml:"writing"`
	ImageUnderstanding UnderstandingRouteConfig      `yaml:"image_understanding"`
	VideoUnderstanding VideoUnderstandingRouteConfig `yaml:"video_understanding"`
	ImageGeneration    ImageGenerationRoutesConfig   `yaml:"image_generation"`
}

type UnderstandingRuntimeConfig struct {
	BaseURL      string
	Key          string
	Model        string
	Provider     string
	ProviderKey  string
	Timeout      time.Duration
	RequireUsage bool
}

type VideoUnderstandingRuntimeConfig struct {
	UnderstandingRuntimeConfig
	RequireNativeVideo       bool
	MaxRecommendedResolution string
}

// BillingRuntimeConfig points the server at the strict fixed-SKU billing
// catalogs required by server startup.
type BillingRuntimeConfig struct {
	ConfigDir   string `yaml:"config_dir" json:"config_dir"`
	AdminAPIKey string `yaml:"admin_api_key" json:"-"`
}

func (c *BillingRuntimeConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("billing_runtime must be a mapping")
	}
	seen := make(map[string]struct{}, len(value.Content)/2)
	for index := 0; index+1 < len(value.Content); index += 2 {
		key := value.Content[index].Value
		switch key {
		case "config_dir", "admin_api_key":
		default:
			return fmt.Errorf("unknown billing_runtime.%s", key)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate billing_runtime.%s", key)
		}
		seen[key] = struct{}{}
	}
	type plain BillingRuntimeConfig
	var decoded plain
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*c = BillingRuntimeConfig(decoded)
	return nil
}

type TokenUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CachedInputTokens        int64 `json:"cached_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	OutputTokens             int64 `json:"output_tokens"`
	TotalTokens              int64 `json:"total_tokens"`
}

// StorageConfig holds file storage configuration.
// Supports "oss" (Alibaba Cloud OSS) or "local" (filesystem).
type StorageConfig struct {
	Provider                   string `yaml:"provider"` // "oss" or "local", default "local"
	Endpoint                   string `yaml:"endpoint"` // OSS endpoint, e.g. "oss-cn-hangzhou.aliyuncs.com"
	AccessKeyID                string `yaml:"access_key_id"`
	AccessKeySecret            string `yaml:"access_key_secret"`
	BucketName                 string `yaml:"bucket_name"`
	Region                     string `yaml:"region"`
	CustomDomain               string `yaml:"custom_domain"`                 // Optional CDN domain for public file URLs
	STSRoleArn                 string `yaml:"sts_role_arn"`                  // Optional RAM role used for browser direct uploads
	STSSessionName             string `yaml:"sts_session_name"`              // Optional direct upload STS session name
	STSEndpoint                string `yaml:"sts_endpoint"`                  // Optional STS endpoint override
	DirectUploadExpiresSeconds int    `yaml:"direct_upload_expires_seconds"` // Default 900
	LocalDataDir               string `yaml:"local_data_dir"`                // Default "./data/files"
}

// SizesConfig is kept for in-memory legacy ImageAPI structs. Semantic server
// config must not use it; business image ratios live in task/project inputs and
// Skill workflows.
type SizesConfig struct {
	ArticleCover    string `yaml:"article_cover"`
	ArticleContent  string `yaml:"article_content"`
	XLSCover        string `yaml:"xls_cover"`
	XLSContent      string `yaml:"xls_content"`
	SeednoteCover   string `yaml:"seednote_cover"`
	SeednoteContent string `yaml:"seednote_content"`
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

// WritingConfig holds the default OpenAI-compatible text LLM route for
// LLM-backed writing-adjacent services.
//
// Markdown-to-WeChat HTML conversion is deterministic and does not use this
// route.
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

// ClaudeConfig owns the direct provider contract for every Claude runtime.
type ClaudeConfig struct {
	Provider       string             `yaml:"provider" json:"provider"`
	BaseURL        string             `yaml:"base_url" json:"base_url"`
	AuthToken      string             `yaml:"auth_token" json:"-"`
	Models         ClaudeModelsConfig `yaml:"models" json:"models"`
	UsageAliases   map[string]string  `yaml:"model_usage_aliases" json:"model_usage_aliases"`
	Executor       string             `yaml:"executor"` // "local" (default), "docker", or "kubernetes"
	Env            map[string]string  `yaml:"env"`
	PluginDir      string             `yaml:"plugin_dir"`       // Path to the Anban Creator plugin directory (contains agents/, skills/)
	Sandbox        bool               `yaml:"sandbox"`          // Enable sandbox isolation for agent execution (recommended in k8s)
	Docker         DockerConfig       `yaml:"docker"`           // Docker executor settings (used when executor=docker)
	Kubernetes     KubernetesConfig   `yaml:"kubernetes"`       // Kubernetes executor settings (used when executor=kubernetes)
	MaxTurns       map[string]int     `yaml:"max_turns"`        // Per-task-type max turns, e.g. {"article": 60, "seednote": 100}
	TaskLogDir     string             `yaml:"task_log_dir"`     // Directory for per-task agent execution logs. Empty = disabled.
	AgentServerURL string             `yaml:"agent_server_url"` // Override server URL for agent MCP connections (e.g. k8s service URL). To env-control, write ${ANBAN_CLAUDE_AGENT_SERVER_URL} in config.yaml.
}

type ClaudeModelsConfig struct {
	Default string `yaml:"default" json:"default"`
	Opus    string `yaml:"opus" json:"opus"`
	Fable   string `yaml:"fable" json:"fable"`
	Sonnet  string `yaml:"sonnet" json:"sonnet"`
	Haiku   string `yaml:"haiku" json:"haiku"`
}

var claudeControlEnvKeys = map[string]bool{
	"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": true,
	"CLAUDE_CODE_DISABLE_AUTO_MEMORY":          true,
	"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          true,
}

const (
	ClaudeProviderVolcengineArk = "volcengine_ark"
	ClaudeArkCompatibleBaseURL  = "https://ark.cn-beijing.volces.com/api/compatible"
)

func (c *ClaudeConfig) UnmarshalYAML(value *yaml.Node) error {
	known := map[string]bool{
		"provider": true, "base_url": true, "auth_token": true, "models": true,
		"model_usage_aliases": true, "executor": true, "env": true, "plugin_dir": true,
		"sandbox": true, "docker": true, "kubernetes": true, "max_turns": true,
		"task_log_dir": true, "agent_server_url": true,
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("claude config must be a mapping")
	}
	for i := 0; i < len(value.Content); i += 2 {
		key := value.Content[i].Value
		if !known[key] {
			if key == "model" {
				return fmt.Errorf("claude.model is not supported; use claude.models.default")
			}
			return fmt.Errorf("unknown claude config field %q", key)
		}
	}
	type plain ClaudeConfig
	return value.Decode((*plain)(c))
}

func (c *ClaudeModelsConfig) UnmarshalYAML(value *yaml.Node) error {
	known := map[string]bool{"default": true, "opus": true, "fable": true, "sonnet": true, "haiku": true}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("claude.models must be a mapping")
	}
	for i := 0; i < len(value.Content); i += 2 {
		if key := value.Content[i].Value; !known[key] {
			return fmt.Errorf("unknown claude.models field %q", key)
		}
	}
	type plain ClaudeModelsConfig
	return value.Decode((*plain)(c))
}

func (c ClaudeConfig) String() string {
	return fmt.Sprintf("ClaudeConfig{Provider:%q BaseURL:%q AuthToken:[REDACTED] Models:%v Executor:%q}", c.Provider, c.BaseURL, c.Models, c.Executor)
}

func (c ClaudeConfig) GoString() string {
	return c.String()
}

func (c ClaudeConfig) RuntimeEnv() map[string]string {
	result := map[string]string{
		"ANTHROPIC_BASE_URL":             c.BaseURL,
		"ANTHROPIC_AUTH_TOKEN":           c.AuthToken,
		"ANTHROPIC_MODEL":                c.Models.Default,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   c.Models.Opus,
		"ANTHROPIC_DEFAULT_FABLE_MODEL":  c.Models.Fable,
		"ANTHROPIC_DEFAULT_SONNET_MODEL": c.Models.Sonnet,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  c.Models.Haiku,
	}
	for key, value := range c.Env {
		result[key] = value
	}
	return result
}

func (c ClaudeConfig) RuntimeModelUsageAliases() map[string]model.ModelUsageIdentity {
	result := make(map[string]model.ModelUsageIdentity)
	for _, canonical := range []string{c.Models.Default, c.Models.Opus, c.Models.Fable, c.Models.Sonnet, c.Models.Haiku} {
		if canonical != "" {
			result[canonical] = model.ModelUsageIdentity{Provider: c.Provider, Model: canonical}
		}
	}
	for raw, canonical := range c.UsageAliases {
		result[raw] = model.ModelUsageIdentity{Provider: c.Provider, Model: canonical}
	}
	return result
}

func (c ClaudeConfig) Validate() error {
	var errs []string
	required := []struct{ path, value string }{
		{"claude.provider", c.Provider}, {"claude.base_url", c.BaseURL}, {"claude.auth_token", c.AuthToken},
		{"claude.models.default", c.Models.Default}, {"claude.models.opus", c.Models.Opus},
		{"claude.models.fable", c.Models.Fable}, {"claude.models.sonnet", c.Models.Sonnet}, {"claude.models.haiku", c.Models.Haiku},
	}
	configuredModels := make(map[string]bool)
	for _, item := range required {
		if strings.TrimSpace(item.value) == "" {
			errs = append(errs, item.path+" is required")
			continue
		}
		if strings.TrimSpace(item.value) != item.value || strings.ContainsRune(item.value, '\x00') {
			errs = append(errs, item.path+" must not contain surrounding whitespace or NUL")
		}
		if strings.Contains(item.value, "[1M]") {
			errs = append(errs, item.path+" must not contain [1M]")
		}
		if strings.HasPrefix(item.path, "claude.models.") {
			configuredModels[item.value] = true
		}
	}
	roleModels := []struct{ role, value, expected string }{
		{"default", c.Models.Default, "doubao-seed-evolving"},
		{"opus", c.Models.Opus, "doubao-seed-evolving"},
		{"fable", c.Models.Fable, "doubao-seed-evolving"},
		{"sonnet", c.Models.Sonnet, "doubao-seed-2-1-pro-260628"},
		{"haiku", c.Models.Haiku, "doubao-seed-2-1-turbo-260628"},
	}
	for _, role := range roleModels {
		if role.value != "" && role.value != role.expected {
			errs = append(errs, "claude.models."+role.role+" must be "+role.expected)
		}
	}
	if err := model.ValidateModelUsageAliases(c.RuntimeModelUsageAliases()); err != nil {
		errs = append(errs, "claude.model_usage_aliases: "+err.Error())
	}
	if c.UsageAliases["doubao-seed-evolving-latest-version"] != "doubao-seed-evolving" {
		errs = append(errs, "claude.model_usage_aliases.doubao-seed-evolving-latest-version is required and must target doubao-seed-evolving")
	}
	if c.Provider != "" && c.Provider != ClaudeProviderVolcengineArk {
		errs = append(errs, "claude.provider must be volcengine_ark")
	}
	if parsed, err := url.Parse(c.BaseURL); err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		errs = append(errs, "claude.base_url must be a direct HTTPS provider URL")
	}
	if c.BaseURL != "" && c.BaseURL != ClaudeArkCompatibleBaseURL {
		errs = append(errs, "claude.base_url must be "+ClaudeArkCompatibleBaseURL)
	}
	for key, value := range c.Env {
		if strings.HasPrefix(key, "ANTHROPIC_") {
			errs = append(errs, "claude.env."+key+" duplicates typed Claude provider configuration")
			continue
		}
		if !claudeControlEnvKeys[key] {
			errs = append(errs, "claude.env."+key+" is not an allowed Claude runtime control")
		}
		if value == "" || strings.TrimSpace(value) != value || strings.ContainsRune(value, '\x00') {
			errs = append(errs, "claude.env."+key+" has an invalid value")
		}
	}
	for raw, canonical := range c.UsageAliases {
		if strings.Contains(raw, "[1M]") || strings.Contains(canonical, "[1M]") {
			errs = append(errs, "claude.model_usage_aliases must not contain [1M]")
		}
		if !configuredModels[canonical] {
			errs = append(errs, "claude.model_usage_aliases."+raw+" must target a configured Claude model")
		}
		if configuredModels[raw] && raw != canonical {
			errs = append(errs, "claude.model_usage_aliases."+raw+" must not remap a canonical Claude model")
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// DockerConfig holds Docker executor settings for container-based task execution.
type DockerConfig struct {
	Image         string            `yaml:"image"`          // Default content image (default: "creator-agent-content:latest")
	ImageProfiles map[string]string `yaml:"image_profiles"` // Images for task types with additional runtime dependencies
	CPUCores      int64             `yaml:"cpu_cores"`      // CPU limit in cores (default: 2)
	MemoryMB      int64             `yaml:"memory_mb"`      // Memory limit in MB (default: 4096)
	TimeoutSec    int               `yaml:"timeout_sec"`    // Container execution timeout in seconds (default: 1800 = 30 min)
	ContainerName string            `yaml:"container_name"` // Persistent content container; profile tasks use one-shot containers and inherit its volumes
	WorkspaceDir  string            `yaml:"workspace_dir"`  // Host-side base directory for task workspaces (persistent container mode, must match volume mount source)
}

// KubernetesConfig holds ACK/Kubernetes Job runtime settings.
type KubernetesConfig struct {
	Namespace            string            `yaml:"namespace"`
	AgentImage           string            `yaml:"agent_image"`
	ImageProfiles        map[string]string `yaml:"image_profiles"`
	ServiceAccount       string            `yaml:"service_account"`
	ImagePullSecret      string            `yaml:"image_pull_secret"`
	ServerCASecret       string            `yaml:"server_ca_secret"`
	ExecutionTokenSecret string            `yaml:"execution_token_secret"`

	NASStorageClass         string                              `yaml:"nas_storage_class"`
	ProjectMemorySize       string                              `yaml:"project_memory_size"`
	TaskWorkspaceSize       string                              `yaml:"task_workspace_size"`
	ActiveDeadlineSeconds   int64                               `yaml:"active_deadline_seconds"`
	HeartbeatTimeoutSeconds int64                               `yaml:"heartbeat_timeout_seconds"`
	CompletionGraceSeconds  int                                 `yaml:"completion_grace_seconds"`
	completionGraceSet      bool                                `yaml:"-"`
	TTLSecondsAfterFinished int32                               `yaml:"ttl_seconds_after_finished"`
	PreStartRetryLimit      int                                 `yaml:"pre_start_retry_limit"`
	preStartRetryLimitSet   bool                                `yaml:"-"`
	Resources               KubernetesResourceConfig            `yaml:"resources"`
	ResourceProfiles        map[string]KubernetesResourceConfig `yaml:"resource_profiles"`
}

type RuntimeImageSelection struct {
	Profile string
	Image   string
}

func imageForTask(defaultImage string, profiles map[string]string, taskType string) RuntimeImageSelection {
	taskType = strings.TrimSpace(taskType)
	if image := strings.TrimSpace(profiles[taskType]); image != "" {
		return RuntimeImageSelection{Profile: taskType, Image: image}
	}
	return RuntimeImageSelection{Profile: "content", Image: strings.TrimSpace(defaultImage)}
}

func (c DockerConfig) ImageForTask(taskType string) RuntimeImageSelection {
	return imageForTask(c.Image, c.ImageProfiles, taskType)
}

func (c KubernetesConfig) ImageForTask(taskType string) RuntimeImageSelection {
	return imageForTask(c.AgentImage, c.ImageProfiles, taskType)
}

func isAgentImageProfileTaskType(taskType string) bool {
	switch taskType {
	case model.PlatformArticle,
		model.PlatformSeednote,
		model.PlatformMoments,
		model.PlatformEcommerce,
		model.PlatformMontage:
		return true
	default:
		return false
	}
}

func (c *KubernetesConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawKubernetesConfig KubernetesConfig
	var raw rawKubernetesConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}

	*c = KubernetesConfig(raw)
	for i := 0; i+1 < len(value.Content); i += 2 {
		switch value.Content[i].Value {
		case "completion_grace_seconds":
			c.completionGraceSet = true
		case "pre_start_retry_limit":
			c.preStartRetryLimitSet = true
		}
	}
	return nil
}

// KubernetesResourceConfig mirrors Kubernetes resource maps without importing
// client-go types into the config package.
type KubernetesResourceConfig struct {
	Requests map[string]string `yaml:"requests"`
	Limits   map[string]string `yaml:"limits"`
}

// ResourcesForTask returns a copy of the default resource contract overlaid by
// the task-specific profile. Profiles may override individual resource keys.
func (c KubernetesConfig) ResourcesForTask(taskType string) KubernetesResourceConfig {
	profile := c.ResourceProfiles[strings.TrimSpace(taskType)]
	return KubernetesResourceConfig{
		Requests: mergeKubernetesResourceValues(c.Resources.Requests, profile.Requests),
		Limits:   mergeKubernetesResourceValues(c.Resources.Limits, profile.Limits),
	}
}

func mergeKubernetesResourceValues(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(override))
	for name, value := range base {
		merged[name] = value
	}
	for name, value := range override {
		merged[name] = value
	}
	return merged
}

// MemoryConfig holds Claude Code project memory projection settings.
type MemoryConfig struct {
	Enabled         bool          `yaml:"enabled"`
	Provider        string        `yaml:"provider"`
	OSSPrefix       string        `yaml:"oss_prefix"`
	RuntimeDir      string        `yaml:"runtime_dir"`
	MaxArchiveBytes int64         `yaml:"max_archive_bytes"`
	MergeOnStatus   string        `yaml:"merge_on_status"`
	LockTTL         time.Duration `yaml:"lock_ttl"`
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
	// with vision-verified images can run ~35-40m when max_turns.article is high.
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
	if err := rejectDeprecatedConfigKeys(data); err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.deriveModelRouteRuntimeConfig(); err != nil {
		return nil, err
	}
	configPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config file path: %w", err)
	}
	cfg.resolvePaths(filepath.Dir(configPath))
	if cfg.BillingRuntime.ConfigDir != "" {
		cfg.BillingBundle, err = serverbilling.LoadBundle(cfg.BillingRuntime.ConfigDir)
		if err != nil {
			return nil, fmt.Errorf("load billing bundle: %w", err)
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func rejectDeprecatedConfigKeys(data []byte) error {
	var top map[string]any
	if err := yaml.Unmarshal(data, &top); err != nil {
		return fmt.Errorf("parse config keys: %w", err)
	}
	deprecated := map[string]string{
		"vision":    "model_routes.image_understanding and model_routes.video_understanding",
		"writing":   "model_routes.writing",
		"image_api": "model_routes.image_generation",
	}
	for key, replacement := range deprecated {
		if _, ok := top[key]; ok {
			return fmt.Errorf("deprecated top-level config key %s; use %s", key, replacement)
		}
	}
	known := map[string]bool{
		"server":          true,
		"logging":         true,
		"database":        true,
		"redis":           true,
		"jwt":             true,
		"wechat":          true,
		"storage":         true,
		"mcp":             true,
		"montage":         true,
		"image_presets":   true,
		"model_providers": true,
		"model_routes":    true,
		"billing_runtime": true,
		"tingwu":          true,
		"claude":          true,
		"cors":            true,
		"asynq":           true,
		"email":           true,
		"invitation":      true,
		"seednote":        true,
		"ilink":           true,
		"memory":          true,
	}
	for key := range top {
		if !known[key] {
			return fmt.Errorf("unknown top-level config key %s", key)
		}
	}
	if modelRoutes, ok := top["model_routes"].(map[string]any); ok {
		if imageGeneration, ok := modelRoutes["image_generation"].(map[string]any); ok {
			if _, ok := imageGeneration["sizes"]; ok {
				return fmt.Errorf("deprecated config key model_routes.image_generation.sizes; put business image sizes in the relevant Skill workflow")
			}
			for _, routeName := range []string{"cover", "content"} {
				if route, ok := imageGeneration[routeName].(map[string]any); ok {
					if _, ok := route["size"]; ok {
						return fmt.Errorf("deprecated config key model_routes.image_generation.%s.size; image size is a business workflow rule, not a model route setting", routeName)
					}
				}
			}
		}
	}
	return nil
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
	if c.Memory.Provider == "" {
		c.Memory.Provider = "oss"
	}
	if c.Memory.OSSPrefix == "" {
		c.Memory.OSSPrefix = "claude-memory/projects"
	}
	if c.Memory.RuntimeDir == "" {
		c.Memory.RuntimeDir = ".claude/memory"
	}
	if c.Memory.MaxArchiveBytes == 0 {
		c.Memory.MaxArchiveBytes = 262144
	}
	if c.Memory.MergeOnStatus == "" {
		c.Memory.MergeOnStatus = "completed"
	}
	if c.Memory.LockTTL == 0 {
		c.Memory.LockTTL = time.Minute
	}
	if c.MCP.ToolTimeouts.GenerateImage == 0 {
		c.MCP.ToolTimeouts.GenerateImage = 10 * time.Minute
	}
	if c.ModelRoutes.ImageGeneration.Cover.Timeout == 0 {
		c.ModelRoutes.ImageGeneration.Cover.Timeout = 5 * time.Minute
	}
	if c.ModelRoutes.ImageGeneration.Content.Timeout == 0 {
		c.ModelRoutes.ImageGeneration.Content.Timeout = 5 * time.Minute
	}
	for key, route := range c.ModelRoutes.ImageGeneration.Designer {
		if route.Timeout == 0 {
			route.Timeout = 5 * time.Minute
			c.ModelRoutes.ImageGeneration.Designer[key] = route
		}
	}
	c.Montage.ApplyDefaults()
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
	// Seednote sidecar defaults.
	if c.Seednote.BaseURL == "" {
		c.Seednote.BaseURL = "http://localhost:18060"
	}
	if c.Seednote.Timeout == 0 {
		c.Seednote.Timeout = 30
	}

	// ilink WeChat assistant channel defaults.
	if c.Ilink.BaseURL == "" {
		c.Ilink.BaseURL = "http://localhost:18070"
	}
	if c.Ilink.Timeout == 0 {
		c.Ilink.Timeout = 30
	}
	if c.Ilink.PollInterval == 0 {
		c.Ilink.PollInterval = 2
	}
	if c.Ilink.NotificationRetryMax == 0 {
		c.Ilink.NotificationRetryMax = 5
	}
	if c.Ilink.AssistantName == "" {
		c.Ilink.AssistantName = "Anban 微信助手"
	}

	// Claude executor defaults.
	if c.Claude.Executor == "" {
		c.Claude.Executor = "local"
	}
	defaultMaxTurns := map[string]int{
		"moments":        25,
		"viral_analysis": 30,
		"seednote":       50,
		"article":        60,
		"ecommerce":      90,
	}
	if c.Claude.MaxTurns == nil {
		c.Claude.MaxTurns = defaultMaxTurns
	} else {
		for taskType, maxTurns := range defaultMaxTurns {
			if _, ok := c.Claude.MaxTurns[taskType]; !ok {
				c.Claude.MaxTurns[taskType] = maxTurns
			}
		}
	}
	if c.Claude.Docker.Image == "" {
		c.Claude.Docker.Image = "creator-agent-content:latest"
	}
	if c.Claude.Docker.ImageProfiles == nil {
		c.Claude.Docker.ImageProfiles = map[string]string{}
	}
	if _, ok := c.Claude.Docker.ImageProfiles[model.PlatformSeednote]; !ok {
		c.Claude.Docker.ImageProfiles[model.PlatformSeednote] = "creator-agent-seednote:latest"
	}
	if _, ok := c.Claude.Docker.ImageProfiles[model.PlatformMontage]; !ok {
		c.Claude.Docker.ImageProfiles[model.PlatformMontage] = "creator-agent-montage:latest"
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
	if c.Claude.Kubernetes.Namespace == "" {
		c.Claude.Kubernetes.Namespace = "default"
	}
	if c.Claude.Kubernetes.ServerCASecret == "" {
		c.Claude.Kubernetes.ServerCASecret = "anban-internal-ca"
	}
	if c.Claude.Kubernetes.ProjectMemorySize == "" {
		c.Claude.Kubernetes.ProjectMemorySize = "1Gi"
	}
	if c.Claude.Kubernetes.TaskWorkspaceSize == "" {
		c.Claude.Kubernetes.TaskWorkspaceSize = "10Gi"
	}
	if c.Claude.Kubernetes.ActiveDeadlineSeconds == 0 {
		c.Claude.Kubernetes.ActiveDeadlineSeconds = 3600
	}
	if c.Claude.Kubernetes.HeartbeatTimeoutSeconds == 0 {
		c.Claude.Kubernetes.HeartbeatTimeoutSeconds = 180
	}
	if c.Claude.Kubernetes.CompletionGraceSeconds == 0 && !c.Claude.Kubernetes.completionGraceSet {
		c.Claude.Kubernetes.CompletionGraceSeconds = 30
	}
	if c.Claude.Kubernetes.TTLSecondsAfterFinished == 0 {
		c.Claude.Kubernetes.TTLSecondsAfterFinished = 600
	}
	if c.Claude.Kubernetes.PreStartRetryLimit == 0 && !c.Claude.Kubernetes.preStartRetryLimitSet {
		c.Claude.Kubernetes.PreStartRetryLimit = 1
	}
	// Auto-detect plugin_dir by searching for agents/.
	if c.Claude.PluginDir == "" {
		c.Claude.PluginDir = detectPluginDir()
	}
}

func (c *Config) deriveModelRouteRuntimeConfig() error {
	if len(c.ModelProviders) == 0 {
		return nil
	}
	provider := func(routeName, providerKey string) (ModelProviderConfig, error) {
		if strings.TrimSpace(providerKey) == "" {
			return ModelProviderConfig{}, fmt.Errorf("%s.provider is required", routeName)
		}
		p, ok := c.ModelProviders[providerKey]
		if !ok {
			return ModelProviderConfig{}, fmt.Errorf("%s.provider %q is not configured in model_providers", routeName, providerKey)
		}
		if strings.TrimSpace(p.BaseURL) == "" || strings.TrimSpace(p.APIKey) == "" {
			return ModelProviderConfig{}, fmt.Errorf("model_providers.%s base_url and api_key are required", providerKey)
		}
		return p, nil
	}
	if c.ModelRoutes.Writing.Model != "" || c.ModelRoutes.Writing.Provider != "" {
		p, err := provider("model_routes.writing", c.ModelRoutes.Writing.Provider)
		if err != nil {
			return err
		}
		c.Writing = WritingConfig{BaseURL: p.BaseURL, Key: p.APIKey, Model: c.ModelRoutes.Writing.Model, Timeout: c.ModelRoutes.Writing.Timeout}
	}
	if c.ModelRoutes.ImageUnderstanding.Model != "" || c.ModelRoutes.ImageUnderstanding.Provider != "" {
		p, err := provider("model_routes.image_understanding", c.ModelRoutes.ImageUnderstanding.Provider)
		if err != nil {
			return err
		}
		c.ImageUnderstanding = UnderstandingRuntimeConfig{
			BaseURL: p.BaseURL, Key: p.APIKey, Model: c.ModelRoutes.ImageUnderstanding.Model,
			Provider: providerKind(c.ModelRoutes.ImageUnderstanding.Provider), ProviderKey: c.ModelRoutes.ImageUnderstanding.Provider,
			Timeout: c.ModelRoutes.ImageUnderstanding.Timeout, RequireUsage: c.ModelRoutes.ImageUnderstanding.RequireUsage,
		}
	}
	if c.ModelRoutes.VideoUnderstanding.Model != "" || c.ModelRoutes.VideoUnderstanding.Provider != "" {
		p, err := provider("model_routes.video_understanding", c.ModelRoutes.VideoUnderstanding.Provider)
		if err != nil {
			return err
		}
		c.VideoUnderstanding = VideoUnderstandingRuntimeConfig{
			UnderstandingRuntimeConfig: UnderstandingRuntimeConfig{
				BaseURL: p.BaseURL, Key: p.APIKey, Model: c.ModelRoutes.VideoUnderstanding.Model,
				Provider: providerKind(c.ModelRoutes.VideoUnderstanding.Provider), ProviderKey: c.ModelRoutes.VideoUnderstanding.Provider,
				Timeout: c.ModelRoutes.VideoUnderstanding.Timeout, RequireUsage: c.ModelRoutes.VideoUnderstanding.RequireUsage,
			},
			RequireNativeVideo:       c.ModelRoutes.VideoUnderstanding.RequireNativeVideo,
			MaxRecommendedResolution: c.ModelRoutes.VideoUnderstanding.MaxRecommendedResolution,
		}
	}
	if c.ModelRoutes.ImageGeneration.Cover.Model != "" || c.ModelRoutes.ImageGeneration.Cover.Provider != "" {
		cfg, err := c.imageAPIFromRoute("model_routes.image_generation.cover", c.ModelRoutes.ImageGeneration.Cover)
		if err != nil {
			return err
		}
		c.ImageAPI.Cover = cfg
	}
	if c.ModelRoutes.ImageGeneration.Content.Model != "" || c.ModelRoutes.ImageGeneration.Content.Provider != "" {
		cfg, err := c.imageAPIFromRoute("model_routes.image_generation.content", c.ModelRoutes.ImageGeneration.Content)
		if err != nil {
			return err
		}
		c.ImageAPI.Content = cfg
	}
	if len(c.ModelRoutes.ImageGeneration.Designer) > 0 {
		c.ImageAPI.Designer = map[string]*appconfig.ImageAPI{}
		c.ImageAPI.designerOrder = c.ImageAPI.designerOrder[:0]
		for key, route := range c.ModelRoutes.ImageGeneration.Designer {
			routeName := "model_routes.image_generation.designer." + key
			if route.Enabled && route.QualityRank <= 0 {
				return fmt.Errorf("%s.quality_rank must be positive when enabled", routeName)
			}
			if err := validateDesignerProviderCapabilities(routeName+".capabilities", route.Capabilities); err != nil {
				return err
			}
			cfg, err := c.imageAPIFromRoute(routeName, route)
			if err != nil {
				return err
			}
			c.ImageAPI.Designer[key] = cfg
			c.ImageAPI.designerOrder = append(c.ImageAPI.designerOrder, key)
		}
	}
	if err := c.resolveImagePresetRoutes(); err != nil {
		return err
	}
	return nil
}

func (c *Config) resolveImagePresetRoutes() error {
	for i := range c.ImagePresets {
		preset := &c.ImagePresets[i]
		if strings.TrimSpace(preset.ProviderRoute) == "" {
			continue
		}
		route, err := c.imageGenerationRouteByPath(preset.ProviderRoute)
		if err != nil {
			return fmt.Errorf("image_presets[%d].provider_route: %w", i, err)
		}
		provider, ok := c.ModelProviders[route.Provider]
		if !ok {
			return fmt.Errorf("image_presets[%d].provider_route provider %q is not configured in model_providers", i, route.Provider)
		}
		preset.Provider = providerKind(route.Provider)
		preset.Model = route.Model
		preset.Endpoint = provider.BaseURL
		preset.APIKey = provider.APIKey
		preset.Timeout = route.Timeout
		preset.QualityRank = route.QualityRank
		preset.Capabilities = route.Capabilities
	}
	return nil
}

func validateDesignerProviderCapabilities(path string, caps DesignerProviderCapabilities) error {
	var errs []string
	defaultSize := strings.TrimSpace(caps.DefaultSize)
	if defaultSize == "" {
		errs = append(errs, "default_size is required")
	}
	if len(caps.SizePresets) == 0 {
		errs = append(errs, "size_presets is required")
	}
	if caps.MaxBatch <= 0 {
		errs = append(errs, "max_batch must be positive")
	}
	if caps.MaxReferenceImages < 0 {
		errs = append(errs, "max_reference_images must not be negative")
	}
	if len(caps.OutputFormats) == 0 {
		errs = append(errs, "output_formats is required")
	}

	hasDefaultSize := false
	for _, preset := range caps.SizePresets {
		preset = strings.TrimSpace(preset)
		if preset == "" {
			errs = append(errs, "size_presets must not contain empty values")
			continue
		}
		if strings.EqualFold(preset, defaultSize) {
			hasDefaultSize = true
		}
	}
	if defaultSize != "" && len(caps.SizePresets) > 0 && !hasDefaultSize {
		errs = append(errs, "default_size must be included in size_presets")
	}
	for _, format := range caps.OutputFormats {
		if strings.TrimSpace(format) == "" {
			errs = append(errs, "output_formats must not contain empty values")
		}
	}
	for _, quality := range caps.QualityLevels {
		if strings.TrimSpace(quality) == "" {
			errs = append(errs, "quality_levels must not contain empty values")
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s invalid: %s", path, strings.Join(errs, "; "))
	}
	return nil
}

func (c *Config) imageGenerationRouteByPath(path string) (ImageGenerationRouteConfig, error) {
	path = strings.TrimPrefix(strings.TrimSpace(path), "model_routes.")
	switch path {
	case "image_generation.cover":
		return c.ModelRoutes.ImageGeneration.Cover, nil
	case "image_generation.content":
		return c.ModelRoutes.ImageGeneration.Content, nil
	}
	const designerPrefix = "image_generation.designer."
	if strings.HasPrefix(path, designerPrefix) {
		key := strings.TrimPrefix(path, designerPrefix)
		if route, ok := c.ModelRoutes.ImageGeneration.Designer[key]; ok {
			return route, nil
		}
		return ImageGenerationRouteConfig{}, fmt.Errorf("designer route %q is not configured", key)
	}
	return ImageGenerationRouteConfig{}, fmt.Errorf("unsupported route %q", path)
}

func (c *Config) imageAPIFromRoute(routeName string, route ImageGenerationRouteConfig) (*appconfig.ImageAPI, error) {
	p, ok := c.ModelProviders[route.Provider]
	if !ok {
		return nil, fmt.Errorf("%s.provider %q is not configured in model_providers", routeName, route.Provider)
	}
	enable := route.Enabled
	cfg := &appconfig.ImageAPI{
		Alias:          route.Alias,
		Enable:         &enable,
		Key:            p.APIKey,
		BaseURL:        p.BaseURL,
		Provider:       providerKind(route.Provider),
		Model:          route.Model,
		TimeoutSec:     int(route.Timeout / time.Second),
		ResponseFormat: route.ResponseFormat,
	}
	return cfg, nil
}

func providerKind(providerKey string) string {
	key := strings.ToLower(providerKey)
	switch {
	case strings.Contains(key, "volc"):
		return "volcengine"
	case strings.Contains(key, "gemini") || strings.Contains(key, "google"):
		return "gemini"
	case strings.Contains(key, "openai"), strings.Contains(key, "moonshot"), strings.Contains(key, "kimi"), strings.Contains(key, "wangcai"):
		return "openai"
	default:
		return providerKey
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
func (c *Config) resolvePaths(rootConfigDir string) {
	if c.BillingRuntime.ConfigDir != "" && !filepath.IsAbs(c.BillingRuntime.ConfigDir) {
		c.BillingRuntime.ConfigDir = filepath.Join(rootConfigDir, c.BillingRuntime.ConfigDir)
	}
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

// detectPluginDir attempts to locate the unified Anban Creator plugin directory
// that contains agents/. It checks the monorepo location first, then searches
// upward from CWD or the executable directory.
func detectPluginDir() string {
	var candidates []string
	if wd, err := os.Getwd(); err == nil {
		pluginDir := filepath.Join(wd, "plugins", "anban")
		if info, err := os.Stat(filepath.Join(pluginDir, "agents")); err == nil && info.IsDir() {
			return pluginDir
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
			pluginDir := filepath.Join(dir, "plugins", "anban")
			if info, err := os.Stat(filepath.Join(pluginDir, "agents")); err == nil && info.IsDir() {
				return pluginDir
			}
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
	if err := c.Claude.Validate(); err != nil {
		errs = append(errs, err.Error())
	}
	if c.BillingRuntime.ConfigDir != "" && strings.TrimSpace(c.BillingRuntime.AdminAPIKey) == "" {
		errs = append(errs, "billing_runtime.admin_api_key is required when billing_runtime.config_dir is configured")
	}

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
		if err := validateStorageCustomDomain(c.Storage.CustomDomain); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if c.Memory.Enabled {
		if c.Memory.Provider != "oss" {
			errs = append(errs, fmt.Sprintf("memory.provider must be \"oss\", got %q", c.Memory.Provider))
		}
		if strings.TrimSpace(c.Memory.OSSPrefix) == "" {
			errs = append(errs, "memory.oss_prefix is required when memory is enabled")
		}
		if !safeRelativeMemoryDir(c.Memory.RuntimeDir) {
			errs = append(errs, fmt.Sprintf("memory.runtime_dir must be a safe relative path, got %q", c.Memory.RuntimeDir))
		}
		if c.Memory.MaxArchiveBytes <= 0 {
			errs = append(errs, "memory.max_archive_bytes must be positive")
		}
		if c.Memory.MergeOnStatus != "completed" {
			errs = append(errs, fmt.Sprintf("memory.merge_on_status must be \"completed\", got %q", c.Memory.MergeOnStatus))
		}
		if c.Memory.LockTTL <= 0 {
			errs = append(errs, "memory.lock_ttl must be positive")
		}
	}

	if err := c.Montage.Validate(); err != nil {
		errs = append(errs, err.Error())
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

	for key, route := range c.ModelRoutes.ImageGeneration.Designer {
		if route.Enabled && route.QualityRank <= 0 {
			errs = append(errs, "model_routes.image_generation.designer."+key+".quality_rank must be positive when enabled")
		}
		if err := validateDesignerProviderCapabilities("model_routes.image_generation.designer."+key+".capabilities", route.Capabilities); err != nil {
			errs = append(errs, err.Error())
		}
	}

	imageRoutes := []struct {
		name  string
		route ImageGenerationRouteConfig
	}{
		{name: "model_routes.image_generation.cover", route: c.ModelRoutes.ImageGeneration.Cover},
		{name: "model_routes.image_generation.content", route: c.ModelRoutes.ImageGeneration.Content},
	}
	for key, route := range c.ModelRoutes.ImageGeneration.Designer {
		imageRoutes = append(imageRoutes, struct {
			name  string
			route ImageGenerationRouteConfig
		}{name: "model_routes.image_generation.designer." + key, route: route})
	}
	for _, candidate := range imageRoutes {
		if strings.TrimSpace(candidate.route.Provider) == "" && strings.TrimSpace(candidate.route.Model) == "" {
			continue
		}
		if candidate.route.Timeout <= 0 {
			errs = append(errs, candidate.name+".timeout must be positive")
			continue
		}
		minimumOperationTimeout := candidate.route.Timeout + c.ModelRoutes.ImageUnderstanding.Timeout
		if c.MCP.ToolTimeouts.GenerateImage <= minimumOperationTimeout {
			errs = append(errs, fmt.Sprintf(
				"mcp.tool_timeouts.generate_image (%s) must be greater than %s.timeout (%s) plus model_routes.image_understanding.timeout (%s)",
				c.MCP.ToolTimeouts.GenerateImage, candidate.name, candidate.route.Timeout, c.ModelRoutes.ImageUnderstanding.Timeout))
		}
	}

	switch c.Claude.Executor {
	case "local", "docker", "kubernetes":
	default:
		errs = append(errs, fmt.Sprintf("claude.executor must be 'local', 'docker', or 'kubernetes', got %q", c.Claude.Executor))
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
		if strings.TrimSpace(c.Claude.Docker.Image) == "" {
			errs = append(errs, "claude.docker.image is required")
		}
		for _, taskType := range []string{model.PlatformSeednote, model.PlatformMontage} {
			if strings.TrimSpace(c.Claude.Docker.ImageProfiles[taskType]) == "" {
				errs = append(errs, fmt.Sprintf("claude.docker.image_profiles.%s is required", taskType))
			}
		}
		for taskType, image := range c.Claude.Docker.ImageProfiles {
			if !isAgentImageProfileTaskType(taskType) {
				errs = append(errs, fmt.Sprintf("claude.docker.image_profiles contains unsupported task type %q", taskType))
			}
			if strings.TrimSpace(image) == "" {
				errs = append(errs, fmt.Sprintf("claude.docker.image_profiles.%s must not be empty", taskType))
			}
		}
	}

	if c.Claude.Executor == "kubernetes" {
		if strings.TrimSpace(c.Server.TLSCertFile) == "" || strings.TrimSpace(c.Server.TLSKeyFile) == "" {
			errs = append(errs, "server.tls_cert_file and server.tls_key_file are required when claude.executor is \"kubernetes\"")
		}
		if c.Storage.Provider != "oss" {
			errs = append(errs, "claude.kubernetes requires storage.provider to be \"oss\"")
		}
		if strings.TrimSpace(c.Storage.STSRoleArn) == "" {
			errs = append(errs, "storage.sts_role_arn is required when claude.executor is \"kubernetes\"")
		}
		if strings.TrimSpace(c.Claude.AgentServerURL) == "" {
			errs = append(errs, "claude.agent_server_url is required when claude.executor is \"kubernetes\"")
		} else if !strings.HasPrefix(strings.TrimSpace(c.Claude.AgentServerURL), "https://") {
			errs = append(errs, "claude.agent_server_url must use https:// when claude.executor is \"kubernetes\"")
		}
		if strings.TrimSpace(c.Claude.Kubernetes.Namespace) == "" {
			errs = append(errs, "claude.kubernetes.namespace is required")
		}
		if strings.TrimSpace(c.Claude.Kubernetes.AgentImage) == "" {
			errs = append(errs, "claude.kubernetes.agent_image is required")
		}
		for _, taskType := range []string{model.PlatformSeednote, model.PlatformMontage} {
			if strings.TrimSpace(c.Claude.Kubernetes.ImageProfiles[taskType]) == "" {
				errs = append(errs, fmt.Sprintf("claude.kubernetes.image_profiles.%s is required", taskType))
			}
		}
		for taskType, image := range c.Claude.Kubernetes.ImageProfiles {
			if !isAgentImageProfileTaskType(taskType) {
				errs = append(errs, fmt.Sprintf("claude.kubernetes.image_profiles contains unsupported task type %q", taskType))
			}
			if strings.TrimSpace(image) == "" {
				errs = append(errs, fmt.Sprintf("claude.kubernetes.image_profiles.%s must not be empty", taskType))
			}
		}
		if strings.TrimSpace(c.Claude.Kubernetes.ServiceAccount) == "" {
			errs = append(errs, "claude.kubernetes.service_account is required")
		}
		if strings.TrimSpace(c.Claude.Kubernetes.ServerCASecret) == "" {
			errs = append(errs, "claude.kubernetes.server_ca_secret is required")
		}
		if len(c.Claude.Kubernetes.ExecutionTokenSecret) < 32 {
			errs = append(errs, "claude.kubernetes.execution_token_secret must be at least 32 bytes")
		}
		if strings.TrimSpace(c.Claude.Kubernetes.NASStorageClass) == "" {
			errs = append(errs, "claude.kubernetes.nas_storage_class is required")
		}
		memorySize, err := resource.ParseQuantity(strings.TrimSpace(c.Claude.Kubernetes.ProjectMemorySize))
		if err != nil {
			errs = append(errs, "claude.kubernetes.project_memory_size must be a valid Kubernetes quantity")
		} else if memorySize.Sign() <= 0 {
			errs = append(errs, "claude.kubernetes.project_memory_size must be positive")
		}
		workspaceSize, err := resource.ParseQuantity(strings.TrimSpace(c.Claude.Kubernetes.TaskWorkspaceSize))
		if err != nil {
			errs = append(errs, "claude.kubernetes.task_workspace_size must be a valid Kubernetes quantity")
		} else if workspaceSize.Sign() <= 0 {
			errs = append(errs, "claude.kubernetes.task_workspace_size must be positive")
		}
		if c.Claude.Kubernetes.ActiveDeadlineSeconds <= 0 {
			errs = append(errs, "claude.kubernetes.active_deadline_seconds must be positive")
		}
		if c.Claude.Kubernetes.HeartbeatTimeoutSeconds < 60 || c.Claude.Kubernetes.HeartbeatTimeoutSeconds >= c.Claude.Kubernetes.ActiveDeadlineSeconds {
			errs = append(errs, "claude.kubernetes.heartbeat_timeout_seconds must be at least 60 and less than active_deadline_seconds")
		}
		if c.Claude.Kubernetes.TTLSecondsAfterFinished <= 0 {
			errs = append(errs, "claude.kubernetes.ttl_seconds_after_finished must be positive")
		}
		if c.Claude.Kubernetes.CompletionGraceSeconds < 0 {
			errs = append(errs, "claude.kubernetes.completion_grace_seconds must not be negative")
		}
		if c.Claude.Kubernetes.PreStartRetryLimit < 0 {
			errs = append(errs, "claude.kubernetes.pre_start_retry_limit must not be negative")
		}
		errs = append(errs, validateKubernetesResourceConfig("claude.kubernetes.resources", c.Claude.Kubernetes.Resources)...)
		for taskType := range c.Claude.Kubernetes.ResourceProfiles {
			if strings.TrimSpace(taskType) == "" {
				errs = append(errs, "claude.kubernetes.resource_profiles contains an empty task type")
				continue
			}
			effective := c.Claude.Kubernetes.ResourcesForTask(taskType)
			errs = append(errs, validateKubernetesResourceConfig("claude.kubernetes.resource_profiles."+taskType, effective)...)
		}
	}
	if (strings.TrimSpace(c.Server.TLSCertFile) == "") != (strings.TrimSpace(c.Server.TLSKeyFile) == "") {
		errs = append(errs, "server.tls_cert_file and server.tls_key_file must be configured together")
	}

	if c.Claude.AgentServerURL != "" {
		u := strings.TrimSpace(c.Claude.AgentServerURL)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			errs = append(errs, fmt.Sprintf("claude.agent_server_url must start with http:// or https://, got %q", u))
		}
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

func validateStorageCustomDomain(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, "://") {
		return fmt.Errorf("storage.custom_domain must be a CDN/storage hostname without scheme, path, query, or fragment; leave it empty for default OSS URLs")
	}
	candidate := raw
	if !strings.Contains(candidate, "://") {
		candidate = "//" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("storage.custom_domain must be a CDN/storage hostname, got %q", raw)
	}
	if strings.Trim(parsed.EscapedPath(), "/") != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("storage.custom_domain must not include a path, query, or fragment; leave it empty for default OSS URLs or set a CDN/storage hostname")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	switch host {
	case "creator.anbanai.com", "api.creator.anbanai.com", "api.anbanai.com":
		return fmt.Errorf("storage.custom_domain must not point at the Studio/API system domain; leave it empty for default OSS URLs or set a CDN/storage hostname")
	}
	return nil
}

func safeRelativeMemoryDir(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
