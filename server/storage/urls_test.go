package storage

import "testing"

func TestStorageKeyFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantKey string
		wantOk  bool
	}{
		{"local relative", "/api/v1/files/uploads/references/user1/abc.png", "uploads/references/user1/abc.png", true},
		{"local relative with query stripped", "/api/v1/files/uploads/references/user1/abc.png?x=1", "uploads/references/user1/abc.png", true},
		{"oss default domain", "https://bucket.oss-cn-hangzhou.aliyuncs.com/uploads/projects/user1/xyz.jpg", "uploads/projects/user1/xyz.jpg", true},
		{"oss custom domain", "https://cdn.example.com/user1/designer/gen1/0.png", "user1/designer/gen1/0.png", true},
		{"oss with query string", "https://bucket.oss-cn-hangzhou.aliyuncs.com/key.png?Expires=1&Signature=abc", "key.png", true},
		{"empty string", "", "", false},
		{"bare path without prefix", "/random/path", "random/path", true},
		{"root only", "/", "", false},
		{"local root after prefix", "/api/v1/files/", "", false},
		{"garbage", "://not-a-url", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotOk := StorageKeyFromURL(tt.url)
			if gotKey != tt.wantKey || gotOk != tt.wantOk {
				t.Errorf("StorageKeyFromURL(%q) = (%q, %v), want (%q, %v)", tt.url, gotKey, gotOk, tt.wantKey, tt.wantOk)
			}
		})
	}
}
