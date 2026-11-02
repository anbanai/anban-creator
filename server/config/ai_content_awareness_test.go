package config

import "testing"

func TestValidateRejectsAIContentAwarenessWithLocalStorage(t *testing.T) {
	cfg := Config{Storage: StorageConfig{Provider: "local", AIContentAwareness: AIContentAwarenessConfig{Enabled: true}}}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected local storage with AI content awareness to be rejected")
	}
}

func TestAIContentAwarenessDefaultsLimitAndTimeout(t *testing.T) {
	cfg := AIContentAwarenessConfig{MaxResults: 1000}
	cfg.ApplyDefaults()
	if cfg.QueryTimeout <= 0 || cfg.MaxResults != 20 {
		t.Fatalf("defaults = %#v", cfg)
	}
}
