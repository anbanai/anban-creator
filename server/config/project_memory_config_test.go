package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectMemoryConfigDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()

	got := cfg.Claude.ProjectMemory
	if got.RootDir != "/app/data/project-memory" || got.MaxProjectBytes != 16*1024*1024 {
		t.Fatalf("project memory storage defaults = %#v", got)
	}
	if got.MaxFiles != 64 || got.MaxDepth != 4 || got.MaxFileBytes != 64*1024 || got.MaxPreviewBytes != 256*1024 {
		t.Fatalf("project memory preview defaults = %#v", got)
	}
	if cfg.Claude.Docker.ProjectMemoryVolume != "creator-project-memory" {
		t.Fatalf("Docker project memory volume = %q", cfg.Claude.Docker.ProjectMemoryVolume)
	}
	if cfg.Claude.Kubernetes.ProjectMemoryClaim != "creator-project-memory" {
		t.Fatalf("Kubernetes project memory claim = %q", cfg.Claude.Kubernetes.ProjectMemoryClaim)
	}
}

func TestProjectMemoryConfigRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := []byte("claude:\n  project_memory:\n    upload_snapshots: true\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewConfig(path)
	if err == nil || !strings.Contains(err.Error(), "claude.project_memory.upload_snapshots") {
		t.Fatalf("NewConfig() error = %v, want unknown snapshot setting rejected", err)
	}
}

func TestLegacyMemoryArchiveConfigIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("memory:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewConfig(path)
	if err == nil || !strings.Contains(err.Error(), "unknown top-level config key memory") {
		t.Fatalf("NewConfig() error = %v, want legacy memory config rejected", err)
	}
}

func TestProjectMemoryConfigRejectsUnsafeOrIncompleteValues(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "relative root", mutate: func(cfg *Config) { cfg.Claude.ProjectMemory.RootDir = "data/memory" }, wantErr: "claude.project_memory.root_dir"},
		{name: "zero quota", mutate: func(cfg *Config) { cfg.Claude.ProjectMemory.MaxProjectBytes = -1 }, wantErr: "max_project_bytes"},
		{name: "preview exceeds quota", mutate: func(cfg *Config) { cfg.Claude.ProjectMemory.MaxPreviewBytes = 32 * 1024 * 1024 }, wantErr: "max_preview_bytes"},
		{name: "missing Docker volume", mutate: func(cfg *Config) { cfg.Claude.Docker.ProjectMemoryVolume = "" }, wantErr: "project_memory_volume"},
		{name: "missing Kubernetes claim", mutate: func(cfg *Config) { cfg.Claude.Executor = "kubernetes"; cfg.Claude.Kubernetes.ProjectMemoryClaim = "" }, wantErr: "project_memory_claim"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &Config{Database: DatabaseConfig{DSN: "dsn"}, JWT: JWTConfig{SecretKey: "secret"}, Claude: validClaudeConfigForTest()}
			cfg.applyDefaults()
			test.mutate(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}
