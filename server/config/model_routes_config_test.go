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

func TestSemanticModelConfigRequiresUnderstandingTimeouts(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "image understanding",
			body: `
model_routes:
  image_understanding:
    provider: moonshot
    model: kimi-k2.7-code-highspeed
`,
			want: "model_routes.image_understanding.timeout must be positive",
		},
		{
			name: "video understanding",
			body: `
model_routes:
  video_understanding:
    provider: moonshot
    model: kimi-k2.7-code-highspeed
    require_native_video: true
`,
			want: "model_routes.video_understanding.timeout must be positive",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			pluginDir := fakePluginDir(t, dir)
			cfgPath := filepath.Join(dir, "config.yaml")
			body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
model_providers:
  moonshot:
    protocol: "openai_compatible"
    base_url: "https://api.example.com/v1"
    api_key: "sk-test"
claude:
  plugin_dir: "` + pluginDir + `"
` + tt.body)
			if err := os.WriteFile(cfgPath, body, 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := NewConfig(cfgPath)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestImageUnderstandingMaxImageEdge(t *testing.T) {
	for _, tt := range []struct {
		name    string
		block   string
		want    int
		wantErr string
	}{
		{name: "defaults when omitted", block: "", want: DefaultMaxImageEdge},
		{name: "explicit edge", block: "    max_image_edge: 2048\n", want: 2048},
		{name: "explicit zero disables downscaling", block: "    max_image_edge: 0\n", want: 0},
		{
			name:    "below the supported minimum",
			block:   "    max_image_edge: 100\n",
			wantErr: "model_routes.image_understanding.max_image_edge must be 0 or between 256 and 8192",
		},
		{
			name:    "above the supported maximum",
			block:   "    max_image_edge: 9000\n",
			wantErr: "model_routes.image_understanding.max_image_edge must be 0 or between 256 and 8192",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath := writeImageUnderstandingTestConfig(t, tt.block)
			cfg, err := NewConfig(cfgPath)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewConfig: %v", err)
			}
			if cfg.ImageUnderstanding.MaxImageEdge != tt.want {
				t.Fatalf("max image edge = %d, want %d", cfg.ImageUnderstanding.MaxImageEdge, tt.want)
			}
		})
	}
}

func writeImageUnderstandingTestConfig(t *testing.T, block string) string {
	t.Helper()
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
  access_expiry: 24h
  refresh_expiry: 168h
model_providers:
  moonshot:
    protocol: "openai_compatible"
    base_url: "https://api.example.com/v1"
    api_key: "sk-test"
model_routes:
  image_understanding:
    provider: moonshot
    model: kimi-k2.7-code-highspeed
    timeout: 90s
` + block + `claude:
  plugin_dir: "` + pluginDir + `"
  executor: docker
  execution_token_secret: 0123456789abcdef0123456789abcdef
  runtime_images:
    wechat-article: creator-agent-wechat-article:latest
    wechat-picture: creator-agent-wechat-picture:latest
    seednote: creator-agent-seednote:latest
    montage: creator-agent-montage:latest
    viral-ip-video-editor: creator-agent-viral-ip-video-editor:latest
    video-edit: creator-agent-video-edit:latest
    whiteboard-animation: creator-agent-whiteboard-animation:latest
    moments: creator-agent-moments:latest
    ecommerce: creator-agent-ecommerce:latest
    hypit: creator-agent-hypit:latest
`)
	if err := os.WriteFile(cfgPath, body, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func TestImageUnderstandingImageQuality(t *testing.T) {
	for _, tt := range []struct {
		name    string
		block   string
		want    int
		wantErr string
	}{
		{name: "defaults when omitted", block: "", want: DefaultImageQuality},
		{name: "explicit quality", block: "    image_quality: 92\n", want: 92},
		{
			name:    "zero is rejected",
			block:   "    image_quality: 0\n",
			wantErr: "model_routes.image_understanding.image_quality must be between 1 and 100",
		},
		{
			name:    "above the supported maximum",
			block:   "    image_quality: 101\n",
			wantErr: "model_routes.image_understanding.image_quality must be between 1 and 100",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := NewConfig(writeImageUnderstandingTestConfig(t, tt.block))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewConfig: %v", err)
			}
			if cfg.ImageUnderstanding.ImageQuality != tt.want {
				t.Fatalf("image quality = %d, want %d", cfg.ImageUnderstanding.ImageQuality, tt.want)
			}
		})
	}
}
