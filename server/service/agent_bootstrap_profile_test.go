package service

import (
	"encoding/json"
	"testing"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
)

func TestAgentBootstrapJSONIncludesEmptyReasoningEffort(t *testing.T) {
	profile := AgentExecutionProfile{
		ID: "cost_effective", DisplayName: "性价比", ModelName: "DeepSeek 4 Pro",
		Provider: "deepseek", ModelID: "deepseek-v4-pro", Protocol: "anthropic",
		MinTier: model.TierFree, Available: true,
	}
	if _, err := NewAgentProfileRegistry([]AgentExecutionProfile{profile}); err != nil {
		t.Fatalf("valid cost_effective profile: %v", err)
	}

	raw, err := json.Marshal(AgentBootstrapResponse{ExecutionProfile: AgentRuntimeProfile{
		ProfileID: profile.ID, Provider: profile.Provider, ModelID: profile.ModelID,
		Protocol: profile.Protocol, ReasoningEffort: profile.ReasoningEffort,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ExecutionProfile map[string]json.RawMessage `json:"execution_profile"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	got, ok := payload.ExecutionProfile["reasoning_effort"]
	if !ok {
		t.Fatalf("execution_profile omitted reasoning_effort: %s", raw)
	}
	if string(got) != `""` {
		t.Fatalf("execution_profile.reasoning_effort = %s, want empty string", got)
	}
}

func TestAgentBootstrapUsesFrozenExecutionProfileRuntime(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	profile := AgentExecutionProfile{
		ID: "maximum_quality", DisplayName: "极致效果", ModelName: "Kimi K3（1M）",
		Provider: "kimi", ModelID: "k3", Protocol: "anthropic", MinTier: model.TierEnterprise,
		BaseURL: "https://api.kimi.com/coding/", AuthToken: "kimi-secret", Available: true,
		ModelUsageAliases: map[string]string{"kimi-k3-latest": "k3"},
		ContextWindow:     1048576, ReasoningEffort: "high", ThinkingRequired: true,
	}
	registry, err := NewAgentProfileRegistry([]AgentExecutionProfile{profile})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := profile.Snapshot()
	task := &model.Task{
		ID: "task-profile", UserID: "user-profile", ProjectID: "project-profile", Type: model.PlatformArticle,
		SkipReferenceImage: true, ExecutionProfile: profile.ID, AgentProfileSnapshot: snapshot,
	}
	execution := model.NewTaskExecutionAgentProfile(snapshot)
	execution.ID = "execution-profile"
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{
		TokenTTL: time.Hour, Registry: registry, RuntimeControls: map[string]string{
			"CLAUDE_CODE_DISABLE_AUTO_MEMORY": "1",
			"ANTHROPIC_MODEL":                 "must-not-override-profile",
			"UNSAFE_RUNTIME_ENV":              "must-not-pass",
		},
	}, zerolog.Nop())

	response, err := svc.buildResponse(t.Context(), &execution, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("buildResponse: %v", err)
	}
	if response.ExecutionProfile.ProfileID != profile.ID || response.ExecutionProfile.Provider != profile.Provider ||
		response.ExecutionProfile.ModelID != "k3" || response.ExecutionProfile.Protocol != profile.Protocol ||
		response.ExecutionProfile.ContextWindow != 1048576 || response.ExecutionProfile.ReasoningEffort != "high" ||
		!response.ExecutionProfile.ThinkingRequired || response.ExecutionProfile.DisplayName != profile.DisplayName ||
		response.ExecutionProfile.RuntimeEnv["ANTHROPIC_BASE_URL"] != profile.BaseURL ||
		response.ExecutionProfile.RuntimeEnv["ANTHROPIC_AUTH_TOKEN"] != profile.AuthToken ||
		response.ExecutionProfile.RuntimeEnv["ANTHROPIC_MODEL"] != "k3" {
		t.Fatalf("profile runtime response = %#v", response)
	}
	if response.ExecutionProfile.RuntimeEnv["CLAUDE_CODE_DISABLE_AUTO_MEMORY"] != "1" || response.ExecutionProfile.RuntimeEnv["ANTHROPIC_MODEL"] != "k3" || response.ExecutionProfile.RuntimeEnv["UNSAFE_RUNTIME_ENV"] != "" {
		t.Fatalf("runtime controls/provider isolation = %#v", response.ExecutionProfile.RuntimeEnv)
	}
	wantAlias := serveragent.ModelUsageIdentity{Provider: "kimi", Model: "k3"}
	if len(response.ExecutionProfile.ModelUsageAliases) != 2 ||
		response.ExecutionProfile.ModelUsageAliases["k3"] != wantAlias ||
		response.ExecutionProfile.ModelUsageAliases["kimi-k3-latest"] != wantAlias {
		t.Fatalf("model aliases = %#v", response.ExecutionProfile.ModelUsageAliases)
	}

	drifted := execution
	drifted.ModelID = "other-model"
	if _, err := svc.buildResponse(t.Context(), &drifted, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("bootstrap accepted drifted execution model")
	}
}
