package model

import (
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
	for _, redundant := range []string{"AgentProfileID", "AgentProfileSnapshot"} {
		if _, exists := executionType.FieldByName(redundant); exists {
			t.Fatalf("TaskExecution must not duplicate task snapshot field %s", redundant)
		}
	}
	snapshot := AgentProfileSnapshot{
		ProfileID: "maximum_quality", Provider: "kimi", ModelID: "k3", Protocol: "anthropic",
		ContextWindow: 1048576, ReasoningEffort: "high", ThinkingRequired: true, DisplayName: "Kimi K3（1M）",
		BaseURL: "https://secret.invalid", AuthToken: "secret-token",
	}
	execution := NewTaskExecutionAgentProfile(snapshot)
	if execution.Provider != "kimi" || execution.ModelID != "k3" || execution.Protocol != "anthropic" || execution.ReasoningEffort != "high" || execution.ContextWindow != 1048576 {
		t.Fatalf("execution = %#v", execution)
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
	for _, fieldName := range []string{"Provider", "ModelID", "Protocol", "ReasoningEffort", "ContextWindow"} {
		assertTagContains(t, TaskExecution{}, fieldName, "not null")
	}
	assertTagContains(t, TaskExecution{}, "Provider", "index:idx_task_executions_provider_model,priority:1")
	assertTagContains(t, TaskExecution{}, "ModelID", "index:idx_task_executions_provider_model,priority:2")
	assertTagContains(t, BillingSKU{}, "ExecutionProfile", "not null", "default:''", "index:idx_billing_skus_execution_profile", "index:idx_billing_skus_catalog_operation_profile,priority:3")
	assertTagContains(t, BillingSKU{}, "CatalogID", "index:idx_billing_skus_catalog_operation_profile,priority:1")
	assertTagContains(t, BillingSKU{}, "Operation", "index:idx_billing_skus_catalog_operation_profile,priority:2")
}
