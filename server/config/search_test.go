package config

import (
	"strings"
	"testing"
	"time"
)

func TestSearchConfigValidationRequiresEnabledProviderCredentials(t *testing.T) {
	cfg := Config{Trends: TrendsConfig{TTL: time.Minute, Timeout: time.Second}, Search: SearchConfig{Enabled: true, Provider: "doubao"}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("enabled search with missing credentials was accepted")
	}
	message := err.Error()
	for _, field := range []string{"search.doubao.base_url", "search.doubao.api_key", "search.doubao.model"} {
		if !strings.Contains(message, field) {
			t.Fatalf("validation error %q missing %s", message, field)
		}
	}
}

func TestSearchConfigValidationRejectsUnknownProvider(t *testing.T) {
	cfg := Config{Trends: TrendsConfig{TTL: time.Minute, Timeout: time.Second}, Search: SearchConfig{Enabled: true, Provider: "unknown"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), `search.provider "unknown" is unsupported`) {
		t.Fatalf("unknown provider error = %v", err)
	}
}

func TestSearchConfigRejectsUnsupportedAutomaticFallback(t *testing.T) {
	cfg := Config{Search: SearchConfig{Fallback: SearchFallbackConfig{Enabled: true}}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "search.fallback.enabled is not supported") {
		t.Fatalf("enabled fallback error = %v", err)
	}
}

func TestSearchConfigValidationAllowsDisabledSearchWithoutCredentials(t *testing.T) {
	cfg := Config{Trends: TrendsConfig{TTL: time.Minute, Timeout: time.Second}, Search: SearchConfig{Enabled: false}}
	if err := cfg.Validate(); err != nil && strings.Contains(err.Error(), "search.") {
		t.Fatalf("disabled search produced search validation error: %v", err)
	}
}
