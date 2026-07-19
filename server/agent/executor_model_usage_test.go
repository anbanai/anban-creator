package agent

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

func TestExecutionResultUsesTerminalPerModelUsage(t *testing.T) {
	cost := 99.75
	topLevelUsage := map[string]any{
		"input_tokens":                999_999,
		"output_tokens":               888_888,
		"cache_read_input_tokens":     777_777,
		"cache_creation_input_tokens": 666_666,
	}
	resultMessage := &claudecode.ResultMessage{
		TotalCostUSD: &cost,
		Usage:        &topLevelUsage,
		ModelUsage: map[string]claudecode.ModelUsage{
			"raw-turbo": {
				InputTokens:              875,
				OutputTokens:             3,
				CacheReadInputTokens:     41,
				CacheCreationInputTokens: 5,
				CostUSD:                  13.25,
				ContextWindow:            200_000,
			},
			"raw-evolving": {
				InputTokens:              4_807,
				OutputTokens:             198,
				CacheReadInputTokens:     7_792,
				CacheCreationInputTokens: 12,
				CostUSD:                  86.50,
				ContextWindow:            1_000_000,
			},
		},
	}
	aliases := map[string]ModelUsageIdentity{
		"raw-turbo":    {Provider: "volcengine_ark", Model: "doubao-seed-2-1-turbo-260628"},
		"raw-evolving": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
	}

	result := &ExecutionResult{}
	PopulateTerminalModelUsage(result, resultMessage, aliases)

	want := []ModelTokenUsage{
		{
			Provider: "volcengine_ark", Model: "doubao-seed-2-1-turbo-260628",
			InputTokens: 875, OutputTokens: 3, CacheReadInputTokens: 41, CacheCreationInputTokens: 5,
		},
		{
			Provider: "volcengine_ark", Model: "doubao-seed-evolving",
			InputTokens: 4_807, OutputTokens: 198, CacheReadInputTokens: 7_792, CacheCreationInputTokens: 12,
		},
	}
	if !reflect.DeepEqual(result.ModelUsage, want) {
		t.Fatalf("ModelUsage = %#v, want %#v", result.ModelUsage, want)
	}
	if result.CostStatus != CostStatusReconciled {
		t.Fatalf("CostStatus = %q, want %q", result.CostStatus, CostStatusReconciled)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	for _, forbidden := range []string{"total_cost_usd", "token_usage", "costUSD", "contextWindow", `"usage":`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("execution result retained non-authoritative %q: %s", forbidden, raw)
		}
	}
}

func TestOnlyTerminalResultMessageCanSetModelUsage(t *testing.T) {
	aliases := map[string]ModelUsageIdentity{
		"raw-evolving": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
	}
	result := &ExecutionResult{}
	assistant := &claudecode.AssistantMessage{Model: "raw-evolving"}
	system := &claudecode.SystemMessage{
		Subtype: "task_notification",
		Data: map[string]any{
			"usage": map[string]any{"total_tokens": 92_233},
			"modelUsage": map[string]any{
				"raw-evolving": map[string]any{"inputTokens": 50_000},
			},
		},
	}

	PopulateTerminalModelUsage(result, assistant, aliases)
	PopulateTerminalModelUsage(result, system, aliases)
	if len(result.ModelUsage) != 0 || result.CostStatus != CostStatusUnreconciled {
		t.Fatalf("partial events changed terminal usage: %+v", result)
	}

	terminal := &claudecode.ResultMessage{ModelUsage: map[string]claudecode.ModelUsage{
		"raw-evolving": {InputTokens: 17, OutputTokens: 4},
	}}
	PopulateTerminalModelUsage(result, terminal, aliases)
	want := append([]ModelTokenUsage(nil), result.ModelUsage...)
	PopulateTerminalModelUsage(result, assistant, aliases)
	PopulateTerminalModelUsage(result, system, aliases)
	if !reflect.DeepEqual(result.ModelUsage, want) || result.CostStatus != CostStatusReconciled {
		t.Fatalf("partial events changed populated terminal usage: %+v, want %+v", result.ModelUsage, want)
	}
}

func TestExecutionResultMissingTerminalModelUsageIsUnreconciled(t *testing.T) {
	cost := 123.45
	topLevelUsage := map[string]any{"input_tokens": 99_999, "output_tokens": 88_888}
	tests := []struct {
		name    string
		message claudecode.Message
	}{
		{name: "missing terminal result"},
		{name: "nil terminal model usage", message: &claudecode.ResultMessage{ModelUsage: nil}},
		{name: "empty terminal model usage", message: &claudecode.ResultMessage{ModelUsage: map[string]claudecode.ModelUsage{}}},
		{
			name: "monetary and top-level usage only",
			message: &claudecode.ResultMessage{
				TotalCostUSD: &cost, Usage: &topLevelUsage,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := &ExecutionResult{}
			PopulateTerminalModelUsage(result, test.message, nil)
			if result.CostStatus != CostStatusUnreconciled {
				t.Fatalf("CostStatus = %q, want %q", result.CostStatus, CostStatusUnreconciled)
			}
			if len(result.ModelUsage) != 0 {
				t.Fatalf("ModelUsage = %+v, want empty", result.ModelUsage)
			}
			if len(result.CostDiagnostics) != 1 || result.CostDiagnostics[0].Code != CostDiagnosticMissingTerminalModelUsage {
				t.Fatalf("CostDiagnostics = %+v, want typed missing-usage diagnostic", result.CostDiagnostics)
			}
		})
	}
}

func TestExecutionResultRejectsInvalidTerminalModelUsageWithoutPanicking(t *testing.T) {
	tests := []struct {
		name       string
		usage      map[string]claudecode.ModelUsage
		aliases    map[string]ModelUsageIdentity
		wantCode   string
		wantModels int
	}{
		{
			name: "negative tokens",
			usage: map[string]claudecode.ModelUsage{
				"raw-evolving": {InputTokens: -1, OutputTokens: 4},
			},
			aliases: map[string]ModelUsageIdentity{
				"raw-evolving": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
			},
			wantCode: CostDiagnosticInvalidModelUsageTokens,
		},
		{
			name: "checked alias merge overflow",
			usage: map[string]claudecode.ModelUsage{
				"raw-evolving-a": {InputTokens: math.MaxInt64},
				"raw-evolving-b": {InputTokens: 1},
			},
			aliases: map[string]ModelUsageIdentity{
				"raw-evolving-a": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
				"raw-evolving-b": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
			},
			wantCode: CostDiagnosticModelUsageTokenOverflow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := &ExecutionResult{}
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Fatalf("PopulateTerminalModelUsage panicked: %v", recovered)
					}
				}()
				PopulateTerminalModelUsage(result, &claudecode.ResultMessage{ModelUsage: test.usage}, test.aliases)
			}()

			if result.CostStatus != CostStatusUnreconciled {
				t.Fatalf("CostStatus = %q, want %q", result.CostStatus, CostStatusUnreconciled)
			}
			if len(result.ModelUsage) != test.wantModels {
				t.Fatalf("ModelUsage = %+v, want %d entries", result.ModelUsage, test.wantModels)
			}
			if len(result.CostDiagnostics) != 1 || result.CostDiagnostics[0].Code != test.wantCode {
				t.Fatalf("CostDiagnostics = %+v, want code %q", result.CostDiagnostics, test.wantCode)
			}
		})
	}
}

func TestExecutionResultNormalizesOnlyExactConfiguredAliases(t *testing.T) {
	result := &ExecutionResult{}
	PopulateTerminalModelUsage(result, &claudecode.ResultMessage{ModelUsage: map[string]claudecode.ModelUsage{
		"doubao-seed-evolving-latest-version": {InputTokens: 10},
		"doubao-seed-evolving":                {OutputTokens: 7},
	}}, map[string]ModelUsageIdentity{
		"doubao-seed-evolving-latest-version": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
	})

	want := []ModelTokenUsage{
		{Model: "doubao-seed-evolving", OutputTokens: 7},
		{Provider: "volcengine_ark", Model: "doubao-seed-evolving", InputTokens: 10},
	}
	if !reflect.DeepEqual(result.ModelUsage, want) {
		t.Fatalf("ModelUsage = %#v, want exact-alias-only result %#v", result.ModelUsage, want)
	}
	if result.CostStatus != CostStatusUnreconciled {
		t.Fatalf("CostStatus = %q, want %q for unmapped raw model", result.CostStatus, CostStatusUnreconciled)
	}
	if len(result.CostDiagnostics) != 1 || result.CostDiagnostics[0].Code != CostDiagnosticUnmappedModelUsageAlias || result.CostDiagnostics[0].RawModel != "doubao-seed-evolving" {
		t.Fatalf("CostDiagnostics = %+v, want exact unmapped alias diagnostic", result.CostDiagnostics)
	}
}

func TestExecutionResultMergesAliasesForSameCanonicalModelOnce(t *testing.T) {
	aliases := map[string]ModelUsageIdentity{
		"evolving-a": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
		"evolving-b": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
	}
	result := &ExecutionResult{}
	PopulateTerminalModelUsage(result, &claudecode.ResultMessage{ModelUsage: map[string]claudecode.ModelUsage{
		"evolving-b": {InputTokens: 7, OutputTokens: 11, CacheReadInputTokens: 13, CacheCreationInputTokens: 17},
		"evolving-a": {InputTokens: 2, OutputTokens: 3, CacheReadInputTokens: 5, CacheCreationInputTokens: 7},
	}}, aliases)

	want := []ModelTokenUsage{{
		Provider: "volcengine_ark", Model: "doubao-seed-evolving",
		InputTokens: 9, OutputTokens: 14, CacheReadInputTokens: 18, CacheCreationInputTokens: 24,
	}}
	if !reflect.DeepEqual(result.ModelUsage, want) {
		t.Fatalf("ModelUsage = %#v, want merged %#v", result.ModelUsage, want)
	}
	if result.CostStatus != CostStatusReconciled || len(result.CostDiagnostics) != 0 {
		t.Fatalf("unexpected reconciliation state: status=%q diagnostics=%+v", result.CostStatus, result.CostDiagnostics)
	}
}

func TestExecutionResultRetainsTerminalModelUsageOnErrorResult(t *testing.T) {
	result := &ExecutionResult{}
	PopulateTerminalModelUsage(result, &claudecode.ResultMessage{
		IsError: true,
		ModelUsage: map[string]claudecode.ModelUsage{
			"raw-evolving": {InputTokens: 37, OutputTokens: 41},
		},
	}, map[string]ModelUsageIdentity{
		"raw-evolving": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
	})

	want := []ModelTokenUsage{{
		Provider: "volcengine_ark", Model: "doubao-seed-evolving", InputTokens: 37, OutputTokens: 41,
	}}
	if !reflect.DeepEqual(result.ModelUsage, want) || result.CostStatus != CostStatusReconciled {
		t.Fatalf("error result terminal usage = %+v status %q, want %+v/reconciled", result.ModelUsage, result.CostStatus, want)
	}
}
