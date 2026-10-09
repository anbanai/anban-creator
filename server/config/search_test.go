package config

import (
	"strings"
	"testing"
	"time"
)

func TestSearchConfigValidationRequiresSelectedEditionCredentials(t *testing.T) {
	for _, edition := range []string{"custom", "global"} {
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
}
