package migrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

type AgentProfileEnvsBackfillOptions struct {
	BatchSize  int
	Profiles   map[string]config.ClaudeExecutionProfileConfig
	DryRun     bool
	VerifyOnly bool
}

type legacyAgentProfileSnapshotV2 struct {
	SchemaVersion     int                       `json:"schema_version"`
	ProfileID         string                    `json:"profile_id"`
	DisplayName       string                    `json:"display_name"`
	Provider          string                    `json:"provider"`
	Protocol          string                    `json:"protocol"`
	Models            model.AgentModelMatrix    `json:"models"`
	Claude            model.AgentClaudeControls `json:"claude"`
	ModelUsageAliases map[string]string         `json:"model_usage_aliases"`
}

type agentProfileBackfillTaskRow struct {
	ID               string `gorm:"column:id"`
	ExecutionProfile string `gorm:"column:execution_profile"`
	Snapshot         string `gorm:"column:agent_profile_snapshot"`
	Fingerprint      string `gorm:"column:agent_profile_fingerprint"`
}

var agentProfileIDV3 = map[string]string{
	"cost_effective":  "effective",
	"balanced":        "balanced",
	"maximum_quality": "quality",
	"effective":       "effective",
	"quality":         "quality",
}

func BackfillAgentProfileEnvs(ctx context.Context, db *gorm.DB, options AgentProfileEnvsBackfillOptions) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if options.BatchSize <= 0 {
		return fmt.Errorf("batch size must be positive")
	}
	for id, profile := range options.Profiles {
		if id != "effective" && id != "balanced" && id != "quality" {
			return fmt.Errorf("unsupported configured profile %q", id)
		}
		if strings.TrimSpace(profile.Provider) == "" {
			return fmt.Errorf("configured profile %q provider is required", id)
		}
		if err := model.ValidateClaudeProfileEnvs(profile.Envs, true); err != nil {
			return fmt.Errorf("configured profile %q envs are invalid: %w", id, err)
		}
	}
	if options.VerifyOnly {
		return VerifyAgentProfileEnvs(ctx, db)
	}

	cursor := ""
	for {
		var rows []agentProfileBackfillTaskRow
		if err := db.WithContext(ctx).Raw(`
SELECT id, execution_profile, agent_profile_snapshot, agent_profile_fingerprint
FROM tasks
WHERE id > ?
ORDER BY id
LIMIT ?`, cursor, options.BatchSize).Scan(&rows).Error; err != nil {
			return fmt.Errorf("load task backfill batch: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, row := range rows {
				if err := backfillAgentProfileTask(tx, row, options); err != nil {
					return err
				}
			}
			if options.DryRun {
				return errAgentProfileDryRunRollback
			}
			return nil
		}); err != nil && err != errAgentProfileDryRunRollback {
			return err
		}
		cursor = rows[len(rows)-1].ID
	}

	if err := backfillAgentProfilePlans(ctx, db, options.DryRun); err != nil {
		return err
	}
	if options.DryRun {
		return nil
	}
	return VerifyAgentProfileEnvs(ctx, db)
}

var errAgentProfileDryRunRollback = fmt.Errorf("agent profile env dry-run rollback")

func backfillAgentProfileTask(tx *gorm.DB, row agentProfileBackfillTaskRow, options AgentProfileEnvsBackfillOptions) error {
	var version struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal([]byte(row.Snapshot), &version); err != nil {
		return fmt.Errorf("task %s snapshot is invalid JSON: %w", row.ID, err)
	}
	if version.SchemaVersion == model.ClaudeProfileSchemaV3 {
		var snapshot model.AgentProfileSnapshot
		if err := json.Unmarshal([]byte(row.Snapshot), &snapshot); err != nil {
			return fmt.Errorf("task %s v3 snapshot is invalid: %w", row.ID, err)
		}
		fingerprint, err := model.AgentProfileFingerprint(snapshot)
		if err != nil || fingerprint != row.Fingerprint || snapshot.ProfileID != row.ExecutionProfile {
			return fmt.Errorf("task %s v3 snapshot verification failed", row.ID)
		}
		return verifyOrBackfillTaskExecutions(tx, row.ID, snapshot, fingerprint, options.DryRun)
	}
	if version.SchemaVersion != 2 {
		return fmt.Errorf("task %s has unsupported snapshot schema %d", row.ID, version.SchemaVersion)
	}

	var legacy legacyAgentProfileSnapshotV2
	if err := json.Unmarshal([]byte(row.Snapshot), &legacy); err != nil {
		return fmt.Errorf("task %s legacy snapshot is invalid: %w", row.ID, err)
	}
	newID, ok := agentProfileIDV3[legacy.ProfileID]
	if !ok {
		return fmt.Errorf("task %s has unknown profile %q", row.ID, legacy.ProfileID)
	}
	if mapped, ok := agentProfileIDV3[row.ExecutionProfile]; !ok || mapped != newID {
		return fmt.Errorf("task %s execution profile conflicts with snapshot", row.ID)
	}
	configured, ok := options.Profiles[newID]
	if !ok {
		return fmt.Errorf("task %s requires missing configured profile %q", row.ID, newID)
	}
	if strings.TrimSpace(configured.Provider) != strings.TrimSpace(legacy.Provider) {
		return fmt.Errorf("task %s provider %q conflicts with configured profile %q", row.ID, legacy.Provider, configured.Provider)
	}
	envs := legacyProfileEnvs(legacy, configured.Envs[model.ClaudeEnvBaseURL])
	snapshot := model.AgentProfileSnapshot{
		SchemaVersion: model.ClaudeProfileSchemaV3, ProfileID: newID, DisplayName: legacy.DisplayName,
		Provider: legacy.Provider, Protocol: legacy.Protocol, Envs: envs,
		ModelUsageAliases: cloneAliases(legacy.ModelUsageAliases),
	}
	fingerprint, err := model.AgentProfileFingerprint(snapshot)
	if err != nil {
		return fmt.Errorf("task %s converted snapshot is invalid: %w", row.ID, err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("task %s encode converted snapshot: %w", row.ID, err)
	}
	if options.DryRun {
		return verifyOrBackfillTaskExecutions(tx, row.ID, snapshot, fingerprint, true)
	}
	if err := tx.Exec(`UPDATE tasks SET execution_profile=?, agent_profile_snapshot=?, agent_profile_fingerprint=? WHERE id=?`, newID, string(encoded), fingerprint, row.ID).Error; err != nil {
		return fmt.Errorf("task %s update snapshot: %w", row.ID, err)
	}
	return verifyOrBackfillTaskExecutions(tx, row.ID, snapshot, fingerprint, false)
}

func legacyProfileEnvs(legacy legacyAgentProfileSnapshotV2, baseURL string) map[string]string {
	envs := map[string]string{
		model.ClaudeEnvBaseURL:           baseURL,
		model.ClaudeEnvModel:             legacy.Models.Default,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   legacy.Models.Opus,
		"ANTHROPIC_DEFAULT_FABLE_MODEL":  legacy.Models.Fable,
		"ANTHROPIC_DEFAULT_SONNET_MODEL": legacy.Models.Sonnet,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  legacy.Models.Haiku,
	}
	stringValue := func(key string, value *string) {
		if value != nil {
			envs[key] = *value
		}
	}
	boolValue := func(key string, value *bool) {
		if value != nil {
			envs[key] = strconv.FormatBool(*value)
		}
	}
	intValue := func(key string, value *int) {
		if value != nil {
			envs[key] = strconv.Itoa(*value)
		}
	}
	stringValue("CLAUDE_CODE_EFFORT_LEVEL", legacy.Claude.EffortLevel)
	boolValue("CLAUDE_CODE_ALWAYS_ENABLE_EFFORT", legacy.Claude.AlwaysEnableEffort)
	intValue("CLAUDE_CODE_MAX_CONTEXT_TOKENS", legacy.Claude.MaxContextTokens)
	intValue("CLAUDE_CODE_MAX_OUTPUT_TOKENS", legacy.Claude.MaxOutputTokens)
	intValue("MAX_THINKING_TOKENS", legacy.Claude.MaxThinkingTokens)
	boolValue("CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING", legacy.Claude.DisableAdaptiveThinking)
	boolValue("CLAUDE_CODE_DISABLE_THINKING", legacy.Claude.DisableThinking)
	intValue("CLAUDE_CODE_AUTO_COMPACT_WINDOW", legacy.Claude.AutoCompactWindow)
	intValue("CLAUDE_AUTOCOMPACT_PCT_OVERRIDE", legacy.Claude.AutocompactPctOverride)
	boolValue("CLAUDE_CODE_DISABLE_1M_CONTEXT", legacy.Claude.Disable1MContext)
	stringValue("CLAUDE_CODE_SUBAGENT_MODEL", legacy.Claude.SubagentModel)
	boolValue("ENABLE_TOOL_SEARCH", legacy.Claude.EnableToolSearch)
	return envs
}

func verifyOrBackfillTaskExecutions(tx *gorm.DB, taskID string, snapshot model.AgentProfileSnapshot, fingerprint string, dryRun bool) error {
	envs, err := json.Marshal(snapshot.Envs)
	if err != nil {
		return err
	}
	if dryRun {
		return nil
	}
	result := tx.Exec(`UPDATE task_executions SET execution_profile=?, provider=?, profile_envs=?, profile_fingerprint=? WHERE task_id=?`, snapshot.ProfileID, snapshot.Provider, string(envs), fingerprint, taskID)
	if result.Error != nil {
		return fmt.Errorf("task %s update executions: %w", taskID, result.Error)
	}
	return nil
}

func backfillAgentProfilePlans(ctx context.Context, db *gorm.DB, dryRun bool) error {
	var rows []struct{ ID, ExecutionProfile string }
	if err := db.WithContext(ctx).Raw(`SELECT id, execution_profile FROM plans ORDER BY id`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load plans: %w", err)
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			mapped, ok := agentProfileIDV3[row.ExecutionProfile]
			if !ok {
				return fmt.Errorf("plan %s has unknown profile %q", row.ID, row.ExecutionProfile)
			}
			if mapped != row.ExecutionProfile {
				if err := tx.Exec(`UPDATE plans SET execution_profile=? WHERE id=?`, mapped, row.ID).Error; err != nil {
					return fmt.Errorf("plan %s update profile: %w", row.ID, err)
				}
			}
		}
		if dryRun {
			return errAgentProfileDryRunRollback
		}
		return nil
	})
	if err == errAgentProfileDryRunRollback {
		return nil
	}
	return err
}

func VerifyAgentProfileEnvs(ctx context.Context, db *gorm.DB) error {
	var tasks []agentProfileBackfillTaskRow
	if err := db.WithContext(ctx).Raw(`SELECT id, execution_profile, agent_profile_snapshot, agent_profile_fingerprint FROM tasks ORDER BY id`).Scan(&tasks).Error; err != nil {
		return err
	}
	for _, row := range tasks {
		var snapshot model.AgentProfileSnapshot
		if err := json.Unmarshal([]byte(row.Snapshot), &snapshot); err != nil {
			return fmt.Errorf("task %s snapshot decode: %w", row.ID, err)
		}
		fingerprint, err := model.AgentProfileFingerprint(snapshot)
		if err != nil || fingerprint != row.Fingerprint || snapshot.ProfileID != row.ExecutionProfile {
			return fmt.Errorf("task %s snapshot fingerprint or identity mismatch", row.ID)
		}
		var executions []struct {
			ID               string `gorm:"column:id"`
			ExecutionProfile string `gorm:"column:execution_profile"`
			Provider         string `gorm:"column:provider"`
			ProfileEnvs      string `gorm:"column:profile_envs"`
			Fingerprint      string `gorm:"column:profile_fingerprint"`
		}
		if err := db.WithContext(ctx).Raw(`SELECT id, execution_profile, provider, profile_envs, profile_fingerprint FROM task_executions WHERE task_id=?`, row.ID).Scan(&executions).Error; err != nil {
			return err
		}
		for _, execution := range executions {
			var envs map[string]string
			if err := json.Unmarshal([]byte(execution.ProfileEnvs), &envs); err != nil {
				return fmt.Errorf("execution %s profile envs invalid: %w", execution.ID, err)
			}
			if execution.ExecutionProfile != snapshot.ProfileID || execution.Provider != snapshot.Provider || execution.Fingerprint != fingerprint || !equalEnvs(envs, snapshot.Envs) {
				return fmt.Errorf("execution %s profile does not match task %s", execution.ID, row.ID)
			}
		}
	}
	var plans []struct{ ID, ExecutionProfile string }
	if err := db.WithContext(ctx).Raw(`SELECT id, execution_profile FROM plans ORDER BY id`).Scan(&plans).Error; err != nil {
		return err
	}
	for _, plan := range plans {
		if plan.ExecutionProfile != "effective" && plan.ExecutionProfile != "balanced" && plan.ExecutionProfile != "quality" {
			return fmt.Errorf("plan %s has obsolete profile %q", plan.ID, plan.ExecutionProfile)
		}
	}
	return nil
}

func cloneAliases(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func equalEnvs(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
