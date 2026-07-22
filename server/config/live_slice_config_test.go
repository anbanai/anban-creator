package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExpandEnvVars covers the ${VAR} / ${VAR:-default} expansion that is now
// the only way environment variables influence config. There is no hidden
// ANBAN_* override layer anymore.
func TestExpandEnvVars(t *testing.T) {
	t.Setenv("ANBAN_EXPAND_SET", "from-env")
	t.Setenv("ANBAN_EXPAND_EMPTY", "")
	// ANBAN_EXPAND_UNSET is deliberately not set.

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"set var", "${ANBAN_EXPAND_SET}", "from-env"},
		{"set var inside text", "addr: ${ANBAN_EXPAND_SET}:6379", "addr: from-env:6379"},
		{"unset no default -> empty", "val=${ANBAN_EXPAND_UNSET}", "val="},
		{"empty no default -> empty", "val=${ANBAN_EXPAND_EMPTY}", "val="},
		{"unset with default", "${ANBAN_EXPAND_UNSET:-localhost:6379}", "localhost:6379"},
		{"empty with default", "${ANBAN_EXPAND_EMPTY:-localhost:6379}", "localhost:6379"},
		{"set var beats default", "${ANBAN_EXPAND_SET:-fallback}", "from-env"},
		{"default with special chars", "${ANBAN_EXPAND_UNSET:-root:p@ss/tcp(host:3306)/db?a=1&b=2}", "root:p@ss/tcp(host:3306)/db?a=1&b=2"},
		// Bare $VAR (no braces) must be left untouched — protects literal '$'.
		{"bare dollar untouched", "price $5 and $HOME stay", "price $5 and $HOME stay"},
		{"literal dollar in password", `pass: "pa$$word"`, `pass: "pa$$word"`},
		{"default containing colon equals", "${ANBAN_X:-a:b:c=d}", "a:b:c=d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(expandEnvVars([]byte(tc.in)))
			if got != tc.want {
				t.Errorf("expandEnvVars(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNewConfigEnvInterpolation verifies the full NewConfig path expands ${...}
// in the YAML file before parsing, and that fields without ${...} are NOT
// affected by unrelated environment variables (the hidden override layer is gone).
func TestNewConfigEnvInterpolation(t *testing.T) {
	t.Setenv("ANBAN_TEST_REDIS", "redis:6379")
	t.Setenv("ANBAN_TEST_ILINK_URL", "http://wcflink:18070")
	t.Setenv("ANBAN_TEST_SEEDNOTE_URL", "http://seednote:18060")
	// An unrelated env var that must NOT leak into config.
	t.Setenv("ANBAN_JWT_SECRET_KEY", "should-be-ignored-without-placeholder")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// executor: docker avoids the local plugin_dir validation requirement.
	yaml := `
database:
  dsn: "root:dev@tcp(${ANBAN_TEST_HOST:-localhost}:3306)/db"
jwt:
  secret_key: "real-secret"
  access_expiry: "24h"
  refresh_expiry: "168h"
redis:
  addr: "${ANBAN_TEST_REDIS:-localhost:6379}"
seednote:
  base_url: "${ANBAN_TEST_SEEDNOTE_URL:-http://localhost:18060}"
ilink:
  enabled: true
  base_url: "${ANBAN_TEST_ILINK_URL:-http://localhost:18070}"
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
  executor: docker
  runtime_images:
    article: creator-agent-article:latest
    seednote: creator-agent-seednote:latest
    montage: creator-agent-montage:latest
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := NewConfig(path)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}

	if cfg.Database.DSN != "root:dev@tcp(localhost:3306)/db" {
		t.Errorf("dsn = %q", cfg.Database.DSN)
	}
	if cfg.Redis.Addr != "redis:6379" {
		t.Errorf("redis.addr = %q, want redis:6379", cfg.Redis.Addr)
	}
	if cfg.Ilink.BaseURL != "http://wcflink:18070" {
		t.Errorf("ilink.base_url = %q, want http://wcflink:18070", cfg.Ilink.BaseURL)
	}
	if cfg.Seednote.BaseURL != "http://seednote:18060" {
		t.Errorf("seednote.base_url = %q, want http://seednote:18060", cfg.Seednote.BaseURL)
	}
	if !cfg.Ilink.Enabled {
		t.Errorf("ilink.enabled = false, want true")
	}
	// ANBAN_JWT_SECRET_KEY is set but the yaml has no ${...} for it, so it must
	// NOT override the literal value — the hidden override layer is gone.
	if cfg.JWT.SecretKey != "real-secret" {
		t.Errorf("jwt.secret_key = %q, want \"real-secret\" (env must not override without ${...})", cfg.JWT.SecretKey)
	}
	// Relocated default from the deleted applyEnvOverrides.
	if cfg.Writing.Timeout == 0 {
		t.Errorf("writing.timeout = 0, want default 10m after applyDefaults")
	}
}

func TestTingWuConfigDoesNotRequireCredentialsWhenUnused(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{DSN: "root:pass@tcp(localhost:3306)/test"},
		JWT:      JWTConfig{SecretKey: "secret", AccessExpiry: "24h", RefreshExpiry: "168h"},
		Claude:   validClaudeConfigForTest(),
	}
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate without TingWu config: %v", err)
	}
}
