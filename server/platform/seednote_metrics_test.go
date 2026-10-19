package platform

import "testing"

func TestExtractSeednoteNoteID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "explore URL", raw: "https://www.xiaohongshu.com/explore/65f123abc456?xsec_token=abc", want: "65f123abc456"},
		{name: "discovery item URL", raw: "https://www.xiaohongshu.com/discovery/item/65f123abc456", want: "65f123abc456"},
		{name: "relative URL", raw: "/explore/65f123abc456", want: "65f123abc456"},
		{name: "escaped slashes", raw: `https:\/\/www.xiaohongshu.com\/discovery\/item\/65f123abc456?xsec_token=abc`, want: "65f123abc456"},
		{name: "empty", raw: "", want: ""},
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
		{raw: "1.2万", want: 12000},
		{raw: "3千", want: 3000},
		{raw: "2,345", want: 2345},
		{raw: "88", want: 88},
		{raw: "点赞 456", want: 456},
		{raw: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := NormalizeSeednoteMetricCount(tt.raw); got != tt.want {
				t.Fatalf("NormalizeSeednoteMetricCount(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}
