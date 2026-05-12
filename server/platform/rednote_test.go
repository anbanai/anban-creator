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
				<script>window.__INITIAL_STATE__={"user":{"nickname":"测试账号","desc":"专注 AI 写作","image":"https://sns-avatar-qc.xhscdn.com/avatar/test.jpg","userid":"abc123"}}</script>
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

	profile, err := provider.FetchProfile(context.Background(), "快来看这个账号 https://xhslink.com/m/share 真的很会写")
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

func TestRednoteFetchProfileRejectsTextWithoutSupportedURL(t *testing.T) {
	_, err := NewRednoteProvider().FetchProfile(context.Background(), "这里只是一段没有链接的分享文案")
	if err == nil {
		t.Fatal("FetchProfile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "supported xiaohongshu URL") {
		t.Fatalf("FetchProfile() error = %q, want supported URL hint", err.Error())
	}
}

func TestRednoteParseProfileFromInitialState(t *testing.T) {
	t.Run("user subtree in INITIAL_STATE", func(t *testing.T) {
		html := `<html><head><title>小红书 - 用户主页</title></head><body>
			<meta property="og:title" content="小红书 - 你的生活兴趣社区">
			<script>window.__INITIAL_STATE__={"user":{"nickname":"旺财云","desc":"专业云计算服务商","image":"https://sns-avatar-qc.xhscdn.com/abc.jpg","userid":"68a58280000000001a023fcf"}}</script>
		</body></html>`

		profile := NewRednoteProvider().parseProfile(html)
		if profile.Name != "旺财云" {
			t.Fatalf("profile.Name = %q, want 旺财云", profile.Name)
		}
		if profile.Positioning != "专业云计算服务商" {
			t.Fatalf("profile.Positioning = %q, want 专业云计算服务商", profile.Positioning)
		}
		if profile.AvatarURL != "https://sns-avatar-qc.xhscdn.com/abc.jpg" {
			t.Fatalf("profile.AvatarURL = %q", profile.AvatarURL)
		}
	})

	t.Run("does not return platform name when user subtree exists", func(t *testing.T) {
		// The page has "小红书" as og:title and "你的生活兴趣社区" in meta,
		// but the __INITIAL_STATE__ user subtree should take precedence.
		html := `<html><head>
			<title>小红书 - 你的生活兴趣社区</title>
			<meta property="og:nickname" content="你的生活兴趣社区">
			<script>window.__INITIAL_STATE__={"user":{"nickname":"旺财云","desc":"专业云计算服务商","image":"https://sns-avatar-qc.xhscdn.com/abc.jpg"}}</script>
		</head><body></body></html>`

		profile := NewRednoteProvider().parseProfile(html)
		if profile.Name != "旺财云" {
			t.Fatalf("profile.Name = %q, want 旺财云 (user data, not platform data)", profile.Name)
		}
		if profile.Positioning != "专业云计算服务商" {
			t.Fatalf("profile.Positioning = %q, want 专业云计算服务商", profile.Positioning)
		}
	})

	t.Run("falls back to title tag when no INITIAL_STATE", func(t *testing.T) {
		html := `<html><head><title>备用账号 - 小红书</title></head><body>
			<div class="avatar"><img src="https://sns-avatar-qc.xhscdn.com/avatar/fallback.jpg"></div>
		</body></html>`

		profile := NewRednoteProvider().parseProfile(html)
		if profile.Name != "备用账号" {
			t.Fatalf("profile.Name = %q, want 备用账号", profile.Name)
		}
		if profile.AvatarURL != "https://sns-avatar-qc.xhscdn.com/avatar/fallback.jpg" {
			t.Fatalf("profile.AvatarURL = %q", profile.AvatarURL)
		}
	})

	t.Run("xiaohongshu prefix in title", func(t *testing.T) {
		html := `<html><head><title>小红书 - 真实账号的主页</title></head></html>`

		profile := NewRednoteProvider().parseProfile(html)
		if profile.Name != "真实账号" {
			t.Fatalf("profile.Name = %q, want 真实账号", profile.Name)
		}
	})
}

func TestRednoteParseProfileIncludesRankedTopPosts(t *testing.T) {
	html := `<html><head><title>小红书 - 用户主页</title></head><body>
			<script>window.__INITIAL_STATE__={"user":{"nickname":"测试账号","desc":"专注 AI 写作"}}</script>
			<section>
				<a href="/explore/one"><span>普通标题</span><span>点赞 10</span></a>
				<a href="/explore/two"><span>爆款标题</span><span>点赞 1.2万</span><span>评论 300</span></a>
				<a href="/explore/three"><span>中等标题</span><span>收藏 800</span></a>
			</section>
		</body></html>`

	profile := NewRednoteProvider().parseProfile(html)
	rawPosts, ok := profile.RawData["top_posts"].([]RednotePost)
	if !ok {
		t.Fatalf("RawData[top_posts] type = %T, want []RednotePost", profile.RawData["top_posts"])
	}
	if len(rawPosts) < 2 {
		t.Fatalf("len(top_posts) = %d, want at least 2", len(rawPosts))
	}
	if rawPosts[0].Title != "爆款标题" {
		t.Fatalf("top post title = %q, want 爆款标题", rawPosts[0].Title)
	}
}

func TestRednoteURLPatternAllowsMobileProfile(t *testing.T) {
	if !rednoteURLPattern.MatchString("https://m.xiaohongshu.com/user/profile/abc") {
		t.Fatal("rednoteURLPattern should allow m.xiaohongshu.com profile URLs")
	}
}

func TestExtractRednoteURLFromShareText(t *testing.T) {
	got, err := extractRednoteURLFromText("12 分享给你一个账号 https://xhslink.com/a/b?token=1，复制打开看看")
	if err != nil {
		t.Fatalf("extractRednoteURLFromText() error = %v", err)
	}
	if got != "https://xhslink.com/a/b?token=1" {
		t.Fatalf("extractRednoteURLFromText() = %q", got)
	}
}

func TestExtractRednoteURLFromTextStartingWithURL(t *testing.T) {
	got, err := extractRednoteURLFromText("https://www.xiaohongshu.com/user/profile/abc?xsec_token=test 这个账号很适合参考")
	if err != nil {
		t.Fatalf("extractRednoteURLFromText() error = %v", err)
	}
	if got != "https://www.xiaohongshu.com/user/profile/abc?xsec_token=test" {
		t.Fatalf("extractRednoteURLFromText() = %q", got)
	}
}

func TestParseRednoteCount(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "plain", in: "123", want: 123},
		{name: "comma", in: "1,234", want: 1234},
		{name: "wan", in: "1.2万", want: 12000},
		{name: "qian", in: "3千", want: 3000},
		{name: "noise", in: "点赞 8.5万", want: 85000},
		{name: "empty", in: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRednoteCount(tt.in); got != tt.want {
				t.Fatalf("parseRednoteCount(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestSelectTopRednotePostsRanksByEngagement(t *testing.T) {
	posts := []RednotePost{
		{Title: "first", LikeCount: 10},
		{Title: "best", LikeCount: 20, CollectCount: 3, CommentCount: 2},
		{Title: "middle", LikeCount: 12},
	}

	got := selectTopRednotePosts(posts, 2)
	if len(got) != 2 {
		t.Fatalf("len(top posts) = %d, want 2", len(got))
	}
	if got[0].Title != "best" || got[1].Title != "middle" {
		t.Fatalf("top posts order = [%s, %s], want [best, middle]", got[0].Title, got[1].Title)
	}
	if got[0].EngagementScore != 25 {
		t.Fatalf("EngagementScore = %d, want 25", got[0].EngagementScore)
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
