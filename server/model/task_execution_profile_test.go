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
	for _, redundant := range []string{"AgentProfileID", "AgentProfileSnapshot", "ModelID", "Protocol", "ReasoningEffort", "ContextWindow"} {
		if _, exists := executionType.FieldByName(redundant); exists {
			t.Fatalf("TaskExecution must not duplicate task snapshot field %s", redundant)
		}
	}
	disabled := false
	snapshot := AgentProfileSnapshot{
		SchemaVersion: 2, ProfileID: "maximum_quality", Provider: "moonshot", Protocol: "anthropic", DisplayName: "Kimi K3（1M）",
		Models:            AgentModelMatrix{Default: "kimi-k3[1m]", Opus: "kimi-k3[1m]", Fable: "kimi-k3[1m]", Sonnet: "kimi-k3[1m]", Haiku: "kimi-k3[1m]"},
		Claude:            AgentClaudeControls{EnableToolSearch: &disabled},
		ModelUsageAliases: map[string]string{"kimi-k3[1m]": "kimi-k3"},
	}
	execution := NewTaskExecutionAgentProfile(snapshot, strings.Repeat("a", 64))
	if execution.ExecutionProfile != "maximum_quality" || execution.Provider != "moonshot" || execution.ModelMatrix.Default != "kimi-k3[1m]" || execution.ClaudeControls.EnableToolSearch == nil || *execution.ClaudeControls.EnableToolSearch || execution.ProfileFingerprint != strings.Repeat("a", 64) {
		t.Fatalf("execution = %#v", execution)
	}
}

func TestAgentProfileSnapshotFingerprintIsCanonical(t *testing.T) {
	disabled := false
	first := AgentProfileSnapshot{
		SchemaVersion: 2, ProfileID: "maximum_quality", DisplayName: "极致效果",
		Provider: "moonshot", Protocol: "anthropic",
		Models:            AgentModelMatrix{Default: "kimi-k3[1m]", Opus: "kimi-k3[1m]", Fable: "kimi-k3[1m]", Sonnet: "kimi-k3[1m]", Haiku: "kimi-k3[1m]"},
		Claude:            AgentClaudeControls{EnableToolSearch: &disabled},
		ModelUsageAliases: map[string]string{"kimi-k3[1m]": "kimi-k3", "kimi-k3": "kimi-k3"},
	}
	second := first
	second.ModelUsageAliases = map[string]string{"kimi-k3": "kimi-k3", "kimi-k3[1m]": "kimi-k3"}
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
	for _, required := range []string{`"schema_version":2`, `"models":`, `"claude":`, `"enable_tool_search":false`, `"model_usage_aliases":`} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("snapshot JSON omitted %q: %s", required, encoded)
		}
	}
	for _, forbidden := range []string{"base_url", "auth_token", "secret", "endpoint"} {
		if strings.Contains(strings.ToLower(encoded), forbidden) {
			t.Fatalf("snapshot JSON leaked %q: %s", forbidden, encoded)
		}
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
	for _, fieldName := range []string{"ExecutionProfile", "Provider", "ModelMatrix", "ClaudeControls", "ProfileFingerprint"} {
		assertTagContains(t, TaskExecution{}, fieldName, "not null")
	}
	assertTagContains(t, TaskExecution{}, "ModelMatrix", "type:json", "serializer:json")
	assertTagContains(t, TaskExecution{}, "ClaudeControls", "type:json", "serializer:json")
	assertTagContains(t, TaskExecution{}, "ProfileFingerprint", "type:char(64)", "index")
	assertTagContains(t, BillingSKU{}, "ExecutionProfile", "not null", "default:''", "index:idx_billing_skus_execution_profile", "index:idx_billing_skus_catalog_operation_profile,priority:3")
	assertTagContains(t, BillingSKU{}, "CatalogID", "index:idx_billing_skus_catalog_operation_profile,priority:1")
	assertTagContains(t, BillingSKU{}, "Operation", "index:idx_billing_skus_catalog_operation_profile,priority:2")
}
