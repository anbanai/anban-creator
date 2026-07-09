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
	if cfg.SubmodulePath != "third_party/OpenMontage" {
		t.Fatalf("SubmodulePath = %q, want %q", cfg.SubmodulePath, "third_party/OpenMontage")
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
	if cfg.ProviderEnv == nil {
		t.Fatal("ProviderEnv = nil, want empty map")
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
		SubmodulePath:          "third_party/OpenMontage",
		DefaultPipeline:        "cinematic",
		AllowedPipelines:       []string{"cinematic", "social-short"},
		MaxDurationSeconds:     600,
		MaxAssets:              20,
		TimeoutMinutes:         90,
		ExecutionTargets:       []string{"cloud", "local"},
		DefaultExecutionTarget: "cloud",
		ProviderEnv: map[string]string{
			"FAL_KEY":                 "fal-secret",
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

func TestMontageConfigRejectsUnknownProviderEnv(t *testing.T) {
	cfg := MontageConfig{
		Enabled:                true,
		SubmodulePath:          "third_party/OpenMontage",
		DefaultPipeline:        "default",
		AllowedPipelines:       []string{"default"},
		MaxDurationSeconds:     600,
		MaxAssets:              20,
		TimeoutMinutes:         90,
		ExecutionTargets:       []string{"cloud"},
		DefaultExecutionTarget: "cloud",
		ProviderEnv: map[string]string{
			"NOT_MONTAGE_KEY": "secret",
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "montage.provider_env contains unsupported key") {
		t.Fatalf("Validate error = %v, want unsupported provider env rejection", err)
	}
}

func TestMontageProviderEnvRedactionDoesNotExposeSecrets(t *testing.T) {
	cfg := MontageConfig{ProviderEnv: map[string]string{
		"FAL_KEY":        "fal-secret",
		"RUNWAY_API_KEY": "",
	}}

	redacted := cfg.RedactedProviderEnv()
	if redacted["FAL_KEY"] != true {
		t.Fatalf("FAL_KEY configured = %v, want true", redacted["FAL_KEY"])
	}
	if redacted["RUNWAY_API_KEY"] != false {
		t.Fatalf("RUNWAY_API_KEY configured = %v, want false", redacted["RUNWAY_API_KEY"])
	}
	data, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("marshal redacted env: %v", err)
	}
	if strings.Contains(string(data), "fal-secret") {
		t.Fatalf("redacted provider env leaked secret: %s", data)
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
	if cfg.Montage.SubmodulePath != "third_party/OpenMontage" {
		t.Fatalf("SubmodulePath = %q, want default path", cfg.Montage.SubmodulePath)
	}
}

func TestMontageConfigFilesDoNotDeclareRunnerImage(t *testing.T) {
	for _, path := range []string{"../config.yaml", "../config.example.yaml"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		raw := string(body)
		if strings.Contains(raw, "ANBAN_MONTAGE_RUNNER_IMAGE") || strings.Contains(raw, "cloud_image:") {
			t.Fatalf("%s must not declare montage.runner.cloud_image; use claude.docker.image or claude.kubernetes.agent_image", path)
		}
	}
}
