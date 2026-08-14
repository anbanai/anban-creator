package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMontageConfigDefaults(t *testing.T) {
	cfg := MontageConfig{}
	cfg.ApplyDefaults()

	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if cfg.DefaultPipeline != "cinematic" {
		t.Fatalf("DefaultPipeline = %q, want cinematic", cfg.DefaultPipeline)
	}
	for _, want := range []string{"cinematic", "talking-head", "screen-demo", "clip-factory"} {
		if !montageStringSliceContains(cfg.AllowedPipelines, want) {
			t.Fatalf("AllowedPipelines = %#v, want %q", cfg.AllowedPipelines, want)
		}
	}
	if cfg.MaxDurationSeconds != 600 {
		t.Fatalf("MaxDurationSeconds = %d, want 600", cfg.MaxDurationSeconds)
	}
	if cfg.MaxAssets != 20 {
		t.Fatalf("MaxAssets = %d, want 20", cfg.MaxAssets)
	}
	if cfg.TimeoutMinutes != 90 {
		t.Fatalf("TimeoutMinutes = %d, want 90", cfg.TimeoutMinutes)
	}
	if cfg.DefaultExecutionTarget != "cloud" {
		t.Fatalf("DefaultExecutionTarget = %q, want cloud", cfg.DefaultExecutionTarget)
	}
	if cfg.Env == nil {
		t.Fatal("Env = nil, want empty map")
	}
	if cfg.ToolPolicy == nil {
		t.Fatal("ToolPolicy = nil, want empty map")
	}
	if cfg.PipelineDefaults == nil {
		t.Fatal("PipelineDefaults = nil, want empty map")
	}
}

func TestMontageConfigValidate(t *testing.T) {
	cfg := MontageConfig{
		Enabled:                true,
		DefaultPipeline:        "cinematic",
		AllowedPipelines:       []string{"cinematic", "social-short"},
		MaxDurationSeconds:     600,
		MaxAssets:              20,
		TimeoutMinutes:         90,
		ExecutionTargets:       []string{"cloud", "local"},
		DefaultExecutionTarget: "cloud",
		Env: map[string]string{
			"NEW_PROVIDER_TOKEN":      "future-secret",
			"VIDEO_GEN_LOCAL_ENABLED": "false",
		},
		ToolPolicy: map[string]MontageToolCapabilityPolicy{
			"video_generation": {Preferred: []string{"fal", "runway"}},
		},
		PipelineDefaults: map[string]map[string]any{
			"cinematic": {"budget_usd": 2.0, "video_generation": "auto"},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate error = %v", err)
	}

	cfg.DefaultPipeline = "missing"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate succeeded with default pipeline outside allowed list")
	}
}

func TestMontageConfigAcceptsArbitraryEnvKeys(t *testing.T) {
	cfg := MontageConfig{
		Enabled:                true,
		DefaultPipeline:        "default",
		AllowedPipelines:       []string{"default"},
		MaxDurationSeconds:     600,
		MaxAssets:              20,
		TimeoutMinutes:         90,
		ExecutionTargets:       []string{"cloud"},
		DefaultExecutionTarget: "cloud",
		Env: map[string]string{
			"NEW_PROVIDER_TOKEN": "secret",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate error = %v, want arbitrary environment key accepted", err)
	}
}

func TestMontageConfigRejectsMalformedEnvEntries(t *testing.T) {
	base := MontageConfig{
		Enabled:                true,
		DefaultPipeline:        "default",
		AllowedPipelines:       []string{"default"},
		MaxDurationSeconds:     600,
		MaxAssets:              20,
		TimeoutMinutes:         90,
		ExecutionTargets:       []string{"cloud"},
		DefaultExecutionTarget: "cloud",
	}
	for _, env := range []map[string]string{
		{"": "secret"},
		{"BAD=KEY": "secret"},
		{"BAD\x00KEY": "secret"},
		{"NEW_PROVIDER_TOKEN": "bad\x00value"},
	} {
		cfg := base
		cfg.Env = env
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "montage.env") {
			t.Fatalf("Validate env %#v error = %v, want malformed entry rejection", env, err)
		}
	}
}

func TestMontageEnvRedactionDoesNotExposeSecrets(t *testing.T) {
	cfg := MontageConfig{Env: map[string]string{
		"NEW_PROVIDER_TOKEN": "future-secret",
		"RUNWAY_API_KEY":     "",
	}}

	redacted := cfg.RedactedEnv()
	if redacted["NEW_PROVIDER_TOKEN"] != true {
		t.Fatalf("NEW_PROVIDER_TOKEN configured = %v, want true", redacted["NEW_PROVIDER_TOKEN"])
	}
	if redacted["RUNWAY_API_KEY"] != false {
		t.Fatalf("RUNWAY_API_KEY configured = %v, want false", redacted["RUNWAY_API_KEY"])
	}
	data, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("marshal redacted env: %v", err)
	}
	if strings.Contains(string(data), "future-secret") {
		t.Fatalf("redacted env leaked secret: %s", data)
	}
}

func TestMontageConfigDecodesEnv(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte("montage:\n  env:\n    NEW_PROVIDER_TOKEN: future-secret\n"), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Montage.Env["NEW_PROVIDER_TOKEN"] != "future-secret" {
		t.Fatalf("Env = %#v, want NEW_PROVIDER_TOKEN", cfg.Montage.Env)
	}
}

func TestMontageConfigDefaultsPreserveExplicitDisabled(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte("montage:\n  enabled: false\n"), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cfg.applyDefaults()

	if cfg.Montage.Enabled {
		t.Fatal("Enabled = true, want explicit false preserved")
	}
}

func TestMontageConfigFilesDoNotDeclareRunnerImage(t *testing.T) {
	for _, path := range []string{"../config.example.yaml"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		raw := string(body)
		if strings.Contains(raw, "ANBAN_MONTAGE_RUNNER_IMAGE") || strings.Contains(raw, "cloud_image:") {
			t.Fatalf("%s must not declare montage.runner.cloud_image; use claude.docker.article_image or claude.kubernetes.article_image", path)
		}
	}
}
