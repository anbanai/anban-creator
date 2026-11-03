package config

import (
	"maps"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestAgentRuntimeEnvsCoversEveryManagedPack(t *testing.T) {
	cfg := Config{
		WechatArticle:     AgentRuntimeEnvConfig{Env: map[string]string{"REDFOX_API_KEY": "article"}},
		WechatPicture:     AgentRuntimeEnvConfig{Env: map[string]string{"REDFOX_API_KEY": "picture"}},
		Seednote:          AgentRuntimeEnvConfig{Env: map[string]string{"REDFOX_API_KEY": "seednote"}},
		ContentPlanning:   AgentRuntimeEnvConfig{Env: map[string]string{"REDFOX_API_KEY": "research"}},
		Montage:           MontageConfig{Env: map[string]string{"FAL_KEY": "montage"}},
		Hypit:             HypitConfig{Env: map[string]string{"HYPIT_PROVIDER_TOKEN": "hypit"}},
		VideoEdit:         VideoEditConfig{ElevenLabsAPIKey: "video-scribe", Env: map[string]string{"VIDEO_EDIT_OPTION": "enabled"}},
		TalkingHeadEditor: TalkingHeadEditorConfig{VideoUseAPIKey: "talking-scribe", Env: map[string]string{"TALKING_HEAD_OPTION": "enabled"}},
	}
	want := map[string]map[string]string{
		model.AgentIDArticle:           {"REDFOX_API_KEY": "article"},
		model.AgentIDWechatPicture:     {"REDFOX_API_KEY": "picture"},
		model.AgentIDSeednote:          {"REDFOX_API_KEY": "seednote"},
		model.AgentIDContentPlanning:   {"REDFOX_API_KEY": "research"},
		model.AgentIDMontage:           {"FAL_KEY": "montage"},
		model.AgentIDHypit:             {"HYPIT_PROVIDER_TOKEN": "hypit"},
		model.AgentIDVideoEdit:         {"VIDEO_EDIT_OPTION": "enabled", "ELEVENLABS_API_KEY": "video-scribe"},
		model.AgentIDTalkingHeadEditor: {"TALKING_HEAD_OPTION": "enabled", "ELEVENLABS_API_KEY": "talking-scribe"},
	}
	got := cfg.AgentRuntimeEnvs()
	if !maps.EqualFunc(got, want, maps.Equal) {
		t.Fatalf("AgentRuntimeEnvs() = %#v, want %#v", got, want)
	}
}

func TestAgentRuntimeEnvsOmitsUnsetAndUnconfiguredPacks(t *testing.T) {
	cfg := Config{
		WechatArticle: AgentRuntimeEnvConfig{Env: map[string]string{
			"REDFOX_API_KEY":      "article-key",
			"REDFOX_API_BASE_URL": "", // unset operator value
		}},
		VideoEdit: VideoEditConfig{ElevenLabsAPIKey: "dedicated", Env: map[string]string{
			"ELEVENLABS_API_KEY": "from-env",
			"VIDEO_EDIT_OPTION":  "",
		}},
	}
	envs := cfg.AgentRuntimeEnvs()
	if got := envs[model.AgentIDArticle]; len(got) != 1 || got["REDFOX_API_KEY"] != "article-key" {
		t.Fatalf("article env = %#v, want only the configured value", got)
	}
	if _, configured := envs[model.AgentIDSeednote]; configured {
		t.Fatal("unconfigured Pack must be absent from the runtime environment map")
	}
	if got := envs[model.AgentIDVideoEdit]; got["ELEVENLABS_API_KEY"] != "dedicated" {
		t.Fatalf("video-edit env = %#v, want the dedicated credential to win", got)
	}
	if got := envs[model.AgentIDVideoEdit]; len(got) != 1 {
		t.Fatalf("video-edit env = %#v, want unset values dropped", got)
	}
}

func TestRuntimeEnvConfigurationRejectsUnsafeKeys(t *testing.T) {
	if err := (AgentRuntimeEnvConfig{Env: map[string]string{"PATH": "/attacker/bin"}}).Validate("wechat-article"); err == nil || !strings.Contains(err.Error(), "reserved for the runtime process") {
		t.Fatalf("config validation error = %v, want a reserved-process-key error", err)
	}
	if err := (AgentRuntimeEnvConfig{Env: map[string]string{"ANBAN_EXECUTION_TOKEN": "forged"}}).Validate("seednote"); err == nil || !strings.Contains(err.Error(), "reserved prefix") {
		t.Fatalf("config validation error = %v, want a reserved-prefix error", err)
	}
	if err := (AgentRuntimeEnvConfig{Env: map[string]string{"REDFOX_API_KEY": ""}}).Validate("wechat-article"); err != nil {
		t.Fatalf("unset operator value must stay valid: %v", err)
	}
	if err := (AgentRuntimeEnvConfig{Env: map[string]string{"REDFOX_API_KEY": "key"}}).Validate("wechat-article"); err != nil {
		t.Fatalf("valid Agent env rejected: %v", err)
	}
}

func TestMontageEnvUsesTheSharedRuntimeEnvironmentContract(t *testing.T) {
	cfg := MontageConfig{
		Enabled:            true,
		DefaultPipeline:    "social-short",
		AllowedPipelines:   []string{"social-short"},
		MaxDurationSeconds: 30,
		MaxAssets:          4,
		TimeoutMinutes:     10,
		Env:                map[string]string{"ANBAN_API_KEY": "forged"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "reserved prefix") {
		t.Fatalf("montage env validation error = %v, want a reserved-prefix error", err)
	}
	cfg.Env = map[string]string{"NEW_PROVIDER_TOKEN": "upstream-owned"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("future upstream provider key rejected: %v", err)
	}
}
