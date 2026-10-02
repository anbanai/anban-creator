package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
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
	updates := []struct {
		types   []string
		agent   string
		channel string
		kind    string
	}{
		{[]string{"article", "wechat", "wechat-article"}, model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration},
		{[]string{"seednote"}, model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindContentGeneration},
		{[]string{"wechat-picture"}, model.AgentIDWechatPicture, model.ChannelWechatPicture, model.TaskKindContentGeneration},
		{[]string{model.TaskTypeViralAnalysis, "viral-analysis"}, model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindViralAnalysis},
	}
	for _, update := range updates {
		if err := tx.Model(&model.Task{}).
			Where("(agent_id IS NULL OR agent_id = '') AND type IN ?", update.types).
			Updates(map[string]any{"agent_id": update.agent, "channel": update.channel, "task_kind": update.kind}).Error; err != nil {
			return rollback(fmt.Errorf("backfill task identity: %w", err))
		}
	}
	// Convert every historical plan into one independent entry. The entry is
	// the durable execution identity; the legacy plan type is never consulted by
	// the scheduler after this backfill.
	var plans []model.Plan
	if err := tx.Find(&plans).Error; err != nil {
		return rollback(fmt.Errorf("load legacy plans: %w", err))
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
		if pack, found := agentpack.Default().ForAgent(agentID); !found || pack.Kind != agentpack.KindManaged {
			// Plugin-only historical workflows remain visible as migrated records,
			// but are paused until a product Agent/channel contract is added.
			entryStatus = model.PlanEntryStatusPaused
		}
		entry := &model.PlanEntry{
			ID:     uuid.NewSHA1(uuid.Nil, []byte("legacy-plan-entry:"+plan.ID)).String(),
			PlanID: plan.ID, AgentID: agentID, Channel: channel, TaskKind: taskKind,
			ExecutionProfile: plan.ExecutionProfile, Status: entryStatus,
		}
		entry.SetAgentInput(plan.AgentInput.Data())
		if err := tx.Create(entry).Error; err != nil {
			return rollback(fmt.Errorf("backfill plan %s entry: %w", plan.ID, err))
		}
	}
	// Move legacy WeChat credentials into the channel-owned configuration row.
	var projects []model.Project
	if err := tx.Where("platform IN ?", []string{model.PlatformWechat, "article"}).Find(&projects).Error; err != nil {
		return rollback(fmt.Errorf("load legacy project credentials: %w", err))
	}
	for _, project := range projects {
		if strings.TrimSpace(project.Config.WechatAppID) == "" && strings.TrimSpace(project.Config.WechatSecret) == "" {
			continue
		}
		config := map[string]any{"wechat_app_id": project.Config.WechatAppID, "wechat_secret": project.Config.WechatSecret}
		row := &model.ProjectChannelConfig{ID: uuid.NewSHA1(uuid.Nil, []byte("legacy-project-channel-config:"+project.ID+":"+model.ChannelArticle)).String(), ProjectID: project.ID, Channel: model.ChannelArticle}
		row.Config = datatypes.NewJSONType(config)
		if err := tx.Where("project_id = ? AND channel = ?", project.ID, model.ChannelArticle).FirstOrCreate(row).Error; err != nil {
			return rollback(fmt.Errorf("backfill article channel config: %w", err))
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
		if queryErr := tx.Where("agent_id IS NULL OR agent_id = ''").Find(&executions).Error; queryErr != nil {
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
	} else if err := tx.Exec(`UPDATE task_executions e JOIN tasks t ON t.id = e.task_id SET e.agent_id = t.agent_id, e.channel = t.channel, e.task_kind = t.task_kind WHERE e.agent_id IS NULL OR e.agent_id = ''`).Error; err != nil {
		return rollback(fmt.Errorf("backfill execution identity: %w", err))
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	return AssertMultiAgentChannelReadiness(ctx, db)
}

// AssertMultiAgentChannelReadiness prevents serving executable rows that have
// not received a complete identity during the one-time cutover.
func AssertMultiAgentChannelReadiness(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return nil
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.Task{}).
		Where("agent_id IS NULL OR agent_id = '' OR channel IS NULL OR channel = '' OR task_kind IS NULL OR task_kind = ''").
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
		Where("agent_id IS NULL OR agent_id = '' OR channel IS NULL OR channel = '' OR task_kind IS NULL OR task_kind = ''").
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
	case "montage", "hypit", "moments", "ecommerce":
		return "", "", "", false
	default:
		value = strings.TrimSpace(value)
		if value == "" {
			return "", "", "", false
		}
		return "", "", "", false
	}
}
