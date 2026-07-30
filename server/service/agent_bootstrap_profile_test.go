package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
)

func profileWithControls(controls model.AgentClaudeControls) AgentExecutionProfile {
	envs := map[string]string{
		model.ClaudeEnvBaseURL:           "https://api.moonshot.cn/anthropic",
		model.ClaudeEnvAuthToken:         "moonshot-secret",
		model.ClaudeEnvModel:             "kimi-default",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "kimi-opus",
		"ANTHROPIC_DEFAULT_FABLE_MODEL":  "kimi-fable",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "kimi-sonnet",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "kimi-haiku",
	}
	setString := func(key string, value *string) {
		if value != nil {
			envs[key] = *value
		}
	}
	setBool := func(key string, value *bool) {
		if value != nil {
			envs[key] = strconv.FormatBool(*value)
		}
	}
	setInt := func(key string, value *int) {
		if value != nil {
			envs[key] = strconv.Itoa(*value)
		}
	}
	setString("CLAUDE_CODE_EFFORT_LEVEL", controls.EffortLevel)
	setBool("CLAUDE_CODE_ALWAYS_ENABLE_EFFORT", controls.AlwaysEnableEffort)
	setInt("CLAUDE_CODE_MAX_CONTEXT_TOKENS", controls.MaxContextTokens)
	setInt("CLAUDE_CODE_MAX_OUTPUT_TOKENS", controls.MaxOutputTokens)
	setInt("MAX_THINKING_TOKENS", controls.MaxThinkingTokens)
	setBool("CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING", controls.DisableAdaptiveThinking)
	setBool("CLAUDE_CODE_DISABLE_THINKING", controls.DisableThinking)
	setInt("CLAUDE_CODE_AUTO_COMPACT_WINDOW", controls.AutoCompactWindow)
	setInt("CLAUDE_AUTOCOMPACT_PCT_OVERRIDE", controls.AutocompactPctOverride)
	setBool("CLAUDE_CODE_DISABLE_1M_CONTEXT", controls.Disable1MContext)
	setString("CLAUDE_CODE_SUBAGENT_MODEL", controls.SubagentModel)
	setBool("ENABLE_TOOL_SEARCH", controls.EnableToolSearch)
	return AgentExecutionProfile{
		ID: "quality", DisplayName: "极致效果", Provider: "moonshot", Protocol: "anthropic", Envs: envs,
		ModelUsageAliases: map[string]string{
			"kimi-default": "kimi-k3", "kimi-opus": "kimi-k3", "kimi-fable": "kimi-k3",
			"kimi-sonnet": "kimi-k3", "kimi-haiku": "kimi-k3",
		},
		MinTier: model.TierEnterprise, Available: true,
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
	effort, enabled, disabled, subagent := "max", true, false, "kimi-subagent"
	contextTokens, outputTokens, thinkingTokens := 101, 202, 303
	compactWindow, compactPercent := 404, 85
	controls := model.AgentClaudeControls{
		EffortLevel: &effort, AlwaysEnableEffort: &enabled, MaxContextTokens: &contextTokens, MaxOutputTokens: &outputTokens,
		MaxThinkingTokens: &thinkingTokens, DisableAdaptiveThinking: &disabled, DisableThinking: &enabled,
		AutoCompactWindow: &compactWindow, AutocompactPctOverride: &compactPercent, Disable1MContext: &disabled,
		SubagentModel: &subagent, EnableToolSearch: &enabled,
	}
	env := profileWithControls(controls).RuntimeEnv()
	want := map[string]string{
		"CLAUDE_CODE_EFFORT_LEVEL": "max", "CLAUDE_CODE_ALWAYS_ENABLE_EFFORT": "true",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "101", "CLAUDE_CODE_MAX_OUTPUT_TOKENS": "202",
		"MAX_THINKING_TOKENS": "303", "CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING": "false",
		"CLAUDE_CODE_DISABLE_THINKING": "true", "CLAUDE_CODE_AUTO_COMPACT_WINDOW": "404",
		"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE": "85", "CLAUDE_CODE_DISABLE_1M_CONTEXT": "false",
		"CLAUDE_CODE_SUBAGENT_MODEL": "kimi-subagent", "ENABLE_TOOL_SEARCH": "true",
	}
	for key, value := range want {
		if env[key] != value {
			t.Fatalf("runtime env[%s] = %q, want %q; env=%#v", key, env[key], value, env)
		}
	}
	for key, value := range map[string]string{
		"ANTHROPIC_BASE_URL": "https://api.moonshot.cn/anthropic", "ANTHROPIC_AUTH_TOKEN": "moonshot-secret",
		"ANTHROPIC_MODEL": "kimi-default", "ANTHROPIC_DEFAULT_OPUS_MODEL": "kimi-opus",
		"ANTHROPIC_DEFAULT_FABLE_MODEL": "kimi-fable", "ANTHROPIC_DEFAULT_SONNET_MODEL": "kimi-sonnet",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": "kimi-haiku",
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
		Models: model.AgentModelMatrixFromClaudeProfileEnvs(snapshot.Envs),
		Claude: model.AgentClaudeControlsFromClaudeProfileEnvs(snapshot.Envs), DisplayName: snapshot.DisplayName,
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
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	executionProfile, ok := payload["execution_profile"].(map[string]any)
	if !ok {
		t.Fatalf("execution_profile = %#v", payload["execution_profile"])
	}
	aliases, ok := executionProfile["model_usage_aliases"].(map[string]any)
	if !ok {
		t.Fatalf("model_usage_aliases = %#v", executionProfile["model_usage_aliases"])
	}
	alias, ok := aliases["kimi-k3[1m]"].(map[string]any)
	if !ok || alias["provider"] != "moonshot" || alias["model"] != "kimi-k3" {
		t.Fatalf("model alias identity = %#v", aliases["kimi-k3[1m]"])
	}
	runtimeEnv, ok := executionProfile["runtime_env"].(map[string]any)
	if !ok || runtimeEnv["ANTHROPIC_BASE_URL"] == "" || runtimeEnv["ANTHROPIC_AUTH_TOKEN"] == "" {
		t.Fatalf("provider connection missing from runtime_env: %#v", executionProfile["runtime_env"])
	}
	delete(executionProfile, "runtime_env")
	redacted, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(redacted), "api.moonshot.cn") || strings.Contains(string(redacted), "moonshot-secret") {
		t.Fatalf("provider connection leaked outside runtime_env: %s", redacted)
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
	if response.ExecutionProfile.Models.Default != "kimi-default" || response.ExecutionProfile.ProfileFingerprint != fingerprint || response.ExecutionProfile.RuntimeEnv["MAX_THINKING_TOKENS"] != "0" || response.ExecutionProfile.RuntimeEnv["ENABLE_TOOL_SEARCH"] != "false" {
		t.Fatalf("profile runtime response = %#v", response.ExecutionProfile)
	}
	wantAlias := serveragent.ModelUsageIdentity{Provider: "moonshot", Model: "kimi-k3"}
	if len(response.ExecutionProfile.ModelUsageAliases) != 5 || response.ExecutionProfile.ModelUsageAliases["kimi-default"] != wantAlias {
		t.Fatalf("model aliases = %#v", response.ExecutionProfile.ModelUsageAliases)
	}

	for _, test := range []struct {
		name   string
		mutate func(*model.TaskExecution)
	}{
		{name: "profile id", mutate: func(candidate *model.TaskExecution) { candidate.ExecutionProfile = "balanced" }},
		{name: "provider", mutate: func(candidate *model.TaskExecution) { candidate.Provider = "other-provider" }},
		{name: "profile envs", mutate: func(candidate *model.TaskExecution) {
			candidate.ProfileEnvs = model.CloneClaudeProfileEnvs(candidate.ProfileEnvs)
			candidate.ProfileEnvs[model.ClaudeEnvModel] = "other-model"
		}},
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
