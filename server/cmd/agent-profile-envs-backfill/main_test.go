package main

import "testing"

func TestSetLegacyProviderBaseURL(t *testing.T) {
	values := map[string]string{}
	if err := setLegacyProviderBaseURL(values, "volcengine_ark=https://ark.example.com/anthropic"); err != nil {
		t.Fatal(err)
	}
	if got := values["volcengine_ark"]; got != "https://ark.example.com/anthropic" {
		t.Fatalf("base URL = %q", got)
	}
	for _, invalid := range []string{
		"", "volcengine_ark", "=https://ark.example.com/anthropic", "volcengine_ark=", "volcengine_ark=http://ark.example.com/anthropic",
	} {
		if err := setLegacyProviderBaseURL(values, invalid); err == nil {
			t.Fatalf("accepted invalid legacy provider Base URL %q", invalid)
		}
	}
}
