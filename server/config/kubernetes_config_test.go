package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
)

func baseKubernetesConfigForTest() Config {
	claude := validClaudeConfigForTest()
	claude.Executor = "kubernetes"
	claude.AgentServerURL = "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443"
	claude.ExecutionTokenSecret = "0123456789abcdef0123456789abcdef"
	claude.RuntimeImages = RuntimeImages{
		"wechat-article":                  "registry.example.com/creator-agent-wechat-article@sha256:" + strings.Repeat("a", 64),
		"wechat-picture":                  "registry.example.com/creator-agent-wechat-picture@sha256:" + strings.Repeat("e", 64),
		model.PlatformSeednote:            "registry.example.com/creator-agent-seednote@sha256:" + strings.Repeat("b", 64),
		model.PlatformMontage:             "registry.example.com/creator-agent-montage@sha256:" + strings.Repeat("c", 64),
		"viral-ip-video-editor":           "registry.example.com/creator-agent-viral-ip-video-editor@sha256:" + strings.Repeat("a", 64),
		model.PlatformVideoEdit:           "registry.example.com/creator-agent-video-edit@sha256:" + strings.Repeat("g", 64),
		"moments":                         "registry.example.com/creator-agent-moments@sha256:" + strings.Repeat("d", 64),
		"ecommerce":                       "registry.example.com/creator-agent-ecommerce@sha256:" + strings.Repeat("f", 64),
		"profile-analysis":                "registry.example.com/creator-agent-profile-analysis@sha256:" + strings.Repeat("a", 64),
		"feedback-analysis":               "registry.example.com/creator-agent-feedback-analysis@sha256:" + strings.Repeat("b", 64),
		model.PlatformWhiteboardAnimation: "registry.example.com/creator-agent-whiteboard-animation@sha256:" + strings.Repeat("c", 64),
		model.PlatformHypit:               "registry.example.com/creator-agent-hypit@sha256:" + strings.Repeat("e", 64),
	}
	claude.Kubernetes = KubernetesConfig{
		Namespace:          "anbanai-prod",
		ServiceAccount:     "creator-agent-runner",
		NASStorageClass:    "nas-sc-creator",
		ProjectMemoryClaim: "creator-project-memory",
		TaskWorkspaceSize:  "10Gi",
		ResourceProfiles:   completeKubernetesResourceProfilesForTest(),
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
		"wechat-article":                  "creator-agent-wechat-article:latest",
		"wechat-picture":                  "creator-agent-wechat-picture:latest",
		"moments":                         "creator-agent-moments:latest",
		"ecommerce":                       "creator-agent-ecommerce:latest",
		model.PlatformSeednote:            "creator-agent-seednote:latest",
		model.PlatformMontage:             "creator-agent-montage:latest",
		"viral-ip-video-editor":           "creator-agent-viral-ip-video-editor:latest",
		model.PlatformVideoEdit:           "creator-agent-video-edit:latest",
		model.PlatformWhiteboardAnimation: "creator-agent-whiteboard-animation:latest",
		"profile-analysis":                "creator-agent-profile-analysis:latest",
		"feedback-analysis":               "creator-agent-feedback-analysis:latest",
		model.PlatformHypit:               "creator-agent-hypit:latest",
	}
	for _, test := range []struct {
		taskType string
		profile  string
		image    string
	}{
		{taskType: model.TaskTypeWechatArticle, profile: "wechat-article", image: "creator-agent-wechat-article:latest"},
		{taskType: model.PlatformMoments, profile: "moments", image: "creator-agent-moments:latest"},
		{taskType: model.PlatformEcommerce, profile: "ecommerce", image: "creator-agent-ecommerce:latest"},
		{taskType: model.PlatformSeednote, profile: "seednote", image: "creator-agent-seednote:latest"},
		{taskType: model.PlatformMontage, profile: "montage", image: "creator-agent-montage:latest"},
		{taskType: model.TaskTypeViralAnalysis, profile: "seednote", image: "creator-agent-seednote:latest"},
		{taskType: model.PlatformVideoEdit, profile: model.PlatformVideoEdit, image: "creator-agent-video-edit:latest"},
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
			want:     "claude.runtime_images.wechat-article is required",
		},
		{
			name:     "missing montage image",
			profiles: map[string]string{"wechat-article": "registry/wechat-article:v1", model.PlatformSeednote: "registry/seednote:v1"},
			want:     "claude.runtime_images.montage is required",
		},
		{
			name:     "empty mapped image",
			profiles: map[string]string{"wechat-article": "registry/wechat-article:v1", model.PlatformSeednote: "registry/seednote:v1", model.PlatformMontage: "  "},
			want:     "claude.runtime_images.montage must not be empty",
		},
		{
			name:     "unsupported task key",
			profiles: map[string]string{"wechat-article": "registry/wechat-article:v1", model.PlatformSeednote: "registry/seednote:v1", model.PlatformMontage: "registry/montage:v1", "unknown": "registry/unknown:v1"},
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

func TestValidateKubernetesAcceptsMutableRuntimeImageReferences(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.RuntimeImages["wechat-picture"] = "registry.example.com/creator-agent-wechat-picture:latest"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want mutable runtime image accepted", err)
	}
}

func TestValidateRequiresHypitRuntimeImageWhenEnabled(t *testing.T) {
	claude := validClaudeConfigForTest()
	delete(claude.RuntimeImages, model.PlatformHypit)
	cfg := Config{
		Database: DatabaseConfig{DSN: "dsn"},
		JWT:      JWTConfig{SecretKey: "secret"},
		Claude:   claude,
		Hypit:    HypitConfig{Enabled: true},
	}
	cfg.applyDefaults()

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.runtime_images.hypit is required when hypit is enabled") {
		t.Fatalf("Config.Validate() error = %v, want actionable Hypit runtime image error", err)
	}
	if !strings.Contains(err.Error(), "hypit.enabled=false") || !strings.Contains(err.Error(), "ANBAN_AGENT_IMAGE_HYPIT") {
		t.Fatalf("Config.Validate() error = %v, want disable and image configuration guidance", err)
	}
}

func TestKubernetesJobRuntimeDefaults(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.Claude.Kubernetes.Namespace != "default" {
		t.Fatalf("namespace = %q, want default", cfg.Claude.Kubernetes.Namespace)
	}
	if cfg.Claude.Kubernetes.ImagePullSecret != "acr-secret" {
		t.Fatalf("image pull secret = %q, want acr-secret", cfg.Claude.Kubernetes.ImagePullSecret)
	}
	if cfg.Dsh.KubernetesImagePullSecret != "acr-secret" {
		t.Fatalf("DSH image pull secret = %q, want acr-secret", cfg.Dsh.KubernetesImagePullSecret)
	}
	if cfg.Claude.Kubernetes.ServerCASecret != "anban-internal-ca" {
		t.Fatalf("server CA secret = %q, want anban-internal-ca", cfg.Claude.Kubernetes.ServerCASecret)
	}
	if cfg.Claude.Kubernetes.ProjectMemoryClaim != "creator-project-memory" {
		t.Fatalf("project memory claim = %q, want creator-project-memory", cfg.Claude.Kubernetes.ProjectMemoryClaim)
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
}

func completeKubernetesResourceProfilesForTest() map[string]KubernetesResourceConfig {
	profiles := make(map[string]KubernetesResourceConfig)
	for _, taskType := range executableKubernetesTaskTypes() {
		profiles[taskType] = KubernetesResourceConfig{
			Requests: map[string]string{"cpu": "500m", "memory": "512Mi"},
			Limits:   map[string]string{"cpu": "1", "memory": "1Gi"},
		}
	}
	return profiles
}

func executableKubernetesTaskTypes() []string {
	types := make([]string, 0)
	for _, pack := range agentpack.Default().Packs {
		if pack.Kind != agentpack.KindManaged || pack.Channel == "" || (pack.Interaction != nil && pack.Interaction.UserQuestions) {
			continue
		}
		for _, taskKind := range pack.Bindings.TaskKinds {
			if taskKind == model.TaskKindContentGeneration {
				types = append(types, pack.Channel)
			} else {
				types = append(types, model.TaskTypeForTaskKind(taskKind))
			}
		}
	}
	return types
}

func TestKubernetesResourcesForTaskRequiresExactProfile(t *testing.T) {
	for _, taskType := range KubernetesTaskTypes() {
		if strings.Contains(taskType, "_") {
			t.Errorf("registered task type %q uses snake_case; resource profile types must be kebab-case", taskType)
		}
	}
	if !slices.Contains(KubernetesTaskTypes(), "viral-analysis") {
		t.Fatal("registered task types missing canonical viral-analysis")
	}

	cfg := KubernetesConfig{
		ResourceProfiles: map[string]KubernetesResourceConfig{
			"wechat": {
				Requests: map[string]string{"cpu": "2", "memory": "4Gi"},
				Limits:   map[string]string{"cpu": "4", "memory": "7Gi"},
			},
		},
	}
	got, err := cfg.ResourcesForTask("wechat-article")
	if err == nil {
		t.Fatal("missing exact profile unexpectedly succeeded")
	}
	if len(got.Requests) != 0 || len(got.Limits) != 0 {
		t.Fatalf("wechat profile unexpectedly matched wechat-article: %#v", got)
	}
	cfg.ResourceProfiles["wechat-article"] = KubernetesResourceConfig{
		Requests: map[string]string{"cpu": "500m", "memory": "500Mi"},
		Limits:   map[string]string{"cpu": "1", "memory": "1Gi"},
	}
	got, err = cfg.ResourcesForTask("wechat-article")
	if err != nil {
		t.Fatalf("exact profile resources: %v", err)
	}
	if got.Requests["cpu"] != "500m" || got.Requests["memory"] != "500Mi" || got.Limits["cpu"] != "1" || got.Limits["memory"] != "1Gi" {
		t.Fatalf("exact profile resources = %#v", got)
	}
}

func TestKubernetesConfigRejectsDefaultResourcesField(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML, "  executor: docker\n", "  executor: kubernetes\n  kubernetes:\n    resources:\n      requests: { cpu: \"1\", memory: \"1Gi\" }\n", 1)
	if _, err := loadClaudeConfigYAML(t, body); err == nil || !strings.Contains(err.Error(), `unknown claude.kubernetes config field "resources"`) {
		t.Fatalf("load config error = %v, want removed default resources field rejected", err)
	}
}

func TestValidateKubernetesResourceProfiles(t *testing.T) {
	tests := []struct {
		name    string
		profile KubernetesResourceConfig
		want    string
	}{
		{name: "invalid quantity", profile: KubernetesResourceConfig{Requests: map[string]string{"cpu": "invalid", "memory": "512Mi"}, Limits: map[string]string{"cpu": "1", "memory": "1Gi"}}, want: "must be a positive Kubernetes quantity"},
		{name: "request exceeds limit", profile: KubernetesResourceConfig{Requests: map[string]string{"cpu": "500m", "memory": "5Gi"}, Limits: map[string]string{"cpu": "1", "memory": "4Gi"}}, want: "must not exceed its limit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := baseKubernetesConfigForTest()
			cfg.Claude.Kubernetes.ResourceProfiles["seednote"] = test.profile
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateRequiresResourceProfileForEveryExecutableTaskType(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	delete(cfg.Claude.Kubernetes.ResourceProfiles, "wechat-article")
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.kubernetes.resource_profiles.wechat-article is required") {
		t.Fatalf("Validate() error = %v, want missing exact task profile", err)
	}
}

func TestValidateRejectsUnknownAndIncompleteResourceProfiles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		key    string
		value  KubernetesResourceConfig
		wanted string
	}{
		{name: "unknown type", key: "wechat", value: KubernetesResourceConfig{Requests: map[string]string{"cpu": "1", "memory": "1Gi"}, Limits: map[string]string{"cpu": "1", "memory": "1Gi"}}, wanted: `claude.kubernetes.resource_profiles contains unsupported task type "wechat"`},
		{name: "missing memory request", key: "wechat-article", value: KubernetesResourceConfig{Requests: map[string]string{"cpu": "500m"}, Limits: map[string]string{"cpu": "1", "memory": "1Gi"}}, wanted: "resource_profiles.wechat-article.requests.memory is required"},
		{name: "missing cpu limit", key: "wechat-article", value: KubernetesResourceConfig{Requests: map[string]string{"cpu": "500m", "memory": "512Mi"}, Limits: map[string]string{"memory": "1Gi"}}, wanted: "resource_profiles.wechat-article.limits.cpu is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseKubernetesConfigForTest()
			cfg.Claude.Kubernetes.ResourceProfiles[tc.key] = tc.value
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wanted) {
				t.Fatalf("Validate() error = %v, want %q", err, tc.wanted)
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

func TestNewConfigRejectsRemovedPreStartRetryLimit(t *testing.T) {
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
  executor: "kubernetes"
  runtime_images:
    wechat-article: "registry.example.com/creator-agent-wechat-article@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    seednote: "registry.example.com/creator-agent-seednote@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    montage: "registry.example.com/creator-agent-montage@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
    viral-ip-video-editor: "registry.example.com/creator-agent-viral-ip-video-editor@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    whiteboard-animation: "registry.example.com/creator-agent-whiteboard-animation@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
  execution_token_secret: "0123456789abcdef0123456789abcdef"
  agent_server_url: "https://creator-api-svc:8443"
  kubernetes:
    service_account: "creator-agent-runner"
    nas_storage_class: "nas-sc-creator"
    project_memory_claim: "creator-project-memory"
    task_workspace_size: "10Gi"
    completion_grace_seconds: 0
    pre_start_retry_limit: 0
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewConfig(path)
	if err == nil || !strings.Contains(err.Error(), `unknown claude.kubernetes config field "pre_start_retry_limit"`) {
		t.Fatalf("NewConfig() error = %v, want removed field rejection", err)
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
			name: "active deadline exceeds execution credential",
			mutate: func(cfg *Config) {
				cfg.Claude.Kubernetes.ActiveDeadlineSeconds = int64(auth.MaximumExecutionTokenLifetime/time.Second) + 1
			},
			wantErr: "claude.kubernetes.active_deadline_seconds must not exceed execution token lifetime",
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

func TestValidateDshKubernetesUsesClaudeServiceAccount(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.Executor = "docker"
	cfg.Claude.Kubernetes.ServiceAccount = ""
	cfg.Dsh.Enabled = true
	cfg.Dsh.Executor = "kubernetes"
	cfg.Dsh.Image = "registry.example.com/creator-dsh@sha256:" + strings.Repeat("a", 64)
	cfg.Dsh.KubernetesNamespace = "anbanai-prod"
	cfg.Dsh.KubernetesProjectMemoryClaim = "creator-project-memory"
	cfg.Dsh.KubernetesProjectMemoryMount = "/workspace/project"
	cfg.Dsh.ServerURL = "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443"
	cfg.Dsh.ServerInstanceID = "server-1"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "claude.kubernetes.service_account is required when dsh.executor is kubernetes") {
		t.Fatalf("Validate() error = %v, want Claude ServiceAccount requirement for DSH", err)
	}
}

func TestValidateDshKubernetesRequiresClaudeNamespace(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Dsh.Enabled = true
	cfg.Dsh.Executor = "kubernetes"
	cfg.Dsh.Image = "registry.example.com/creator-dsh@sha256:" + strings.Repeat("a", 64)
	cfg.Dsh.KubernetesNamespace = "other-namespace"
	cfg.Dsh.KubernetesProjectMemoryClaim = "creator-project-memory"
	cfg.Dsh.KubernetesProjectMemoryMount = "/workspace/project"
	cfg.Dsh.ServerURL = "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443"
	cfg.Dsh.ServerInstanceID = "server-1"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "dsh.kubernetes_namespace must match claude.kubernetes.namespace") {
		t.Fatalf("Validate() error = %v, want DSH/Claude namespace invariant", err)
	}
}

func TestValidateDshKubernetesRejectsMalformedDigestImageReference(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Dsh.Enabled = true
	cfg.Dsh.Executor = "kubernetes"
	cfg.Dsh.Image = "crpi.example.com/bx_anbanai/runtime-dsh: 4ca2ec7bd14a7981133a648cae01b5982a7d73c9@sha256:" + strings.Repeat("c", 64)
	cfg.Dsh.KubernetesNamespace = "anbanai-prod"
	cfg.Dsh.KubernetesProjectMemoryClaim = "creator-project-memory"
	cfg.Dsh.KubernetesProjectMemoryMount = "/workspace/project"
	cfg.Dsh.ServerURL = "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443"
	cfg.Dsh.ServerInstanceID = "server-1"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "dsh.image must be a valid container image reference without whitespace") {
		t.Fatalf("Config.Validate() error = %v, want malformed DSH image reference rejected", err)
	}
}

func TestValidateDshBaseURLRejectsUnicodeHostname(t *testing.T) {
	err := validateDshBaseURL("https://实际的deepseek-anthropic兼容地址")
	if err == nil || !strings.Contains(err.Error(), "ASCII hostname") {
		t.Fatalf("validateDshBaseURL() error = %v, want Unicode hostname rejected", err)
	}
}

func TestValidateDshKubernetesAcceptsTaggedAndImmutableImageReferences(t *testing.T) {
	for _, image := range []string{
		"crpi-671lgv9lrdyll4t9.cn-chengdu.personal.cr.aliyuncs.com/bx_anbanai/runtime-dsh:e5e395c067ef3fe548347a1e61f292bcdd96dc9b",
		"registry.example.com/creator-dsh",
		"registry.example.com/creator-dsh:release",
		"registry.example.com/creator-dsh@sha256:" + strings.Repeat("a", 64),
		"registry.example.com/creator-dsh:release@sha256:" + strings.Repeat("b", 64),
	} {
		t.Run(image, func(t *testing.T) {
			cfg := baseKubernetesConfigForTest()
			cfg.Dsh.Enabled = true
			cfg.Dsh.Executor = "kubernetes"
			cfg.Dsh.Route = "effective"
			cfg.Dsh.Routes = map[string]DshRouteConfig{"effective": {
				Provider: "deepseek-official", API: "deepseek-messages",
				BaseURL: "https://api.deepseek.com/anthropic", APIKey: "test-key", Model: "deepseek-flash",
			}}
			cfg.Dsh.Image = image
			cfg.Dsh.KubernetesNamespace = "anbanai-prod"
			cfg.Dsh.KubernetesProjectMemoryClaim = "creator-project-memory"
			cfg.Dsh.KubernetesProjectMemoryMount = "/workspace/project"
			cfg.Dsh.ServerURL = "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443"
			cfg.Dsh.ServerInstanceID = "server-1"

			if err := cfg.Validate(); err != nil {
				t.Fatalf("Config.Validate() error = %v, want tagged or immutable image reference accepted", err)
			}
		})
	}
}

func TestValidateDshKubernetesRejectsMalformedAndWhitespaceImageReferences(t *testing.T) {
	for _, image := range []string{
		" registry.example.com/creator-dsh@sha256:" + strings.Repeat("a", 64),
		"registry.example.com/creator-dsh@sha256:" + strings.Repeat("a", 64) + "\n",
		"registry.example.com/runtime..dsh:latest",
	} {
		t.Run(image, func(t *testing.T) {
			cfg := baseKubernetesConfigForTest()
			cfg.Dsh.Enabled = true
			cfg.Dsh.Executor = "kubernetes"
			cfg.Dsh.Image = image
			cfg.Dsh.KubernetesNamespace = "anbanai-prod"
			cfg.Dsh.KubernetesProjectMemoryClaim = "creator-project-memory"
			cfg.Dsh.KubernetesProjectMemoryMount = "/workspace/project"
			cfg.Dsh.ServerURL = "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443"
			cfg.Dsh.ServerInstanceID = "server-1"

			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "dsh.image must be a valid container image reference without whitespace") {
				t.Fatalf("Config.Validate() error = %v, want invalid image reference rejected", err)
			}
		})
	}
}

func TestValidateDisabledDshSkipsKubernetesRuntimeRequirements(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Dsh.Enabled = false
	cfg.Claude.Executor = "docker"
	cfg.Dsh.Executor = "kubernetes"
	cfg.Dsh.Image = "creator-dsh-runtime:latest"
	cfg.Dsh.KubernetesNamespace = ""
	cfg.Dsh.KubernetesProjectMemoryClaim = ""
	cfg.Dsh.KubernetesProjectMemoryMount = ""
	cfg.Dsh.Resources = KubernetesResourceConfig{}
	cfg.Claude.Kubernetes.ServiceAccount = ""

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want disabled DSH to be ignored", err)
	}
}

func TestValidateKubernetesAllowsZeroCompletionGrace(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.Kubernetes.CompletionGraceSeconds = 0

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want zero completion grace accepted", err)
	}
}

func TestAgentServerURLUsesConfiguredKubernetesServiceURL(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.AgentServerURL = "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443/"
	if got := cfg.AgentServerURL(); got != "https://creator-api-svc.anbanai-prod.svc.cluster.local:8443" {
		t.Fatalf("AgentServerURL() = %q", got)
	}
}
