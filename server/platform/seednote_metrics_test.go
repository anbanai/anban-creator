package platform

import (
	"context"
	"fmt"
	"testing"

	"github.com/royalrick/anbanwriter/server/seednote"
)

func TestExtractSeednoteNoteID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"explore URL", "https://www.xiaohongshu.com/explore/65f123abc456?xsec_token=abc", "65f123abc456"},
		{"discovery item URL", "https://www.xiaohongshu.com/discovery/item/65f123abc456", "65f123abc456"},
		{"relative URL", "/explore/65f123abc456", "65f123abc456"},
		{"escaped slashes", `https:\/\/www.xiaohongshu.com\/discovery\/item\/65f123abc456?xsec_token=abc`, "65f123abc456"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractSeednoteNoteID(tt.raw); got != tt.want {
				t.Fatalf("ExtractSeednoteNoteID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeSeednoteMetricCount(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{"1.2万", 12000},
		{"3千", 3000},
		{"2,345", 2345},
		{"88", 88},
		{"点赞 456", 456},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := NormalizeSeednoteMetricCount(tt.raw); got != tt.want {
				t.Fatalf("NormalizeSeednoteMetricCount(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseCountString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"plain number", "500", 500},
		{"wan", "1.5万", 15000},
		{"qian", "3千", 3000},
		{"empty", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseCountString(tt.in); got != tt.want {
				t.Fatalf("parseCountString(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestMapFeedsToPosts(t *testing.T) {
	feeds := []seednote.Feed{
		{
			ID: "f1", XsecToken: "t1",
			NoteCard: seednote.NoteCard{
				DisplayTitle: "爆款标题",
				User:         seednote.User{UserID: "u1", Nickname: "author1"},
				InteractInfo: seednote.InteractInfo{LikedCount: "1.2万", CollectedCount: "3000", CommentCount: "500", SharedCount: "100"},
				Cover:        seednote.Cover{URLDefault: "https://example.com/cover.jpg"},
			},
		},
		{
			ID: "f2", XsecToken: "",
			NoteCard: seednote.NoteCard{
				DisplayTitle: "普通标题",
				User:         seednote.User{UserID: "u2", Nickname: "author2"},
				InteractInfo: seednote.InteractInfo{LikedCount: "50", CollectedCount: "10", CommentCount: "5", SharedCount: "1"},
			},
		},
	}

	posts := mapFeedsToPosts(feeds)
	if len(posts) != 2 {
		t.Fatalf("len(posts) = %d, want 2", len(posts))
	}

	// First feed should have full URL with token.
	if posts[0].NoteID != "f1" {
		t.Fatalf("posts[0].NoteID = %q, want f1", posts[0].NoteID)
	}
	expectedURL := fmt.Sprintf("https://www.xiaohongshu.com/explore/f1?xsec_token=t1")
	if posts[0].URL != expectedURL {
		t.Fatalf("posts[0].URL = %q, want %q", posts[0].URL, expectedURL)
	}
	if posts[0].LikeCount != 12000 {
		t.Fatalf("posts[0].LikeCount = %d, want 12000", posts[0].LikeCount)
	}

	// Second feed without token should have URL without token.
	if posts[1].URL != "https://www.xiaohongshu.com/explore/f2" {
		t.Fatalf("posts[1].URL = %q, want URL without token", posts[1].URL)
	}
}

func TestMapUserProfile(t *testing.T) {
	profile := &seednote.UserProfile{
		UserBasicInfo: seednote.UserBasicInfo{
			Nickname: "种草笔记用户",
			RedID:    "red123",
			Desc:     "AI 内容创作者",
			Avatar:   "https://example.com/avatar.jpg",
		},
		Interactions: []seednote.UserInteractions{
			{Type: "follows", Name: "关注", Count: "200"},
			{Type: "fans", Name: "粉丝", Count: "10000"},
			{Type: "interaction", Name: "获赞与收藏", Count: "5万"},
		},
		Feeds: []seednote.Feed{
			{
				ID: "f1", XsecToken: "t1",
				NoteCard: seednote.NoteCard{
					DisplayTitle: "笔记1",
					User:         seednote.User{UserID: "u1", Nickname: "a1"},
					InteractInfo: seednote.InteractInfo{LikedCount: "1000", CollectedCount: "500", CommentCount: "200"},
				},
			},
		},
	}

	result := mapUserProfile(profile, "https://www.xiaohongshu.com/user/profile/test")
	if result.Name != "种草笔记用户" {
		t.Fatalf("Name = %q, want 种草笔记用户", result.Name)
	}
	if result.AvatarURL != "https://example.com/avatar.jpg" {
		t.Fatalf("AvatarURL = %q, want https://example.com/avatar.jpg", result.AvatarURL)
	}
	if result.RawData["red_id"] != "red123" {
		t.Fatalf("RawData[red_id] = %v, want red123", result.RawData["red_id"])
	}
	if result.RawData["fans"] != "10000" {
		t.Fatalf("RawData[fans] = %v, want 10000", result.RawData["fans"])
	}

	posts, ok := result.RawData["posts"].([]SeednotePost)
	if !ok || len(posts) != 1 {
		t.Fatalf("len(posts) = %v, want 1", len(posts))
	}
	if posts[0].Title != "笔记1" {
		t.Fatalf("post title = %q, want 笔记1", posts[0].Title)
	}
}

func TestResolveNoteURL(t *testing.T) {
	// Valid explore URL with token
	feedID, token, err := resolveNoteURL("https://www.xiaohongshu.com/explore/abc123?xsec_token=xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if feedID != "abc123" {
		t.Fatalf("feedID = %q, want abc123", feedID)
	}
	if token != "xyz" {
		t.Fatalf("xsecToken = %q, want xyz", token)
	}

	// Valid explore URL without token
	feedID, token, err = resolveNoteURL("https://www.xiaohongshu.com/explore/abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if feedID != "abc123" {
		t.Fatalf("feedID = %q, want abc123", feedID)
	}
	if token != "" {
		t.Fatalf("xsecToken = %q, want empty", token)
	}

	// Invalid URL
	_, _, err = resolveNoteURL("https://example.com/other")
	if err == nil {
		t.Fatal("expected error for non-seednote URL")
	}
}

func TestFetchProfileEmptyURL(t *testing.T) {
	_, err := NewSeednoteProvider(nil).FetchProfile(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestFetchProfilePostsEmptyURL(t *testing.T) {
	_, err := NewSeednoteProvider(nil).FetchProfilePosts(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestFetchNoteContentEmptyURL(t *testing.T) {
	_, err := NewSeednoteProvider(nil).FetchNoteContent(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestFetchPostMetricsEmptyURL(t *testing.T) {
	_, err := NewSeednoteProvider(nil).FetchPostMetrics(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestFetchPostMetricsInvalidNoteURL(t *testing.T) {
	provider := NewSeednoteProvider(nil)
	_, err := provider.FetchPostMetrics(context.Background(), "https://example.com/other")
	if err == nil {
		t.Fatal("expected error for non-seednote URL")
	}
}
