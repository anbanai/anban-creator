package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemanticModelConfigRejectsDeprecatedVision(t *testing.T) {
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
vision:
  base_url: "https://api.example.com/v1"
  key: "sk-test"
  model: "old-vision"
claude:
  plugin_dir: "` + pluginDir + `"
`)
	if err := os.WriteFile(cfgPath, body, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewConfig(cfgPath)
	if err == nil {
		t.Fatal("NewConfig() succeeded, want deprecated vision error")
	}
	if !strings.Contains(err.Error(), "deprecated top-level config key vision") {
		t.Fatalf("error = %v, want deprecated vision hint", err)
	}
}

func TestSemanticModelConfigRejectsUnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
image_api_typo:
  content:
    sizes:
      article_cover: "1:1"
claude:
  plugin_dir: "` + pluginDir + `"
`)
	if err := os.WriteFile(cfgPath, body, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewConfig(cfgPath)
	if err == nil {
		t.Fatal("NewConfig() succeeded, want unknown key error")
	}
	if !strings.Contains(err.Error(), "unknown top-level config key image_api_typo") {
		t.Fatalf("error = %v, want unknown key hint", err)
	}
}

func TestSemanticModelConfigRejectsWritingRoute(t *testing.T) {
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
model_routes:
  writing:
    provider: moonshot
    model: kimi-k2.7-code
claude:
  plugin_dir: "` + pluginDir + `"
`)
	if err := os.WriteFile(cfgPath, body, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewConfig(cfgPath)
	if err == nil {
		t.Fatal("NewConfig() succeeded, want model_routes.writing error")
	}
	if !strings.Contains(err.Error(), "model_routes.writing") {
		t.Fatalf("error = %v, want model_routes.writing hint", err)
	}
}

func TestSemanticModelConfigRejectsNonNativeVideoUnderstandingRoute(t *testing.T) {
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
model_routes:
  video_understanding:
    provider: moonshot
    model: kimi-k2.7-code-highspeed
    require_native_video: false
claude:
  plugin_dir: "` + pluginDir + `"
`)
	if err := os.WriteFile(cfgPath, body, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewConfig(cfgPath)
	if err == nil {
		t.Fatal("NewConfig() succeeded, want native-video requirement error")
	}
	if !strings.Contains(err.Error(), "model_routes.video_understanding.require_native_video must be true") {
		t.Fatalf("error = %v, want native-video requirement hint", err)
	}
}

func fakePluginDir(t *testing.T, dir string) string {
	t.Helper()
	pluginDir := filepath.Join(dir, "plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, "agents"), 0755); err != nil {
		t.Fatalf("create fake plugin dir: %v", err)
	}
	return pluginDir
}
