package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Onboarding is a platform-funded, bounded onboarding capability, not a task SKU.
// Credentials stay exclusively on Server. Missing configuration disables it.
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
