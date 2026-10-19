package config

import (
	"strings"
	"testing"
	"time"
)

func TestSearchConfigValidationRequiresSelectedProviderCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, capability, provider, want string
	}{
		{name: "web deepseek", capability: "web", provider: "deepseek", want: "search.providers.deepseek.api_key"},
		{name: "web doubao custom", capability: "web", provider: "doubao_custom", want: "search.providers.doubao.api_key"},
		{name: "fetch deepseek", capability: "fetch", provider: "deepseek", want: "search.providers.deepseek.api_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			search := SearchConfig{}
			if tc.capability == "web" {
				search.Web = SearchCapabilityConfig{Enabled: true, Provider: tc.provider}
			} else {
				search.Fetch = SearchCapabilityConfig{Enabled: true, Provider: tc.provider}
			}
			cfg := Config{Trends: TrendsConfig{TTL: time.Minute, Timeout: time.Second}, Search: search}
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation error = %v, want %s", err, tc.want)
			}
			if strings.Contains(err.Error(), ".model") {
				t.Fatalf("search API must not require a model: %v", err)
			}
		})
	}
}

func TestSearchConfigValidationRejectsUnknownProvider(t *testing.T) {
	cfg := Config{Trends: TrendsConfig{TTL: time.Minute, Timeout: time.Second}, Search: SearchConfig{Web: SearchCapabilityConfig{Enabled: true, Provider: "unknown"}}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), `search.web.provider "unknown" is unsupported`) {
		t.Fatalf("unknown provider error = %v", err)
	}
}

func TestSearchConfigValidationAllowsDisabledSearchWithoutCredentials(t *testing.T) {
	cfg := Config{Trends: TrendsConfig{TTL: time.Minute, Timeout: time.Second}, Search: SearchConfig{}}
	if err := cfg.Validate(); err != nil && strings.Contains(err.Error(), "search.") {
		t.Fatalf("disabled search produced search validation error: %v", err)
	}
}

func TestSearchConfigDefaultsEditionSpecificEndpoints(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.Search.Providers.Doubao.CustomEndpoint != "https://open.feedcoopapi.com/search_api/web_search" {
		t.Fatalf("custom endpoint = %q", cfg.Search.Providers.Doubao.CustomEndpoint)
	}
	if cfg.Search.Providers.Doubao.GlobalEndpoint != "https://open.feedcoopapi.com/search_api/global_search" {
		t.Fatalf("global endpoint = %q", cfg.Search.Providers.Doubao.GlobalEndpoint)
	}
	if cfg.Search.Providers.DeepSeek.SearchEndpoint != "https://api.deepseek.com/v1/tools/search" {
		t.Fatalf("deepseek endpoint = %q", cfg.Search.Providers.DeepSeek.SearchEndpoint)
	}
}

func TestSearchConfigValidationAcceptsDeepSeekWithinOfficialLimits(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Search = SearchConfig{
		Web:       SearchCapabilityConfig{Enabled: true, Provider: "deepseek"},
		Providers: SearchProvidersConfig{DeepSeek: SearchDeepSeekConfig{SearchEndpoint: "https://api.deepseek.com/v1/tools/search", APIKey: "secret", Timeout: time.Second, MaxResults: 50}},
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
		{name: "endpoint", cfg: SearchDeepSeekConfig{APIKey: "secret", Timeout: time.Second, MaxResults: 10}, want: "search.providers.deepseek.search_endpoint"},
		{name: "api key", cfg: SearchDeepSeekConfig{SearchEndpoint: "https://api.deepseek.com/v1/tools/search", Timeout: time.Second, MaxResults: 10}, want: "search.providers.deepseek.api_key"},
		{name: "insecure endpoint", cfg: SearchDeepSeekConfig{SearchEndpoint: "http://api.deepseek.com/v1/tools/search", APIKey: "secret", Timeout: time.Second, MaxResults: 10}, want: "absolute HTTPS URL"},
		{name: "timeout", cfg: SearchDeepSeekConfig{SearchEndpoint: "https://api.deepseek.com/v1/tools/search", APIKey: "secret", MaxResults: 10}, want: "search.providers.deepseek.timeout"},
		{name: "max results", cfg: SearchDeepSeekConfig{SearchEndpoint: "https://api.deepseek.com/v1/tools/search", APIKey: "secret", Timeout: time.Second}, want: "search.providers.deepseek.max_results"},
		{name: "too many results", cfg: SearchDeepSeekConfig{SearchEndpoint: "https://api.deepseek.com/v1/tools/search", APIKey: "secret", Timeout: time.Second, MaxResults: 51}, want: "search.providers.deepseek.max_results"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Search = SearchConfig{Web: SearchCapabilityConfig{Enabled: true, Provider: "deepseek"}, Providers: SearchProvidersConfig{DeepSeek: tc.cfg}}
			err = cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestSearchConfigValidationUsesCapabilityRoutes(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Search = SearchConfig{
		Web:   SearchCapabilityConfig{Enabled: true, Provider: "deepseek"},
		Fetch: SearchCapabilityConfig{Enabled: true, Provider: "deepseek"},
		Providers: SearchProvidersConfig{DeepSeek: SearchDeepSeekConfig{
			APIKey: "secret", SearchEndpoint: "https://api.deepseek.com/v1/tools/search", FetchEndpoint: "https://api.deepseek.com/v1/tools/fetch", Timeout: time.Second, MaxResults: 10,
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("capability config rejected: %v", err)
	}
}

func TestSearchConfigValidationDoesNotRequireDisabledImageProvider(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Search = SearchConfig{Images: SearchCapabilityConfig{Enabled: false, Provider: "doubao"}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled image search rejected: %v", err)
	}
}

func TestSearchConfigValidationRejectsUnimplementedImageSearch(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Search = SearchConfig{Images: SearchCapabilityConfig{Enabled: true, Provider: "doubao"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "search.images is not available") {
		t.Fatalf("enabled image search validation error = %v", err)
	}
}
