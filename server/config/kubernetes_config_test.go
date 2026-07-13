package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func baseKubernetesConfigForTest() Config {
	cfg := Config{
		Database: DatabaseConfig{DSN: "dsn"},
		JWT:      JWTConfig{SecretKey: "secret", AccessExpiry: "24h", RefreshExpiry: "168h"},
		Asynq:    AsynqConfig{ContentGenerateTimeout: time.Hour},
		Claude: ClaudeConfig{
			Executor: "kubernetes",
			Kubernetes: KubernetesConfig{
				Namespace:            "anbanai-prod",
				AgentImage:           "registry.example.com/anban-agent:latest",
				ServiceAccount:       "creator-agent-runner",
				MemoryStorageClass:   "alicloud-nas",
				MemorySize:           "1Gi",
				ExecutionTokenSecret: "0123456789abcdef0123456789abcdef",
			},
			AgentServerURL: "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443",
		},
		Storage: StorageConfig{
			Provider:        "oss",
			Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
			AccessKeyID:     "ak",
			AccessKeySecret: "sk",
			BucketName:      "bucket",
			STSRoleArn:      "acs:ram::123:role/upload",
		},
	}
	cfg.applyDefaults()
	return cfg
}

func TestValidateAcceptsKubernetesExecutor(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want kubernetes executor accepted", err)
	}
}

func TestKubernetesAgentImageMustBeExplicit(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.Claude.Kubernetes.AgentImage != "" {
		t.Fatalf("kubernetes agent image default = %q, want explicit production image", cfg.Claude.Kubernetes.AgentImage)
	}
	if cfg.Claude.Docker.Image != "anban-creator-agent:latest" {
		t.Fatalf("docker image default = %q, want Docker executor default unchanged", cfg.Claude.Docker.Image)
	}
}

func TestKubernetesJobRuntimeDefaults(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.Claude.Kubernetes.Namespace != "default" {
		t.Fatalf("namespace = %q, want default", cfg.Claude.Kubernetes.Namespace)
	}
	if cfg.Claude.Kubernetes.ServerCASecret != "anban-server-tls" {
		t.Fatalf("server CA secret = %q, want anban-server-tls", cfg.Claude.Kubernetes.ServerCASecret)
	}
	if cfg.Claude.Kubernetes.MemorySize != "1Gi" {
		t.Fatalf("memory size = %q, want 1Gi", cfg.Claude.Kubernetes.MemorySize)
	}
	if cfg.Claude.Kubernetes.ActiveDeadlineSeconds != 3600 {
		t.Fatal("active deadline")
	}
	if cfg.Claude.Kubernetes.CompletionGraceSeconds != 30 {
		t.Fatal("completion grace")
	}
	if cfg.Claude.Kubernetes.TTLSecondsAfterFinished != 600 {
		t.Fatal("job ttl")
	}
	if cfg.Claude.Kubernetes.PreStartRetryLimit != 1 {
		t.Fatal("pre-start retry limit")
	}
}

func TestNewConfigPreservesExplicitZeroKubernetesJobControls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := []byte(`
database:
  dsn: "dsn"
jwt:
  secret_key: "secret"
storage:
  provider: "oss"
  endpoint: "oss-cn-hangzhou.aliyuncs.com"
  access_key_id: "ak"
  access_key_secret: "sk"
  bucket_name: "bucket"
  sts_role_arn: "acs:ram::123:role/upload"
claude:
  executor: "kubernetes"
  agent_server_url: "https://creator-api-svc:8443"
  kubernetes:
    agent_image: "registry.example.com/anban-agent:latest"
    service_account: "creator-agent-runner"
    execution_token_secret: "0123456789abcdef0123456789abcdef"
    memory_storage_class: "alicloud-nas"
    memory_size: "1Gi"
    completion_grace_seconds: 0
    pre_start_retry_limit: 0
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := NewConfig(path)
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}
	if cfg.Claude.Kubernetes.CompletionGraceSeconds != 0 || cfg.Claude.Kubernetes.PreStartRetryLimit != 0 {
		t.Fatalf(
			"job controls = %d/%d, want explicit zero values preserved",
			cfg.Claude.Kubernetes.CompletionGraceSeconds,
			cfg.Claude.Kubernetes.PreStartRetryLimit,
		)
	}
}

func TestValidateKubernetesRequiresExplicitMemoryStorageClass(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.Kubernetes.MemoryStorageClass = ""
	cfg.applyDefaults()

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.kubernetes.memory_storage_class is required") {
		t.Fatalf("Validate() error = %v, want explicit memory storage class requirement", err)
	}
}

func TestKubernetesConfigHasNoReusablePodFields(t *testing.T) {
	typ := reflect.TypeOf(KubernetesConfig{})
	for _, name := range []string{"WorkspaceMountPath", "WorkspacePVCName", "PodRevision", "PodTTLSeconds", "ExecTimeoutSec"} {
		if _, ok := typ.FieldByName(name); ok {
			t.Fatalf("obsolete field %s remains", name)
		}
	}
}

func TestValidateKubernetesRequiresOSS(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Storage.Provider = "local"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.kubernetes requires storage.provider to be \"oss\"") {
		t.Fatalf("Validate() error = %v, want OSS requirement", err)
	}
}

func TestValidateKubernetesRequiresAgentServerURL(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.AgentServerURL = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.agent_server_url is required when claude.executor is \"kubernetes\"") {
		t.Fatalf("Validate() error = %v, want agent_server_url requirement", err)
	}
}

func TestValidateKubernetesRequiresHTTPSAgentServerURL(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.AgentServerURL = "http://creator-api-svc:8080"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.agent_server_url must use https://") {
		t.Fatalf("Validate() error = %v, want Kubernetes HTTPS requirement", err)
	}
}

func TestValidateKubernetesJobRuntimeRequirements(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "STS role",
			mutate: func(cfg *Config) {
				cfg.Storage.STSRoleArn = ""
			},
			wantErr: "storage.sts_role_arn is required",
		},
		{
			name: "service account",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.ServiceAccount = ""
			},
			wantErr: "claude.kubernetes.service_account is required",
		},
		{
			name: "execution token secret",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.ExecutionTokenSecret = "too-short"
			},
			wantErr: "claude.kubernetes.execution_token_secret must be at least 32 bytes",
		},
		{
			name: "memory storage class",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.MemoryStorageClass = ""
			},
			wantErr: "claude.kubernetes.memory_storage_class is required",
		},
		{
			name: "invalid memory size",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.MemorySize = "not-a-quantity"
			},
			wantErr: "claude.kubernetes.memory_size must be a valid Kubernetes quantity",
		},
		{
			name: "zero memory size",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.MemorySize = "0"
			},
			wantErr: "claude.kubernetes.memory_size must be positive",
		},
		{
			name: "negative memory size",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.MemorySize = "-1Gi"
			},
			wantErr: "claude.kubernetes.memory_size must be positive",
		},
		{
			name: "active deadline",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.ActiveDeadlineSeconds = -1
			},
			wantErr: "claude.kubernetes.active_deadline_seconds must be positive",
		},
		{
			name: "job TTL",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.TTLSecondsAfterFinished = -1
			},
			wantErr: "claude.kubernetes.ttl_seconds_after_finished must be positive",
		},
		{
			name: "negative completion grace",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.CompletionGraceSeconds = -1
			},
			wantErr: "claude.kubernetes.completion_grace_seconds must not be negative",
		},
		{
			name: "negative pre-start retry limit",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.PreStartRetryLimit = -1
			},
			wantErr: "claude.kubernetes.pre_start_retry_limit must not be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseKubernetesConfigForTest()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateKubernetesAllowsZeroJobControls(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.Kubernetes.CompletionGraceSeconds = 0
	cfg.Claude.Kubernetes.PreStartRetryLimit = 0

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want zero completion grace and retries accepted", err)
	}
}

func TestAgentServerURLUsesConfiguredKubernetesServiceURL(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.AgentServerURL = "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443/"
	if got := cfg.AgentServerURL(); got != "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443" {
		t.Fatalf("AgentServerURL() = %q", got)
	}
}
