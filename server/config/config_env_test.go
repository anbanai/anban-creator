package config

import (
	"os"
	"path/filepath"
	"strings"
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
	t.Setenv("ANBAN_TEST_ILINK_URL", "http://sidecar-ilink:18070")
	t.Setenv("ANBAN_TEST_SEEDNOTE_URL", "http://sidecar-seednote:18060")
	t.Setenv("ANBAN_TEST_DEEPSEEK_KEY", "existing-server-only-key")
	t.Setenv("ANBAN_TEST_MEMORY_ROOT", filepath.ToSlash(t.TempDir()))
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
interactive:
  bridge_dir: ./runtime-dsh
  dsh_patch: ./harness/dsh/project-creator.patch.yml
seednote:
  base_url: "${ANBAN_TEST_SEEDNOTE_URL:-http://localhost:18060}"
ilink:
  enabled: true
  base_url: "${ANBAN_TEST_ILINK_URL:-http://localhost:18070}"
claude:
  execution_profiles:
    effective:
      provider: deepseek
      envs:
        ANTHROPIC_BASE_URL: https://api.deepseek.com/anthropic
        ANTHROPIC_AUTH_TOKEN: ${ANBAN_TEST_DEEPSEEK_KEY}
        ANTHROPIC_MODEL: deepseek-flash
        ANTHROPIC_DEFAULT_OPUS_MODEL: deepseek-flash
        ANTHROPIC_DEFAULT_SONNET_MODEL: deepseek-flash
        ANTHROPIC_DEFAULT_HAIKU_MODEL: deepseek-flash
        ANTHROPIC_DEFAULT_FABLE_MODEL: deepseek-flash
  project_memory:
    root_dir: ${ANBAN_TEST_MEMORY_ROOT}
  executor: docker
  execution_token_secret: 0123456789abcdef0123456789abcdef
  runtime_images:
    wechat-article: creator-agent-wechat-article:latest
    wechat-picture: creator-agent-wechat-picture:latest
    seednote: creator-agent-seednote:latest
    montage: creator-agent-montage:latest
    moments: creator-agent-moments:latest
    ecommerce: creator-agent-ecommerce:latest
    hypit: creator-agent-hypit:latest
    whiteboard-animation: creator-agent-whiteboard-animation:latest
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
	if cfg.Ilink.BaseURL != "http://sidecar-ilink:18070" {
		t.Errorf("ilink.base_url = %q, want http://sidecar-ilink:18070", cfg.Ilink.BaseURL)
	}
	if cfg.Seednote.BaseURL != "http://sidecar-seednote:18060" {
		t.Errorf("seednote.base_url = %q, want http://sidecar-seednote:18060", cfg.Seednote.BaseURL)
	}
	if !cfg.Ilink.Enabled {
		t.Errorf("ilink.enabled = false, want true")
	}
	// ANBAN_JWT_SECRET_KEY is set but the yaml has no ${...} for it, so it must
	// NOT override the literal value — the hidden override layer is gone.
	if cfg.JWT.SecretKey != "real-secret" {
		t.Errorf("jwt.secret_key = %q, want \"real-secret\" (env must not override without ${...})", cfg.JWT.SecretKey)
	}
	if !cfg.Onboarding.Enabled || cfg.Onboarding.Chat.APIKey != "existing-server-only-key" || cfg.Onboarding.Chat.BaseURL != "https://api.deepseek.com" {
		t.Fatal("NewConfig did not reuse the resolved server provider")
	}
	if err := os.WriteFile(path, []byte("onboarding:\n  enabled: false\n"+yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	disabled, err := NewConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Onboarding.Enabled || disabled.Onboarding.Chat.APIKey != "" {
		t.Fatal("explicit disable must prevent inheritance")
	}
	wantBridgeDir := filepath.Join(dir, "runtime-dsh")
	if cfg.Interactive.BridgeDir != wantBridgeDir {
		t.Errorf("interactive.bridge_dir = %q, want %q", cfg.Interactive.BridgeDir, wantBridgeDir)
	}
	wantPatch := filepath.Join(dir, "harness/dsh/project-creator.patch.yml")
	if cfg.Interactive.DSHPatch != wantPatch {
		t.Errorf("interactive.dsh_patch = %q, want %q", cfg.Interactive.DSHPatch, wantPatch)
	}
	if cfg.Interactive.ProviderAPIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Errorf("interactive.provider_api_key_env = %q, want DEEPSEEK_API_KEY", cfg.Interactive.ProviderAPIKeyEnv)
	}
}

func TestDisabledInteractiveRuntimeDoesNotValidateProviderCredentialSelector(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
database:
  dsn: "dsn"
jwt:
  secret_key: "secret"
interactive:
  enabled: false
  provider_api_key_env: "DEEPSEEK-API-KEY"
claude:
  executor: docker
  execution_token_secret: "0123456789abcdef0123456789abcdef"
  runtime_images:
    wechat-article: creator-agent-wechat-article:latest
    wechat-picture: creator-agent-wechat-picture:latest
    seednote: creator-agent-seednote:latest
    montage: creator-agent-montage:latest
    moments: creator-agent-moments:latest
    ecommerce: creator-agent-ecommerce:latest
    profile-analysis: creator-agent-profile-analysis:latest
    feedback-analysis: creator-agent-feedback-analysis:latest
    whiteboard-animation: creator-agent-whiteboard-animation:latest
    hypit: creator-agent-hypit:latest
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	if _, err := NewConfig(path); err != nil {
		t.Fatalf("disabled interactive runtime must not reject provider_api_key_env: %v", err)
	}
}

func TestEnabledInteractiveRuntimeRejectsInvalidProviderCredentialSelector(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
database:
  dsn: "dsn"
jwt:
  secret_key: "secret"
interactive:
  enabled: true
  provider_api_key_env: "DEEPSEEK-API-KEY"
claude:
  executor: docker
  execution_token_secret: "0123456789abcdef0123456789abcdef"
  runtime_images:
    wechat-article: creator-agent-wechat-article:latest
    wechat-picture: creator-agent-wechat-picture:latest
    seednote: creator-agent-seednote:latest
    montage: creator-agent-montage:latest
    moments: creator-agent-moments:latest
    ecommerce: creator-agent-ecommerce:latest
    profile-analysis: creator-agent-profile-analysis:latest
    feedback-analysis: creator-agent-feedback-analysis:latest
    whiteboard-animation: creator-agent-whiteboard-animation:latest
    hypit: creator-agent-hypit:latest
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	if _, err := NewConfig(path); err == nil || !strings.Contains(err.Error(), "interactive.provider_api_key_env must be a valid environment variable name") {
		t.Fatalf("enabled interactive runtime error = %v", err)
	}
}

func TestKubernetesDefaultsSynthesizeLocalRuntimeImages(t *testing.T) {
	cfg := Config{}
	cfg.Claude.Executor = "kubernetes"
	cfg.applyDefaults()
	if got := cfg.Claude.RuntimeImages["wechat-article"]; got != "creator-agent-wechat-article:latest" {
		t.Fatalf("Kubernetes article runtime image default = %q, want creator-agent-wechat-article:latest", got)
	}
}
