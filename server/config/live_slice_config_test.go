package config

import "testing"

func TestTingWuEnvOverrides(t *testing.T) {
	t.Setenv("ANBAN_SERVER_TINGWU_ENDPOINT", "tingwu.cn-beijing.aliyuncs.com")
	t.Setenv("ANBAN_SERVER_TINGWU_REGION", "cn-beijing")
	t.Setenv("ANBAN_SERVER_TINGWU_APP_KEY", "app-key")
	t.Setenv("ANBAN_SERVER_TINGWU_ACCESS_KEY", "access-key")
	t.Setenv("ANBAN_SERVER_TINGWU_ACCESS_SECRET", "access-secret")

	cfg := &Config{}
	cfg.applyEnvOverrides()

	if cfg.TingWu.Endpoint != "tingwu.cn-beijing.aliyuncs.com" {
		t.Fatalf("endpoint = %q", cfg.TingWu.Endpoint)
	}
	if cfg.TingWu.Region != "cn-beijing" {
		t.Fatalf("region = %q", cfg.TingWu.Region)
	}
	if cfg.TingWu.AppKey != "app-key" {
		t.Fatalf("app key = %q", cfg.TingWu.AppKey)
	}
	if cfg.TingWu.AccessKey != "access-key" {
		t.Fatalf("access key = %q", cfg.TingWu.AccessKey)
	}
	if cfg.TingWu.AccessSecret != "access-secret" {
		t.Fatalf("access secret = %q", cfg.TingWu.AccessSecret)
	}
}

func TestTingWuConfigDoesNotRequireCredentialsWhenUnused(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{DSN: "root:pass@tcp(localhost:3306)/test"},
		JWT:      JWTConfig{SecretKey: "secret", AccessExpiry: "24h", RefreshExpiry: "168h"},
		Claude:   ClaudeConfig{Executor: "docker"},
	}
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate without TingWu config: %v", err)
	}
}
