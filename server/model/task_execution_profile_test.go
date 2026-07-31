package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestExecutionProfileColumnsHaveNoDatabaseDefault(t *testing.T) {
	for _, item := range []struct {
		name      string
		model     any
		fieldName string
	}{
		{name: "task", model: Task{}, fieldName: "ExecutionProfile"},
		{name: "plan", model: Plan{}, fieldName: "ExecutionProfile"},
	} {
		t.Run(item.name, func(t *testing.T) {
			field, ok := reflect.TypeOf(item.model).FieldByName(item.fieldName)
			if !ok {
				t.Fatalf("%s field is missing", item.fieldName)
			}
			if tag := field.Tag.Get("gorm"); strings.Contains(tag, "default:") {
				t.Fatalf("ExecutionProfile gorm tag = %q, want no database default", tag)
			}
		})
	}
}

func TestTaskExecutionStoresOnlyFrozenRuntimeIdentity(t *testing.T) {
	executionType := reflect.TypeOf(TaskExecution{})
	for _, redundant := range []string{"AgentProfileID", "AgentProfileSnapshot", "ModelID", "Protocol", "ReasoningEffort", "ContextWindow", "ModelMatrix", "ClaudeControls"} {
		if _, exists := executionType.FieldByName(redundant); exists {
			t.Fatalf("TaskExecution must not duplicate task snapshot field %s", redundant)
		}
	}
	snapshot := validAgentProfileSnapshot()
	snapshot.Envs = withClaudeEnvs(snapshot.Envs, map[string]string{
		ClaudeEnvAuthToken:                      "must-not-persist",
		"CLAUDE_CODE_EFFORT_LEVEL":              "high",
		"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT":      "true",
		"MAX_THINKING_TOKENS":                   "0",
		"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING": "false",
		"CLAUDE_CODE_DISABLE_THINKING":          "true",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS":        "1000000",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS":         "64000",
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":       "800000",
		"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":       "80",
		"CLAUDE_CODE_DISABLE_1M_CONTEXT":        "false",
		"CLAUDE_CODE_SUBAGENT_MODEL":            "opus-model",
		"ENABLE_TOOL_SEARCH":                    "false",
	})
	execution := NewTaskExecutionAgentProfile(snapshot, strings.Repeat("a", 64))
	if execution.ExecutionProfile != "quality" || execution.Provider != "moonshot" ||
		execution.ProfileEnvs[ClaudeEnvModel] != "default-model" ||
		execution.ProfileEnvs["CLAUDE_CODE_EFFORT_LEVEL"] != "high" ||
		execution.ProfileEnvs["MAX_THINKING_TOKENS"] != "0" ||
		execution.ProfileFingerprint != strings.Repeat("a", 64) {
		t.Fatalf("execution = %#v", execution)
	}
	if _, ok := execution.ProfileEnvs[ClaudeEnvAuthToken]; ok {
		t.Fatal("execution profile envs contain auth token")
	}
	snapshot.Envs[ClaudeEnvModel] = "changed"
	if execution.ProfileEnvs[ClaudeEnvModel] == "changed" {
		t.Fatal("execution profile envs alias task snapshot")
	}
	raw, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "must-not-persist") || strings.Contains(string(raw), ClaudeEnvAuthToken) {
		t.Fatalf("execution persisted auth token: %s", raw)
	}
}

func TestAgentProfileSnapshotFingerprintIsCanonical(t *testing.T) {
	first := validAgentProfileSnapshot()
	second := first
	second.Envs = map[string]string{
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": "haiku-model", "ANTHROPIC_DEFAULT_SONNET_MODEL": "sonnet-model",
		"ANTHROPIC_DEFAULT_FABLE_MODEL": "fable-model", ClaudeEnvBaseURL: "https://api.example.com/anthropic",
		"ANTHROPIC_DEFAULT_OPUS_MODEL": "opus-model", ClaudeEnvModel: "default-model",
	}
	second.ModelUsageAliases = map[string]string{
		"sonnet-model": "sonnet", "opus-model": "opus", "haiku-model": "haiku",
		"fable-model": "fable", "default-model": "default",
	}
	a, err := AgentProfileFingerprint(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := AgentProfileFingerprint(second)
	if err != nil {
		t.Fatal(err)
	}
	if a != b || len(a) != 64 {
		t.Fatalf("fingerprints=%q %q", a, b)
	}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	for _, required := range []string{`"schema_version":3`, `"envs":`, `"ANTHROPIC_BASE_URL":"https://api.example.com/anthropic"`, `"model_usage_aliases":`} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("snapshot JSON omitted %q: %s", required, encoded)
		}
	}
	for _, forbidden := range []string{"auth_token", "secret-token"} {
		if strings.Contains(strings.ToLower(encoded), forbidden) {
			t.Fatalf("snapshot JSON leaked %q: %s", forbidden, encoded)
		}
	}
	changed := first
	changed.Envs = CloneClaudeProfileEnvs(first.Envs)
	changed.Envs[ClaudeEnvBaseURL] = "https://other.example.com/anthropic"
	c, err := AgentProfileFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("fingerprint did not change when a redacted env changed")
	}
}

func TestAgentProfileSnapshotFingerprintCrossRuntimeVector(t *testing.T) {
	snapshot := AgentProfileSnapshot{
		SchemaVersion: ClaudeProfileSchemaV3,
		ProfileID:     "quality",
		DisplayName:   "极致<&>效果",
		Provider:      "moonshot",
		Protocol:      "anthropic",
		Envs: map[string]string{
			"ANTHROPIC_BASE_URL":                    "https://api.moonshot.cn/anthropic",
			"ANTHROPIC_MODEL":                       "kimi-k3[1m]",
			"ANTHROPIC_DEFAULT_OPUS_MODEL":          "kimi-k3[1m]",
			"ANTHROPIC_DEFAULT_FABLE_MODEL":         "kimi-k3[1m]",
			"ANTHROPIC_DEFAULT_SONNET_MODEL":        "kimi-k3[1m]",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":         "kimi-k3[1m]",
			"CLAUDE_CODE_EFFORT_LEVEL":              "high",
			"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT":      "true",
			"CLAUDE_CODE_MAX_CONTEXT_TOKENS":        "1048576",
			"CLAUDE_CODE_MAX_OUTPUT_TOKENS":         "131072",
			"MAX_THINKING_TOKENS":                   "0",
			"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING": "false",
			"CLAUDE_CODE_DISABLE_THINKING":          "false",
			"CLAUDE_CODE_AUTO_COMPACT_WINDOW":       "1048576",
			"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":       "80",
			"CLAUDE_CODE_DISABLE_1M_CONTEXT":        "false",
			"CLAUDE_CODE_SUBAGENT_MODEL":            "kimi-k3[1m]",
			"ENABLE_TOOL_SEARCH":                    "true",
		},
		ModelUsageAliases: map[string]string{"kimi-k3[1m]": "kimi-k3"},
	}

	got, err := AgentProfileFingerprint(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	const want = "ddeb3859ae9f15f7fd674caa0982326d05cce496d2426a8be892845643d31aa7"
	if got != want {
		t.Fatalf("fingerprint = %q, want cross-runtime vector %q", got, want)
	}
}

func TestAgentProfileSnapshotValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AgentProfileSnapshot)
		valid  bool
	}{
		{name: "valid", mutate: func(*AgentProfileSnapshot) {}, valid: true},
		{name: "schema v2", mutate: func(snapshot *AgentProfileSnapshot) { snapshot.SchemaVersion = 2 }},
		{name: "missing profile id", mutate: func(snapshot *AgentProfileSnapshot) { snapshot.ProfileID = "" }},
		{name: "missing display name", mutate: func(snapshot *AgentProfileSnapshot) { snapshot.DisplayName = " " }},
		{name: "missing provider", mutate: func(snapshot *AgentProfileSnapshot) { snapshot.Provider = "" }},
		{name: "wrong protocol", mutate: func(snapshot *AgentProfileSnapshot) { snapshot.Protocol = "openai" }},
		{name: "auth token", mutate: func(snapshot *AgentProfileSnapshot) { snapshot.Envs[ClaudeEnvAuthToken] = "secret-token" }},
		{name: "invalid env", mutate: func(snapshot *AgentProfileSnapshot) { snapshot.Envs["UNKNOWN"] = "value" }},
		{name: "missing referenced model alias uses identity", mutate: func(snapshot *AgentProfileSnapshot) { delete(snapshot.ModelUsageAliases, "opus-model") }, valid: true},
		{name: "empty alias source", mutate: func(snapshot *AgentProfileSnapshot) { snapshot.ModelUsageAliases[""] = "default" }},
		{name: "empty alias target", mutate: func(snapshot *AgentProfileSnapshot) { snapshot.ModelUsageAliases["extra"] = " " }},
		{name: "alias target contains slash", mutate: func(snapshot *AgentProfileSnapshot) { snapshot.ModelUsageAliases["extra"] = "provider/model" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validAgentProfileSnapshot()
			tt.mutate(&snapshot)
			err := ValidateAgentProfileSnapshot(snapshot)
			if tt.valid && err != nil {
				t.Fatalf("ValidateAgentProfileSnapshot() error = %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("ValidateAgentProfileSnapshot() error = nil, want error")
			}
		})
	}
}

func validAgentProfileSnapshot() AgentProfileSnapshot {
	return AgentProfileSnapshot{
		SchemaVersion: ClaudeProfileSchemaV3,
		ProfileID:     "quality",
		DisplayName:   "Maximum quality",
		Provider:      "moonshot",
		Protocol:      "anthropic",
		Envs:          validClaudeProfileEnvs(),
		ModelUsageAliases: map[string]string{
			"default-model": "default",
			"opus-model":    "opus",
			"fable-model":   "fable",
			"sonnet-model":  "sonnet",
			"haiku-model":   "haiku",
		},
	}
}

func TestAgentExecutionProfileSchemaMatchesMigrationContract(t *testing.T) {
	assertTagContains := func(t *testing.T, value any, fieldName string, want ...string) {
		t.Helper()
		field, ok := reflect.TypeOf(value).FieldByName(fieldName)
		if !ok {
			t.Fatalf("%T.%s is missing", value, fieldName)
		}
		tag := field.Tag.Get("gorm")
		for _, fragment := range want {
			if !strings.Contains(tag, fragment) {
				t.Fatalf("%T.%s gorm tag = %q, want %q", value, fieldName, tag, fragment)
			}
		}
	}

	assertTagContains(t, Task{}, "AgentProfileSnapshot", "type:json", "serializer:json", "not null")
	assertTagContains(t, Task{}, "AgentProfileFingerprint", "type:char(64)", "not null")
	for _, fieldName := range []string{"ExecutionProfile", "Provider", "ProfileEnvs", "ProfileFingerprint"} {
		assertTagContains(t, TaskExecution{}, fieldName, "not null")
	}
	assertTagContains(t, TaskExecution{}, "ProfileEnvs", "type:json", "serializer:json")
	assertTagContains(t, TaskExecution{}, "ProfileFingerprint", "type:char(64)", "index")
	assertTagContains(t, BillingSKU{}, "ExecutionProfile", "not null", "default:''", "index:idx_billing_skus_execution_profile", "index:idx_billing_skus_catalog_operation_profile,priority:3")
	assertTagContains(t, BillingSKU{}, "CatalogID", "index:idx_billing_skus_catalog_operation_profile,priority:1")
	assertTagContains(t, BillingSKU{}, "Operation", "index:idx_billing_skus_catalog_operation_profile,priority:2")
}
