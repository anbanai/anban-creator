package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentProfileSnapshotAPISnapshot(t *testing.T) {
	snapshot := validAgentProfileSnapshot()
	snapshot.Envs = withClaudeEnvs(snapshot.Envs, map[string]string{
		claudeEnvEffortLevel:      "high",
		claudeEnvMaxContextTokens: "1000000",
		claudeEnvDisableThinking:  "false",
	})

	public := snapshot.APISnapshot()
	if public.SchemaVersion != ClaudeProfileSchemaV3 || public.ProfileID != "quality" || public.DisplayName != "Maximum quality" {
		t.Fatalf("public identity = %#v", public)
	}
	wantEnvs := map[string]string{
		claudeEnvEffortLevel:      "high",
		claudeEnvMaxContextTokens: "1000000",
		claudeEnvDisableThinking:  "false",
	}
	if len(public.Envs) != len(wantEnvs) {
		t.Fatalf("public envs = %#v, want %#v", public.Envs, wantEnvs)
	}
	for key, want := range wantEnvs {
		if got := public.Envs[key]; got != want {
			t.Fatalf("env %s = %q, want %q", key, got, want)
		}
	}

	raw, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal public snapshot: %v", err)
	}
	for _, leaked := range []string{
		ClaudeEnvBaseURL, ClaudeEnvModel,
		"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL",
		claudeEnvSubagentModel, "moonshot", "anthropic", "api.example.com", "default-model",
	} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("public snapshot leaks %q: %s", leaked, raw)
		}
	}
}

func TestAgentProfileSnapshotAPISnapshotEmpty(t *testing.T) {
	var empty AgentProfileSnapshot
	public := empty.APISnapshot()
	if public.SchemaVersion != 0 || public.ProfileID != "" || public.DisplayName != "" || len(public.Envs) != 0 {
		t.Fatalf("empty snapshot projected non-zero public view: %#v", public)
	}
}
