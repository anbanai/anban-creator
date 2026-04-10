package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	appconfig "github.com/royalrick/anbanwriter/app/config"
)

// Config holds all server configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	JWT      JWTConfig      `yaml:"jwt"`
	WeChat   WeChatConfig   `yaml:"wechat"`
	Storage  StorageConfig  `yaml:"storage"`
	ImageAPI ImageAPIConfig `yaml:"image_api"`
	Claude   ClaudeConfig   `yaml:"claude"`
}

type ServerConfig struct {
	Port int    `yaml:"port"` // default 8080
	Host string `yaml:"host"` // default "0.0.0.0"
}

type DatabaseConfig struct {
	DSN             string `yaml:"dsn"`
	MaxOpenConns    int    `yaml:"max_open_conns"`    // default 10
	MaxIdleConns    int    `yaml:"max_idle_conns"`    // default 5
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"` // default 3600 seconds
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`      // default "localhost:6379"
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`        // default 0
}

type JWTConfig struct {
	SecretKey     string `yaml:"secret_key"`
	AccessExpiry  string `yaml:"access_expiry"`  // default "24h"
	RefreshExpiry string `yaml:"refresh_expiry"` // default "168h"
}

type WeChatConfig struct {
	AppID     string `yaml:"app_id"`
	AppSecret string `yaml:"app_secret"`
}

// StorageConfig holds file storage configuration.
// Supports "oss" (Alibaba Cloud OSS) or "local" (filesystem).
type StorageConfig struct {
	Provider        string `yaml:"provider"`          // "oss" or "local", default "local"
	Endpoint        string `yaml:"endpoint"`          // OSS endpoint, e.g. "oss-cn-hangzhou.aliyuncs.com"
	AccessKeyID     string `yaml:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret"`
	BucketName      string `yaml:"bucket_name"`
	Region          string `yaml:"region"`
	CustomDomain    string `yaml:"custom_domain"`     // Optional CDN domain for public file URLs
	LocalDataDir    string `yaml:"local_data_dir"`    // Default "./data/files"
}

// SizesConfig holds per-platform image size defaults (ratio:tier format, e.g. "16:9", "3:4:4K").
type SizesConfig struct {
	ArticleCover   string `yaml:"article_cover"`   // default "16:9"
	ArticleContent string `yaml:"article_content"` // default "16:9"
	XlsCover       string `yaml:"xls_cover"`       // default "3:4"
	XlsContent     string `yaml:"xls_content"`     // default "3:4"
	RednoteCover   string `yaml:"rednote_cover"`   // default "3:4"
	RednoteContent string `yaml:"rednote_content"` // default "3:4"
}

// ImageAPIConfig holds global image generation API configuration.
// All channels share this server-level config.
type ImageAPIConfig struct {
	Cover   *appconfig.ImageAPI `yaml:"cover"`
	Content *appconfig.ImageAPI `yaml:"content"`
	Sizes   SizesConfig         `yaml:"sizes"`
}

// ClaudeConfig holds configuration for the Claude CLI subprocess.
// The Env map is passed as environment variables to the CLI process,
// supporting auth tokens, base URLs, model overrides, etc.
type ClaudeConfig struct {
	Env       map[string]string `yaml:"env"`
	PluginDir string            `yaml:"plugin_dir"` // Path to the anbanwriter plugin directory (contains claudecode/agents, skills, etc.)
	Sandbox   bool              `yaml:"sandbox"`    // Enable sandbox isolation for agent execution (recommended in k8s)
}

// NewConfig loads configuration from a YAML file, applies defaults, then
// overlays any ANBAN_SERVER_ prefixed environment variables.
func NewConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	cfg.applyDefaults()
	cfg.applyEnvOverrides()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
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

	// Per-platform image size defaults (ratio:tier format).
	if c.ImageAPI.Sizes.ArticleCover == "" {
		c.ImageAPI.Sizes.ArticleCover = "16:9"
	}
	if c.ImageAPI.Sizes.ArticleContent == "" {
		c.ImageAPI.Sizes.ArticleContent = "16:9"
	}
	if c.ImageAPI.Sizes.XlsCover == "" {
		c.ImageAPI.Sizes.XlsCover = "3:4"
	}
	if c.ImageAPI.Sizes.XlsContent == "" {
		c.ImageAPI.Sizes.XlsContent = "3:4"
	}
	if c.ImageAPI.Sizes.RednoteCover == "" {
		c.ImageAPI.Sizes.RednoteCover = "3:4"
	}
	if c.ImageAPI.Sizes.RednoteContent == "" {
		c.ImageAPI.Sizes.RednoteContent = "3:4"
	}
}

// applyEnvOverrides reads ANBAN_SERVER_ prefixed environment variables and
// overwrites the corresponding config fields. The mapping follows the struct
// hierarchy using underscores as separators, e.g.:
//
//	ANBAN_SERVER_DATABASE_DSN
//	ANBAN_SERVER_JWT_SECRET_KEY
//	ANBAN_SERVER_REDIS_ADDR
//	ANBAN_SERVER_SERVER_PORT
func (c *Config) applyEnvOverrides() {
	prefix := "ANBAN_SERVER_"

	if v := os.Getenv(prefix + "SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.Server.Port = port
		}
	}
	if v := os.Getenv(prefix + "SERVER_HOST"); v != "" {
		c.Server.Host = v
	}

	if v := os.Getenv(prefix + "DATABASE_DSN"); v != "" {
		c.Database.DSN = v
	}
	if v := os.Getenv(prefix + "DATABASE_MAX_OPEN_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.MaxOpenConns = n
		}
	}
	if v := os.Getenv(prefix + "DATABASE_MAX_IDLE_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.MaxIdleConns = n
		}
	}
	if v := os.Getenv(prefix + "DATABASE_CONN_MAX_LIFETIME"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.ConnMaxLifetime = n
		}
	}

	if v := os.Getenv(prefix + "REDIS_ADDR"); v != "" {
		c.Redis.Addr = v
	}
	if v := os.Getenv(prefix + "REDIS_PASSWORD"); v != "" {
		c.Redis.Password = v
	}
	if v := os.Getenv(prefix + "REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Redis.DB = n
		}
	}

	if v := os.Getenv(prefix + "JWT_SECRET_KEY"); v != "" {
		c.JWT.SecretKey = v
	}
	if v := os.Getenv(prefix + "JWT_ACCESS_EXPIRY"); v != "" {
		c.JWT.AccessExpiry = v
	}
	if v := os.Getenv(prefix + "JWT_REFRESH_EXPIRY"); v != "" {
		c.JWT.RefreshExpiry = v
	}

	if v := os.Getenv(prefix + "WECHAT_APP_ID"); v != "" {
		c.WeChat.AppID = v
	}
	if v := os.Getenv(prefix + "WECHAT_APP_SECRET"); v != "" {
		c.WeChat.AppSecret = v
	}

	if v := os.Getenv(prefix + "STORAGE_PROVIDER"); v != "" {
		c.Storage.Provider = v
	}
	if v := os.Getenv(prefix + "STORAGE_ENDPOINT"); v != "" {
		c.Storage.Endpoint = v
	}
	if v := os.Getenv(prefix + "STORAGE_ACCESS_KEY_ID"); v != "" {
		c.Storage.AccessKeyID = v
	}
	if v := os.Getenv(prefix + "STORAGE_ACCESS_KEY_SECRET"); v != "" {
		c.Storage.AccessKeySecret = v
	}
	if v := os.Getenv(prefix + "STORAGE_BUCKET_NAME"); v != "" {
		c.Storage.BucketName = v
	}
	if v := os.Getenv(prefix + "STORAGE_REGION"); v != "" {
		c.Storage.Region = v
	}
	if v := os.Getenv(prefix + "STORAGE_CUSTOM_DOMAIN"); v != "" {
		c.Storage.CustomDomain = v
	}
	if v := os.Getenv(prefix + "STORAGE_LOCAL_DATA_DIR"); v != "" {
		c.Storage.LocalDataDir = v
	}

	if v := os.Getenv(prefix + "CLAUDE_PLUGIN_DIR"); v != "" {
		c.Claude.PluginDir = v
	}
	if v := os.Getenv(prefix + "CLAUDE_SANDBOX"); v != "" {
		c.Claude.Sandbox = v == "true" || v == "1"
	}
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

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}
