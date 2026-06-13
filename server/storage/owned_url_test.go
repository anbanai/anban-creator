package storage

import "testing"

func TestOSSProvider_IsOwnedURL(t *testing.T) {
	p := &OSSProvider{
		bucketName: "anbancreator",
		endpoint:   "oss-cn-chengdu.aliyuncs.com",
	}

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"signed url", "https://anbancreator.oss-cn-chengdu.aliyuncs.com/user1/designer/gen1/0.png?Expires=1&Signature=abc", true},
		{"uppercase host", "https://ANBANCREATOR.oss-cn-chengdu.aliyuncs.com/x.png", true},
		{"trailing dot in host", "https://anbancreator.oss-cn-chengdu.aliyuncs.com./x.png", true},
		{"explicit port", "https://anbancreator.oss-cn-chengdu.aliyuncs.com:443/x.png", true},
		{"subdomain spoof", "https://anbancreator.oss-cn-chengdu.aliyuncs.com.evil.com/x", false},
		{"query string spoof", "https://evil.com/?fake=anbancreator.oss-cn-chengdu.aliyuncs.com", false},
		{"wrong bucket same endpoint", "https://otherbucket.oss-cn-chengdu.aliyuncs.com/x", false},
		{"right bucket wrong endpoint", "https://anbancreator.oss-cn-beijing.aliyuncs.com/x", false},
		{"ftp scheme rejected", "ftp://anbancreator.oss-cn-chengdu.aliyuncs.com/x", false},
		{"file scheme rejected", "file:///etc/passwd", false},
		{"relative url rejected", "/api/v1/files/x", false},
		{"empty string", "", false},
		{"garbage", "://not-a-url", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.IsOwnedURL(tt.url); got != tt.want {
				t.Errorf("IsOwnedURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestOSSProvider_IsOwnedURL_CustomDomain(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		url    string
		want   bool
	}{
		{"bare domain", "cdn.example.com", "https://cdn.example.com/key", true},
		{"trailing slash tolerated", "cdn.example.com/", "https://cdn.example.com/key", true},
		{"path in config tolerated", "cdn.example.com/files/", "https://cdn.example.com/files/key", true},
		{"scheme in config tolerated", "https://cdn.example.com", "https://cdn.example.com/key", true},
		{"case-insensitive host", "cdn.example.com", "https://CDN.example.com/key", true},
		{"wrong domain", "cdn.example.com", "https://cdn.evil.com/key", false},
		{"spoofed subdomain", "cdn.example.com", "https://cdn.example.com.evil.com/key", false},
		{"default bucket ignored when custom set", "cdn.example.com", "https://anbancreator.oss-cn-chengdu.aliyuncs.com/x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &OSSProvider{
				bucketName:   "anbancreator",
				endpoint:     "oss-cn-chengdu.aliyuncs.com",
				customDomain: tt.domain,
			}
			if got := p.IsOwnedURL(tt.url); got != tt.want {
				t.Errorf("customDomain=%q IsOwnedURL(%q) = %v, want %v", tt.domain, tt.url, got, tt.want)
			}
		})
	}
}

func TestLocalProvider_IsOwnedURL(t *testing.T) {
	p := &LocalProvider{dataDir: "/tmp"}

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"valid local path", "/api/v1/files/user1/designer/gen1/0.png", true},
		{"http absolute rejected", "http://localhost/api/v1/files/x", false},
		{"https absolute rejected", "https://example.com/x", false},
		{"empty", "", false},
		{"double leading slash", "//api/v1/files/x", false},
		{"missing prefix", "/files/x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.IsOwnedURL(tt.url); got != tt.want {
				t.Errorf("IsOwnedURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
