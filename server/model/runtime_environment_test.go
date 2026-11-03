package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateRuntimeEnvironmentAcceptsProviderCredentials(t *testing.T) {
	for _, env := range []map[string]string{
		{"REDFOX_API_KEY": "key", "REDFOX_API_BASE_URL": "https://redfox.example.test"},
		{"FAL_KEY": "key", "PEXELS_API_KEY": "key", "UNSPLASH_ACCESS_KEY": "key"},
		{"ELEVENLABS_API_KEY": "key"},
		{"HYPIT_PROVIDER_TOKEN": "token"},
	} {
		if err := ValidateRuntimeEnvironment("pack", env); err != nil {
			t.Fatalf("ValidateRuntimeEnvironment(%v) = %v", env, err)
		}
	}
}

func TestValidateRuntimeEnvironmentRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "lowercase key", env: map[string]string{"redfox_key": "v"}, want: "must match"},
		{name: "leading digit", env: map[string]string{"1KEY": "v"}, want: "must match"},
		{name: "dash in key", env: map[string]string{"REDFOX-KEY": "v"}, want: "must match"},
		{name: "empty key", env: map[string]string{"": "v"}, want: "must match"},
		{name: "oversized key", env: map[string]string{strings.Repeat("K", MaxRuntimeEnvironmentKeyBytes+1): "v"}, want: "must match"},
		{name: "control plane prefix", env: map[string]string{"ANBAN_EXECUTION_TOKEN": "v"}, want: "reserved prefix"},
		{name: "model provider prefix", env: map[string]string{"ANTHROPIC_AUTH_TOKEN": "v"}, want: "reserved prefix"},
		{name: "claude prefix", env: map[string]string{"CLAUDE_CODE_DISABLE_THINKING": "v"}, want: "reserved prefix"},
		{name: "node prefix", env: map[string]string{"NODE_OPTIONS": "v"}, want: "reserved prefix"},
		{name: "dynamic loader prefix", env: map[string]string{"LD_PRELOAD": "v"}, want: "reserved prefix"},
		{name: "python path", env: map[string]string{"PYTHONPATH": "v"}, want: "reserved prefix"},
		{name: "git config", env: map[string]string{"GIT_SSH_COMMAND": "v"}, want: "reserved prefix"},
		{name: "path", env: map[string]string{"PATH": "/attacker/bin"}, want: "reserved for the runtime process"},
		{name: "home", env: map[string]string{"HOME": "/attacker"}, want: "reserved for the runtime process"},
		{name: "ifs", env: map[string]string{"IFS": "/"}, want: "reserved for the runtime process"},
		{name: "empty value", env: map[string]string{"REDFOX_API_KEY": ""}, want: "non-empty"},
		{name: "padded value", env: map[string]string{"REDFOX_API_KEY": " key "}, want: "padded"},
		{name: "newline value", env: map[string]string{"REDFOX_API_KEY": "key\ninjected"}, want: "control character"},
		{name: "nul value", env: map[string]string{"REDFOX_API_KEY": "key\x00"}, want: "control character"},
		{name: "oversized value", env: map[string]string{"REDFOX_API_KEY": strings.Repeat("x", MaxRuntimeEnvironmentValueBytes+1)}, want: "exceeds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateRuntimeEnvironment("pack", test.env)
			if err == nil {
				t.Fatalf("ValidateRuntimeEnvironment(%v) accepted an unsafe entry", test.env)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want it to mention %q", err, test.want)
			}
		})
	}
}

func TestValidateRuntimeEnvironmentEnforcesAggregateLimits(t *testing.T) {
	tooMany := make(map[string]string, MaxRuntimeEnvironmentEntries+1)
	for index := 0; index <= MaxRuntimeEnvironmentEntries; index++ {
		tooMany[fmt.Sprintf("KEY_%03d", index)] = "value"
	}
	if err := ValidateRuntimeEnvironment("pack", tooMany); err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("entry limit error = %v, want an entries error", err)
	}

}

func TestNormalizeRuntimeEnvironmentDropsUnsetValues(t *testing.T) {
	normalized := NormalizeRuntimeEnvironment(map[string]string{
		"REDFOX_API_KEY": "key",
		"UNSET_KEY":      "",
		"WHITESPACE_KEY": "   ",
	})
	if len(normalized) != 1 || normalized["REDFOX_API_KEY"] != "key" {
		t.Fatalf("normalized env = %#v", normalized)
	}
	if _, exists := normalized["UNSET_KEY"]; exists {
		t.Fatal("unset key survived normalization")
	}
}
