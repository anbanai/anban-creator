package config

import (
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
				Namespace:          "anbanai-prod",
				AgentImage:         "registry.example.com/anban-agent:latest",
				ServiceAccount:     "creator-agent-runner",
				MemoryStorageClass: "alicloud-nas",
				MemorySize:         "1Gi",
			},
			AgentServerURL: "http://creator-api-svc.anbanai-prod.svc.cluster.local:8080",
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
	if cfg.Claude.Kubernetes.MemoryStorageClass != "alicloud-nas" {
		t.Fatal("memory storage class")
	}
	if cfg.Claude.Kubernetes.ActiveDeadlineSeconds != 3600 {
		t.Fatal("active deadline")
	}
	if cfg.Claude.Kubernetes.TTLSecondsAfterFinished != 600 {
		t.Fatal("job ttl")
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
			name: "memory storage class",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.MemoryStorageClass = ""
			},
			wantErr: "claude.kubernetes.memory_storage_class is required",
		},
		{
			name: "memory size",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.MemorySize = "not-a-quantity"
			},
			wantErr: "claude.kubernetes.memory_size must be a valid Kubernetes quantity",
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

func TestAgentServerURLUsesConfiguredKubernetesServiceURL(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.AgentServerURL = "http://creator-api-svc.anbanai-prod.svc.cluster.local:8080/"
	if got := cfg.AgentServerURL(); got != "http://creator-api-svc.anbanai-prod.svc.cluster.local:8080" {
		t.Fatalf("AgentServerURL() = %q", got)
	}
}
