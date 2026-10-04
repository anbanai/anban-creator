package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func identityRepairDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, sql := range []string{
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, type TEXT, agent_id TEXT, channel TEXT, task_kind TEXT, status TEXT, config_snapshot TEXT, billing_sku_id TEXT)`,
		`CREATE TABLE strategy_snapshots (id TEXT PRIMARY KEY, applicable_tasks TEXT)`,
		`CREATE TABLE task_executions (id TEXT PRIMARY KEY, task_id TEXT, agent_id TEXT, channel TEXT, task_kind TEXT, status TEXT, runtime_profile TEXT, runtime_image TEXT)`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestWechatIdentityRepairEvidenceAndIdempotence(t *testing.T) {
	db := identityRepairDB(t)
	for id, typ := range map[string]string{"canonical": "wechat-article", "legacy": "article", "repeated": "wechat-wechat-article", "truncated": "wechat-wechat-wechat-", "conflict": "seednote"} {
		if err := db.Exec(`INSERT INTO tasks VALUES (?, ?, ?, ?, ?, 'completed', '{"frozen":"article"}', 'task.article.old')`, id, typ, model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Exec(`INSERT INTO tasks VALUES ('unknown','wechat-wechat-articl','','','', 'completed','{}','')`)
	db.Exec(`INSERT INTO strategy_snapshots VALUES ('strategy','["article","wechat-article","my-article","wechat-wechat-article"]')`)
	db.Exec(`INSERT INTO task_executions VALUES ('execution','canonical', ?, ?, ?, 'completed', 'wechat-wechat', 'immutable@sha256:abc')`, model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration)
	report, err := RepairWechatIdentities(context.Background(), db, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Candidates != 4 || report.Conflicts != 2 || report.RuntimeProfileConflicts != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	var original string
	db.Raw(`SELECT type FROM tasks WHERE id='repeated'`).Scan(&original)
	if original != "wechat-wechat-article" || db.Migrator().HasTable("wechat_identity_repair_audits") {
		t.Fatal("dry run wrote data")
	}
	report, err = RepairWechatIdentities(context.Background(), db, true)
	if err != nil || report.Applied != 4 {
		t.Fatalf("apply: %+v %v", report, err)
	}
	for i := 0; i < 3; i++ {
		report, err = RepairWechatIdentities(context.Background(), db, true)
		if err != nil || report.Applied != 0 {
			t.Fatalf("repeat %d: %+v %v", i, report, err)
		}
	}
	var snapshot, sku, profile, image, tokens string
	db.Raw(`SELECT config_snapshot,billing_sku_id FROM tasks WHERE id='legacy'`).Row().Scan(&snapshot, &sku)
	db.Raw(`SELECT runtime_profile,runtime_image FROM task_executions WHERE id='execution'`).Row().Scan(&profile, &image)
	db.Raw(`SELECT applicable_tasks FROM strategy_snapshots`).Scan(&tokens)
	if snapshot != `{"frozen":"article"}` || sku != "task.article.old" || profile != "wechat-wechat" || image != "immutable@sha256:abc" {
		t.Fatal("frozen evidence changed")
	}
	if tokens != `["wechat-article","wechat-article","my-article","wechat-article"]` {
		t.Fatalf("tokens=%s", tokens)
	}
	var count int64
	db.Table("wechat_identity_repair_audits").Count(&count)
	if count != 4 {
		t.Fatalf("audit count=%d", count)
	}
}

func TestExecutableIdentityReadinessIsReadOnlyAndRejectsPollution(t *testing.T) {
	for _, tc := range []struct {
		name, typ, agent, channel, kind string
		valid                           bool
	}{
		{"valid", "wechat-article", model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration, true},
		{"polluted", "wechat-wechat-articl", model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration, false},
		{"missing", "article", "", "", "", false},
		{"mismatch", "wechat-article", model.AgentIDArticle, model.ChannelSeednote, model.TaskKindContentGeneration, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := identityRepairDB(t)
			db.Exec(`INSERT INTO tasks VALUES ('task',?,?,?,?, 'pending','{}','')`, tc.typ, tc.agent, tc.channel, tc.kind)
			for i := 0; i < 3; i++ {
				if err := AssertExecutableIdentityReadiness(context.Background(), db); (err == nil) != tc.valid {
					t.Fatalf("readiness=%v", err)
				}
			}
			var typ string
			db.Raw(`SELECT type FROM tasks`).Scan(&typ)
			if typ != tc.typ {
				t.Fatal("readiness mutated task")
			}
		})
	}
}

func TestWechatIdentityRepairRollsBackWhenAuditFails(t *testing.T) {
	db := identityRepairDB(t)
	db.Exec(`INSERT INTO tasks VALUES ('task','wechat-wechat-articl',?,?,?, 'completed','{}','')`, model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration)
	if err := db.AutoMigrate(&wechatIdentityRepairAudit{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_identity_audit BEFORE INSERT ON wechat_identity_repair_audits BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	report, err := RepairWechatIdentities(context.Background(), db, true)
	if err == nil || report.Applied != 0 {
		t.Fatalf("audit failure must abort repair: %+v %v", report, err)
	}
	var typ string
	db.Raw(`SELECT type FROM tasks`).Scan(&typ)
	if typ != "wechat-wechat-articl" {
		t.Fatal("mutation committed without audit")
	}
}

func TestExecutableIdentityReadinessAcceptsFreshAndCurrentSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	for i := 0; i < 3; i++ {
		if err := AssertExecutableIdentityReadiness(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}, &model.Plan{}, &model.PlanEntry{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := AssertExecutableIdentityReadiness(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWechatIdentityRepairRejectsNonStringStrategyTokens(t *testing.T) {
	for _, value := range []string{`null`, `["article",null]`, `["article",1]`, `{"article":true}`} {
		t.Run(value, func(t *testing.T) {
			db := identityRepairDB(t)
			if err := db.Exec(`INSERT INTO strategy_snapshots VALUES ('invalid',?)`, value).Error; err != nil {
				t.Fatal(err)
			}
			report, err := RepairWechatIdentities(context.Background(), db, true)
			if err != nil || report.Applied != 0 || report.Conflicts != 1 {
				t.Fatalf("report=%+v err=%v", report, err)
			}
			var after string
			db.Raw(`SELECT applicable_tasks FROM strategy_snapshots`).Scan(&after)
			if after != value {
				t.Fatal("invalid JSON token was rewritten")
			}
		})
	}
}

func TestWechatIdentityRepairLeavesTruncatedStrategyConflict(t *testing.T) {
	db := identityRepairDB(t)
	value := `["article","wechat-wechat-articl","my-article"]`
	db.Exec(`INSERT INTO strategy_snapshots VALUES ('conflict',?)`, value)
	report, err := RepairWechatIdentities(context.Background(), db, true)
	if err != nil || report.Applied != 0 || report.Conflicts != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	var after string
	db.Raw(`SELECT applicable_tasks FROM strategy_snapshots`).Scan(&after)
	if after != value {
		t.Fatal("ambiguous array changed")
	}
}

func TestExecutableIdentityReadinessPreservesHistoricalRuntimeProfile(t *testing.T) {
	db := identityRepairDB(t)
	db.Exec(`INSERT INTO task_executions VALUES ('old','task',?,?,?,'running','historical-article-profile','image@sha256:frozen')`, model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration)
	if err := AssertExecutableIdentityReadiness(context.Background(), db); err != nil {
		t.Fatalf("historical frozen runtime profile is not current catalog policy: %v", err)
	}
	db.Exec(`UPDATE task_executions SET runtime_profile='wechat-wechat' WHERE id='old'`)
	if err := AssertExecutableIdentityReadiness(context.Background(), db); err == nil {
		t.Fatal("active corrupted profile accepted")
	}
}
