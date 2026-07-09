package config

import (
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
				Namespace:          "anban",
				AgentImage:         "registry.example.com/anban-agent:latest",
				ServiceAccount:     "anban-agent-runner",
				WorkspaceMountPath: "/workspace",
				WorkspacePVCName:   "anban-agent-nas",
				ExecTimeoutSec:     3600,
			},
			AgentServerURL: "http://anban-server.anban.svc.cluster.local:8080",
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

func TestValidateRejectsKubernetesExecutorUntilExecutorIsWired(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "until a Kubernetes executor is wired") {
		t.Fatalf("Validate() error = %v, want fail-closed executor rejection", err)
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

func TestValidateKubernetesRequiresWorkspaceVolume(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.Kubernetes.WorkspacePVCName = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.kubernetes.workspace_pvc_name is required") {
		t.Fatalf("Validate() error = %v, want workspace pvc requirement", err)
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

func TestAgentServerURLUsesConfiguredKubernetesServiceURL(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.AgentServerURL = "http://anban-server.anban.svc.cluster.local:8080/"
	if got := cfg.AgentServerURL(); got != "http://anban-server.anban.svc.cluster.local:8080" {
		t.Fatalf("AgentServerURL() = %q", got)
	}
}
