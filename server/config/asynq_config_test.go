package config

import (
	"strings"
	"testing"
	"time"
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

func TestValidate_DockerTimeoutOKForLocalExecutor(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{DSN: "dsn"},
		JWT:      JWTConfig{SecretKey: "secret", AccessExpiry: "24h", RefreshExpiry: "168h"},
		Claude:   validClaudeConfigForTest(),
	}
	cfg.Claude.Executor = "local"
	cfg.Claude.PluginDir = "." // local needs plugin_dir
	cfg.applyDefaults()
	cfg.Claude.Docker.TimeoutSec = 1800 // would fail the docker check, but executor is local

	if err := cfg.Validate(); err != nil {
		// plugin_dir "." may not contain agents/; only assert the docker-timeout
		// check did not fire for the local executor.
		if strings.Contains(err.Error(), "must be >=") {
			t.Fatalf("docker-timeout check must not fire for local executor: %v", err)
		}
	}
}
