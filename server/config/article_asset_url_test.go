package config

import "testing"

import "strings"

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
