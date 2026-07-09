package config

import "testing"

func TestOpenMontageConfigDefaults(t *testing.T) {
	cfg := OpenMontageConfig{}
	cfg.ApplyDefaults()

	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if cfg.SubmodulePath != "third_party/OpenMontage" {
		t.Fatalf("SubmodulePath = %q, want %q", cfg.SubmodulePath, "third_party/OpenMontage")
	}
	if cfg.DefaultPipeline != "default" {
		t.Fatalf("DefaultPipeline = %q, want default", cfg.DefaultPipeline)
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
	if cfg.Runner.CloudImage != "anban/openmontage-runner:latest" {
		t.Fatalf("CloudImage = %q, want default runner image", cfg.Runner.CloudImage)
	}
}

func TestOpenMontageConfigValidate(t *testing.T) {
	cfg := OpenMontageConfig{
		Enabled:                true,
		SubmodulePath:          "third_party/OpenMontage",
		DefaultPipeline:        "default",
		AllowedPipelines:       []string{"default", "social-short"},
		MaxDurationSeconds:     600,
		MaxAssets:              20,
		TimeoutMinutes:         90,
		ExecutionTargets:       []string{"cloud", "local"},
		DefaultExecutionTarget: "cloud",
		Runner: OpenMontageRunnerConfig{
			CloudImage: "anban/openmontage-runner:latest",
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
