package config

import (
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/auth"
)

func TestAsynqConfigTimeoutDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()

	if cfg.Asynq.ContentGenerateTimeout != 60*time.Minute {
		t.Errorf("asynq.content_generate_timeout default = %v, want 60m", cfg.Asynq.ContentGenerateTimeout)
	}
	if cfg.Asynq.PersistTimeout != 10*time.Minute {
		t.Errorf("asynq.persist_timeout default = %v, want 10m", cfg.Asynq.PersistTimeout)
	}
	// Docker container timeout default must stay >= content_generate_timeout so the
	// container is not killed before the asynq task deadline.
	if cfg.Claude.Docker.TimeoutSec != 3600 {
		t.Errorf("claude.docker.timeout_sec default = %d, want 3600", cfg.Claude.Docker.TimeoutSec)
	}
}

func TestDockerSchedulerDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()

	if cfg.Claude.Docker.Network != "creator-runtime-network" {
		t.Errorf("claude.docker.network default = %q, want creator-runtime-network", cfg.Claude.Docker.Network)
	}
	if cfg.Claude.Docker.CPUCores != 2 {
		t.Errorf("claude.docker.cpu_cores default = %d, want 2", cfg.Claude.Docker.CPUCores)
	}
	if cfg.Claude.Docker.MemoryMB != 4096 {
		t.Errorf("claude.docker.memory_mb default = %d, want 4096", cfg.Claude.Docker.MemoryMB)
	}
	if cfg.Claude.Docker.PidsLimit != 512 {
		t.Errorf("claude.docker.pids_limit default = %d, want 512", cfg.Claude.Docker.PidsLimit)
	}
}

func TestValidateDockerSchedulerSettingsMustBePositive(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*DockerConfig)
		want   string
	}{
		{name: "zero CPU", mutate: func(cfg *DockerConfig) { cfg.CPUCores = 0 }, want: "claude.docker.cpu_cores must be positive"},
		{name: "negative CPU", mutate: func(cfg *DockerConfig) { cfg.CPUCores = -1 }, want: "claude.docker.cpu_cores must be positive"},
		{name: "zero memory", mutate: func(cfg *DockerConfig) { cfg.MemoryMB = 0 }, want: "claude.docker.memory_mb must be positive"},
		{name: "negative memory", mutate: func(cfg *DockerConfig) { cfg.MemoryMB = -1 }, want: "claude.docker.memory_mb must be positive"},
		{name: "zero PID limit", mutate: func(cfg *DockerConfig) { cfg.PidsLimit = 0 }, want: "claude.docker.pids_limit must be positive"},
		{name: "negative PID limit", mutate: func(cfg *DockerConfig) { cfg.PidsLimit = -1 }, want: "claude.docker.pids_limit must be positive"},
		{name: "zero timeout", mutate: func(cfg *DockerConfig) { cfg.TimeoutSec = 0 }, want: "claude.docker.timeout_sec must be positive"},
		{name: "negative timeout", mutate: func(cfg *DockerConfig) { cfg.TimeoutSec = -1 }, want: "claude.docker.timeout_sec must be positive"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &Config{
				Database: DatabaseConfig{DSN: "dsn"},
				JWT:      JWTConfig{SecretKey: "secret", AccessExpiry: "24h", RefreshExpiry: "168h"},
				Claude:   validClaudeConfigForTest(),
			}
			cfg.applyDefaults()
			test.mutate(&cfg.Claude.Docker)

			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateDockerRequiresExecutionTokenSecret(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{DSN: "dsn"},
		JWT:      JWTConfig{SecretKey: "secret", AccessExpiry: "24h", RefreshExpiry: "168h"},
		Claude:   validClaudeConfigForTest(),
	}
	cfg.applyDefaults()
	cfg.Claude.ExecutionTokenSecret = "too-short"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.execution_token_secret must be at least 32 bytes") {
		t.Fatalf("Validate() error = %v, want Docker execution token secret requirement", err)
	}
}

func TestValidate_DockerTimeoutMustExceedContentGenerateTimeout(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{DSN: "dsn"},
		JWT:      JWTConfig{SecretKey: "secret", AccessExpiry: "24h", RefreshExpiry: "168h"},
		Claude:   validClaudeConfigForTest(),
	}
	cfg.applyDefaults()
	// Force docker timeout below content_generate_timeout (60m) to trip the check.
	cfg.Claude.Docker.TimeoutSec = 1800

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error when claude.docker.timeout_sec < asynq.content_generate_timeout")
	}
	if !strings.Contains(err.Error(), "must be >=") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAsynqLifecycleFitsExecutionCredential(t *testing.T) {
	for _, test := range []struct {
		name    string
		content time.Duration
		persist time.Duration
		want    string
	}{
		{name: "negative execution", content: -time.Minute, persist: time.Minute, want: "asynq.content_generate_timeout must be positive"},
		{name: "negative persistence", content: time.Minute, persist: -time.Minute, want: "asynq.persist_timeout must be positive"},
		{name: "credential lifetime exceeded", content: auth.MaximumExecutionTokenLifetime, persist: time.Second, want: "must not exceed execution token lifetime"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &Config{
				Database: DatabaseConfig{DSN: "dsn"},
				JWT:      JWTConfig{SecretKey: "secret", AccessExpiry: "24h", RefreshExpiry: "168h"},
				Claude:   validClaudeConfigForTest(),
			}
			cfg.applyDefaults()
			cfg.Asynq.ContentGenerateTimeout = test.content
			cfg.Asynq.PersistTimeout = test.persist
			cfg.Claude.Docker.TimeoutSec = int(auth.MaximumExecutionTokenLifetime / time.Second)

			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateDockerDeadlineFitsExecutionCredential(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{DSN: "dsn"},
		JWT:      JWTConfig{SecretKey: "secret", AccessExpiry: "24h", RefreshExpiry: "168h"},
		Claude:   validClaudeConfigForTest(),
	}
	cfg.applyDefaults()
	cfg.Claude.Docker.TimeoutSec = int(auth.MaximumExecutionTokenLifetime/time.Second) + 1

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.docker.timeout_sec must not exceed execution token lifetime") {
		t.Fatalf("Validate() error = %v, want Docker execution token lifetime bound", err)
	}
}
