package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	appconfig "github.com/anbanai/anban-creator/app/config"
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
	VideoAPI           VideoAPIConfig                  `yaml:"video_api"`
	ImagePresets       []ImageModelPreset              `yaml:"image_presets"`
	Writing            WritingConfig                   `yaml:"writing"`
	Vision             VisionConfig                    `yaml:"vision"`
	ModelProviders     map[string]ModelProviderConfig  `yaml:"model_providers"`
	ModelRoutes        ModelRoutesConfig               `yaml:"model_routes"`
	ModelPrices        ModelPricesConfig               `yaml:"model_prices"`
	Billing            BillingConfig                   `yaml:"billing"`
	RechargeTiers      []RechargeTierConfig            `yaml:"recharge_tiers"`
	ImageUnderstanding UnderstandingRuntimeConfig      `yaml:"-"`
	VideoUnderstanding VideoUnderstandingRuntimeConfig `yaml:"-"`
	TingWu             TingWuConfig                    `yaml:"tingwu"`
	FunASR             FunASRConfig                    `yaml:"funasr"`
	Claude             ClaudeConfig                    `yaml:"claude"`
	Credits            CreditsConfig                   `yaml:"credits"`
	CORS               CORSConfig                      `yaml:"cors"`
	Asynq              AsynqConfig                     `yaml:"asynq"`
	Email              EmailConfig                     `yaml:"email"`
	Invitation         InvitationConfig                `yaml:"invitation"`
	Seednote           SeednoteConfig                  `yaml:"seednote"`
	Ilink              IlinkConfig                     `yaml:"ilink"`
	Memory             MemoryConfig                    `yaml:"memory"`
}

// ImageModelPreset defines a system-managed image model that users can select
// when creating tasks or plans. Each preset has a minimum tier that gates access.
type ImageModelPreset struct {
	Key           string `yaml:"key"`            // unique identifier, e.g. "volcengine-standard"
	DisplayName   string `yaml:"display_name"`   // user-facing label
	ProviderRoute string `yaml:"provider_route"` // semantic route, e.g. image_generation.designer.seedream
	Provider      string `yaml:"provider"`       // derived provider kind or legacy direct provider
	Model         string `yaml:"model"`          // concrete model id
	Endpoint      string `yaml:"endpoint"`
	APIKey        string `yaml:"api_key"`
	MinTier       string `yaml:"min_tier"` // free / pro / enterprise
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
	Model                 string             `yaml:"model"`
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
	Provider       string `yaml:"provider"`
	Model          string `yaml:"model"`
	Alias          string `yaml:"alias"`
	Enabled        bool   `yaml:"enabled"`
	ResponseFormat string `yaml:"response_format"`
}

type ImageGenerationRoutesConfig struct {
	Cover    ImageGenerationRouteConfig            `yaml:"cover"`
	Content  ImageGenerationRouteConfig            `yaml:"content"`
	Designer map[string]ImageGenerationRouteConfig `yaml:"designer"`
}

type VideoGenerationRouteConfig struct {
	Provider     string                   `yaml:"provider"`
	Timeout      time.Duration            `yaml:"timeout"`
	DefaultModel string                   `yaml:"default_model"`
	ModelCatalog []VideoModelCatalogEntry `yaml:"model_catalog"`
}

type ModelRoutesConfig struct {
	Writing            RouteConfig                   `yaml:"writing"`
	ImageUnderstanding UnderstandingRouteConfig      `yaml:"image_understanding"`
	VideoUnderstanding VideoUnderstandingRouteConfig `yaml:"video_understanding"`
	ImageGeneration    ImageGenerationRoutesConfig   `yaml:"image_generation"`
	VideoGeneration    VideoGenerationRouteConfig    `yaml:"video_generation"`
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

type FlexibleFloat float64

func (f *FlexibleFloat) UnmarshalYAML(value *yaml.Node) error {
	var raw any
	if err := value.Decode(&raw); err != nil {
		return err
	}
	switch v := raw.(type) {
	case int:
		*f = FlexibleFloat(v)
	case int64:
		*f = FlexibleFloat(v)
	case float64:
		*f = FlexibleFloat(v)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return fmt.Errorf("parse float %q: %w", v, err)
		}
		*f = FlexibleFloat(parsed)
	default:
		return fmt.Errorf("unsupported numeric value %T", raw)
	}
	return nil
}

func (f FlexibleFloat) Float64() float64 { return float64(f) }

type CurrencyRate struct {
	ToCNY FlexibleFloat `yaml:"to_cny" json:"to_cny"`
}

type TokenModelPrice struct {
	Currency    string        `yaml:"currency" json:"currency"`
	Unit        int64         `yaml:"unit" json:"unit"`
	CachedInput FlexibleFloat `yaml:"cached_input" json:"cached_input"`
	Input       FlexibleFloat `yaml:"input" json:"input"`
	Output      FlexibleFloat `yaml:"output" json:"output"`
}

type UnitModelPrice struct {
	Currency string        `yaml:"currency" json:"currency"`
	Unit     string        `yaml:"unit" json:"unit"`
	Price    FlexibleFloat `yaml:"price" json:"price"`
}

const (
	ImagePricingTypePerImage    = "per_image"
	ImagePricingTypeOpenAIUsage = "openai_image_usage"
)

type ImageGenerationPrice struct {
	PricingType      string                              `yaml:"pricing_type" json:"pricing_type"`
	Currency         string                              `yaml:"currency" json:"currency"`
	Unit             any                                 `yaml:"unit" json:"unit"`
	Price            FlexibleFloat                       `yaml:"price" json:"price,omitempty"`
	TextInput        FlexibleFloat                       `yaml:"text_input" json:"text_input,omitempty"`
	TextCachedInput  FlexibleFloat                       `yaml:"text_cached_input" json:"text_cached_input,omitempty"`
	ImageInput       FlexibleFloat                       `yaml:"image_input" json:"image_input,omitempty"`
	ImageCachedInput FlexibleFloat                       `yaml:"image_cached_input" json:"image_cached_input,omitempty"`
	ImageOutput      FlexibleFloat                       `yaml:"image_output" json:"image_output,omitempty"`
	RequireUsage     bool                                `yaml:"require_usage" json:"require_usage"`
	EstimateTable    map[string]map[string]FlexibleFloat `yaml:"estimate_table" json:"estimate_table,omitempty"`
}

type VideoGenerationPrice struct {
	Currency              string             `yaml:"currency" json:"currency"`
	NoInputPricePerSecond map[string]float64 `yaml:"no_input_price_per_second" json:"no_input_price_per_second"`
	VideoInput5sMinPrice  map[string]float64 `yaml:"video_input_5s_min_price" json:"video_input_5s_min_price"`
	VideoInput5sMaxPrice  map[string]float64 `yaml:"video_input_5s_max_price" json:"video_input_5s_max_price"`
}

type ModelPricesConfig struct {
	CurrencyRates   map[string]CurrencyRate         `yaml:"currency_rates" json:"currency_rates"`
	TokenModels     map[string]TokenModelPrice      `yaml:"token_models" json:"token_models"`
	ImageGeneration map[string]ImageGenerationPrice `yaml:"image_generation" json:"image_generation"`
	VideoGeneration map[string]VideoGenerationPrice `yaml:"video_generation" json:"video_generation"`
}

type BillingConfig struct {
	CreditsPerCNY         int                `yaml:"credits_per_cny" json:"credits_per_cny"`
	TierMultipliers       map[string]float64 `yaml:"tier_multipliers" json:"tier_multipliers"`
	DefaultUserMultiplier float64            `yaml:"default_user_multiplier" json:"default_user_multiplier"`
	MinimumChargeCredits  int                `yaml:"minimum_charge_credits" json:"minimum_charge_credits"`
}

type RechargeTierConfig struct {
	Key          string `yaml:"key" json:"key"`
	Label        string `yaml:"label" json:"label"`
	PriceCNY     int    `yaml:"price_cny" json:"price_cny"`
	Credits      int    `yaml:"credits" json:"credits"`
	BonusCredits int    `yaml:"bonus_credits" json:"bonus_credits"`
	Enabled      bool   `yaml:"enabled" json:"enabled"`
}

type TokenUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64 `json:"output_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
}

type PriceSnapshot struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Currency         string  `json:"currency"`
	Unit             int64   `json:"unit"`
	CurrencyToCNY    float64 `json:"currency_to_cny"`
	CreditsPerCNY    int     `json:"credits_per_cny"`
	CachedInput      float64 `json:"cached_input"`
	Input            float64 `json:"input"`
	Output           float64 `json:"output"`
	PricingType      string  `json:"pricing_type,omitempty"`
	Price            float64 `json:"price,omitempty"`
	TextInput        float64 `json:"text_input,omitempty"`
	TextCachedInput  float64 `json:"text_cached_input,omitempty"`
	ImageInput       float64 `json:"image_input,omitempty"`
	ImageCachedInput float64 `json:"image_cached_input,omitempty"`
	ImageOutput      float64 `json:"image_output,omitempty"`
	Size             string  `json:"size,omitempty"`
	Quality          string  `json:"quality,omitempty"`
	Count            int     `json:"count,omitempty"`
	OfficialEstimate float64 `json:"official_estimate,omitempty"`
}

type TokenCreditCost struct {
	BaseCredits    int           `json:"base_credits"`
	FinalCredits   int           `json:"final_credits"`
	TierMultiplier float64       `json:"tier_multiplier"`
	UserMultiplier float64       `json:"user_multiplier"`
	Usage          TokenUsage    `json:"usage"`
	PriceSnapshot  PriceSnapshot `json:"price_snapshot"`
}

type ImageGenerationUsage struct {
	Size                   string `json:"size"`
	Quality                string `json:"quality"`
	Count                  int    `json:"count"`
	TextInputTokens        int64  `json:"text_input_tokens,omitempty"`
	TextCachedInputTokens  int64  `json:"text_cached_input_tokens,omitempty"`
	ImageInputTokens       int64  `json:"image_input_tokens,omitempty"`
	ImageCachedInputTokens int64  `json:"image_cached_input_tokens,omitempty"`
	ImageOutputTokens      int64  `json:"image_output_tokens,omitempty"`
	TotalTokens            int64  `json:"total_tokens,omitempty"`
	ReferenceImageCount    int    `json:"reference_image_count,omitempty"`
}

type ImageGenerationCreditCost struct {
	BaseCredits    int                  `json:"base_credits"`
	FinalCredits   int                  `json:"final_credits"`
	TierMultiplier float64              `json:"tier_multiplier"`
	UserMultiplier float64              `json:"user_multiplier"`
	Usage          ImageGenerationUsage `json:"usage"`
	Estimated      bool                 `json:"estimated"`
	PriceSnapshot  PriceSnapshot        `json:"price_snapshot"`
}

func (c VideoAPIConfig) CreditMultiplierOrDefault() int {
	if c.CreditMultiplier > 0 {
		return c.CreditMultiplier
	}
	return 1000
}

func (c *Config) CalculateTokenModelCredits(provider, modelName string, usage TokenUsage, tier string, userMultiplier float64) (TokenCreditCost, error) {
	if c == nil {
		return TokenCreditCost{}, fmt.Errorf("config is nil")
	}
	key := provider + "/" + modelName
	price, ok := c.ModelPrices.TokenModels[key]
	if !ok {
		return TokenCreditCost{}, fmt.Errorf("token model price not configured for %s", key)
	}
	unit := price.Unit
	if unit <= 0 {
		unit = 1_000_000
	}
	currency := strings.ToUpper(strings.TrimSpace(price.Currency))
	if currency == "" {
		currency = "CNY"
	}
	rate := 1.0
	if currency != "CNY" {
		r, ok := c.ModelPrices.CurrencyRates[currency]
		if !ok || r.ToCNY.Float64() <= 0 {
			return TokenCreditCost{}, fmt.Errorf("currency rate not configured for %s", currency)
		}
		rate = r.ToCNY.Float64()
	}
	creditsPerCNY := c.Billing.CreditsPerCNY
	if creditsPerCNY <= 0 {
		creditsPerCNY = 1000
	}
	minCharge := c.Billing.MinimumChargeCredits
	if minCharge <= 0 {
		minCharge = 1
	}
	tierMultiplier := c.Billing.DefaultUserMultiplier
	if tierMultiplier <= 0 {
		tierMultiplier = 1
	}
	if m, ok := c.Billing.TierMultipliers[strings.ToLower(strings.TrimSpace(tier))]; ok && m > 0 {
		tierMultiplier = m
	}
	if userMultiplier <= 0 {
		userMultiplier = 1
	}
	nonCachedInput := usage.InputTokens - usage.CachedInputTokens
	if nonCachedInput < 0 {
		nonCachedInput = 0
	}
	costInCurrency := (float64(nonCachedInput)*price.Input.Float64() + float64(usage.CachedInputTokens)*price.CachedInput.Float64() + float64(usage.OutputTokens)*price.Output.Float64()) / float64(unit)
	baseCredits := int(math.Ceil(costInCurrency * rate * float64(creditsPerCNY)))
	if usage.TotalTokens > 0 && baseCredits < minCharge {
		baseCredits = minCharge
	}
	finalCredits := int(math.Ceil(float64(baseCredits) * tierMultiplier * userMultiplier))
	if usage.TotalTokens > 0 && finalCredits < minCharge {
		finalCredits = minCharge
	}
	return TokenCreditCost{
		BaseCredits:    baseCredits,
		FinalCredits:   finalCredits,
		TierMultiplier: tierMultiplier,
		UserMultiplier: userMultiplier,
		Usage:          usage,
		PriceSnapshot: PriceSnapshot{
			Provider:      provider,
			Model:         modelName,
			Currency:      currency,
			Unit:          unit,
			CurrencyToCNY: rate,
			CreditsPerCNY: creditsPerCNY,
			CachedInput:   price.CachedInput.Float64(),
			Input:         price.Input.Float64(),
			Output:        price.Output.Float64(),
		},
	}, nil
}

func (c *Config) EnabledRechargeTiers() []RechargeTierConfig {
	if c == nil || len(c.RechargeTiers) == 0 {
		return nil
	}
	tiers := make([]RechargeTierConfig, 0, len(c.RechargeTiers))
	for _, tier := range c.RechargeTiers {
		if tier.Enabled {
			tiers = append(tiers, tier)
		}
	}
	return tiers
}

func (c *Config) CalculateImageGenerationEstimateCredits(provider, modelName string, usage ImageGenerationUsage, tier string, userMultiplier float64) (ImageGenerationCreditCost, error) {
	return c.calculateImageGenerationCredits(provider, modelName, usage, tier, userMultiplier, true)
}

func (c *Config) CalculateImageGenerationUsageCredits(provider, modelName string, usage ImageGenerationUsage, tier string, userMultiplier float64) (ImageGenerationCreditCost, error) {
	return c.calculateImageGenerationCredits(provider, modelName, usage, tier, userMultiplier, false)
}

func (c *Config) calculateImageGenerationCredits(provider, modelName string, usage ImageGenerationUsage, tier string, userMultiplier float64, estimate bool) (ImageGenerationCreditCost, error) {
	if c == nil {
		return ImageGenerationCreditCost{}, fmt.Errorf("config is nil")
	}
	key := provider + "/" + modelName
	price, ok := c.ModelPrices.ImageGeneration[key]
	if !ok {
		return ImageGenerationCreditCost{}, fmt.Errorf("image generation price not configured for %s", key)
	}
	pricingType := strings.TrimSpace(price.PricingType)
	if pricingType == "" {
		if strings.EqualFold(strings.TrimSpace(price.UnitString()), "image") {
			pricingType = ImagePricingTypePerImage
		}
	}
	if usage.Count <= 0 {
		usage.Count = 1
	}
	currency, rate, err := c.priceCurrencyRate(price.Currency)
	if err != nil {
		return ImageGenerationCreditCost{}, err
	}
	creditsPerCNY, minCharge, tierMultiplier, userMultiplier := c.billingInputs(tier, userMultiplier)

	var costInCurrency float64
	var estimated bool
	var officialEstimate float64
	unit := price.UnitInt64()
	if unit <= 0 {
		unit = 1_000_000
	}
	switch pricingType {
	case ImagePricingTypePerImage:
		if price.Price.Float64() <= 0 {
			return ImageGenerationCreditCost{}, fmt.Errorf("per-image price must be positive for %s", key)
		}
		costInCurrency = price.Price.Float64() * float64(usage.Count)
		unit = 0
	case ImagePricingTypeOpenAIUsage:
		if estimate {
			size := strings.TrimSpace(usage.Size)
			quality := normalizeImageQuality(usage.Quality)
			if size == "" || strings.EqualFold(size, "auto") {
				return ImageGenerationCreditCost{}, fmt.Errorf("resolved image size is required for %s estimate", key)
			}
			byQuality, ok := price.EstimateTable[size]
			if !ok {
				return ImageGenerationCreditCost{}, fmt.Errorf("estimate table missing size %s for %s", size, key)
			}
			estimatePrice, ok := byQuality[quality]
			if !ok || estimatePrice.Float64() <= 0 {
				return ImageGenerationCreditCost{}, fmt.Errorf("estimate table missing quality %s for %s size %s", quality, key, size)
			}
			usage.Quality = quality
			officialEstimate = estimatePrice.Float64()
			costInCurrency = officialEstimate * float64(usage.Count)
			estimated = true
		} else {
			if usage.TotalTokens <= 0 || usage.ImageOutputTokens <= 0 {
				return ImageGenerationCreditCost{}, fmt.Errorf("%s usage is required for billing", modelName)
			}
			textInput := usage.TextInputTokens - usage.TextCachedInputTokens
			if textInput < 0 {
				textInput = 0
			}
			imageInput := usage.ImageInputTokens - usage.ImageCachedInputTokens
			if imageInput < 0 {
				imageInput = 0
			}
			costInCurrency = (float64(textInput)*price.TextInput.Float64() +
				float64(usage.TextCachedInputTokens)*price.TextCachedInput.Float64() +
				float64(imageInput)*price.ImageInput.Float64() +
				float64(usage.ImageCachedInputTokens)*price.ImageCachedInput.Float64() +
				float64(usage.ImageOutputTokens)*price.ImageOutput.Float64()) / float64(unit)
		}
	default:
		return ImageGenerationCreditCost{}, fmt.Errorf("unsupported image pricing_type %q for %s", price.PricingType, key)
	}

	baseCredits := int(math.Ceil(costInCurrency * rate * float64(creditsPerCNY)))
	if baseCredits < minCharge {
		baseCredits = minCharge
	}
	finalCredits := int(math.Ceil(float64(baseCredits) * tierMultiplier * userMultiplier))
	if finalCredits < minCharge {
		finalCredits = minCharge
	}
	return ImageGenerationCreditCost{
		BaseCredits:    baseCredits,
		FinalCredits:   finalCredits,
		TierMultiplier: tierMultiplier,
		UserMultiplier: userMultiplier,
		Usage:          usage,
		Estimated:      estimated,
		PriceSnapshot: PriceSnapshot{
			Provider:         provider,
			Model:            modelName,
			Currency:         currency,
			Unit:             unit,
			CurrencyToCNY:    rate,
			CreditsPerCNY:    creditsPerCNY,
			PricingType:      pricingType,
			Price:            price.Price.Float64(),
			TextInput:        price.TextInput.Float64(),
			TextCachedInput:  price.TextCachedInput.Float64(),
			ImageInput:       price.ImageInput.Float64(),
			ImageCachedInput: price.ImageCachedInput.Float64(),
			ImageOutput:      price.ImageOutput.Float64(),
			Size:             usage.Size,
			Quality:          usage.Quality,
			Count:            usage.Count,
			OfficialEstimate: officialEstimate,
		},
	}, nil
}

func (p ImageGenerationPrice) UnitString() string {
	switch v := p.Unit.(type) {
	case string:
		return strings.TrimSpace(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if v == math.Trunc(v) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func (p ImageGenerationPrice) UnitInt64() int64 {
	switch v := p.Unit.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func normalizeImageQuality(quality string) string {
	quality = strings.ToLower(strings.TrimSpace(quality))
	if quality == "" || quality == "auto" {
		return "medium"
	}
	return quality
}

func (c *Config) priceCurrencyRate(currencyValue string) (string, float64, error) {
	currency := strings.ToUpper(strings.TrimSpace(currencyValue))
	if currency == "" {
		currency = "CNY"
	}
	if currency == "CNY" {
		return currency, 1, nil
	}
	r, ok := c.ModelPrices.CurrencyRates[currency]
	if !ok || r.ToCNY.Float64() <= 0 {
		return "", 0, fmt.Errorf("currency rate not configured for %s", currency)
	}
	return currency, r.ToCNY.Float64(), nil
}

func (c *Config) billingInputs(tier string, userMultiplier float64) (creditsPerCNY int, minCharge int, tierMultiplier float64, normalizedUserMultiplier float64) {
	creditsPerCNY = c.Billing.CreditsPerCNY
	if creditsPerCNY <= 0 {
		creditsPerCNY = 1000
	}
	minCharge = c.Billing.MinimumChargeCredits
	if minCharge <= 0 {
		minCharge = 1
	}
	tierMultiplier = c.Billing.DefaultUserMultiplier
	if tierMultiplier <= 0 {
		tierMultiplier = 1
	}
	if m, ok := c.Billing.TierMultipliers[strings.ToLower(strings.TrimSpace(tier))]; ok && m > 0 {
		tierMultiplier = m
	}
	normalizedUserMultiplier = userMultiplier
	if normalizedUserMultiplier <= 0 {
		normalizedUserMultiplier = 1
	}
	return creditsPerCNY, minCharge, tierMultiplier, normalizedUserMultiplier
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

// FunASRConfig holds Aliyun Fun-ASR recorded speech HTTP configuration.
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

// Complete reports whether all required settings for Aliyun Fun-ASR HTTP calls exist.
func (c FunASRConfig) Complete() bool {
	return strings.TrimSpace(c.BaseURL) != "" &&
		strings.TrimSpace(c.APIKey) != ""
}

// ClaudeConfig holds configuration for the Claude CLI subprocess.
// The Env map is passed as environment variables to the CLI process,
// supporting auth tokens, base URLs, model overrides, etc.
type ClaudeConfig struct {
	Model          string            `yaml:"model"`    // Model for agent execution (empty = use env vars like ANTHROPIC_MODEL)
	Executor       string            `yaml:"executor"` // "local" (default) or "docker"
	Env            map[string]string `yaml:"env"`
	PluginDir      string            `yaml:"plugin_dir"`       // Path to the Anban Creator plugin directory (contains agents/, skills/)
	Sandbox        bool              `yaml:"sandbox"`          // Enable sandbox isolation for agent execution (recommended in k8s)
	Docker         DockerConfig      `yaml:"docker"`           // Docker executor settings (used when executor=docker)
	MaxTurns       map[string]int    `yaml:"max_turns"`        // Per-task-type max turns, e.g. {"article": 300, "seednote": 150}
	TaskLogDir     string            `yaml:"task_log_dir"`     // Directory for per-task agent execution logs. Empty = disabled.
	AgentServerURL string            `yaml:"agent_server_url"` // Override server URL for agent MCP connections (e.g. k8s service URL). To env-control, write ${ANBAN_CLAUDE_AGENT_SERVER_URL} in config.yaml.
}

// DockerConfig holds Docker executor settings for container-based task execution.
type DockerConfig struct {
	Image         string `yaml:"image"`          // Docker image name (default: "anban-creator-agent:latest")
	CPUCores      int64  `yaml:"cpu_cores"`      // CPU limit in cores (default: 2)
	MemoryMB      int64  `yaml:"memory_mb"`      // Memory limit in MB (default: 4096)
	TimeoutSec    int    `yaml:"timeout_sec"`    // Container execution timeout in seconds (default: 1800 = 30 min)
	ContainerName string `yaml:"container_name"` // Name of a persistent container to reuse via docker exec (empty = create+destroy per task)
	WorkspaceDir  string `yaml:"workspace_dir"`  // Host-side base directory for task workspaces (persistent container mode, must match volume mount source)
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

// CreditsConfig holds credits/points system configuration.
type CreditsConfig struct {
	DailySignIn   int                       `yaml:"daily_sign_in"`  // credits awarded per daily sign-in (default 100)
	RegisterBonus int                       `yaml:"register_bonus"` // credits awarded on registration (default 1000)
	InviteReward  int                       `yaml:"invite_reward"`  // credits awarded to inviter when invitee registers (default 1000)
	TaskCosts     map[string]int            `yaml:"task_costs"`     // base service fees by task type, e.g. {"article": 4000, "seednote": 3600}
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
	cfg.resolvePaths()

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
		"video_api": "model_routes.video_generation",
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
		"image_presets":   true,
		"model_providers": true,
		"model_routes":    true,
		"model_prices":    true,
		"billing":         true,
		"recharge_tiers":  true,
		"tingwu":          true,
		"funasr":          true,
		"claude":          true,
		"credits":         true,
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
	if credits, ok := top["credits"].(map[string]any); ok {
		if _, ok := credits["model_costs"]; ok {
			return fmt.Errorf("deprecated config key credits.model_costs; use model_prices and billing")
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
	if c.VideoAPI.BaseURL == "" {
		c.VideoAPI.BaseURL = DefaultVideoAPIBaseURL
	}
	if c.VideoAPI.Timeout == 0 {
		c.VideoAPI.Timeout = 10 * time.Minute
	}
	if c.VideoAPI.CreditMultiplier == 0 {
		c.VideoAPI.CreditMultiplier = 1000
	}
	if c.Billing.CreditsPerCNY == 0 {
		c.Billing.CreditsPerCNY = 1600
	}
	if c.Billing.TierMultipliers == nil {
		c.Billing.TierMultipliers = map[string]float64{
			"free":       1.35,
			"pro":        1.20,
			"enterprise": 1.05,
		}
	}
	if c.Billing.DefaultUserMultiplier == 0 {
		c.Billing.DefaultUserMultiplier = 1
	}
	if c.Billing.MinimumChargeCredits == 0 {
		c.Billing.MinimumChargeCredits = 1
	}

	// Credits defaults.
	if c.Credits.DailySignIn == 0 {
		c.Credits.DailySignIn = 100
	}
	if c.Credits.RegisterBonus == 0 {
		c.Credits.RegisterBonus = 1000
	}
	if c.Credits.InviteReward == 0 {
		c.Credits.InviteReward = 1000
	}
	if c.Credits.TaskCosts == nil {
		c.Credits.TaskCosts = map[string]int{
			"article":        4000,
			"seednote":       3600,
			"viral_analysis": 1200,
		}
	} else if _, ok := c.Credits.TaskCosts["viral_analysis"]; !ok {
		c.Credits.TaskCosts["viral_analysis"] = 1200
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
	if c.RechargeTiers == nil {
		c.RechargeTiers = []RechargeTierConfig{
			{Key: "basic", Label: "基础包", PriceCNY: 10, Credits: 10000, Enabled: true},
			{Key: "standard", Label: "标准包", PriceCNY: 50, Credits: 52000, BonusCredits: 2000, Enabled: true},
			{Key: "pro", Label: "进阶包", PriceCNY: 100, Credits: 110000, BonusCredits: 10000, Enabled: true},
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
	if c.Claude.MaxTurns == nil {
		c.Claude.MaxTurns = map[string]int{
			"article":   100,
			"seednote":  60,
			"ecommerce": 120, // 多产品图 → 产品档案 → 主图/详情/封面/分享/SKU 批量 + 视觉自检循环，给足余量
		}
	}
	if c.Claude.Docker.Image == "" {
		c.Claude.Docker.Image = "anban-creator-agent:latest"
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
			cfg, err := c.imageAPIFromRoute("model_routes.image_generation.designer."+key, route)
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
	if c.ModelRoutes.VideoGeneration.Provider != "" || len(c.ModelRoutes.VideoGeneration.ModelCatalog) > 0 {
		p, err := provider("model_routes.video_generation", c.ModelRoutes.VideoGeneration.Provider)
		if err != nil {
			return err
		}
		c.VideoAPI.Key = p.APIKey
		c.VideoAPI.BaseURL = p.BaseURL
		c.VideoAPI.Timeout = c.ModelRoutes.VideoGeneration.Timeout
		c.VideoAPI.ModelCatalog = c.ModelRoutes.VideoGeneration.ModelCatalog
		for i := range c.VideoAPI.ModelCatalog {
			if c.VideoAPI.ModelCatalog[i].ModelID == "" {
				c.VideoAPI.ModelCatalog[i].ModelID = c.VideoAPI.ModelCatalog[i].Model
			}
			priceKey := c.ModelRoutes.VideoGeneration.Provider + "/" + c.VideoAPI.ModelCatalog[i].ModelID
			if price, ok := c.ModelPrices.VideoGeneration[priceKey]; ok {
				c.VideoAPI.ModelCatalog[i].NoInputPricePerSecond = price.NoInputPricePerSecond
				c.VideoAPI.ModelCatalog[i].VideoInput5sMinPrice = price.VideoInput5sMinPrice
				c.VideoAPI.ModelCatalog[i].VideoInput5sMaxPrice = price.VideoInput5sMaxPrice
			}
		}
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
		ResponseFormat: route.ResponseFormat,
	}
	if price, ok := c.ModelPrices.ImageGeneration[route.Provider+"/"+route.Model]; ok {
		pricingType := strings.TrimSpace(price.PricingType)
		if pricingType == "" && strings.EqualFold(price.UnitString(), "image") {
			pricingType = ImagePricingTypePerImage
		}
		if isGPTImageModelName(route.Model) && pricingType != ImagePricingTypeOpenAIUsage {
			return nil, fmt.Errorf("%s model %s requires pricing_type %s; fixed per-image pricing is not allowed", routeName, route.Model, ImagePricingTypeOpenAIUsage)
		}
		if pricingType == ImagePricingTypePerImage {
			cfg.Credits = int(math.Ceil(price.Price.Float64() * c.currencyToCNY(price.Currency) * float64(c.creditsPerCNY())))
		}
	} else if isGPTImageModelName(route.Model) {
		return nil, fmt.Errorf("%s model %s requires model_prices.image_generation entry with pricing_type %s", routeName, route.Model, ImagePricingTypeOpenAIUsage)
	}
	return cfg, nil
}

func isGPTImageModelName(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.HasPrefix(modelName, "gpt-image-") || modelName == "chatgpt-image-latest"
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

func (c *Config) currencyToCNY(currency string) float64 {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" || currency == "CNY" {
		return 1
	}
	if rate, ok := c.ModelPrices.CurrencyRates[currency]; ok && rate.ToCNY.Float64() > 0 {
		return rate.ToCNY.Float64()
	}
	return 1
}

func (c *Config) creditsPerCNY() int {
	if c.Billing.CreditsPerCNY > 0 {
		return c.Billing.CreditsPerCNY
	}
	return 1000
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

// detectPluginDir attempts to locate the Anban Creator plugin directory
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
