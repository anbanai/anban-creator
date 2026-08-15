package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/resource"

	appconfig "github.com/anbanai/anban-creator/app/config"
	"github.com/anbanai/anban-creator/server/agentpack"
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
	Montage            MontageConfig                   `yaml:"montage"`
	ServerInternal     ModelRuntimeConfig              `yaml:"-"`
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
	Worldtree          WorldtreeConfig                 `yaml:"worldtree"`
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

// SeednoteConfig holds Seednote (种草笔记) sidecar configuration.
type SeednoteConfig struct {
	BaseURL string `yaml:"base_url"` // default "http://localhost:18060"
	Timeout int    `yaml:"timeout"`  // default 30 (seconds)
}

// WorldtreeConfig holds the server-only credentials for the third-party
// WeChat Channels data API. The key is never exposed to agents or Studio.
type WorldtreeConfig struct {
	BaseURL string `yaml:"base_url"`
	Key     string `yaml:"key"`
	Timeout int    `yaml:"timeout"`
}

// IlinkConfig holds the ilink WeChat assistant channel configuration. The iLink
// sidecar is the transport implementation behind this platform channel.
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
	Provider           string                  `yaml:"provider" json:"-"`
	Model              string                  `yaml:"model" json:"-"`
	Timeout            time.Duration           `yaml:"timeout" json:"-"`
	MinTier            string                  `yaml:"min_tier" json:"min_tier"`
	Alias              string                  `yaml:"alias"`
	Description        string                  `yaml:"description"`
	SortOrder          int                     `yaml:"sort_order" json:"sort_order"`
	BillingSKU         string                  `yaml:"billing_sku" json:"-"`
	Enabled            bool                    `yaml:"enabled"`
	QualityRank        int                     `yaml:"quality_rank" json:"quality_rank"`
	ResponseFormat     string                  `yaml:"response_format" json:"-"`
	BaseURL            string                  `yaml:"base_url" json:"-"`
	APIKey             string                  `yaml:"api_key" json:"-"`
	GenerationFeatures ImageGenerationFeatures `yaml:"generation_features" json:"generation_features"`
}

func (c *ImageGenerationRouteConfig) UnmarshalYAML(value *yaml.Node) error {
	if err := validateYAMLMappingFields(value, "image generation route", map[string]bool{
		"provider": true, "model": true, "timeout": true,
		"min_tier": true, "alias": true,
		"description": true, "sort_order": true, "billing_sku": true,
		"enabled": true, "quality_rank": true, "response_format": true,
		"base_url": true, "api_key": true, "generation_features": true,
	}); err != nil {
		return err
	}
	type plain ImageGenerationRouteConfig
	return value.Decode((*plain)(c))
}

type ImageGenerationRoutesConfig struct {
	DefaultCapability string                                `yaml:"default_capability" json:"default_capability"`
	Capabilities      map[string]ImageGenerationRouteConfig `yaml:"capabilities" json:"-"`
}

func (c *ImageGenerationRoutesConfig) UnmarshalYAML(value *yaml.Node) error {
	if err := validateYAMLMappingFields(value, "model_routes.image_generation", map[string]bool{
		"default_capability": true,
		"capabilities":       true,
	}); err != nil {
		return err
	}
	type plain ImageGenerationRoutesConfig
	return value.Decode((*plain)(c))
}

type ImageGenerationFeatures struct {
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
	ServerInternal     RouteConfig                   `yaml:"server_internal"`
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

// ImageAPIConfig is an internal runtime adapter for app/image. It is built from
// one selected capability and is never decoded from server YAML.
type ImageAPIConfig struct {
	API *appconfig.ImageAPI
}

// ModelRuntimeConfig is the provider-resolved route used only for synchronous
// Server-internal decisions before agent execution.
type ModelRuntimeConfig struct {
	BaseURL     string
	Key         string
	Model       string
	Provider    string
	ProviderKey string
	Timeout     time.Duration
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

// ClaudeConfig owns managed Agent dispatch and selectable execution profiles.
type ClaudeConfig struct {
	ExecutionProfiles    map[string]ClaudeExecutionProfileConfig `yaml:"execution_profiles" json:"execution_profiles"`
	Executor             string                                  `yaml:"executor"` // "docker" or "kubernetes"
	RuntimeImages        RuntimeImages                           `yaml:"runtime_images"`
	ExecutionTokenSecret string                                  `yaml:"execution_token_secret" json:"-"`
	PluginDir            string                                  `yaml:"plugin_dir"`       // Path to the Anban Creator plugin directory (contains agents/, skills/)
	Sandbox              bool                                    `yaml:"sandbox"`          // Enable sandbox isolation for agent execution (recommended in k8s)
	Docker               DockerConfig                            `yaml:"docker"`           // Docker executor settings (used when executor=docker)
	Kubernetes           KubernetesConfig                        `yaml:"kubernetes"`       // Kubernetes executor settings (used when executor=kubernetes)
	MaxTurns             map[string]int                          `yaml:"max_turns"`        // Per-task-type max turns, e.g. {"article": 60, "seednote": 100}
	TaskLogDir           string                                  `yaml:"task_log_dir"`     // Directory for per-task agent execution logs. Empty = disabled.
	AgentServerURL       string                                  `yaml:"agent_server_url"` // Override server URL for agent MCP connections (e.g. k8s service URL). To env-control, write ${ANBAN_CLAUDE_AGENT_SERVER_URL} in config.yaml.
}

type ClaudeExecutionProfileConfig struct {
	Provider          string            `yaml:"provider" json:"provider"`
	Description       string            `yaml:"description" json:"description"`
	Envs              map[string]string `yaml:"envs" json:"-"`
	ModelUsageAliases map[string]string `yaml:"model_usage_aliases" json:"model_usage_aliases"`
}

func (c *ClaudeConfig) UnmarshalYAML(value *yaml.Node) error {
	known := map[string]bool{
		"execution_profiles": true, "executor": true, "runtime_images": true,
		"execution_token_secret": true, "plugin_dir": true,
		"sandbox": true, "docker": true, "kubernetes": true, "max_turns": true,
		"task_log_dir": true, "agent_server_url": true,
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("claude config must be a mapping")
	}
	for i := 0; i < len(value.Content); i += 2 {
		key := value.Content[i].Value
		if !known[key] {
			return fmt.Errorf("unknown claude config field %q", key)
		}
	}
	if err := validateClaudeNestedFields(value); err != nil {
		return err
	}
	type plain ClaudeConfig
	if err := value.Decode((*plain)(c)); err != nil {
		return err
	}
	return nil
}

func validateClaudeNestedFields(value *yaml.Node) error {
	profiles := yamlMappingValue(value, "execution_profiles")
	if profiles == nil {
		return nil
	}
	if profiles.Kind != yaml.MappingNode {
		return fmt.Errorf("claude.execution_profiles must be a mapping")
	}
	for i := 0; i < len(profiles.Content); i += 2 {
		name, profile := profiles.Content[i].Value, profiles.Content[i+1]
		path := "claude.execution_profiles." + name
		if err := validateYAMLMappingFields(profile, path, map[string]bool{
			"provider": true, "description": true, "envs": true, "model_usage_aliases": true,
		}); err != nil {
			return err
		}
		if envs := yamlMappingValue(profile, "envs"); envs != nil {
			if err := validateClaudeEnvFields(envs, path+".envs"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateClaudeEnvFields(node *yaml.Node, path string) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s must be a mapping", path)
	}
	allowed := make(map[string]bool, len(model.ClaudeProfileEnvKeys()))
	for _, key := range model.ClaudeProfileEnvKeys() {
		allowed[key] = true
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		fieldPath := path + "." + key
		if !allowed[key] {
			return fmt.Errorf("unknown config field %q", fieldPath)
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			return fmt.Errorf("%s must be a string", fieldPath)
		}
	}
	return nil
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func validateYAMLMappingFields(node *yaml.Node, path string, known map[string]bool) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s must be a mapping", path)
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if !known[key] {
			return fmt.Errorf("unknown config field %q", path+"."+key)
		}
	}
	return nil
}

func (c ClaudeConfig) String() string {
	return fmt.Sprintf("ClaudeConfig{ExecutionProfiles:%d Executor:%q}", len(c.ExecutionProfiles), c.Executor)
}

func (c ClaudeConfig) GoString() string {
	return c.String()
}

func (c ClaudeConfig) Validate() error {
	var errs []string
	for name, profile := range c.ExecutionProfiles {
		path := "claude.execution_profiles." + name
		if strings.TrimSpace(name) == "" {
			errs = append(errs, "claude.execution_profiles contains an empty profile name")
		}
		if err := model.ValidateClaudeProfileEnvs(profile.Envs, true); err != nil {
			errs = append(errs, path+".envs: "+err.Error())
		}
	}
	requiredRuntimeProfiles := []string{model.PlatformArticle, model.PlatformSeednote, model.PlatformMontage}
	for _, profile := range requiredRuntimeProfiles {
		image, ok := c.RuntimeImages[profile]
		if !ok {
			errs = append(errs, "claude.runtime_images."+profile+" is required")
			continue
		}
		if strings.TrimSpace(image) == "" {
			errs = append(errs, "claude.runtime_images."+profile+" must not be empty")
		}
	}
	for profile := range c.RuntimeImages {
		if profile != model.PlatformArticle && profile != model.PlatformSeednote && profile != model.PlatformMontage {
			errs = append(errs, fmt.Sprintf("claude.runtime_images contains unsupported profile %q", profile))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// DockerConfig holds Docker executor settings for container-based task execution.
type DockerConfig struct {
	Network       string `yaml:"network"`
	CPUCores      int64  `yaml:"cpu_cores"`
	cpuCoresSet   bool   `yaml:"-"`
	MemoryMB      int64  `yaml:"memory_mb"`
	memoryMBSet   bool   `yaml:"-"`
	PidsLimit     int64  `yaml:"pids_limit"`
	pidsLimitSet  bool   `yaml:"-"`
	TimeoutSec    int    `yaml:"timeout_sec"`
	timeoutSecSet bool   `yaml:"-"`
}

// KubernetesConfig holds ACK/Kubernetes Job runtime settings.
type KubernetesConfig struct {
	Namespace       string `yaml:"namespace"`
	ServiceAccount  string `yaml:"service_account"`
	ImagePullSecret string `yaml:"image_pull_secret"`
	ServerCASecret  string `yaml:"server_ca_secret"`

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

type RuntimeImages map[string]string

func canonicalRuntimeProfile(taskType string) string {
	if pack, ok := agentpack.Default().ForTaskType(strings.TrimSpace(taskType)); ok {
		return pack.Runtime.Profile
	}
	return model.PlatformArticle
}

func (c RuntimeImages) ForTask(taskType string) RuntimeImageSelection {
	profile := canonicalRuntimeProfile(taskType)
	return RuntimeImageSelection{Profile: profile, Image: strings.TrimSpace(c[profile])}
}

func (c *DockerConfig) UnmarshalYAML(value *yaml.Node) error {
	known := map[string]bool{
		"network": true, "cpu_cores": true, "memory_mb": true,
		"pids_limit": true, "timeout_sec": true,
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("claude.docker config must be a mapping")
	}
	for i := 0; i < len(value.Content); i += 2 {
		if key := value.Content[i].Value; !known[key] {
			return fmt.Errorf("unknown claude.docker config field %q", key)
		}
	}
	type plain DockerConfig
	if err := value.Decode((*plain)(c)); err != nil {
		return err
	}
	for i := 0; i < len(value.Content); i += 2 {
		switch value.Content[i].Value {
		case "cpu_cores":
			c.cpuCoresSet = true
		case "memory_mb":
			c.memoryMBSet = true
		case "pids_limit":
			c.pidsLimitSet = true
		case "timeout_sec":
			c.timeoutSecSet = true
		}
	}
	return nil
}

func (c *KubernetesConfig) UnmarshalYAML(value *yaml.Node) error {
	known := map[string]bool{
		"namespace": true, "service_account": true, "image_pull_secret": true,
		"server_ca_secret": true, "nas_storage_class": true,
		"project_memory_size": true, "task_workspace_size": true,
		"active_deadline_seconds": true, "heartbeat_timeout_seconds": true,
		"completion_grace_seconds": true, "ttl_seconds_after_finished": true,
		"pre_start_retry_limit": true, "resources": true, "resource_profiles": true,
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("claude.kubernetes config must be a mapping")
	}
	for i := 0; i < len(value.Content); i += 2 {
		if key := value.Content[i].Value; !known[key] {
			return fmt.Errorf("unknown claude.kubernetes config field %q", key)
		}
	}
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
	// before the asynq ctx expires. Default 60m (the article pipeline
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
		"writing":   "model_routes.server_internal",
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
		"worldtree":       true,
		"ilink":           true,
		"memory":          true,
	}
	for key := range top {
		if !known[key] {
			return fmt.Errorf("unknown top-level config key %s", key)
		}
	}
	if modelRoutes, ok := top["model_routes"].(map[string]any); ok {
		if _, ok := modelRoutes["writing"]; ok {
			return fmt.Errorf("deprecated config key model_routes.writing; use model_routes.server_internal")
		}
		knownModelRoutes := map[string]bool{
			"server_internal":     true,
			"image_understanding": true,
			"video_understanding": true,
			"image_generation":    true,
		}
		for key := range modelRoutes {
			if !knownModelRoutes[key] {
				return fmt.Errorf("unknown model_routes config key %s", key)
			}
		}
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
	for key, route := range c.ModelRoutes.ImageGeneration.Capabilities {
		if route.Timeout == 0 {
			route.Timeout = 5 * time.Minute
			c.ModelRoutes.ImageGeneration.Capabilities[key] = route
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

	// Server-internal semantic route defaults.
	if (c.ModelRoutes.ServerInternal.Model != "" || c.ModelRoutes.ServerInternal.Provider != "") && c.ModelRoutes.ServerInternal.Timeout == 0 {
		c.ModelRoutes.ServerInternal.Timeout = 10 * time.Minute
	}
	// Seednote sidecar defaults.
	if c.Seednote.BaseURL == "" {
		c.Seednote.BaseURL = "http://localhost:18060"
	}
	if c.Seednote.Timeout == 0 {
		c.Seednote.Timeout = 30
	}
	if c.Worldtree.BaseURL == "" {
		c.Worldtree.BaseURL = "https://www.worldtreetech.cn"
	}
	if c.Worldtree.Timeout == 0 {
		c.Worldtree.Timeout = 30
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

	// Agent Packs own default turn budgets. Claude.MaxTurns contains only
	// explicit operator overrides and is intentionally left sparse.
	if c.Claude.Docker.Network == "" {
		c.Claude.Docker.Network = "creator-runtime-network"
	}
	if c.Claude.Docker.CPUCores == 0 && !c.Claude.Docker.cpuCoresSet {
		c.Claude.Docker.CPUCores = 2
	}
	if c.Claude.Docker.MemoryMB == 0 && !c.Claude.Docker.memoryMBSet {
		c.Claude.Docker.MemoryMB = 4096
	}
	if c.Claude.Docker.PidsLimit == 0 && !c.Claude.Docker.pidsLimitSet {
		c.Claude.Docker.PidsLimit = 512
	}
	if c.Claude.Docker.TimeoutSec == 0 && !c.Claude.Docker.timeoutSecSet {
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
	if (c.ModelRoutes.VideoUnderstanding.Model != "" || c.ModelRoutes.VideoUnderstanding.Provider != "") &&
		!c.ModelRoutes.VideoUnderstanding.RequireNativeVideo {
		return fmt.Errorf("model_routes.video_understanding.require_native_video must be true")
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
	if c.ModelRoutes.ServerInternal.Model != "" || c.ModelRoutes.ServerInternal.Provider != "" {
		p, err := provider("model_routes.server_internal", c.ModelRoutes.ServerInternal.Provider)
		if err != nil {
			return err
		}
		c.ServerInternal = ModelRuntimeConfig{
			BaseURL:     p.BaseURL,
			Key:         p.APIKey,
			Model:       c.ModelRoutes.ServerInternal.Model,
			Provider:    providerKind(c.ModelRoutes.ServerInternal.Provider),
			ProviderKey: c.ModelRoutes.ServerInternal.Provider,
			Timeout:     c.ModelRoutes.ServerInternal.Timeout,
		}
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
	for key, route := range c.ModelRoutes.ImageGeneration.Capabilities {
		if err := validateEnabledImageCapability("model_routes.image_generation.capabilities."+key, key, route); err != nil {
			return err
		}
	}
	return nil
}

func validateEnabledImageCapability(path, key string, route ImageGenerationRouteConfig) error {
	var errs []string
	if key == "" || len(key) > 50 || strings.ContainsAny(key, " \t\n\r") || key == "custom" {
		errs = append(errs, path+" key must be a non-reserved value up to 50 characters without whitespace")
	}
	publicText := strings.ToLower(key + "\n" + route.Alias + "\n" + route.Description)
	for _, term := range []string{"openai", "chatgpt", "gpt", "gemini", "claude", "seedream", "doubao"} {
		if strings.Contains(publicText, term) {
			errs = append(errs, fmt.Sprintf("%s public text contains blocked brand %q", path, term))
		}
	}
	if strings.TrimSpace(route.Provider) == "" {
		errs = append(errs, path+".provider is required")
	} else if !supportsSemanticTaskAspectRatioProvider(route.Provider) {
		errs = append(errs, path+".provider does not implement the semantic task aspect-ratio protocol")
	}
	if strings.TrimSpace(route.Model) == "" {
		errs = append(errs, path+".model is required")
	}
	if strings.TrimSpace(route.BaseURL) == "" {
		errs = append(errs, path+".base_url is required")
	}
	if strings.TrimSpace(route.APIKey) == "" {
		errs = append(errs, path+".api_key is required")
	}
	switch route.MinTier {
	case "free", "pro", "enterprise":
	default:
		errs = append(errs, path+".min_tier must be free, pro, or enterprise")
	}
	if strings.TrimSpace(route.Alias) == "" {
		errs = append(errs, path+".alias is required")
	}
	if strings.TrimSpace(route.Description) == "" {
		errs = append(errs, path+".description is required")
	}
	if strings.TrimSpace(route.BillingSKU) == "" {
		errs = append(errs, path+".billing_sku is required")
	}
	if route.QualityRank <= 0 {
		errs = append(errs, path+".quality_rank must be positive")
	}
	if err := validateImageGenerationFeatures(path+".generation_features", route.GenerationFeatures); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func supportsSemanticTaskAspectRatioProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "wangcai_openai", "volcengine", "volcengine_ark", "volc", "seedream":
		return true
	default:
		return false
	}
}

func validateImageGenerationFeatures(path string, caps ImageGenerationFeatures) error {
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
	if len(caps.QualityLevels) == 0 {
		errs = append(errs, "quality_levels is required")
	}

	hasDefaultSize := false
	for _, preset := range caps.SizePresets {
		preset = strings.TrimSpace(preset)
		if preset == "" {
			errs = append(errs, "size_presets must not contain empty values")
			continue
		}
		if !isFixedImageGenerationSizePreset(preset) {
			errs = append(errs, "size_presets must use width:height:1K|2K|4K format")
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

func isFixedImageGenerationSizePreset(value string) bool {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(value)), ":")
	if len(parts) != 3 {
		return false
	}
	width, widthErr := strconv.Atoi(parts[0])
	height, heightErr := strconv.Atoi(parts[1])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return false
	}
	switch parts[2] {
	case "1K", "2K", "4K":
		return true
	default:
		return false
	}
}

func imageAPIFromCapability(route ImageGenerationRouteConfig) *appconfig.ImageAPI {
	enable := route.Enabled
	return &appconfig.ImageAPI{
		Alias:          route.Alias,
		Enable:         &enable,
		Key:            route.APIKey,
		BaseURL:        route.BaseURL,
		Provider:       route.Provider,
		Model:          route.Model,
		TimeoutSec:     int(route.Timeout / time.Second),
		ResponseFormat: route.ResponseFormat,
	}
}

func (route ImageGenerationRouteConfig) RuntimeImageAPI() *appconfig.ImageAPI {
	return imageAPIFromCapability(route)
}

func (c *Config) ImageCapability(key string) (ImageGenerationRouteConfig, bool) {
	if c == nil {
		return ImageGenerationRouteConfig{}, false
	}
	key = strings.TrimSpace(key)
	if key == "" || key == model.ImageCapabilityKeySystemDefault {
		key = c.ModelRoutes.ImageGeneration.DefaultCapability
	}
	route, ok := c.ModelRoutes.ImageGeneration.Capabilities[key]
	return route, ok
}

func (c *Config) ImageAPIForCapability(key string) (*ImageAPIConfig, bool) {
	route, ok := c.ImageCapability(key)
	if !ok || !route.Enabled {
		return nil, false
	}
	api := imageAPIFromCapability(route)
	return &ImageAPIConfig{API: api}, true
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
		pluginDir := filepath.Join(wd, "plugins")
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
			pluginDir := filepath.Join(dir, "plugins")
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

	if c.ModelRoutes.VideoUnderstanding.Model != "" || c.ModelRoutes.VideoUnderstanding.Provider != "" {
		if !c.ModelRoutes.VideoUnderstanding.RequireNativeVideo {
			errs = append(errs, "model_routes.video_understanding.require_native_video must be true")
		}
	}

	imageGeneration := c.ModelRoutes.ImageGeneration
	if strings.TrimSpace(imageGeneration.DefaultCapability) != "" || len(imageGeneration.Capabilities) > 0 {
		for _, key := range []string{"standard", "professional"} {
			if _, ok := imageGeneration.Capabilities[key]; !ok {
				errs = append(errs, "model_routes.image_generation.capabilities."+key+" is required")
			}
		}
		if strings.TrimSpace(imageGeneration.DefaultCapability) == "" {
			errs = append(errs, "model_routes.image_generation.default_capability is required")
		} else if route, ok := imageGeneration.Capabilities[imageGeneration.DefaultCapability]; !ok {
			errs = append(errs, "model_routes.image_generation.default_capability must exist in capabilities")
		} else if !route.Enabled {
			errs = append(errs, "model_routes.image_generation.default_capability must be enabled")
		} else if route.MinTier != "free" {
			errs = append(errs, "model_routes.image_generation.default_capability must require free tier")
		}
	}
	for key, route := range imageGeneration.Capabilities {
		if err := validateEnabledImageCapability("model_routes.image_generation.capabilities."+key, key, route); err != nil {
			errs = append(errs, err.Error())
		}
		if c.BillingBundle != nil && route.Enabled {
			modelID := strings.TrimSpace(route.Provider) + "/" + strings.TrimSpace(route.Model)
			if _, ok := c.BillingBundle.Costs.Models[modelID]; !ok {
				errs = append(errs, fmt.Sprintf(
					"model_routes.image_generation.capabilities.%s provider/model %q is missing from billing costs.models",
					key, modelID,
				))
			}
		}
	}

	for key, route := range imageGeneration.Capabilities {
		if !route.Enabled {
			continue
		}
		name := "model_routes.image_generation.capabilities." + key
		if route.Timeout <= 0 {
			errs = append(errs, name+".timeout must be positive")
			continue
		}
		minimumOperationTimeout := route.Timeout + c.ModelRoutes.ImageUnderstanding.Timeout
		if c.MCP.ToolTimeouts.GenerateImage <= minimumOperationTimeout {
			errs = append(errs, fmt.Sprintf(
				"mcp.tool_timeouts.generate_image (%s) must be greater than %s.timeout (%s) plus model_routes.image_understanding.timeout (%s)",
				c.MCP.ToolTimeouts.GenerateImage, name, route.Timeout, c.ModelRoutes.ImageUnderstanding.Timeout))
		}
	}

	switch c.Claude.Executor {
	case "docker", "kubernetes":
	default:
		errs = append(errs, fmt.Sprintf("claude.executor must be 'docker' or 'kubernetes', got %q", c.Claude.Executor))
	}
	if len(c.Claude.ExecutionTokenSecret) < 32 {
		errs = append(errs, "claude.execution_token_secret must be at least 32 bytes")
	}

	// When using the Docker executor, the container timeout must be at least as
	// long as content_generate_timeout, otherwise the container is killed before
	// the asynq task deadline (the agent's work is lost mid-pipeline).
	if c.Claude.Executor == "docker" {
		if c.Claude.Docker.CPUCores <= 0 {
			errs = append(errs, "claude.docker.cpu_cores must be positive")
		}
		if c.Claude.Docker.MemoryMB <= 0 {
			errs = append(errs, "claude.docker.memory_mb must be positive")
		}
		if c.Claude.Docker.PidsLimit <= 0 {
			errs = append(errs, "claude.docker.pids_limit must be positive")
		}
		if c.Claude.Docker.TimeoutSec <= 0 {
			errs = append(errs, "claude.docker.timeout_sec must be positive")
		} else if time.Duration(c.Claude.Docker.TimeoutSec)*time.Second < c.Asynq.ContentGenerateTimeout {
			errs = append(errs, fmt.Sprintf(
				"claude.docker.timeout_sec (%ds) must be >= asynq.content_generate_timeout (%s); otherwise the container is killed before the task deadline",
				c.Claude.Docker.TimeoutSec, c.Asynq.ContentGenerateTimeout))
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
		if strings.TrimSpace(c.Claude.Kubernetes.ServiceAccount) == "" {
			errs = append(errs, "claude.kubernetes.service_account is required")
		}
		if strings.TrimSpace(c.Claude.Kubernetes.ServerCASecret) == "" {
			errs = append(errs, "claude.kubernetes.server_ca_secret is required")
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
