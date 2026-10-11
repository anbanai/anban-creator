package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestOnboardingConfig(t *testing.T) {
	c := Config{}
	c.applyDefaults()
	if err := c.Onboarding.Validate(); err != nil {
		t.Fatal("disabled must preserve old deployments", err)
	}
	c.Onboarding.Enabled = true
	if c.Onboarding.Validate() == nil {
		t.Fatal("enabled without provider")
	}
	c.Onboarding.Chat = OnboardingProvider{BaseURL: "https://api.example.com/v1", APIKey: "test", Model: "model"}
	if err := c.Onboarding.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"http://api.example.com", "https://user:pass@api.example.com", "https://api.example.com/?key=secret"} {
		next := c.Onboarding
		next.Chat.BaseURL = url
		if next.Validate() == nil {
			t.Fatal("accepted", url)
		}
	}
	c.Onboarding.Speech.Model = "partial"
	if c.Onboarding.Validate() == nil {
		t.Fatal("accepted partial speech config")
	}
}

func TestOnboardingReusesOnlyExistingOfficialDeepSeekProfile(t *testing.T) {
	for _, tc := range []struct {
		name, section, base, provider, key, model string
		want                                      bool
	}{
		{"official", "", "https://api.deepseek.com/anthropic", "deepseek", "private-token", "deepseek-flash", true},
		{"disabled wins", "onboarding: {enabled: false}", "https://api.deepseek.com/anthropic", "deepseek", "private-token", "deepseek-flash", false},
		{"null is explicit", "onboarding: null", "https://api.deepseek.com/anthropic", "deepseek", "private-token", "deepseek-flash", false},
		{"custom host", "", "https://proxy.example.com/anthropic", "deepseek", "private-token", "deepseek-flash", false},
		{"lookalike host", "", "https://api.deepseek.com.example.com/anthropic", "deepseek", "private-token", "deepseek-flash", false},
		{"untrusted path", "", "https://api.deepseek.com/custom", "deepseek", "private-token", "deepseek-flash", false},
		{"url credentials", "", "https://user:secret@api.deepseek.com/anthropic", "deepseek", "private-token", "deepseek-flash", false},
		{"no key", "", "https://api.deepseek.com/anthropic", "deepseek", "", "deepseek-flash", false},
		{"no model", "", "https://api.deepseek.com/anthropic", "deepseek", "private-token", "", false},
		{"other provider", "", "https://api.deepseek.com/anthropic", "other", "private-token", "deepseek-flash", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{Claude: ClaudeConfig{ExecutionProfiles: map[string]ClaudeExecutionProfileConfig{
				"effective": {Provider: tc.provider, Envs: map[string]string{"ANTHROPIC_BASE_URL": tc.base, "ANTHROPIC_AUTH_TOKEN": tc.key, "ANTHROPIC_MODEL": tc.model}},
			}}}
			c.applyDefaults()
			if err := c.inheritOnboardingProvider([]byte(tc.section)); err != nil {
				t.Fatal(err)
			}
			if c.Onboarding.Enabled != tc.want {
				t.Fatalf("enabled=%v, want %v", c.Onboarding.Enabled, tc.want)
			}
			if tc.want {
				if c.Onboarding.Chat.BaseURL != "https://api.deepseek.com" || c.Onboarding.Chat.APIKey != tc.key || c.Onboarding.Chat.Model != tc.model {
					t.Fatal("existing provider was not correctly reused")
				}
				if err := c.Onboarding.Validate(); err != nil {
					t.Fatal(err)
				}
				if c.Onboarding.Speech.Configured() {
					t.Fatal("text provider must not become a speech provider")
				}
				encoded, err := json.Marshal(c.Onboarding)
				if err != nil {
					t.Fatal(err)
				}
				for _, output := range []string{string(encoded), fmt.Sprintf("%+v", c.Onboarding), fmt.Sprintf("%#v", c.Onboarding)} {
					if strings.Contains(output, tc.key) {
						t.Fatal("credential exposed by serialization or formatting")
					}
				}
			}
		})
	}
}
