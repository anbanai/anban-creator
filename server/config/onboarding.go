package config

import (
	"fmt"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

// Onboarding is a platform-funded, bounded onboarding capability, not a task SKU.
// Credentials stay exclusively on Server. An omitted section can reuse the
// existing effective DeepSeek profile; an explicit section always takes priority.
type OnboardingConfig struct {
	Enabled          bool               `yaml:"enabled"`
	Chat             OnboardingProvider `yaml:"chat"`
	Speech           OnboardingProvider `yaml:"speech"`
	DailyUserLimit   int                `yaml:"daily_user_limit"`
	DailyGlobalLimit int                `yaml:"daily_global_limit"`
}

type OnboardingProvider struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key" json:"-"`
	Model   string `yaml:"model"`
}

func (p OnboardingProvider) Configured() bool {
	return strings.TrimSpace(p.BaseURL) != "" && strings.TrimSpace(p.APIKey) != "" && strings.TrimSpace(p.Model) != ""
}

func (p OnboardingProvider) String() string {
	return fmt.Sprintf("OnboardingProvider{Configured:%t}", p.Configured())
}

func (p OnboardingProvider) GoString() string { return p.String() }

// Reuse the already-resolved Server profile, never read provider credentials
// from a client or silently send an existing key to a different provider.
// Official DeepSeek supports both protocols on the same origin. Custom proxy
// paths require explicit onboarding config rather than a guessed conversion.
func (c *Config) inheritOnboardingProvider(data []byte) error {
	var top map[string]yaml.Node
	if err := yaml.Unmarshal(data, &top); err != nil {
		return fmt.Errorf("parse onboarding configuration: %w", err)
	}
	if _, explicit := top["onboarding"]; explicit {
		return nil
	}
	p, ok := c.Claude.ExecutionProfiles["effective"]
	if !ok || !strings.EqualFold(strings.TrimSpace(p.Provider), "deepseek") {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(p.Envs["ANTHROPIC_BASE_URL"]))
	if err != nil || u.Scheme != "https" || u.Host != "api.deepseek.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil
	}
	switch strings.TrimRight(u.Path, "/") {
	case "", "/v1", "/anthropic":
		u.Path = ""
	default:
		return nil
	}
	model := strings.TrimSpace(p.Envs["ANTHROPIC_MODEL"])
	if alias := strings.TrimSpace(p.ModelUsageAliases[model]); alias != "" {
		model = alias
	}
	provider := OnboardingProvider{BaseURL: u.String(), APIKey: strings.TrimSpace(p.Envs["ANTHROPIC_AUTH_TOKEN"]), Model: model}
	if !provider.Configured() {
		return nil
	}
	c.Onboarding.Chat = provider
	c.Onboarding.Enabled = true
	return nil
}

func (c OnboardingConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if !c.Chat.Configured() {
		return fmt.Errorf("onboarding.chat requires base_url, api_key and model")
	}
	for name, p := range map[string]OnboardingProvider{"chat": c.Chat, "speech": c.Speech} {
		if name == "speech" && p.BaseURL == "" && p.APIKey == "" && p.Model == "" {
			continue
		}
		u, err := url.Parse(p.BaseURL)
		if !p.Configured() || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("onboarding.%s requires a complete HTTPS provider configuration", name)
		}
	}
	if c.DailyUserLimit < 1 || c.DailyGlobalLimit < 1 {
		return fmt.Errorf("onboarding daily limits must be positive")
	}
	return nil
}
