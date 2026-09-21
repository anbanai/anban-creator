package config

import "testing"

func TestHypitDefaultsDisabledAndNativeSecretReferences(t *testing.T) {
	c := HypitConfig{}
	c.ApplyDefaults()
	if c.Enabled || c.Limits.MaxProjectBytes != 2<<30 {
		t.Fatalf("bad defaults: %+v", c.Limits)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.RuntimeProfile = map[string]any{"format": "hypit.runtime-local@1", "apiKey": "secret"}
	if c.Validate() == nil {
		t.Fatal("inline credential accepted")
	}
	c.RuntimeProfile = map[string]any{"format": "hypit.runtime-local@1", "apiKey": map[string]any{"store": "env", "key": "KEY"}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Env["ANBAN_TASK_ID"] = "bad"
	if c.Validate() == nil {
		t.Fatal("reserved env accepted")
	}
}

func TestHypitAcceptsNativeProviderProfilesWithoutBindings(t *testing.T) {
	for _, provider := range []string{"@hypit/provider-hypihub", "@hypit/provider-hiapi"} {
		t.Run(provider, func(t *testing.T) {
			c := HypitConfig{RuntimeProfile: map[string]any{"format": "hypit.runtime-local@1", "credentials": map[string]any{"env": map[string]any{"use": "@hypit/credential-store-env"}}, "endpoints": map[string]any{"generation": map[string]any{"use": provider, "config": map[string]any{"baseUrl": "https://api.example.invalid", "apiKey": map[string]any{"store": "env", "key": "PROVIDER_API_KEY"}}}}}, Env: map[string]string{"PROVIDER_API_KEY": "test-only"}}
			c.ApplyDefaults()
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
			if missing := c.MissingConfiguration(); len(missing) > 0 {
				t.Fatalf("native auto-routing rejected: %v", missing)
			}
			c.RuntimeProfile["bindings"] = map[string]any{}
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
			delete(c.Env, "PROVIDER_API_KEY")
			if len(c.MissingConfiguration()) == 0 {
				t.Fatal("missing env credential accepted")
			}
		})
	}
}
