package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPublicBaseURLMustBeHTTPSBase(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "https origin", url: "https://studio.example.com", want: true},
		{name: "https prefix", url: "https://cdn.example.com/anban", want: true},
		{name: "http rejected", url: "http://studio.example.com"},
		{name: "credentials rejected", url: "https://user:pass@studio.example.com"},
		{name: "query rejected", url: "https://studio.example.com?x=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Server: ServerConfig{PublicBaseURL: tc.url}}
			err := cfg.Validate()
			if tc.want && err != nil && strings.Contains(err.Error(), "server.public_base_url") {
				t.Fatalf("Validate() rejected valid URL: %v", err)
			}
			if !tc.want && (err == nil || !strings.Contains(err.Error(), "server.public_base_url")) {
				t.Fatalf("Validate() error = %v, want public base URL validation error", err)
			}
		})
	}
}

func TestStudioBaseURLMustBeHTTPSBase(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "https origin", url: "https://creator.anbanai.com", want: true},
		{name: "https prefix", url: "https://studio.example.com/anban", want: true},
		{name: "http rejected", url: "http://studio.example.com"},
		{name: "credentials rejected", url: "https://user:pass@studio.example.com"},
		{name: "query rejected", url: "https://studio.example.com?x=1"},
		{name: "fragment rejected", url: "https://studio.example.com#projects"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			if err := yaml.Unmarshal([]byte("server:\n  studio_base_url: "+tc.url+"\n"), &cfg); err != nil {
				t.Fatal(err)
			}
			err := cfg.Validate()
			if tc.want && err != nil && strings.Contains(err.Error(), "server.studio_base_url") {
				t.Fatalf("Validate() rejected valid URL: %v", err)
			}
			if !tc.want && (err == nil || !strings.Contains(err.Error(), "server.studio_base_url")) {
				t.Fatalf("Validate() error = %v, want Studio base URL validation error", err)
			}
		})
	}
}

func TestWechatAuthorizationRequiresStudioBaseURL(t *testing.T) {
	cfg := &Config{
		Server:             ServerConfig{PublicBaseURL: "https://api-creator.anbanai.com"},
		WechatOpenPlatform: WechatOpenPlatformConfig{ComponentAppID: "component-app"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "server.studio_base_url is required") {
		t.Fatalf("Validate() error = %v, want explicit Studio return address", err)
	}
}
