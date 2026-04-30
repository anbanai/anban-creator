package platform

import "testing"

func TestExtractRednoteNoteID(t *testing.T) {
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
			if got := ExtractRednoteNoteID(tt.raw); got != tt.want {
				t.Fatalf("ExtractRednoteNoteID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeRednoteMetricCount(t *testing.T) {
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
			if got := NormalizeRednoteMetricCount(tt.raw); got != tt.want {
				t.Fatalf("NormalizeRednoteMetricCount(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseRednotePostsIncludesNoteIDAndMetrics(t *testing.T) {
	html := `
	<a href="/explore/65f123abc456">
		<img src="https://img.example/cover.jpg">
		<span>早起效率翻倍的方法</span>
		<span>点赞 1.2万</span>
		<span>收藏 300</span>
		<span>评论 45</span>
		<span>分享 6</span>
	</a>`

	posts := parseRednotePosts(html)
	if len(posts) != 1 {
		t.Fatalf("len(posts) = %d, want 1", len(posts))
	}
	post := posts[0]
	if post.NoteID != "65f123abc456" {
		t.Fatalf("NoteID = %q, want 65f123abc456", post.NoteID)
	}
	if post.URL != "https://www.xiaohongshu.com/explore/65f123abc456" {
		t.Fatalf("URL = %q", post.URL)
	}
	if post.LikeCount != 12000 || post.CollectCount != 300 || post.CommentCount != 45 || post.ShareCount != 6 {
		t.Fatalf("metrics = %+v", post)
	}
}

func TestParseRednotePostMetrics(t *testing.T) {
	html := `
	<html>
		<body>
			<div>点赞 123</div>
			<div>收藏 45</div>
			<div>评论 6</div>
			<div>分享 2</div>
		</body>
	</html>`

	metrics := parseRednotePostMetrics(html)
	if metrics.LikeCount != 123 || metrics.CollectCount != 45 || metrics.CommentCount != 6 || metrics.ShareCount != 2 {
		t.Fatalf("metrics = %+v", metrics)
	}
	if metrics.ViewCount != nil {
		t.Fatalf("ViewCount = %v, want nil", *metrics.ViewCount)
	}
}
