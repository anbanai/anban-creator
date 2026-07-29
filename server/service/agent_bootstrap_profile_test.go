package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
)

func profileWithControls(controls model.AgentClaudeControls) AgentExecutionProfile {
	return AgentExecutionProfile{
		ID: "maximum_quality", DisplayName: "极致效果", Provider: "moonshot", Protocol: "anthropic",
		Models: model.AgentModelMatrix{Default: "kimi-k3[1m]", Opus: "kimi-k3[1m]", Fable: "kimi-k3[1m]", Sonnet: "kimi-k3[1m]", Haiku: "kimi-k3[1m]"},
		Claude: controls, ModelUsageAliases: map[string]string{"kimi-k3[1m]": "kimi-k3"},
		BaseURL: "https://api.moonshot.cn/anthropic", AuthToken: "moonshot-secret", MinTier: model.TierEnterprise, Available: true,
	}
}

func TestAgentRuntimeProfileEmitsConfiguredFalseAndZero(t *testing.T) {
	zero, disabled, pct := 0, false, 85
	env := profileWithControls(model.AgentClaudeControls{MaxThinkingTokens: &zero, EnableToolSearch: &disabled, AutocompactPctOverride: &pct}).RuntimeEnv()
	if env["MAX_THINKING_TOKENS"] != "0" || env["ENABLE_TOOL_SEARCH"] != "false" || env["CLAUDE_AUTOCOMPACT_PCT_OVERRIDE"] != "85" {
		t.Fatalf("runtime env = %#v", env)
	}
	if _, exists := env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"]; exists {
		t.Fatal("omitted control emitted")
	}
}

func TestAgentRuntimeProfileMapsEveryClaudeControl(t *testing.T) {
	effort, enabled, integer, disabled, subagent := "max", true, 123, false, "kimi-k3[1m]"
	controls := model.AgentClaudeControls{
		EffortLevel: &effort, AlwaysEnableEffort: &enabled, MaxContextTokens: &integer, MaxOutputTokens: &integer,
		MaxThinkingTokens: &integer, DisableAdaptiveThinking: &disabled, DisableThinking: &disabled,
		AutoCompactWindow: &integer, AutocompactPctOverride: &integer, Disable1MContext: &disabled,
		SubagentModel: &subagent, EnableToolSearch: &enabled,
	}
	env := profileWithControls(controls).RuntimeEnv()
	want := map[string]string{
		"CLAUDE_CODE_EFFORT_LEVEL": "max", "CLAUDE_CODE_ALWAYS_ENABLE_EFFORT": "true",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "123", "CLAUDE_CODE_MAX_OUTPUT_TOKENS": "123",
		"MAX_THINKING_TOKENS": "123", "CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING": "false",
		"CLAUDE_CODE_DISABLE_THINKING": "false", "CLAUDE_CODE_AUTO_COMPACT_WINDOW": "123",
		"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE": "123", "CLAUDE_CODE_DISABLE_1M_CONTEXT": "false",
		"CLAUDE_CODE_SUBAGENT_MODEL": "kimi-k3[1m]", "ENABLE_TOOL_SEARCH": "true",
	}
	for key, value := range want {
		if env[key] != value {
			t.Fatalf("runtime env[%s] = %q, want %q; env=%#v", key, env[key], value, env)
		}
	}
	for key, value := range map[string]string{
		"ANTHROPIC_BASE_URL": "https://api.moonshot.cn/anthropic", "ANTHROPIC_AUTH_TOKEN": "moonshot-secret",
		"ANTHROPIC_MODEL": "kimi-k3[1m]", "ANTHROPIC_DEFAULT_OPUS_MODEL": "kimi-k3[1m]",
		"ANTHROPIC_DEFAULT_FABLE_MODEL": "kimi-k3[1m]", "ANTHROPIC_DEFAULT_SONNET_MODEL": "kimi-k3[1m]",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": "kimi-k3[1m]",
	} {
		if env[key] != value {
			t.Fatalf("runtime env[%s] = %q", key, env[key])
		}
	}
}

func TestAgentBootstrapJSONUsesMatrixContractOnly(t *testing.T) {
	profile := profileWithControls(model.AgentClaudeControls{})
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(AgentBootstrapResponse{ExecutionProfile: AgentRuntimeProfile{
		ProfileID: snapshot.ProfileID, Provider: snapshot.Provider, Protocol: snapshot.Protocol,
		Models: snapshot.Models, Claude: snapshot.Claude, DisplayName: snapshot.DisplayName,
		ProfileFingerprint: fingerprint, RuntimeEnv: profile.RuntimeEnv(),
		ModelUsageAliases: map[string]serveragent.ModelUsageIdentity{"kimi-k3[1m]": {Provider: "moonshot", Model: "kimi-k3"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	for _, required := range []string{`"models":`, `"claude":`, `"profile_fingerprint":`, `"model_usage_aliases":`} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("bootstrap JSON omitted %q: %s", required, encoded)
		}
	}
	for _, forbidden := range []string{`"model_id"`, `"context_window"`, `"reasoning_effort"`, `"thinking_required"`} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("bootstrap JSON retained %q: %s", forbidden, encoded)
		}
	}
}

func TestAgentBootstrapUsesFrozenExecutionProfileRuntime(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	zero, disabled := 0, false
	profile := profileWithControls(model.AgentClaudeControls{MaxThinkingTokens: &zero, EnableToolSearch: &disabled})
	registry, err := NewAgentProfileRegistry([]AgentExecutionProfile{profile})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task-profile", UserID: "user-profile", ProjectID: "project-profile", Type: model.PlatformArticle, SkipReferenceImage: true, ExecutionProfile: profile.ID, AgentProfileSnapshot: snapshot, AgentProfileFingerprint: fingerprint}
	execution := model.NewTaskExecutionAgentProfile(snapshot, fingerprint)
	execution.ID = "execution-profile"
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{TokenTTL: time.Hour, Registry: registry}, zerolog.Nop())

	response, err := svc.buildResponse(t.Context(), &execution, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("buildResponse: %v", err)
	}
	if response.ExecutionProfile.Models.Default != "kimi-k3[1m]" || response.ExecutionProfile.ProfileFingerprint != fingerprint || response.ExecutionProfile.RuntimeEnv["MAX_THINKING_TOKENS"] != "0" || response.ExecutionProfile.RuntimeEnv["ENABLE_TOOL_SEARCH"] != "false" {
		t.Fatalf("profile runtime response = %#v", response.ExecutionProfile)
	}
	wantAlias := serveragent.ModelUsageIdentity{Provider: "moonshot", Model: "kimi-k3"}
	if len(response.ExecutionProfile.ModelUsageAliases) != 1 || response.ExecutionProfile.ModelUsageAliases["kimi-k3[1m]"] != wantAlias {
		t.Fatalf("model aliases = %#v", response.ExecutionProfile.ModelUsageAliases)
	}

	for _, test := range []struct {
		name   string
		mutate func(*model.TaskExecution)
	}{
		{name: "profile id", mutate: func(candidate *model.TaskExecution) { candidate.ExecutionProfile = "balanced" }},
		{name: "provider", mutate: func(candidate *model.TaskExecution) { candidate.Provider = "other-provider" }},
		{name: "model matrix", mutate: func(candidate *model.TaskExecution) { candidate.ModelMatrix.Default = "other-model" }},
		{name: "Claude controls", mutate: func(candidate *model.TaskExecution) { candidate.ClaudeControls = model.AgentClaudeControls{} }},
		{name: "fingerprint", mutate: func(candidate *model.TaskExecution) { candidate.ProfileFingerprint = strings.Repeat("f", 64) }},
	} {
		t.Run("rejects drifted "+test.name, func(t *testing.T) {
			drifted := execution
			test.mutate(&drifted)
			if _, err := svc.resolveExecutionProfile(&drifted, task); err == nil {
				t.Fatalf("bootstrap accepted drifted %s", test.name)
			}
		})
	}
}
