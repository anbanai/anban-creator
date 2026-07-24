package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func baseKubernetesConfigForTest() Config {
	claude := validClaudeConfigForTest()
	claude.Executor = "kubernetes"
	claude.AgentServerURL = "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443"
	claude.ExecutionTokenSecret = "0123456789abcdef0123456789abcdef"
	claude.RuntimeImages = RuntimeImages{
		model.PlatformArticle:  "registry.example.com/creator-agent-article@sha256:" + strings.Repeat("a", 64),
		model.PlatformSeednote: "registry.example.com/creator-agent-seednote@sha256:" + strings.Repeat("b", 64),
		model.PlatformMontage:  "registry.example.com/creator-agent-montage@sha256:" + strings.Repeat("c", 64),
	}
	claude.Kubernetes = KubernetesConfig{
		Namespace:         "anbanai-prod",
		ServiceAccount:    "creator-agent-runner",
		NASStorageClass:   "nas-sc-creator",
		ProjectMemorySize: "1Gi",
		TaskWorkspaceSize: "10Gi",
	}
	cfg := Config{
		Server:   ServerConfig{TLSCertFile: "/tls/tls.crt", TLSKeyFile: "/tls/tls.key"},
		Database: DatabaseConfig{DSN: "dsn"},
		JWT:      JWTConfig{SecretKey: "secret", AccessExpiry: "24h", RefreshExpiry: "168h"},
		Asynq:    AsynqConfig{ContentGenerateTimeout: time.Hour},
		Claude:   claude,
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

func TestRuntimeImageForTaskUsesCanonicalProfileMap(t *testing.T) {
	images := RuntimeImages{
		model.PlatformArticle:  "creator-agent-article:latest",
		model.PlatformSeednote: "creator-agent-seednote:latest",
		model.PlatformMontage:  "creator-agent-montage:latest",
	}
	for _, test := range []struct {
		taskType string
		profile  string
		image    string
	}{
		{taskType: model.PlatformArticle, profile: "article", image: "creator-agent-article:latest"},
		{taskType: model.PlatformMoments, profile: "article", image: "creator-agent-article:latest"},
		{taskType: model.PlatformEcommerce, profile: "article", image: "creator-agent-article:latest"},
		{taskType: model.PlatformSeednote, profile: "seednote", image: "creator-agent-seednote:latest"},
		{taskType: model.PlatformMontage, profile: "montage", image: "creator-agent-montage:latest"},
		{taskType: model.TaskTypeLiveSlicer, profile: "montage", image: "creator-agent-montage:latest"},
	} {
		if got := images.ForTask(test.taskType); got.Profile != test.profile || got.Image != test.image {
			t.Fatalf("task %s runtime = %#v, want %s/%s", test.taskType, got, test.profile, test.image)
		}
	}
}

func TestValidateRuntimeImagesRequiresExactCanonicalProfiles(t *testing.T) {
	for _, test := range []struct {
		name     string
		profiles map[string]string
		want     string
	}{
		{
			name:     "missing article image",
			profiles: map[string]string{model.PlatformMontage: "registry/montage:v1"},
			want:     "claude.runtime_images.article is required",
		},
		{
			name:     "missing montage image",
			profiles: map[string]string{model.PlatformArticle: "registry/article:v1", model.PlatformSeednote: "registry/seednote:v1"},
			want:     "claude.runtime_images.montage is required",
		},
		{
			name:     "empty mapped image",
			profiles: map[string]string{model.PlatformArticle: "registry/article:v1", model.PlatformSeednote: "registry/seednote:v1", model.PlatformMontage: "  "},
			want:     "claude.runtime_images.montage must not be empty",
		},
		{
			name:     "unsupported task key",
			profiles: map[string]string{model.PlatformArticle: "registry/article:v1", model.PlatformSeednote: "registry/seednote:v1", model.PlatformMontage: "registry/montage:v1", "unknown": "registry/unknown:v1"},
			want:     `claude.runtime_images contains unsupported profile "unknown"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			claude := validClaudeConfigForTest()
			claude.RuntimeImages = RuntimeImages(test.profiles)
			if err := claude.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ClaudeConfig.Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestKubernetesJobRuntimeDefaults(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.Claude.Kubernetes.Namespace != "default" {
		t.Fatalf("namespace = %q, want default", cfg.Claude.Kubernetes.Namespace)
	}
	if cfg.Claude.Kubernetes.ServerCASecret != "anban-internal-ca" {
		t.Fatalf("server CA secret = %q, want anban-internal-ca", cfg.Claude.Kubernetes.ServerCASecret)
	}
	if cfg.Claude.Kubernetes.ProjectMemorySize != "1Gi" {
		t.Fatalf("project memory size = %q, want 1Gi", cfg.Claude.Kubernetes.ProjectMemorySize)
	}
	if cfg.Claude.Kubernetes.TaskWorkspaceSize != "10Gi" {
		t.Fatalf("task workspace size = %q, want 10Gi", cfg.Claude.Kubernetes.TaskWorkspaceSize)
	}
	if cfg.Claude.Kubernetes.ActiveDeadlineSeconds != 3600 {
		t.Fatal("active deadline")
	}
	if cfg.Claude.Kubernetes.HeartbeatTimeoutSeconds != 180 {
		t.Fatal("heartbeat timeout")
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

func TestKubernetesResourcesForTaskOverlaysDefaults(t *testing.T) {
	cfg := KubernetesConfig{
		Resources: KubernetesResourceConfig{
			Requests: map[string]string{"cpu": "1", "memory": "2Gi"},
			Limits:   map[string]string{"cpu": "2", "memory": "4Gi"},
		},
		ResourceProfiles: map[string]KubernetesResourceConfig{
			"video": {
				Requests: map[string]string{"cpu": "2"},
				Limits:   map[string]string{"cpu": "4", "memory": "7Gi"},
			},
		},
	}
	got := cfg.ResourcesForTask("video")
	if got.Requests["cpu"] != "2" || got.Requests["memory"] != "2Gi" || got.Limits["cpu"] != "4" || got.Limits["memory"] != "7Gi" {
		t.Fatalf("profile resources = %#v", got)
	}
	got.Requests["cpu"] = "mutated"
	if cfg.Resources.Requests["cpu"] != "1" {
		t.Fatal("resource selection mutated config defaults")
	}
}

func TestValidateKubernetesResourceProfiles(t *testing.T) {
	tests := []struct {
		name    string
		profile KubernetesResourceConfig
		want    string
	}{
		{name: "invalid quantity", profile: KubernetesResourceConfig{Requests: map[string]string{"cpu": "invalid"}}, want: "must be a positive Kubernetes quantity"},
		{name: "request exceeds limit", profile: KubernetesResourceConfig{Requests: map[string]string{"memory": "5Gi"}, Limits: map[string]string{"memory": "4Gi"}}, want: "must not exceed its limit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := baseKubernetesConfigForTest()
			cfg.Claude.Kubernetes.Resources = KubernetesResourceConfig{Requests: map[string]string{"cpu": "1", "memory": "2Gi"}, Limits: map[string]string{"cpu": "2", "memory": "4Gi"}}
			cfg.Claude.Kubernetes.ResourceProfiles = map[string]KubernetesResourceConfig{"seednote": test.profile}
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateKubernetesHeartbeatTimeout(t *testing.T) {
	for _, timeout := range []int64{59, 3600} {
		cfg := baseKubernetesConfigForTest()
		cfg.Claude.Kubernetes.HeartbeatTimeoutSeconds = timeout
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "heartbeat_timeout_seconds") {
			t.Fatalf("timeout=%d Validate() error = %v", timeout, err)
		}
	}
}

func TestNewConfigPreservesExplicitZeroKubernetesJobControls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := []byte(`
database:
  dsn: "dsn"
server:
  tls_cert_file: "/tls/tls.crt"
  tls_key_file: "/tls/tls.key"
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
  provider: volcengine_ark
  base_url: https://ark.cn-beijing.volces.com/api/compatible
  auth_token: test-auth-token
  models:
    default: doubao-seed-evolving
    opus: doubao-seed-evolving
    fable: doubao-seed-evolving
    sonnet: doubao-seed-2-1-pro-260628
    haiku: doubao-seed-2-1-turbo-260628
  model_usage_aliases:
    doubao-seed-evolving-latest-version: doubao-seed-evolving
  executor: "kubernetes"
  runtime_images:
    article: "registry.example.com/creator-agent-article@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    seednote: "registry.example.com/creator-agent-seednote@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    montage: "registry.example.com/creator-agent-montage@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
  execution_token_secret: "0123456789abcdef0123456789abcdef"
  agent_server_url: "https://creator-api-svc:8443"
  kubernetes:
    service_account: "creator-agent-runner"
    nas_storage_class: "nas-sc-creator"
    project_memory_size: "1Gi"
    task_workspace_size: "10Gi"
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

func TestValidateKubernetesRequiresExplicitNASStorageClass(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.Kubernetes.NASStorageClass = ""
	cfg.applyDefaults()

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.kubernetes.nas_storage_class is required") {
		t.Fatalf("Validate() error = %v, want explicit NAS storage class requirement", err)
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
				cfg.Claude.ExecutionTokenSecret = "too-short"
			},
			wantErr: "claude.execution_token_secret must be at least 32 bytes",
		},
		{
			name: "NAS storage class",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.NASStorageClass = ""
			},
			wantErr: "claude.kubernetes.nas_storage_class is required",
		},
		{
			name: "invalid project memory size",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.ProjectMemorySize = "not-a-quantity"
			},
			wantErr: "claude.kubernetes.project_memory_size must be a valid Kubernetes quantity",
		},
		{
			name: "zero project memory size",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.ProjectMemorySize = "0"
			},
			wantErr: "claude.kubernetes.project_memory_size must be positive",
		},
		{
			name: "negative project memory size",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.ProjectMemorySize = "-1Gi"
			},
			wantErr: "claude.kubernetes.project_memory_size must be positive",
		},
		{
			name: "invalid task workspace size",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.TaskWorkspaceSize = "not-a-quantity"
			},
			wantErr: "claude.kubernetes.task_workspace_size must be a valid Kubernetes quantity",
		},
		{
			name: "zero task workspace size",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.TaskWorkspaceSize = "0"
			},
			wantErr: "claude.kubernetes.task_workspace_size must be positive",
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
