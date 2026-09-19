package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

func TestTaskAPIResponseHidesServerInternalFields(t *testing.T) {
	now := time.Now()
	task := &model.Task{
		ID:                      uuid.NewString(),
		UserID:                  uuid.NewString(),
		ProjectID:               uuid.NewString(),
		Type:                    model.PlatformSeednote,
		Status:                  model.TaskStatusRunning,
		Prompt:                  "结合当下热点和时节创作",
		AgentProfileFingerprint: strings.Repeat("a", 64),
		CurrentExecutionID:      ptr(uuid.NewString()),
		LastHeartbeatAt:         &now,
		MaxRetries:              3,
		RetryCount:              1,
		RateLimitRetryCount:     2,
		BillingQuoteID:          "quote-internal",
		AgentProfileSnapshot: model.AgentProfileSnapshot{
			SchemaVersion: model.ClaudeProfileSchemaV3,
			ProfileID:     "effective",
			DisplayName:   "性价比",
			Provider:      "deepseek",
			Protocol:      "anthropic",
			Envs: map[string]string{
				model.ClaudeEnvBaseURL:                     "https://api.deepseek.com/anthropic",
				model.ClaudeEnvModel:                       "deepseek-flash[1m]",
				"ANTHROPIC_DEFAULT_OPUS_MODEL":             "deepseek-flash[1m]",
				"ANTHROPIC_DEFAULT_SONNET_MODEL":           "deepseek-flash[1m]",
				"ANTHROPIC_DEFAULT_FABLE_MODEL":            "deepseek-flash",
				"ANTHROPIC_DEFAULT_HAIKU_MODEL":            "deepseek-flash",
				"CLAUDE_CODE_EFFORT_LEVEL":                 "max",
				"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           "1048576",
				"CLAUDE_CODE_DISABLE_THINKING":             "false",
				"CLAUDE_CODE_SUBAGENT_MODEL":               "deepseek-flash",
				"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "786432",
				"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			},
			ModelUsageAliases: map[string]string{
				"deepseek-flash":     "deepseek-flash",
				"deepseek-flash[1m]": "deepseek-flash",
			},
		},
		ProgressLog: "[claude-code:unrecognized_model] {\"model\":\"deepseek-flash[1m]\"}\n" +
			"选题研究完成，开始内容创作\n" +
			"Using tool: mcp__anban__set_task_progress_plan\n" +
			"Using tool: mcp__anban__search_seednote_feeds\n" +
			"正在生成封面图\n",
	}

	resp := taskAPIResponse(task, nil)

	for _, key := range []string{
		"agent_profile_fingerprint",
		"current_execution_id",
		"last_heartbeat_at",
		"max_retries",
		"rate_limit_retry_count",
		"retry_count",
		"billing_quote_id",
	} {
		if _, exists := resp[key]; exists {
			t.Fatalf("response leaked Server-internal field %q", key)
		}
	}

	profile, ok := resp["agent_profile_snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("agent_profile_snapshot = %#v, want map", resp["agent_profile_snapshot"])
	}
	if profile["profile_id"] != "effective" || profile["display_name"] != "性价比" || profile["schema_version"] != float64(model.ClaudeProfileSchemaV3) {
		t.Fatalf("agent_profile_snapshot identity = %#v", profile)
	}
	for _, key := range []string{"provider", "protocol", "model_usage_aliases"} {
		if _, exists := profile[key]; exists {
			t.Fatalf("agent_profile_snapshot leaked internal key %q", key)
		}
	}
	envs, ok := profile["envs"].(map[string]any)
	if !ok {
		t.Fatalf("agent_profile_snapshot envs = %#v, want map", profile["envs"])
	}
	for _, key := range []string{
		model.ClaudeEnvBaseURL, model.ClaudeEnvModel,
		"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_FABLE_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"CLAUDE_CODE_SUBAGENT_MODEL", "CLAUDE_CODE_AUTO_COMPACT_WINDOW",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
	} {
		if _, exists := envs[key]; exists {
			t.Fatalf("agent_profile_snapshot envs leaked internal key %q", key)
		}
	}
	for _, key := range []string{
		"CLAUDE_CODE_EFFORT_LEVEL", "CLAUDE_CODE_MAX_CONTEXT_TOKENS", "CLAUDE_CODE_DISABLE_THINKING",
	} {
		if _, exists := envs[key]; !exists {
			t.Fatalf("agent_profile_snapshot envs missing public key %q", key)
		}
	}

	log, ok := resp["progress_log"].(string)
	if !ok {
		t.Fatalf("progress_log = %#v, want string", resp["progress_log"])
	}
	if strings.Contains(log, "[claude-code:") || strings.Contains(log, "unrecognized_model") || strings.Contains(log, "Using tool:") {
		t.Fatalf("progress_log leaked internal lines: %q", log)
	}
	for _, want := range []string{"选题研究完成，开始内容创作", "正在生成封面图"} {
		if !strings.Contains(log, want) {
			t.Fatalf("progress_log dropped user-facing line %q: %q", want, log)
		}
	}
}

func TestTaskAPIResponseDropsEmptyProfileAndCleansProgressLog(t *testing.T) {
	task := &model.Task{
		ID:          uuid.NewString(),
		UserID:      uuid.NewString(),
		ProjectID:   uuid.NewString(),
		Type:        model.PlatformArticle,
		Status:      model.TaskStatusCompleted,
		Prompt:      "测试",
		ProgressLog: "Using tool: Read\n完成\n",
	}
	resp := taskAPIResponse(task, nil)
	if _, exists := resp["agent_profile_snapshot"]; exists {
		t.Fatalf("empty agent profile snapshot should be dropped, got %#v", resp["agent_profile_snapshot"])
	}
	if got := resp["progress_log"]; got != "完成" {
		t.Fatalf("progress_log = %#v, want %q", got, "完成")
	}
}

func ptr(v string) *string { return &v }
