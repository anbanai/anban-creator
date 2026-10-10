package config

import "testing"

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
