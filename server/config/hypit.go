package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type HypitLimits struct {
	MaxDurationSeconds int64 `yaml:"max_duration_seconds" json:"max_duration_seconds"`
	MaxAssets          int   `yaml:"max_assets" json:"max_assets"`
	MaxAssetBytes      int64 `yaml:"max_asset_bytes" json:"max_asset_bytes"`
	MaxInputBytes      int64 `yaml:"max_input_bytes" json:"max_input_bytes"`
	MaxProjectBytes    int64 `yaml:"max_project_bytes" json:"max_project_bytes"`
	MaxExpandedBytes   int64 `yaml:"max_expanded_bytes" json:"max_expanded_bytes"`
	MaxProjectFiles    int   `yaml:"max_project_files" json:"max_project_files"`
	MaxVideoBytes      int64 `yaml:"max_video_bytes" json:"max_video_bytes"`
	TimeoutMinutes     int   `yaml:"timeout_minutes" json:"timeout_minutes"`
}
type HypitConfig struct {
	Enabled        bool              `yaml:"enabled"`
	RuntimeProfile map[string]any    `yaml:"runtime_profile"`
	Env            map[string]string `yaml:"env"`
	Limits         HypitLimits       `yaml:"limits"`
}

func (c *HypitConfig) ApplyDefaults() {
	l := &c.Limits
	if l.MaxDurationSeconds == 0 {
		l.MaxDurationSeconds = 180
	}
	if l.MaxAssets == 0 {
		l.MaxAssets = 20
	}
	if l.MaxAssetBytes == 0 {
		l.MaxAssetBytes = 256 << 20
	}
	if l.MaxInputBytes == 0 {
		l.MaxInputBytes = 512 << 20
	}
	if l.MaxProjectBytes == 0 {
		l.MaxProjectBytes = 2 << 30
	}
	if l.MaxExpandedBytes == 0 {
		l.MaxExpandedBytes = 8 << 30
	}
	if l.MaxProjectFiles == 0 {
		l.MaxProjectFiles = 10000
	}
	if l.MaxVideoBytes == 0 {
		l.MaxVideoBytes = 512 << 20
	}
	if l.TimeoutMinutes == 0 {
		l.TimeoutMinutes = 90
	}
	if c.Env == nil {
		c.Env = map[string]string{}
	}
}

var hypitEnvName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func (c HypitConfig) Validate() error {
	c.ApplyDefaults()
	l := c.Limits
	if l.MaxDurationSeconds <= 0 || l.MaxAssets <= 0 || l.MaxAssetBytes <= 0 || l.MaxInputBytes <= 0 || l.MaxProjectBytes <= 0 || l.MaxExpandedBytes <= 0 || l.MaxProjectFiles <= 0 || l.MaxVideoBytes <= 0 || l.TimeoutMinutes <= 0 {
		return fmt.Errorf("hypit.limits must be positive")
	}
	if l.MaxDurationSeconds > 180 || l.MaxAssets > 20 || l.MaxAssetBytes > 256<<20 || l.MaxInputBytes > 512<<20 || l.MaxProjectBytes > 2<<30 || l.MaxExpandedBytes > 8<<30 || l.MaxProjectFiles > 10000 || l.MaxVideoBytes > 512<<20 || l.TimeoutMinutes > 90 {
		return fmt.Errorf("hypit.limits exceed managed runtime safety ceilings")
	}
	for k, v := range c.Env {
		reserved := false
		for _, prefix := range []string{"ANBAN_", "ANTHROPIC_", "CLAUDE_", "CODEX_", "NODE_", "BUN_", "LD_", "DYLD_", "PYTHON", "NPM_", "GIT_", "SSH_", "AWS_"} {
			if strings.HasPrefix(k, prefix) {
				reserved = true
			}
		}
		for _, name := range []string{"PATH", "HOME", "SHELL", "PWD", "OLDPWD", "TMPDIR", "TMP", "TEMP", "ENV", "BASH_ENV", "ZDOTDIR", "IFS", "GOOGLE_APPLICATION_CREDENTIALS"} {
			if k == name {
				reserved = true
			}
		}
		if !hypitEnvName.MatchString(k) || len(v) > 16384 || strings.ContainsAny(v, "\x00\r\n") || reserved {
			return fmt.Errorf("hypit.env contains invalid or reserved key %q", k)
		}
	}

	if len(c.RuntimeProfile) > 0 {
		if stores, ok := c.RuntimeProfile["credentials"].(map[string]any); ok {
			for name, v := range stores {
				m, ok := v.(map[string]any)
				if !ok || name != "env" || m["use"] != "@hypit/credential-store-env" || len(m) != 1 {
					return fmt.Errorf("hypit.runtime_profile.credentials must use the native env store")
				}
			}
		}
		if c.RuntimeProfile["format"] != "hypit.runtime-local@1" {
			return fmt.Errorf("hypit.runtime_profile.format must be hypit.runtime-local@1")
		}
		if raw, exists := c.RuntimeProfile["bindings"]; exists {
			bindings, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("hypit.runtime_profile.bindings must be a map")
			}
			endpoints, _ := c.RuntimeProfile["endpoints"].(map[string]any)
			for _, v := range bindings {
				target, ok := v.(string)
				if !ok || target == "" || endpoints[target] == nil {
					return fmt.Errorf("hypit.runtime_profile.bindings references an unavailable endpoint")
				}
			}
		}
		return validateHypitProfile(c.RuntimeProfile)
	}
	return nil
}
func validateHypitProfile(v any) error {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			lower := strings.ToLower(k)
			if strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.HasSuffix(lower, "token") || strings.Contains(lower, "apikey") || strings.Contains(lower, "api_key") || lower == "authorization" || strings.Contains(lower, "accesskey") {
				ref, ok := v.(map[string]any)
				key, _ := ref["key"].(string)
				if !ok || ref["store"] != "env" || !hypitEnvName.MatchString(key) || len(ref) != 2 {
					return fmt.Errorf("hypit.runtime_profile credentials must use environment references")
				}
			}
			if k == "store" && v != "env" {
				return fmt.Errorf("hypit.runtime_profile credential references must use env store")
			}
			if err := validateHypitProfile(v); err != nil {
				return err
			}
		}
	case []any:
		for _, v := range x {
			if err := validateHypitProfile(v); err != nil {
				return err
			}
		}
	}
	return nil
}
func (c HypitConfig) MissingConfiguration() []string {
	m := []string{}
	if c.RuntimeProfile["format"] != "hypit.runtime-local@1" {
		m = append(m, "runtime_profile")
	}
	for _, k := range []string{"endpoints"} {
		v, _ := c.RuntimeProfile[k].(map[string]any)
		if len(v) == 0 {
			m = append(m, "runtime_profile."+k)
		}
	}
	var refs func(any)
	refs = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["store"] == "env" {
				k, _ := x["key"].(string)
				if strings.TrimSpace(c.Env[k]) == "" {
					m = append(m, "env."+k)
				}
			}
			for _, v := range x {
				refs(v)
			}
		case []any:
			for _, v := range x {
				refs(v)
			}
		}
	}
	refs(c.RuntimeProfile)
	return m
}
func (c HypitConfig) NativeProfile() map[string]any {
	b, _ := json.Marshal(c.RuntimeProfile)
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	if p == nil {
		p = map[string]any{}
	}
	p["dataRoot"] = "/workspace/project/.hypit/execution"
	return p
}

// Formatting configuration must never serialize provider credentials.
func (c HypitConfig) String() string {
	return fmt.Sprintf("HypitConfig{Enabled:%t RuntimeProfileConfigured:%t EnvKeys:%d}", c.Enabled, len(c.RuntimeProfile) > 0, len(c.Env))
}
func (c HypitConfig) GoString() string { return c.String() }
