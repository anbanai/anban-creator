package config

import (
	"strings"
	"testing"
	"time"
)

func TestSearchConfigValidationRequiresSelectedEditionCredentials(t *testing.T) {
	for _, edition := range []string{"custom", "global", "deepseek"} {
		t.Run(edition, func(t *testing.T) {
			cfg := Config{Trends: TrendsConfig{TTL: time.Minute, Timeout: time.Second}, Search: SearchConfig{Enabled: true, Provider: edition}}
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "search."+edition+".api_key") {
				t.Fatalf("validation error = %v, want selected edition credentials", err)
			}
			if strings.Contains(err.Error(), ".model") {
				t.Fatalf("search API must not require a model: %v", err)
			}
		})
	}
}

func TestSearchConfigValidationRejectsUnknownProvider(t *testing.T) {
	cfg := Config{Trends: TrendsConfig{TTL: time.Minute, Timeout: time.Second}, Search: SearchConfig{Enabled: true, Provider: "unknown"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), `search.provider "unknown" is unsupported`) {
		t.Fatalf("unknown provider error = %v", err)
	}
}

func TestSearchConfigValidationAllowsDisabledSearchWithoutCredentials(t *testing.T) {
	cfg := Config{Trends: TrendsConfig{TTL: time.Minute, Timeout: time.Second}, Search: SearchConfig{Enabled: false}}
	if err := cfg.Validate(); err != nil && strings.Contains(err.Error(), "search.") {
		t.Fatalf("disabled search produced search validation error: %v", err)
	}
}

func TestSearchConfigDefaultsEditionSpecificEndpoints(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.Search.Custom.Endpoint != "https://open.feedcoopapi.com/search_api/web_search" {
		t.Fatalf("custom endpoint = %q", cfg.Search.Custom.Endpoint)
	}
	if cfg.Search.Global.Endpoint != "https://open.feedcoopapi.com/search_api/global_search" {
		t.Fatalf("global endpoint = %q", cfg.Search.Global.Endpoint)
	}
	if cfg.Search.DeepSeek.Endpoint != "https://api.deepseek.com/v1/tools/search" {
		t.Fatalf("deepseek endpoint = %q", cfg.Search.DeepSeek.Endpoint)
	}
}

func TestSearchConfigValidationAcceptsDeepSeekWithinOfficialLimits(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Search = SearchConfig{
		Enabled:  true,
		Provider: "deepseek",
		DeepSeek: SearchDeepSeekConfig{Endpoint: "https://api.deepseek.com/v1/tools/search", APIKey: "secret", Timeout: time.Second, MaxResults: 50},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid DeepSeek search config rejected: %v", err)
	}
}

func TestSearchConfigValidationRejectsIncompleteDeepSeekConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  SearchDeepSeekConfig
		want string
	}{
		{name: "endpoint", cfg: SearchDeepSeekConfig{APIKey: "secret", Timeout: time.Second, MaxResults: 10}, want: "search.deepseek.endpoint"},
		{name: "api key", cfg: SearchDeepSeekConfig{Endpoint: "https://api.deepseek.com/v1/tools/search", Timeout: time.Second, MaxResults: 10}, want: "search.deepseek.api_key"},
		{name: "insecure endpoint", cfg: SearchDeepSeekConfig{Endpoint: "http://api.deepseek.com/v1/tools/search", APIKey: "secret", Timeout: time.Second, MaxResults: 10}, want: "absolute HTTPS URL"},
		{name: "timeout", cfg: SearchDeepSeekConfig{Endpoint: "https://api.deepseek.com/v1/tools/search", APIKey: "secret", MaxResults: 10}, want: "search.deepseek.timeout"},
		{name: "max results", cfg: SearchDeepSeekConfig{Endpoint: "https://api.deepseek.com/v1/tools/search", APIKey: "secret", Timeout: time.Second}, want: "search.deepseek.max_results"},
		{name: "too many results", cfg: SearchDeepSeekConfig{Endpoint: "https://api.deepseek.com/v1/tools/search", APIKey: "secret", Timeout: time.Second, MaxResults: 51}, want: "search.deepseek.max_results"},
		{name: "zero results", cfg: SearchDeepSeekConfig{Endpoint: "https://api.deepseek.com/v1/tools/search", APIKey: "secret", Timeout: time.Second, MaxResults: 0}, want: "search.deepseek.max_results"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Search = SearchConfig{Enabled: true, Provider: "deepseek", DeepSeek: tc.cfg}
			err = cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %v, want %s", err, tc.want)
			}
		})
	}
}
