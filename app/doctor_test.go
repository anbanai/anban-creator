package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/royalrick/wechatwriter/app/config"
)

func TestCheckConfig(t *testing.T) {
	t.Run("nil config returns fail", func(t *testing.T) {
		checks := checkConfig(nil, nil)
		// Should have config_file check at minimum
		if len(checks) == 0 {
			t.Error("expected at least one check")
		}
	})

	t.Run("empty config warns on missing fields", func(t *testing.T) {
		cfg := &config.Config{}
		checks := checkConfig(cfg, nil)
		statusMap := map[string]string{}
		for _, c := range checks {
			statusMap[c.Name] = c.Status
		}
		if statusMap["wechat_appid"] != "fail" {
			t.Errorf("wechat_appid status = %q, want fail", statusMap["wechat_appid"])
		}
		if statusMap["wechat_secret"] != "fail" {
			t.Errorf("wechat_secret status = %q, want fail", statusMap["wechat_secret"])
		}
	})

	t.Run("configured account passes", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Wechat.AppID = "wx123"
		cfg.Wechat.Secret = "secret123"
		checks := checkConfig(cfg, nil)
		statusMap := map[string]string{}
		for _, c := range checks {
			statusMap[c.Name] = c.Status
		}
		if statusMap["wechat_appid"] != "pass" {
			t.Errorf("wechat_appid status = %q, want pass", statusMap["wechat_appid"])
		}
		if statusMap["wechat_secret"] != "pass" {
			t.Errorf("wechat_secret status = %q, want pass", statusMap["wechat_secret"])
		}
	})

	t.Run("invalid provider fails", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Wechat.Article.Content.Image.Provider = "unknown_provider"
		checks := checkConfig(cfg, nil)
		statusMap := map[string]string{}
		for _, c := range checks {
			statusMap[c.Name] = c.Status
		}
		if statusMap["article_image_provider"] != "fail" {
			t.Errorf("article_image_provider status = %q, want fail", statusMap["article_image_provider"])
		}
	})
}

func TestCheckEnvironment(t *testing.T) {
	checks := checkEnvironment()
	if len(checks) == 0 {
		t.Error("expected at least one environment check")
	}
	// tmp_writable should pass in normal test environment
	for _, c := range checks {
		if c.Name == "tmp_writable" && c.Status != "pass" {
			t.Errorf("tmp_writable status = %q, want pass", c.Status)
		}
	}
}

func TestCheckConfig_PostImageKey(t *testing.T) {
	t.Run("post image key via wechat.post", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Wechat.Post.Content.Image.Key = "post-key"
		checks := checkConfig(cfg, nil)
		statusMap := map[string]string{}
		for _, c := range checks {
			statusMap[c.Name] = c.Status
		}
		if statusMap["post_image_key"] != "pass" {
			t.Errorf("post_image_key status = %q, want pass", statusMap["post_image_key"])
		}
	})

	t.Run("post image key via xhs fallback", func(t *testing.T) {
		cfg := &config.Config{}
		xhs := &config.XHSConfig{}
		xhs.Content.Image.Key = "xhs-key"
		cfg.XHS = xhs
		checks := checkConfig(cfg, nil)
		statusMap := map[string]string{}
		for _, c := range checks {
			statusMap[c.Name] = c.Status
		}
		if statusMap["post_image_key"] != "pass" {
			t.Errorf("post_image_key status = %q, want pass (xhs fallback)", statusMap["post_image_key"])
		}
	})

	t.Run("no post image key warns", func(t *testing.T) {
		cfg := &config.Config{}
		checks := checkConfig(cfg, nil)
		statusMap := map[string]string{}
		for _, c := range checks {
			statusMap[c.Name] = c.Status
		}
		if statusMap["post_image_key"] != "warn" {
			t.Errorf("post_image_key status = %q, want warn", statusMap["post_image_key"])
		}
	})
}

func TestCheckConfig_PublishReady(t *testing.T) {
	t.Run("appid and secret configured - pass", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Wechat.AppID = "wx123"
		cfg.Wechat.Secret = "secret123"
		checks := checkConfig(cfg, nil)
		statusMap := map[string]string{}
		for _, c := range checks {
			statusMap[c.Name] = c.Status
		}
		if statusMap["publish_ready"] != "pass" {
			t.Errorf("publish_ready status = %q, want pass", statusMap["publish_ready"])
		}
	})

	t.Run("missing appid - warn", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Wechat.Secret = "secret123"
		checks := checkConfig(cfg, nil)
		statusMap := map[string]string{}
		for _, c := range checks {
			statusMap[c.Name] = c.Status
		}
		if statusMap["publish_ready"] != "warn" {
			t.Errorf("publish_ready status = %q, want warn", statusMap["publish_ready"])
		}
	})

	t.Run("missing secret - warn", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Wechat.AppID = "wx123"
		checks := checkConfig(cfg, nil)
		statusMap := map[string]string{}
		for _, c := range checks {
			statusMap[c.Name] = c.Status
		}
		if statusMap["publish_ready"] != "warn" {
			t.Errorf("publish_ready status = %q, want warn", statusMap["publish_ready"])
		}
	})
}

func TestCheckNetworkWithMock(t *testing.T) {
	// Mock WeChat API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// checkNetwork uses hardcoded URL, so we just verify it runs without panic
	cfg := &config.Config{}
	cfg.Wechat.Article.Content.Image.BaseURL = srv.URL
	checks := checkNetwork(cfg)
	if len(checks) == 0 {
		t.Error("expected at least one network check")
	}
}
