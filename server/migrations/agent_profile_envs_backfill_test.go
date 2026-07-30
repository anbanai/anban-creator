package migrations

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func backfillProfiles() map[string]config.ClaudeExecutionProfileConfig {
	envs := func(endpoint, token, modelID string) map[string]string {
		return map[string]string{
			model.ClaudeEnvBaseURL: endpoint, model.ClaudeEnvAuthToken: token, model.ClaudeEnvModel: modelID,
			"ANTHROPIC_DEFAULT_OPUS_MODEL": modelID, "ANTHROPIC_DEFAULT_FABLE_MODEL": modelID,
			"ANTHROPIC_DEFAULT_SONNET_MODEL": modelID, "ANTHROPIC_DEFAULT_HAIKU_MODEL": modelID,
		}
	}
	return map[string]config.ClaudeExecutionProfileConfig{
		"effective": {Provider: "deepseek", Envs: envs("https://deepseek.test/anthropic", "secret", "new-effective"), ModelUsageAliases: map[string]string{"new-effective": "new-effective"}},
		"balanced":  {Provider: "volcengine_ark", Envs: envs("https://ark.test/anthropic", "secret", "new-balanced"), ModelUsageAliases: map[string]string{"new-balanced": "new-balanced"}},
		"quality":   {Provider: "moonshot", Envs: envs("https://moonshot.test/anthropic", "secret", "new-quality"), ModelUsageAliases: map[string]string{"new-quality": "new-quality"}},
	}
}

func openBackfillFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "-")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE tasks (id text PRIMARY KEY, execution_profile text NOT NULL, agent_profile_snapshot text NOT NULL, agent_profile_fingerprint text NOT NULL)`,
		`CREATE TABLE plans (id text PRIMARY KEY, execution_profile text NOT NULL)`,
		`CREATE TABLE task_executions (id text PRIMARY KEY, task_id text NOT NULL, execution_profile text NOT NULL, provider text NOT NULL, model_matrix text NOT NULL, claude_controls text NOT NULL, profile_envs text NULL, profile_fingerprint text NOT NULL)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func legacySnapshotJSON(t *testing.T, profileID string) string {
	t.Helper()
	provider := map[string]string{"cost_effective": "deepseek", "balanced": "volcengine_ark", "maximum_quality": "moonshot"}[profileID]
	modelID := map[string]string{"cost_effective": "legacy-effective", "balanced": "legacy-balanced", "maximum_quality": "legacy-quality"}[profileID]
	snapshot := map[string]any{
		"schema_version": 2, "profile_id": profileID, "display_name": "Legacy", "provider": provider, "protocol": "anthropic",
		"models":              map[string]string{"default": modelID, "opus": modelID, "fable": modelID, "sonnet": modelID, "haiku": modelID},
		"claude":              map[string]any{"effort_level": "high", "max_thinking_tokens": 0, "disable_thinking": false},
		"model_usage_aliases": map[string]string{modelID: modelID},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func seedLegacyTask(t *testing.T, db *gorm.DB, id, profileID string) {
	t.Helper()
	snapshot := legacySnapshotJSON(t, profileID)
	if err := db.Exec(`INSERT INTO tasks(id, execution_profile, agent_profile_snapshot, agent_profile_fingerprint) VALUES(?,?,?,?)`, id, profileID, snapshot, strings.Repeat("0", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO task_executions(id, task_id, execution_profile, provider, model_matrix, claude_controls, profile_fingerprint) VALUES(?,?,?,?,?,?,?)`, "exec-"+id, id, profileID, "legacy", `{}`, `{}`, strings.Repeat("0", 64)).Error; err != nil {
		t.Fatal(err)
	}
}

func TestBackfillAgentProfileEnvsConvertsV2AndIsIdempotent(t *testing.T) {
	db := openBackfillFixture(t)
	seedLegacyTask(t, db, "task-a", "cost_effective")
	if err := db.Exec(`INSERT INTO plans(id, execution_profile) VALUES('plan-a','maximum_quality')`).Error; err != nil {
		t.Fatal(err)
	}
	options := AgentProfileEnvsBackfillOptions{BatchSize: 1, Profiles: backfillProfiles()}
	if err := BackfillAgentProfileEnvs(t.Context(), db, options); err != nil {
		t.Fatal(err)
	}
	if err := BackfillAgentProfileEnvs(t.Context(), db, options); err != nil {
		t.Fatalf("idempotent rerun: %v", err)
	}
	var task struct{ ExecutionProfile, Snapshot, Fingerprint string }
	if err := db.Raw(`SELECT execution_profile, agent_profile_snapshot AS snapshot, agent_profile_fingerprint AS fingerprint FROM tasks WHERE id='task-a'`).Scan(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.ExecutionProfile != "effective" || len(task.Fingerprint) != 64 {
		t.Fatalf("task = %#v", task)
	}
	var snapshot model.AgentProfileSnapshot
	if err := json.Unmarshal([]byte(task.Snapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != 3 || snapshot.ProfileID != "effective" || snapshot.Envs[model.ClaudeEnvModel] != "legacy-effective" || snapshot.Envs[model.ClaudeEnvBaseURL] != "https://deepseek.test/anthropic" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if _, ok := snapshot.Envs[model.ClaudeEnvAuthToken]; ok {
		t.Fatal("backfilled snapshot contains token")
	}
	if got, err := model.AgentProfileFingerprint(snapshot); err != nil || got != task.Fingerprint {
		t.Fatalf("fingerprint = %q, want %q, err=%v", task.Fingerprint, got, err)
	}
	var execution struct{ ExecutionProfile, Provider, ProfileEnvs, Fingerprint string }
	if err := db.Raw(`SELECT execution_profile, provider, profile_envs, profile_fingerprint AS fingerprint FROM task_executions WHERE task_id='task-a'`).Scan(&execution).Error; err != nil {
		t.Fatal(err)
	}
	if execution.ExecutionProfile != "effective" || execution.Provider != "deepseek" || execution.Fingerprint != task.Fingerprint || strings.Contains(execution.ProfileEnvs, model.ClaudeEnvAuthToken) {
		t.Fatalf("execution = %#v", execution)
	}
	var planProfile string
	if err := db.Raw(`SELECT execution_profile FROM plans WHERE id='plan-a'`).Scan(&planProfile).Error; err != nil || planProfile != "quality" {
		t.Fatalf("plan profile=%q err=%v", planProfile, err)
	}
}

func TestBackfillAgentProfileEnvsRollsBackFailedBatch(t *testing.T) {
	db := openBackfillFixture(t)
	seedLegacyTask(t, db, "task-a", "cost_effective")
	seedLegacyTask(t, db, "task-b", "unknown")
	err := BackfillAgentProfileEnvs(t.Context(), db, AgentProfileEnvsBackfillOptions{BatchSize: 2, Profiles: backfillProfiles()})
	if err == nil {
		t.Fatal("backfill accepted unknown profile")
	}
	var profile string
	if scanErr := db.Raw(`SELECT execution_profile FROM tasks WHERE id='task-a'`).Scan(&profile).Error; scanErr != nil || profile != "cost_effective" {
		t.Fatalf("first task escaped rollback: profile=%q err=%v", profile, scanErr)
	}
}

func TestBackfillAgentProfileEnvsDryRunDoesNotWrite(t *testing.T) {
	db := openBackfillFixture(t)
	seedLegacyTask(t, db, "task-a", "cost_effective")
	if err := db.Exec(`INSERT INTO plans(id, execution_profile) VALUES('plan-a','maximum_quality')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillAgentProfileEnvs(t.Context(), db, AgentProfileEnvsBackfillOptions{BatchSize: 1, Profiles: backfillProfiles(), DryRun: true}); err != nil {
		t.Fatal(err)
	}
	var taskProfile, planProfile string
	if err := db.Raw(`SELECT execution_profile FROM tasks WHERE id='task-a'`).Scan(&taskProfile).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT execution_profile FROM plans WHERE id='plan-a'`).Scan(&planProfile).Error; err != nil {
		t.Fatal(err)
	}
	if taskProfile != "cost_effective" || planProfile != "maximum_quality" {
		t.Fatalf("dry run wrote task=%q plan=%q", taskProfile, planProfile)
	}
}

func TestVerifyAgentProfileEnvsRejectsFingerprintMismatch(t *testing.T) {
	db := openBackfillFixture(t)
	seedLegacyTask(t, db, "task-a", "balanced")
	options := AgentProfileEnvsBackfillOptions{BatchSize: 1, Profiles: backfillProfiles()}
	if err := BackfillAgentProfileEnvs(t.Context(), db, options); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE tasks SET agent_profile_fingerprint=? WHERE id='task-a'`, strings.Repeat("f", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillAgentProfileEnvs(t.Context(), db, AgentProfileEnvsBackfillOptions{BatchSize: 1, Profiles: backfillProfiles(), VerifyOnly: true}); err == nil {
		t.Fatal("verify-only accepted mismatched fingerprint")
	}
}
