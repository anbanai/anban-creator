package config

import (
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"testing"
)

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

func TestMediaLibraryPrefixDefaultsAndValidation(t *testing.T) {
	cfg := Config{Storage: StorageConfig{Provider: "oss"}}
	cfg.applyDefaults()
	if cfg.Storage.MediaLibraryPrefix != "assets/media-library/" {
		t.Fatalf("media library prefix = %q, want default prefix", cfg.Storage.MediaLibraryPrefix)
	}

	cfg.Storage.MediaLibraryPrefix = "../assets/"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "storage.media_library_prefix") {
		t.Fatalf("expected prefix validation error, got %v", err)
	}
}

func TestNormalizeMediaLibraryPrefix(t *testing.T) {
	for _, tt := range []struct {
		input, want string
		invalid     bool
	}{
		{input: "", want: "assets/media-library/"},
		{input: " custom/library ", want: "custom/library/"},
		{input: "custom/library/", want: "custom/library/"},
		{input: "library/", want: "library/"},
		{input: "/assets/media-library/", invalid: true},
		{input: "assets/../library/", invalid: true},
		{input: "assets/./library/", invalid: true},
		{input: "assets//library/", invalid: true},
		{input: "assets/library//", invalid: true},
		{input: "https://bucket/library/", invalid: true},
		{input: "assets/", invalid: true},
		{input: "assets/users/", invalid: true},
		{input: "assets/users/user-1/", invalid: true},
		{input: "uploads/", invalid: true},
		{input: "uploads/pending/", invalid: true},
		{input: "library/%2e%2e/", invalid: true},
		{input: "library/*/", invalid: true},
		{input: strings.Repeat("a", 120), invalid: true},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got, err := NormalizeMediaLibraryPrefix(tt.input)
			if (err != nil) != tt.invalid || got != tt.want {
				t.Fatalf("prefix = %q, %v; want %q, invalid=%v", got, err, tt.want, tt.invalid)
			}
		})
	}
}

func TestExampleDeclaresMediaLibraryPrefix(t *testing.T) {
	raw, err := os.ReadFile("../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var example struct {
		Storage struct {
			MediaLibraryPrefix string `yaml:"media_library_prefix"`
		} `yaml:"storage"`
	}
	if err := yaml.Unmarshal(raw, &example); err != nil {
		t.Fatal(err)
	}
	if example.Storage.MediaLibraryPrefix != DefaultMediaLibraryPrefix {
		t.Fatalf("example prefix = %q", example.Storage.MediaLibraryPrefix)
	}
}
