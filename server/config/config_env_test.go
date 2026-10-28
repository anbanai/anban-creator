package config

import (
	"fmt"
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
dsh:
  route: effective
  routes:
    effective:
      provider: deepseek-official
      api: deepseek-messages
      base_url: https://api.deepseek.com/anthropic
      api_key: ${ANBAN_TEST_DEEPSEEK_KEY}
      model: deepseek-flash
  bridge_dir: ./runtime-dsh
  dsh_patch: ./harness/dsh/project-creator.patch.yml
ilink:
  enabled: true
  base_url: "${ANBAN_TEST_ILINK_URL:-http://localhost:18070}"
claude:
  profiles:
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
    viral-ip-video-editor: creator-agent-viral-ip-video-editor:latest
    video-edit: creator-agent-video-edit:latest
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
	if cfg.Dsh.BridgeDir != wantBridgeDir {
		t.Errorf("dsh.bridge_dir = %q, want %q", cfg.Dsh.BridgeDir, wantBridgeDir)
	}
	wantPatch := filepath.Join(dir, "harness/dsh/project-creator.patch.yml")
	if cfg.Dsh.DSHPatch != wantPatch {
		t.Errorf("dsh.dsh_patch = %q, want %q", cfg.Dsh.DSHPatch, wantPatch)
	}
	if cfg.Dsh.Routes["effective"].APIKey != "existing-server-only-key" {
		t.Errorf("dsh.routes.effective.api_key = %q, want expanded API key", cfg.Dsh.Routes["effective"].APIKey)
	}
}

func TestDshRouteValuesAreCanonicalized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := strings.Replace(validClaudeConfigYAML, "claude:\n", `dsh:
  route: Effective
  routes:
    Effective:
      provider: DeepSeek-Official
      api: DeepSeek-Messages
      base_url: " https://api.deepseek.com/anthropic "
      api_key: " direct-api-key "
      model: " deepseek-flash "
claude:
`, 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewConfig(path)
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}
	route := cfg.Dsh.Routes["Effective"]
	if route.Provider != "deepseek-official" || route.API != "deepseek-messages" || route.BaseURL != "https://api.deepseek.com/anthropic" || route.APIKey != " direct-api-key " || route.Model != "deepseek-flash" {
		t.Fatalf("canonical DSH route = %#v", route)
	}
}

func TestConfigRejectsLegacyInteractiveAndInvalidDshRoute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for _, body := range []string{
		"database:\n  dsn: dsn\njwt:\n  secret_key: secret\ninteractive: {}\n",
		"database:\n  dsn: dsn\njwt:\n  secret_key: secret\nclaude:\n  execution_profiles: {}\n",
		"database:\n  dsn: dsn\njwt:\n  secret_key: secret\ndsh:\n  route: effective\n  routes:\n    effective:\n      provider: deepseek-official\n      api: deepseek-messages\n      base_url: https://api.deepseek.com/anthropic\n      provider_api_key_env: DEEPSEEK_API_KEY\n      model: deepseek-flash\n",
		"database:\n  dsn: dsn\njwt:\n  secret_key: secret\ndsh:\n  route: effective\n  routes:\n    effective:\n      provider: deepseek-official\n      api: deepseek-messages\n      base_url: https://api.deepseek.com/anthropic\n      api_key_env: DEEPSEEK-API-KEY\n      model: deepseek-flash\n",
		"database:\n  dsn: dsn\njwt:\n  secret_key: secret\ndsh:\n  route: effective\n  routes:\n    effective:\n      provider: deepseek-official\n      api: deepseek-messages\n      base_url: https://api.deepseek.com/anthropic\n      api_key: \"\"\n      model: deepseek-flash\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewConfig(path); err == nil {
			t.Fatalf("NewConfig(%q) succeeded", body)
		}
	}
}

func TestConfigRejectsDeprecatedDshEnvironmentVariables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("database:\n  dsn: dsn\njwt:\n  secret_key: secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"ANBAN_DSH_API_KEY",
		"ANBAN_DSH_BASE_URL",
		"ANBAN_DSH_MODEL",
		"ANBAN_DSH_PROVIDER",
		"ANBAN_DSH_PROFILE",
		"ANBAN_INTERACTIVE_BRIDGE_DIR",
		"ANBAN_INTERACTIVE_DSH_PATCH",
		"ANBAN_INTERACTIVE_MCP_URL",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "legacy-value")
			_, err := NewConfig(path)
			if err == nil || !strings.Contains(err.Error(), "deprecated environment variable "+name) {
				t.Fatalf("NewConfig() error = %v, want explicit rejection for %s", err, name)
			}
			if strings.Contains(err.Error(), "legacy-value") {
				t.Fatalf("NewConfig() leaked environment value: %v", err)
			}
		})
	}
}

func TestConfigRejectsInvalidDshRouteSelectionAndProviderFields(t *testing.T) {
	base := "database:\n  dsn: dsn\njwt:\n  secret_key: secret\ndsh:\n  enabled: true\n  route: %s\n  routes:\n    effective:\n      provider: %s\n      api: %s\n      base_url: %s\n      api_key: %s\n      model: %s\n"
	tests := []struct {
		name string
		yaml string
	}{
		{"missing selected route", fmt.Sprintf(base, "", "deepseek-official", "deepseek-messages", "https://api.deepseek.com/anthropic", "direct-api-key", "deepseek-flash")},
		{"unknown selected route", fmt.Sprintf(base, "quality", "deepseek-official", "deepseek-messages", "https://api.deepseek.com/anthropic", "direct-api-key", "deepseek-flash")},
		{"deepseek api mismatch", fmt.Sprintf(base, "effective", "deepseek-official", "anthropic-messages", "https://api.deepseek.com/anthropic", "direct-api-key", "deepseek-flash")},
		{"anthropic api mismatch", fmt.Sprintf(base, "effective", "anthropic", "deepseek-messages", "https://api.deepseek.com/anthropic", "direct-api-key", "deepseek-flash")},
		{"invalid base url", fmt.Sprintf(base, "effective", "anthropic", "anthropic-messages", "not-a-url", "direct-api-key", "deepseek-flash")},
		{"missing api key", fmt.Sprintf(base, "effective", "anthropic", "anthropic-messages", "https://api.deepseek.com/anthropic", "", "deepseek-flash")},
		{"missing model", fmt.Sprintf(base, "effective", "anthropic", "anthropic-messages", "https://api.deepseek.com/anthropic", "direct-api-key", "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewConfig(path); err == nil {
				t.Fatal("invalid DSH route accepted")
			}
		})
	}
}

func TestDisabledDshAllowsUnsetRouteAPIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := strings.Replace(validClaudeConfigYAML, "claude:\n", `dsh:
  enabled: false
  route: effective
  routes:
    effective:
      provider: deepseek-official
      api: deepseek-messages
      base_url: https://api.deepseek.com/anthropic
      api_key: ""
      model: deepseek-flash
claude:
`, 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewConfig(path); err != nil {
		t.Fatalf("disabled DSH rejected an unset route API key: %v", err)
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
