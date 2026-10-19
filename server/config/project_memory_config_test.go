package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProjectMemoryConfigDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()
	if cfg.Dsh.TimeoutSec != 24*60*60 {
		t.Fatalf("DSH timeout default = %d, want 86400 seconds", cfg.Dsh.TimeoutSec)
	}
	if cfg.Dsh.IdleTimeoutSec != 30*60 {
		t.Fatalf("DSH idle timeout default = %d, want 1800 seconds", cfg.Dsh.IdleTimeoutSec)
	}

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

func TestDshKubernetesServiceAccountIsNotConfigurable(t *testing.T) {
	var dsh DshConfig
	err := yaml.Unmarshal([]byte("kubernetes_service_account: creator-agent-runner\n"), &dsh)
	if err == nil || !strings.Contains(err.Error(), "dsh.kubernetes_service_account") {
		t.Fatalf("NewConfig() error = %v, want duplicate DSH ServiceAccount setting rejected", err)
	}
}

func TestDshResourcesUseKubernetesQuantitySyntax(t *testing.T) {
	var dsh DshConfig
	if err := yaml.Unmarshal([]byte(`
executor: kubernetes
resources:
  requests:
    cpu: 500m
    memory: 512Mi
  limits:
    cpu: 500m
    memory: 512Mi
`), &dsh); err != nil {
		t.Fatalf("unmarshal DSH resources: %v", err)
	}
	if dsh.Resources.Requests["cpu"] != "500m" || dsh.Resources.Requests["memory"] != "512Mi" {
		t.Fatalf("DSH resource requests = %#v", dsh.Resources.Requests)
	}
	if dsh.Resources.Limits["cpu"] != "500m" || dsh.Resources.Limits["memory"] != "512Mi" {
		t.Fatalf("DSH resource limits = %#v", dsh.Resources.Limits)
	}
}

func TestDshLegacyFractionalCPURequiresKubernetesQuantity(t *testing.T) {
	var dsh DshConfig
	err := yaml.Unmarshal([]byte("cpu_cores: 0.5\n"), &dsh)
	if err == nil || !strings.Contains(err.Error(), "dsh.cpu_cores") || !strings.Contains(err.Error(), "dsh.resources") {
		t.Fatalf("fractional legacy CPU error = %v, want migration guidance", err)
	}
}

func TestDshResourcesRejectUnknownNestedFields(t *testing.T) {
	var dsh DshConfig
	err := yaml.Unmarshal([]byte(`
resources:
  request:
    cpu: 500m
  limits:
    cpu: 500m
    memory: 512Mi
`), &dsh)
	if err == nil || !strings.Contains(err.Error(), "kubernetes resources.request") {
		t.Fatalf("unknown DSH resource field error = %v, want nested field rejection", err)
	}
}

func TestDshKubernetesResourcesRequireRequestsAndLimits(t *testing.T) {
	cfg := DshConfig{
		Executor:            "kubernetes",
		Image:               "registry.example/dsh@sha256:" + strings.Repeat("a", 64),
		KubernetesNamespace: "anban", KubernetesProjectMemoryClaim: "creator-project-memory",
		KubernetesProjectMemoryMount: "/workspace/project", TimeoutSec: 3600,
		ServerURL: "http://server:8080", ServerInstanceID: "server-1",
		Resources: KubernetesResourceConfig{Requests: map[string]string{"cpu": "500m", "memory": "512Mi"}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "dsh.resources.limits.cpu") || !strings.Contains(err.Error(), "dsh.resources.limits.memory") {
		t.Fatalf("incomplete DSH resource error = %v, want limits requirements", err)
	}
}

func TestDshDiagnosticRetentionDefaultsAndRejectsNegative(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()
	if cfg.Dsh.RuntimeDiagnosticRetentionSeconds != 600 {
		t.Fatalf("DSH diagnostic retention = %d, want 600", cfg.Dsh.RuntimeDiagnosticRetentionSeconds)
	}
	cfg.Dsh.RuntimeDiagnosticRetentionSeconds = -1
	if err := cfg.Dsh.Validate(); err == nil || !strings.Contains(err.Error(), "dsh.runtime_diagnostic_retention_seconds") {
		t.Fatalf("Dsh.Validate() error = %v, want negative retention rejected", err)
	}
}

func TestDshTimeoutMustExceedIdleTimeout(t *testing.T) {
	base := DshConfig{
		Enabled:        true,
		Executor:       "process",
		TimeoutSec:     1800,
		IdleTimeoutSec: 1800,
		Route:          "default",
		Routes: map[string]DshRouteConfig{
			"default": {
				Provider: "deepseek-official",
				API:      "deepseek-messages",
				BaseURL:  "https://api.deepseek.com/anthropic",
				APIKey:   "test-key",
				Model:    "deepseek-chat",
			},
		},
	}
	for _, test := range []struct {
		name       string
		timeoutSec int
	}{
		{name: "equal", timeoutSec: 1800},
		{name: "shorter", timeoutSec: 1799},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			cfg.TimeoutSec = test.timeoutSec
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "dsh.timeout_sec must be greater than dsh.idle_timeout_sec") {
				t.Fatalf("Dsh.Validate() error = %v, want timeout/idle ordering error", err)
			}
		})
	}

	base.TimeoutSec = 1801
	if err := base.Validate(); err != nil {
		t.Fatalf("Dsh.Validate() with longer runtime timeout = %v, want nil", err)
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
		{name: "project quota too large", mutate: func(cfg *Config) { cfg.Claude.ProjectMemory.MaxProjectBytes = 16*1024*1024 + 1 }, wantErr: "max_project_bytes"},
		{name: "preview exceeds quota", mutate: func(cfg *Config) { cfg.Claude.ProjectMemory.MaxPreviewBytes = 32 * 1024 * 1024 }, wantErr: "max_preview_bytes"},
		{name: "too many preview files", mutate: func(cfg *Config) { cfg.Claude.ProjectMemory.MaxFiles = 65 }, wantErr: "max_files"},
		{name: "preview too deep", mutate: func(cfg *Config) { cfg.Claude.ProjectMemory.MaxDepth = 5 }, wantErr: "max_depth"},
		{name: "preview file too large", mutate: func(cfg *Config) { cfg.Claude.ProjectMemory.MaxFileBytes = 64*1024 + 1 }, wantErr: "max_file_bytes"},
		{name: "preview response too large", mutate: func(cfg *Config) { cfg.Claude.ProjectMemory.MaxPreviewBytes = 256*1024 + 1 }, wantErr: "max_preview_bytes"},
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
