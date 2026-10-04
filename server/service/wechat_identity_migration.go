package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IdentityRepairFinding contains identity evidence only, never frozen credentials.
type IdentityRepairFinding struct {
	Table    string `json:"table"`
	ID       string `json:"id"`
	Column   string `json:"column"`
	Before   string `json:"before"`
	After    string `json:"after,omitempty"`
	Evidence string `json:"evidence"`
	Conflict bool   `json:"conflict,omitempty"`
}
type IdentityRepairReport struct {
	DryRun                  bool                    `json:"dry_run"`
	Candidates              int                     `json:"candidates"`
	Applied                 int                     `json:"applied"`
	Conflicts               int                     `json:"conflicts"` // excludes separately counted frozen runtime-profile discrepancies
	RuntimeProfileConflicts int                     `json:"runtime_profile_conflicts"`
	Findings                []IdentityRepairFinding `json:"findings"`
}

type wechatIdentityRepairAudit struct {
	ID             string `gorm:"type:char(36);primaryKey"`
	TableNameValue string `gorm:"column:table_name;type:varchar(80);not null"`
	RowID          string `gorm:"type:varchar(191);not null"`
	ColumnName     string `gorm:"type:varchar(80);not null"`
	BeforeValue    string `gorm:"type:text;not null"`
	AfterValue     string `gorm:"type:text;not null"`
	Evidence       string `gorm:"type:text;not null"`
	CreatedAt      time.Time
}

func (wechatIdentityRepairAudit) TableName() string { return "wechat_identity_repair_audits" }

type identityRepairTask struct{ ID, Type, AgentID, Channel, TaskKind, Status string }

// RepairWechatIdentities is an operator-only repair. Default callers use apply=false.
// Changes and their evidence commit atomically. Frozen executions, images, billing,
// and task snapshots are never updated. Stop writers before using apply=true.
func RepairWechatIdentities(ctx context.Context, db *gorm.DB, apply bool) (IdentityRepairReport, error) {
	report := IdentityRepairReport{DryRun: !apply, Findings: []IdentityRepairFinding{}}
	if db == nil {
		return report, fmt.Errorf("database is required")
	}
	if apply {
		if err := db.AutoMigrate(&wechatIdentityRepairAudit{}); err != nil {
			return report, err
		}
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		add := func(f IdentityRepairFinding) error {
			report.Findings = append(report.Findings, f)
			if f.Conflict {
				report.Conflicts++
				return nil
			}
			report.Candidates++
			if !apply {
				return nil
			}
			predicate := "id = ? AND " + f.Column + " = ?"
			if tx.Dialector.Name() == "mysql" && f.Column == "applicable_tasks" {
				predicate = "id = ? AND applicable_tasks = CAST(? AS JSON)"
			}
			result := tx.Table(f.Table).Where(predicate, f.ID, f.Before).UpdateColumn(f.Column, f.After)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("%s %s changed during repair; retry dry run", f.Table, f.ID)
			}
			audit := wechatIdentityRepairAudit{ID: uuid.NewString(), TableNameValue: f.Table, RowID: f.ID, ColumnName: f.Column, BeforeValue: f.Before, AfterValue: f.After, Evidence: f.Evidence, CreatedAt: time.Now().UTC()}
			if err := tx.Create(&audit).Error; err != nil {
				return err
			}
			report.Applied++
			return nil
		}
		if tx.Migrator().HasTable("tasks") {
			var rows []identityRepairTask
			query := tx.Table("tasks").Select("id,type,agent_id,channel,task_kind,status").Order("id")
			if apply {
				query = query.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			if err := query.Find(&rows).Error; err != nil {
				return err
			}
			for _, r := range rows {
				a, c, k, _, err := resolveTaskIdentity(nil, CreateManualParams{AgentID: r.AgentID, Channel: r.Channel, TaskKind: r.TaskKind})
				expected := taskTypeForIdentity(a, c, k)
				if err == nil && expected == r.Type {
					continue
				}
				evidence := fmt.Sprintf("agent_id=%q channel=%q task_kind=%q", r.AgentID, r.Channel, r.TaskKind)
				f := IdentityRepairFinding{Table: "tasks", ID: r.ID, Column: "type", Before: r.Type, Evidence: evidence, Conflict: true}
				// Only the known article substring corruption is repairable. A different
				// valid task type conflicts with explicit identity and needs human evidence.
				if err == nil && expected == model.TaskTypeWechatArticle && corruptedArticleType(r.Type) {
					f.After = expected
					f.Conflict = false
				}
				if err != nil {
					f.Evidence += "; " + err.Error()
				}
				if err := add(f); err != nil {
					return err
				}
			}
		}
		if tx.Migrator().HasTable("strategy_snapshots") && tx.Migrator().HasColumn("strategy_snapshots", "applicable_tasks") {
			var rows []struct{ ID, ApplicableTasks string }
			if err := tx.Table("strategy_snapshots").Select("id,applicable_tasks").Order("id").Find(&rows).Error; err != nil {
				return err
			}
			for _, r := range rows {
				tokens, valid := exactStrategyTokens(r.ApplicableTasks)
				if !valid {
					if err := add(IdentityRepairFinding{Table: "strategy_snapshots", ID: r.ID, Column: "applicable_tasks", Before: r.ApplicableTasks, Evidence: "expected JSON string array", Conflict: true}); err != nil {
						return err
					}
					continue
				}
				changed, ambiguous := false, false
				for i, v := range tokens {
					remainder := v
					for strings.HasPrefix(remainder, "wechat-") {
						remainder = strings.TrimPrefix(remainder, "wechat-")
					}
					if remainder == "article" && v != model.TaskTypeWechatArticle {
						tokens[i] = model.TaskTypeWechatArticle
						changed = true
					} else if v != model.TaskTypeWechatArticle && corruptedArticleType(v) {
						ambiguous = true
					}
				}
				if ambiguous {
					if err := add(IdentityRepairFinding{Table: "strategy_snapshots", ID: r.ID, Column: "applicable_tasks", Before: r.ApplicableTasks, Evidence: "truncated article token lacks independent identity evidence; array left unchanged", Conflict: true}); err != nil {
						return err
					}
					continue
				}
				if changed {
					encoded, _ := json.Marshal(tokens)
					if err := add(IdentityRepairFinding{Table: "strategy_snapshots", ID: r.ID, Column: "applicable_tasks", Before: r.ApplicableTasks, After: string(encoded), Evidence: "complete exact JSON article token, including repeated wechat- prefixes, -> wechat-article"}); err != nil {
						return err
					}
				}
			}
		}
		if tx.Migrator().HasTable("task_executions") {
			var rows []struct{ ID, AgentID, Channel, TaskKind, RuntimeProfile string }
			if err := tx.Table("task_executions").Select("id,agent_id,channel,task_kind,runtime_profile").Order("id").Find(&rows).Error; err != nil {
				return err
			}
			for _, r := range rows {
				_, _, _, pack, err := resolveTaskIdentity(nil, CreateManualParams{AgentID: r.AgentID, Channel: r.Channel, TaskKind: r.TaskKind})
				if err == nil && r.RuntimeProfile == pack.Runtime.Profile {
					continue
				}
				report.RuntimeProfileConflicts++
				report.Findings = append(report.Findings, IdentityRepairFinding{Table: "task_executions", ID: r.ID, Column: "runtime_profile", Before: r.RuntimeProfile, Evidence: fmt.Sprintf("frozen execution: agent_id=%q channel=%q task_kind=%q; inspect runtime evidence, do not rewrite", r.AgentID, r.Channel, r.TaskKind), Conflict: true})
			}
		}
		return nil
	})
	if err != nil {
		report.Applied = 0
	}
	return report, err
}

func corruptedArticleType(value string) bool {
	if value == "article" {
		return true
	}
	// The old VARCHAR(20) may truncate any repeated-prefix result. Require a
	// complete wechat- prefix; explicit validated identity is the authority.
	if !strings.HasPrefix(value, "wechat-") {
		return false
	}
	for strings.HasPrefix(value, "wechat-") {
		value = strings.TrimPrefix(value, "wechat-")
	}
	return strings.HasPrefix("article", value) || strings.HasPrefix("wechat-", value)
}

// AssertExecutableIdentityReadiness performs no writes and blocks admission of
// inconsistent executable rows. Historical terminal rows remain repair evidence.
func AssertExecutableIdentityReadiness(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return nil
	}
	db = db.WithContext(ctx)
	if db.Migrator().HasTable("tasks") {
		var rows []identityRepairTask
		if err := db.Table("tasks").Select("id,type,agent_id,channel,task_kind,status").Where("status IN ?", []string{model.TaskStatusPending, model.TaskStatusRunning}).Find(&rows).Error; err != nil {
			return fmt.Errorf("identity readiness: %w", err)
		}
		for _, r := range rows {
			if err := validateTaskIdentity(&model.Task{Type: r.Type, AgentID: r.AgentID, Channel: r.Channel, TaskKind: r.TaskKind}); err != nil {
				return fmt.Errorf("task %s identity is not ready; run repair-wechat-identities dry run: %w", r.ID, err)
			}
		}
	}
	if db.Migrator().HasTable("task_executions") {
		var rows []struct{ ID, AgentID, Channel, TaskKind, RuntimeProfile string }
		if err := db.Table("task_executions").Select("id,agent_id,channel,task_kind,runtime_profile").Where("status IN ?", []string{model.TaskExecutionCreated, model.TaskExecutionDispatching, model.TaskExecutionStarting, model.TaskExecutionRunning}).Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			_, _, _, _, err := resolveTaskIdentity(nil, CreateManualParams{AgentID: r.AgentID, Channel: r.Channel, TaskKind: r.TaskKind})
			if err != nil || r.RuntimeProfile == "" || strings.HasPrefix(r.RuntimeProfile, "wechat-wechat") {
				return fmt.Errorf("execution %s frozen identity/runtime profile is not ready; inspect evidence without rewriting snapshot", r.ID)
			}
		}
	}
	if db.Migrator().HasTable("plans") {
		var count int64
		query := db.Table("plans").Where("status = ?", model.PlanStatusActive)
		if db.Migrator().HasTable("plan_entries") {
			query = query.Where("NOT EXISTS (SELECT 1 FROM plan_entries WHERE plan_entries.plan_id = plans.id)")
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("%d active plans lack canonical plan entries; complete the reviewed multi-agent cutover before startup", count)
		}
	}
	if db.Migrator().HasTable("plan_entries") {
		var rows []model.PlanEntry
		if err := db.Where("status = ?", model.PlanEntryStatusActive).Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			_, _, _, pack, err := resolveTaskIdentity(nil, CreateManualParams{AgentID: r.AgentID, Channel: r.Channel, TaskKind: r.TaskKind})
			if err != nil || !pack.SupportsPlan() || r.TaskKind != pack.PlanTaskKind {
				return fmt.Errorf("plan entry %s identity is not ready", r.ID)
			}
		}
	}
	return nil
}

func exactStrategyTokens(raw string) ([]string, bool) {
	var values []any
	if err := json.Unmarshal([]byte(raw), &values); err != nil || values == nil {
		return nil, false
	}
	tokens := make([]string, len(values))
	for i, value := range values {
		s, ok := value.(string)
		if !ok {
			return nil, false
		}
		tokens[i] = s
	}
	return tokens, true
}
