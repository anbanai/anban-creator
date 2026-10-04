package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MigrateMultiAgentChannelIdentity backfills the new task/execution identity
// columns from historical type/platform values. It is intentionally separate
// from AutoMigrate so operators can run it as one reviewed cutover.
func MigrateMultiAgentChannelIdentity(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return nil
	}
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	legacyCutover := tx.Migrator().HasColumn("projects", "config") || tx.Migrator().HasColumn("plans", "type")
	var tasks []model.Task
	if err := tx.Where("agent_id IS NULL OR agent_id = '' OR channel IS NULL OR channel = '' OR task_kind IS NULL OR task_kind = ''").Find(&tasks).Error; err != nil {
		return rollback(fmt.Errorf("load legacy tasks: %w", err))
	}
	for _, task := range tasks {
		agentID, channel, taskKind, ok := migrateLegacyTaskIdentity(task)
		if !ok {
			// Leave unknown historical rows untouched. Readiness will still fail for
			// active rows, but terminal rows do not need an invented Agent identity.
			continue
		}
		if err := tx.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{
			"agent_id": agentID, "channel": channel, "task_kind": taskKind,
		}).Error; err != nil {
			return rollback(fmt.Errorf("backfill task %s identity: %w", task.ID, err))
		}
	}
	// These workflows previously shared the generic generation kind. Their
	// current task contracts use a dedicated kind, including fully populated rows.
	for _, agentID := range []string{model.AgentIDMontage, model.AgentIDWhiteboard} {
		for _, table := range []string{"tasks", "task_executions"} {
			if err := tx.Table(table).Where("agent_id = ? AND task_kind = ?", agentID, model.TaskKindContentGeneration).Update("task_kind", agentID).Error; err != nil {
				return rollback(fmt.Errorf("normalize %s workflow kind: %w", table, err))
			}
		}
	}
	// Convert every historical plan into one independent entry. The legacy plan
	// columns are read as raw migration data because the new Plan model no longer
	// exposes them. The entry is the durable execution identity after this pass.
	var plans []struct {
		ID               string
		Type             string
		ExecutionProfile string
		AgentInput       datatypes.JSON
	}
	if tx.Migrator().HasTable("plans") && tx.Migrator().HasColumn("plans", "type") {
		columns := []string{"id", "type", "execution_profile"}
		if tx.Migrator().HasColumn("plans", "agent_input") {
			columns = append(columns, "agent_input")
		}
		if err := tx.Table("plans").Select(strings.Join(columns, ", ")).Find(&plans).Error; err != nil {
			return rollback(fmt.Errorf("load legacy plans: %w", err))
		}
	}
	for _, plan := range plans {
		var existing int64
		if err := tx.Model(&model.PlanEntry{}).Where("plan_id = ?", plan.ID).Count(&existing).Error; err != nil {
			return rollback(fmt.Errorf("check plan entries: %w", err))
		}
		if existing > 0 {
			continue
		}
		agentID, channel, taskKind, ok := migrateLegacyIdentity(plan.Type)
		if !ok {
			// Plugin-only plans predate PlanEntry identity. Preserve their
			// schedule as a paused entry so startup is not blocked and no
			// historical plan can execute through an inferred content Agent.
			typeName := strings.TrimSpace(plan.Type)
			if typeName == "" {
				return rollback(fmt.Errorf("plan %s has empty legacy type", plan.ID))
			}
			agentID, channel, taskKind = typeName, typeName, typeName
			ok = true
		}
		entryStatus := model.PlanEntryStatusActive
		if pack, found := agentpack.Default().ForAgent(agentID); !found || !pack.SupportsPlan() {
			// Plugin-only historical workflows remain visible as migrated records,
			// but are paused until a product Agent/channel contract is added.
			entryStatus = model.PlanEntryStatusPaused
		}
		if entryStatus == model.PlanEntryStatusPaused {
			if err := tx.Model(&model.Plan{}).Where("id = ?", plan.ID).Updates(map[string]any{"status": model.PlanStatusPaused, "next_run_at": nil}).Error; err != nil {
				return rollback(fmt.Errorf("pause unsupported plan %s: %w", plan.ID, err))
			}
		}
		if pack, found := agentpack.Default().ForAgent(agentID); found && pack.SupportsPlan() {
			channel, taskKind = pack.Channel, pack.PlanTaskKind
		}
		entry := &model.PlanEntry{
			ID:     uuid.NewSHA1(uuid.Nil, []byte("legacy-plan-entry:"+plan.ID)).String(),
			PlanID: plan.ID, AgentID: agentID, Channel: channel, TaskKind: taskKind,
			ExecutionProfile: plan.ExecutionProfile, Status: entryStatus,
		}
		if len(plan.AgentInput) > 0 {
			var input map[string]any
			if err := json.Unmarshal(plan.AgentInput, &input); err != nil {
				return rollback(fmt.Errorf("decode legacy plan %s agent input: %w", plan.ID, err))
			}
			entry.SetAgentInput(input)
		}
		if err := tx.Create(entry).Error; err != nil {
			return rollback(fmt.Errorf("backfill plan %s entry: %w", plan.ID, err))
		}
	}
	// Existing entries may predate the public plan eligibility contract too.
	var existingEntries []model.PlanEntry
	if err := tx.Find(&existingEntries).Error; err != nil {
		return rollback(fmt.Errorf("load plan entries for cutover: %w", err))
	}
	for _, entry := range existingEntries {
		pack, found := agentpack.Default().ForAgent(entry.AgentID)
		if found && pack.SupportsPlan() && entry.Channel == pack.Channel && entry.TaskKind == pack.PlanTaskKind {
			continue
		}
		if err := tx.Model(&model.PlanEntry{}).Where("id = ?", entry.ID).Update("status", model.PlanEntryStatusPaused).Error; err != nil {
			return rollback(err)
		}
		if err := tx.Model(&model.Plan{}).Where("id = ?", entry.PlanID).Updates(map[string]any{"status": model.PlanStatusPaused, "next_run_at": nil}).Error; err != nil {
			return rollback(err)
		}
	}
	// Move legacy WeChat credentials into the channel-owned configuration row.
	// Legacy project credentials lived in the removed `projects.config` JSON
	// column. Read that column only when it still exists, then write the
	// canonical channel-owned row. New databases have no such column.
	if tx.Migrator().HasTable("projects") && tx.Migrator().HasColumn("projects", "config") {
		var projects []struct {
			ID       string
			Platform string
			Config   datatypes.JSON
		}
		if err := tx.Table("projects").Select("id, platform, config").Where("platform IN ?", []string{model.PlatformWechat, "article"}).Find(&projects).Error; err != nil {
			return rollback(fmt.Errorf("load legacy project credentials: %w", err))
		}
		for _, project := range projects {
			var legacy struct {
				WechatAppID  string `json:"wechat_app_id"`
				WechatSecret string `json:"wechat_secret"`
			}
			if len(project.Config) == 0 || json.Unmarshal(project.Config, &legacy) != nil || (strings.TrimSpace(legacy.WechatAppID) == "" && strings.TrimSpace(legacy.WechatSecret) == "") {
				continue
			}
			config := map[string]any{"wechat_app_id": legacy.WechatAppID, "wechat_secret": legacy.WechatSecret}
			row := &model.ProjectChannelConfig{ID: uuid.NewSHA1(uuid.Nil, []byte("legacy-project-channel-config:"+project.ID+":"+model.ChannelArticle)).String(), ProjectID: project.ID, Channel: model.ChannelArticle}
			row.Config = datatypes.NewJSONType(config)
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
				return rollback(fmt.Errorf("backfill article channel config: %w", err))
			}
		}
	}
	// Before the cutover, picture publication used the article connector when
	// no picture-specific connector existed. Preserve that effective account once;
	// runtime publication continues to read its exact task channel only.
	if legacyCutover {
		var articleConfigs []model.ProjectChannelConfig
		if err := tx.Where("channel = ?", model.ChannelArticle).Find(&articleConfigs).Error; err != nil {
			return rollback(err)
		}
		for _, article := range articleConfigs {
			picture := &model.ProjectChannelConfig{
				ID:        uuid.NewSHA1(uuid.Nil, []byte("legacy-project-channel-config:"+article.ProjectID+":"+model.ChannelWechatPicture)).String(),
				ProjectID: article.ProjectID, Channel: model.ChannelWechatPicture, Config: article.Config,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(picture).Error; err != nil {
				return rollback(fmt.Errorf("backfill picture connector: %w", err))
			}
		}
	}

	// Remove the old plan/project configuration columns only after their values
	// have been copied into channel-owned config and PlanEntry snapshots.
	legacyPlanColumns := []string{
		"type", "agent_input", "has_content_image", "has_tail_image",
		"article_with_cover", "article_with_content_images", "cover_use_portrait",
		"hypit_input", "montage_input", "visual_style", "writer", "author", "theme",
	}
	for _, column := range legacyPlanColumns {
		if tx.Migrator().HasColumn("plans", column) {
			if err := tx.Exec("ALTER TABLE `plans` DROP COLUMN `" + column + "`").Error; err != nil {
				return rollback(fmt.Errorf("drop plans.%s: %w", column, err))
			}
		}
	}
	var analytics []model.AnalyticsContent
	if err := tx.Where("channel IS NULL OR channel = ''").Find(&analytics).Error; err != nil {
		return rollback(fmt.Errorf("load analytics content: %w", err))
	}
	for _, content := range analytics {
		channel := content.Platform
		if channel == model.PlatformWechat || channel == "article" {
			channel = model.ChannelArticle
		} else if channel == model.PlatformSeednote {
			channel = model.ChannelSeednote
		}
		if channel == "" {
			continue
		}
		if err := tx.Model(&model.AnalyticsContent{}).Where("id = ?", content.ID).Update("channel", channel).Error; err != nil {
			return rollback(fmt.Errorf("backfill analytics channel: %w", err))
		}
	}
	if strings.Contains(strings.ToLower(tx.Dialector.Name()), "sqlite") {
		// SQLite does not support MySQL JOIN UPDATE; use a portable per-row pass.
		var executions []model.TaskExecution
		if queryErr := tx.Where("agent_id IS NULL OR agent_id = '' OR channel IS NULL OR channel = '' OR task_kind IS NULL OR task_kind = ''").Find(&executions).Error; queryErr != nil {
			return rollback(queryErr)
		}
		for _, execution := range executions {
			var task model.Task
			if queryErr := tx.First(&task, "id = ?", execution.TaskID).Error; queryErr != nil {
				return rollback(queryErr)
			}
			if queryErr := tx.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).Updates(map[string]any{"agent_id": task.AgentID, "channel": task.Channel, "task_kind": task.TaskKind}).Error; queryErr != nil {
				return rollback(queryErr)
			}
		}
	} else if err := tx.Exec(`UPDATE task_executions e JOIN tasks t ON t.id = e.task_id SET e.agent_id = t.agent_id, e.channel = t.channel, e.task_kind = t.task_kind WHERE e.agent_id IS NULL OR e.agent_id = '' OR e.channel IS NULL OR e.channel = '' OR e.task_kind IS NULL OR e.task_kind = ''`).Error; err != nil {
		return rollback(fmt.Errorf("backfill execution identity: %w", err))
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	return AssertMultiAgentChannelReadiness(ctx, db)
}

// MigrateProjectAgentConfigRemoval is the irreversible project-context
// cutover. Agent configuration was never part of a project's durable public
// contract after plans became the output selector, so old rows are removed
// rather than copied into the new channel configuration.
func MigrateProjectAgentConfigRemoval(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return nil
	}
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	// ProjectSnapshot is JSON, so clean the frozen evidence in Go. This keeps
	// the migration portable across SQLite and MySQL and avoids dialect-specific
	// JSON path syntax.
	if tx.Migrator().HasTable("tasks") {
		var tasks []struct {
			ID              string
			ProjectSnapshot datatypes.JSON
		}
		if err := tx.Table("tasks").Select("id", "project_snapshot").Find(&tasks).Error; err != nil {
			return rollback(fmt.Errorf("load task snapshots for Agent config cleanup: %w", err))
		}
		for _, task := range tasks {
			if len(task.ProjectSnapshot) == 0 {
				continue
			}
			var snapshot map[string]any
			if err := json.Unmarshal(task.ProjectSnapshot, &snapshot); err != nil {
				return rollback(fmt.Errorf("decode task %s project snapshot: %w", task.ID, err))
			}
			if _, exists := snapshot["agent_config"]; !exists {
				continue
			}
			delete(snapshot, "agent_config")
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				return rollback(fmt.Errorf("encode task %s project snapshot: %w", task.ID, err))
			}
			if err := tx.Model(&model.Task{}).Where("id = ?", task.ID).Update("project_snapshot", encoded).Error; err != nil {
				return rollback(fmt.Errorf("clean task %s Agent config snapshot: %w", task.ID, err))
			}
		}
	}

	if tx.Migrator().HasTable("project_agent_configs") {
		if err := tx.Migrator().DropTable("project_agent_configs"); err != nil {
			return rollback(fmt.Errorf("drop project_agent_configs: %w", err))
		}
	}
	if tx.Migrator().HasTable("projects") && tx.Migrator().HasColumn("projects", "agent_config") {
		if err := tx.Exec("ALTER TABLE `projects` DROP COLUMN `agent_config`").Error; err != nil {
			return rollback(fmt.Errorf("drop projects.agent_config: %w", err))
		}
	}
	if tx.Migrator().HasTable("projects") && tx.Migrator().HasColumn("projects", "config") {
		if err := tx.Exec("ALTER TABLE `projects` DROP COLUMN `config`").Error; err != nil {
			return rollback(fmt.Errorf("drop projects.config: %w", err))
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	return nil
}

// AssertMultiAgentChannelReadiness prevents serving executable rows that have
// not received a complete identity during the one-time cutover.
func AssertMultiAgentChannelReadiness(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return nil
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.Task{}).
		Where("status IN ? AND (agent_id IS NULL OR agent_id = '' OR channel IS NULL OR channel = '' OR task_kind IS NULL OR task_kind = '')", []string{model.TaskStatusPending, model.TaskStatusRunning}).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("multi-agent identity migration incomplete: %d executable tasks lack agent_id/channel/task_kind", count)
	}
	if err := db.WithContext(ctx).Model(&model.Task{}).
		Where(`status IN ? AND (agent_id <> channel OR (task_kind = ? AND agent_id <> ?))`,
			[]string{model.TaskStatusPending, model.TaskStatusRunning},
			model.TaskKindViralAnalysis, model.AgentIDSeednote).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("multi-agent identity migration found %d tasks outside supported product identities", count)
	}
	if err := db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("status IN ? AND (agent_id IS NULL OR agent_id = '' OR channel IS NULL OR channel = '' OR task_kind IS NULL OR task_kind = '')", []string{
			model.TaskExecutionCreated, model.TaskExecutionDispatching, model.TaskExecutionStarting, model.TaskExecutionRunning,
		}).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("multi-agent identity migration incomplete: %d executions lack agent_id/channel/task_kind", count)
	}
	var plansWithoutEntries int64
	if err := db.WithContext(ctx).Model(&model.Plan{}).
		Where("status = ? AND NOT EXISTS (SELECT 1 FROM plan_entries WHERE plan_entries.plan_id = plans.id)", model.PlanStatusActive).
		Count(&plansWithoutEntries).Error; err != nil {
		return err
	}
	if plansWithoutEntries > 0 {
		return fmt.Errorf("multi-agent identity migration incomplete: %d plans lack plan entries", plansWithoutEntries)
	}
	return nil
}

func migrateLegacyIdentity(value string) (agentID, channel, taskKind string, ok bool) {
	switch strings.TrimSpace(value) {
	case "article", "wechat", "wechat-article":
		return model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration, true
	case "seednote":
		return model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindContentGeneration, true
	case "wechat-picture":
		return model.AgentIDWechatPicture, model.ChannelWechatPicture, model.TaskKindContentGeneration, true
	case model.TaskTypeViralAnalysis, "viral-analysis":
		return model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindViralAnalysis, true
	case "channels-video":
		return model.AgentIDMontage, model.ChannelMontage, model.PlatformMontage, true
	case model.TaskTypeProfileAnalysis, "profile-analysis":
		return model.AgentIDProfileAnalysis, model.ChannelProfileAnalysis, model.TaskKindProfileAnalysis, true
	case model.PlatformMontage:
		return model.AgentIDMontage, model.ChannelMontage, model.PlatformMontage, true
	case model.PlatformHypit:
		return model.AgentIDHypit, model.ChannelHypit, model.PlatformHypit, true
	case model.PlatformMoments:
		return model.PlatformMoments, model.ChannelMoments, model.PlatformMoments, true
	case model.PlatformEcommerce:
		return model.PlatformEcommerce, model.ChannelEcommerce, model.PlatformEcommerce, true
	case model.PlatformWhiteboardAnimation:
		return model.AgentIDWhiteboard, model.ChannelWhiteboard, model.PlatformWhiteboardAnimation, true
	case "feedback":
		return model.AgentIDFeedback, model.ChannelFeedback, model.TaskKindFeedbackAnalysis, true
	default:
		value = strings.TrimSpace(value)
		if value == "" {
			return "", "", "", false
		}
		return "", "", "", false
	}
}

func migrateLegacyTaskIdentity(task model.Task) (agentID, channel, taskKind string, ok bool) {
	if agentID, channel, taskKind, ok = migrateLegacyIdentity(task.Type); ok {
		return agentID, channel, taskKind, true
	}
	// A partially migrated row may have lost its legacy type's exact spelling,
	// but still has the stable Agent ID. Recover the remaining fields from the
	// Agent Pack contract instead of guessing from project metadata.
	agentID = strings.TrimSpace(task.AgentID)
	if agentID == "" {
		return "", "", "", false
	}
	if channel, ok = model.AgentChannel(agentID); ok {
		if taskKind = strings.TrimSpace(task.TaskKind); taskKind == "" {
			taskKind = model.TaskKindContentGeneration
			if agentID == model.AgentIDMontage || agentID == model.AgentIDWhiteboard {
				taskKind = agentID
			}
		}
		return agentID, channel, taskKind, true
	}
	pack, found := agentpack.Default().ForAgent(agentID)
	if !found {
		return "", "", "", false
	}
	channel = strings.TrimSpace(pack.Channel)
	if channel == "" && pack.Kind == agentpack.KindPlugin {
		channel = agentID
	}
	if channel == "" {
		return "", "", "", false
	}
	taskKind = strings.TrimSpace(task.TaskKind)
	if taskKind == "" {
		taskKind = strings.TrimSpace(task.Type)
	}
	if taskKind == "" {
		return "", "", "", false
	}
	return agentID, channel, taskKind, true
}
