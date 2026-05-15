package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/royalrick/anbanwriter/server/seednote"
)

// NOTE: TestExtractSeednoteNoteID and TestNormalizeSeednoteMetricCount are in seednote_metrics_test.go.

func TestSeednoteFetchProfileRejectsTextWithoutSupportedURL(t *testing.T) {
	_, err := NewSeednoteProvider(nil).FetchProfile(context.Background(), "这里只是一段没有链接的分享文案")
	if err == nil {
		t.Fatal("FetchProfile() error = nil, want error")
	}
	if err.Error() != "profile text must contain a supported seednote URL" {
		t.Fatalf("FetchProfile() error = %q, want supported URL hint", err.Error())
	}
}

func TestExtractSeednoteURLFromShareText(t *testing.T) {
	got, err := extractSeednoteURLFromText("12 分享给你一个账号 https://xhslink.com/a/b?token=1，复制打开看看")
	if err != nil {
		t.Fatalf("extractSeednoteURLFromText() error = %v", err)
	}
	if got != "https://xhslink.com/a/b?token=1" {
		t.Fatalf("extractSeednoteURLFromText() = %q", got)
	}
}

func TestExtractSeednoteURLFromTextStartingWithURL(t *testing.T) {
	got, err := extractSeednoteURLFromText("https://www.xiaohongshu.com/user/profile/abc?xsec_token=test 这个账号很适合参考")
	if err != nil {
		t.Fatalf("extractSeednoteURLFromText() error = %v", err)
	}
	if got != "https://www.xiaohongshu.com/user/profile/abc?xsec_token=test" {
		t.Fatalf("extractSeednoteURLFromText() = %q", got)
	}
}

func TestSelectTopSeednotePostsRanksByEngagement(t *testing.T) {
	posts := []SeednotePost{
		{Title: "first", LikeCount: 10, EngagementScore: 10},
		{Title: "best", LikeCount: 20, CollectCount: 3, CommentCount: 2, EngagementScore: 25},
		{Title: "middle", LikeCount: 12, EngagementScore: 12},
	}
	got := selectTopSeednotePosts(posts, 2)
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

// --- Integration tests with mock Seednote sidecar ---

func setupMockSeednoteServer() (*httptest.Server, *seednote.Client) {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/v1/user/profile", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(seednote.APIResponse[seednote.UserProfile]{
			Success: true,
			Data: seednote.UserProfile{
				UserBasicInfo: seednote.UserBasicInfo{
					Nickname: "测试用户",
					RedID:    "red_test_123",
					Desc:     "专注 AI 创作领域",
					Avatar:   "https://example.com/avatar.jpg",
				},
				Interactions: []seednote.UserInteractions{
					{Type: "follows", Name: "关注", Count: "100"},
					{Type: "fans", Name: "粉丝", Count: "5000"},
					{Type: "interaction", Name: "获赞与收藏", Count: "1.2万"},
				},
				Feeds: []seednote.Feed{
					{
						ID: "feed1", XsecToken: "token1",
						NoteCard: seednote.NoteCard{
							DisplayTitle: "爆款笔记",
							User:         seednote.User{UserID: "u1", Nickname: "author1"},
							InteractInfo: seednote.InteractInfo{LikedCount: "1.5万", CollectedCount: "8000", CommentCount: "500"},
						},
					},
					{
						ID: "feed2", XsecToken: "token2",
						NoteCard: seednote.NoteCard{
							DisplayTitle: "普通笔记",
							User:         seednote.User{UserID: "u2", Nickname: "author2"},
							InteractInfo: seednote.InteractInfo{LikedCount: "100", CollectedCount: "50", CommentCount: "10"},
						},
					},
				},
			},
		})
	})

	mux.HandleFunc("/api/v1/feeds/detail", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(seednote.APIResponse[seednote.FeedDetail]{
			Success: true,
			Data: seednote.FeedDetail{
				Note: seednote.FeedNote{
					NoteID: "note123",
					Title:  "测试笔记标题",
					Desc:   "这是一篇关于 #AI写作 的笔记内容",
					Type:   "normal",
					User:   seednote.User{UserID: "u1", Nickname: "测试作者"},
					InteractInfo: seednote.InteractInfo{LikedCount: "500", CollectedCount: "200", CommentCount: "50", SharedCount: "30"},
					ImageList: []seednote.DetailImage{
						{Width: 1080, Height: 1440, URLDefault: "https://example.com/img1.jpg"},
					},
				},
				Comments: seednote.CommentList{
					List:    []seednote.Comment{},
					HasMore: false,
				},
			},
		})
	})

	mux.HandleFunc("/api/v1/login/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(seednote.LoginStatusResponse{Success: true, LoggedIn: true})
	})

	server := httptest.NewServer(mux)
	client := seednote.NewClient(server.URL, 0)
	return server, client
}

func TestFetchProfileViaSDK(t *testing.T) {
	_, client := setupMockSeednoteServer()
	provider := NewSeednoteProvider(client)

	profile, err := provider.FetchProfile(context.Background(), "https://www.xiaohongshu.com/user/profile/test_user?xsec_token=abc")
	if err != nil {
		t.Fatalf("FetchProfile() error = %v", err)
	}

	if profile.Name != "测试用户" {
		t.Fatalf("Name = %q, want 测试用户", profile.Name)
	}
	if profile.AvatarURL != "https://example.com/avatar.jpg" {
		t.Fatalf("AvatarURL = %q, want https://example.com/avatar.jpg", profile.AvatarURL)
	}

	fans, ok := profile.RawData["fans"].(string)
	if !ok || fans != "5000" {
		t.Fatalf("RawData[fans] = %v, want 5000", profile.RawData["fans"])
	}

	posts, ok := profile.RawData["posts"].([]SeednotePost)
	if !ok || len(posts) != 2 {
		t.Fatalf("len(posts) = %d, want 2", len(posts))
	}
	if posts[0].Title != "爆款笔记" {
		t.Fatalf("first post title = %q, want 爆款笔记", posts[0].Title)
	}
}

func TestFetchProfilePostsViaSDK(t *testing.T) {
	_, client := setupMockSeednoteServer()
	provider := NewSeednoteProvider(client)

	posts, err := provider.FetchProfilePosts(context.Background(), "https://www.xiaohongshu.com/user/profile/test_user?xsec_token=abc")
	if err != nil {
		t.Fatalf("FetchProfilePosts() error = %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("len(posts) = %d, want 2", len(posts))
	}
	if posts[0].NoteID != "feed1" {
		t.Fatalf("first post NoteID = %q, want feed1", posts[0].NoteID)
	}
	if posts[0].LikeCount != 15000 {
		t.Fatalf("first post LikeCount = %d, want 15000", posts[0].LikeCount)
	}
}

func TestFetchNoteContentViaSDK(t *testing.T) {
	_, client := setupMockSeednoteServer()
	provider := NewSeednoteProvider(client)

	content, err := provider.FetchNoteContent(context.Background(), "https://www.xiaohongshu.com/explore/note123?xsec_token=abc")
	if err != nil {
		t.Fatalf("FetchNoteContent() error = %v", err)
	}
	if content.Title != "测试笔记标题" {
		t.Fatalf("Title = %q, want 测试笔记标题", content.Title)
	}
	if content.AuthorName != "测试作者" {
		t.Fatalf("AuthorName = %q, want 测试作者", content.AuthorName)
	}
	if content.Tags == "" {
		t.Fatal("Tags should not be empty")
	}
	if !contains(content.Tags, "AI写作") {
		t.Fatalf("Tags should contain AI写作, got %q", content.Tags)
	}
}

func TestFetchPostMetricsViaSDK(t *testing.T) {
	_, client := setupMockSeednoteServer()
	provider := NewSeednoteProvider(client)

	metrics, err := provider.FetchPostMetrics(context.Background(), "https://www.xiaohongshu.com/explore/note123?xsec_token=abc")
	if err != nil {
		t.Fatalf("FetchPostMetrics() error = %v", err)
	}
	if metrics.LikeCount != 500 {
		t.Fatalf("LikeCount = %d, want 500", metrics.LikeCount)
	}
	if metrics.CollectCount != 200 {
		t.Fatalf("CollectCount = %d, want 200", metrics.CollectCount)
	}
	if metrics.CommentCount != 50 {
		t.Fatalf("CommentCount = %d, want 50", metrics.CommentCount)
	}
	if metrics.ShareCount != 30 {
		t.Fatalf("ShareCount = %d, want 30", metrics.ShareCount)
	}
}

func TestResolveProfileURL(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantUserID string
		wantToken  string
		wantErr    bool
	}{
		{name: "standard", url: "https://www.xiaohongshu.com/user/profile/abc123?xsec_token=token", wantUserID: "abc123", wantToken: "token"},
		{name: "no token", url: "https://www.xiaohongshu.com/user/profile/abc123", wantUserID: "abc123", wantToken: ""},
	{name: "no user segment", url: "https://example.com/", wantErr: true},
		{name: "empty", url: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID, token, err := seednote.ResolveProfileURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if userID != tt.wantUserID {
				t.Fatalf("userID = %q, want %q", userID, tt.wantUserID)
			}
			if token != tt.wantToken {
				t.Fatalf("token = %q, want %q", token, tt.wantToken)
			}
		})
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
