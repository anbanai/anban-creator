package config

import (
	"strings"
	"testing"
	"time"
)

func TestMemoryConfigDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()

	if cfg.Memory.Enabled {
		t.Fatal("Memory.Enabled default = true, want false")
	}
	if cfg.Memory.Provider != "oss" {
		t.Fatalf("Memory.Provider = %q, want oss", cfg.Memory.Provider)
	}
	if cfg.Memory.OSSPrefix != "claude-memory/projects" {
		t.Fatalf("Memory.OSSPrefix = %q, want claude-memory/projects", cfg.Memory.OSSPrefix)
	}
	if cfg.Memory.RuntimeDir != ".claude/memory" {
		t.Fatalf("Memory.RuntimeDir = %q, want .claude/memory", cfg.Memory.RuntimeDir)
	}
	if cfg.Memory.MaxArchiveBytes != 262144 {
		t.Fatalf("Memory.MaxArchiveBytes = %d, want 262144", cfg.Memory.MaxArchiveBytes)
	}
	if cfg.Memory.MergeOnStatus != "completed" {
		t.Fatalf("Memory.MergeOnStatus = %q, want completed", cfg.Memory.MergeOnStatus)
	}
	if cfg.Memory.LockTTL != time.Minute {
		t.Fatalf("Memory.LockTTL = %s, want 1m", cfg.Memory.LockTTL)
	}
}

func TestMemoryConfigRejectsUnsafeRuntimeDir(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{DSN: "dsn"},
		JWT:      JWTConfig{SecretKey: "secret"},
		Claude:   ClaudeConfig{Executor: "docker"},
		Memory: MemoryConfig{
			Enabled:    true,
			RuntimeDir: "../.claude/memory",
		},
	}
	cfg.applyDefaults()

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want unsafe memory.runtime_dir error")
	}
	if !strings.Contains(err.Error(), "memory.runtime_dir") {
		t.Fatalf("Validate() error = %v, want memory.runtime_dir mention", err)
	}
}
