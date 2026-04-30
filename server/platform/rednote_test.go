package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRednoteFetchProfileResolvesShortLink(t *testing.T) {
	var shortLinkRequested bool
	var profileRequested bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "xhslink.com" && r.URL.Path == "/m/share":
			shortLinkRequested = true
			if r.Method != http.MethodGet {
				t.Fatalf("short link request method = %s, want GET", r.Method)
			}
			http.Redirect(w, r, "https://www.xiaohongshu.com/user/profile/abc123?xsec_token=test", http.StatusFound)
		case r.Host == "www.xiaohongshu.com" && strings.HasPrefix(r.URL.Path, "/user/profile/"):
			profileRequested = true
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>小红书 - 用户主页</title></head><body>
				<img class="avatar" src="https://sns-avatar-qc.xhscdn.com/avatar/test.jpg">
				<script>window.__INITIAL_STATE__={"nickname":"测试账号","desc":"专注 AI 写作","redId":"writer"}</script>
			</body></html>`))
		default:
			t.Fatalf("unexpected request: host=%s path=%s", r.Host, r.URL.Path)
		}
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := rewriteHostTransport{baseURL: baseURL, rt: http.DefaultTransport}
	provider := &RednoteProvider{
		client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
		},
		noRedirect: &http.Client{
			Timeout:       5 * time.Second,
			Transport:     transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
		},
	}

	profile, err := provider.FetchProfile(context.Background(), "https://xhslink.com/m/share")
	if err != nil {
		t.Fatalf("FetchProfile() error = %v", err)
	}
	if !shortLinkRequested || !profileRequested {
		t.Fatalf("shortLinkRequested=%v profileRequested=%v, want both true", shortLinkRequested, profileRequested)
	}
	if profile.Name != "测试账号" {
		t.Fatalf("profile.Name = %q, want 测试账号", profile.Name)
	}
	if profile.AvatarURL != "https://sns-avatar-qc.xhscdn.com/avatar/test.jpg" {
		t.Fatalf("profile.AvatarURL = %q", profile.AvatarURL)
	}
	if profile.Positioning != "专注 AI 写作" {
		t.Fatalf("profile.Positioning = %q, want 专注 AI 写作", profile.Positioning)
	}
}

func TestRednoteParseProfileFallbacks(t *testing.T) {
	t.Run("name before separator", func(t *testing.T) {
		profile := NewRednoteProvider().parseProfile(`<html><head><title>备用账号 - 小红书</title></head><body>
			<div class="avatar"><img src="https://sns-avatar-qc.xhscdn.com/avatar/fallback.jpg"></div>
		</body></html>`)

		if profile.Name != "备用账号" {
			t.Fatalf("profile.Name = %q, want 备用账号", profile.Name)
		}
		if profile.AvatarURL != "https://sns-avatar-qc.xhscdn.com/avatar/fallback.jpg" {
			t.Fatalf("profile.AvatarURL = %q", profile.AvatarURL)
		}
	})

	t.Run("xiaohongshu prefix", func(t *testing.T) {
		profile := NewRednoteProvider().parseProfile(`<html><head><title>小红书 - 真实账号的主页</title></head></html>`)

		if profile.Name != "真实账号" {
			t.Fatalf("profile.Name = %q, want 真实账号", profile.Name)
		}
	})
}

func TestRednoteURLPatternAllowsMobileProfile(t *testing.T) {
	if !rednoteURLPattern.MatchString("https://m.xiaohongshu.com/user/profile/abc") {
		t.Fatal("rednoteURLPattern should allow m.xiaohongshu.com profile URLs")
	}
}

type rewriteHostTransport struct {
	baseURL *url.URL
	rt      http.RoundTripper
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL = clone.URL.ResolveReference(&url.URL{})
	clone.URL.Scheme = t.baseURL.Scheme
	clone.URL.Host = t.baseURL.Host
	clone.Host = req.URL.Host
	return t.rt.RoundTrip(clone)
}
