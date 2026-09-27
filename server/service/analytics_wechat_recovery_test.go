package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func wechatRecoveryFixture(t *testing.T) (*AnalyticsService, *AnalyticsRebuildManager, *gorm.DB) {
	t.Helper()
	var s *AnalyticsService
	var db *gorm.DB
	if dsn := os.Getenv("ANBAN_ANALYTICS_RECOVERY_MYSQL_DSN"); dsn != "" {
		cfg, err := mysqldriver.ParseDSN(dsn)
		if err != nil {
			t.Fatal(err)
		}
		cfg.DBName = ""
		cfg.MultiStatements = true
		admin, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		name := "analytics_recovery_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if err = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4").Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := admin.Exec("DROP DATABASE `" + name + "`").Error; err != nil {
				t.Error(err)
			}
			c, _ := admin.DB()
			_ = c.Close()
		})
		cfg.DBName = name
		db, err = gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c, _ := db.DB(); _ = c.Close() })
		if err = db.AutoMigrate(&model.Project{}, &model.AnalyticsState{}, &model.AnalyticsContent{}, &model.AnalyticsObservation{}, &model.AnalyticsRawPayload{}, &model.AnalyticsBucket{}, &model.AnalyticsIdempotency{}); err != nil {
			t.Fatal(err)
		}
		if err = db.Create(&model.Project{ID: "p", UserID: "u", Platform: model.PlatformArticle}).Error; err != nil {
			t.Fatal(err)
		}
		s = NewAnalyticsService(repository.New(db))
	} else {
		s, db = analyticsFixture(t)
	}
	if err := db.Model(&model.Project{}).Where("id = ?", "p").Update("platform", model.PlatformArticle).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AnalyticsRebuildJob{}, &model.WechatAnalyticsImportBatch{}, &model.WechatAnalyticsSnapshot{}, &model.Task{}, &model.WechatPublication{}); err != nil {
		t.Fatal(err)
	}
	if db.Dialector.Name() == "mysql" {
		// Match the deployed legacy schema: it has no overlong full-URL index.
		raw, err := os.ReadFile("../migrations/20260918_wechat_content_workbench.sql")
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(string(raw), "CREATE TABLE `wechat_analytics_import_rows`")
		if start < 0 {
			t.Fatal("missing import table DDL")
		}
		ddl := string(raw)[start:]
		if err = db.Exec(ddl[:strings.Index(ddl, ";")+1]).Error; err != nil {
			t.Fatal(err)
		}
		if err = db.Exec("ALTER TABLE wechat_analytics_import_rows ADD task_id CHAR(36) NULL").Error; err != nil {
			t.Fatal(err)
		}
	} else if err := db.AutoMigrate(&model.WechatAnalyticsImportRow{}); err != nil {
		t.Fatal(err)
	}
	return s, NewAnalyticsRebuildManager(repository.New(db)), db
}

func TestWechatRecoveryQueueSQLMySQL(t *testing.T) {
	if os.Getenv("ANBAN_ANALYTICS_RECOVERY_MYSQL_DSN") == "" {
		t.Skip("set ANBAN_ANALYTICS_RECOVERY_MYSQL_DSN for real MySQL integration")
	}
	s, m, db := wechatRecoveryFixture(t)
	seedWechatRecoveryRow(t, db, "history", "b-history", "2026-09-25", "https://mp.weixin.qq.com/s?__biz=account&mid=100&idx=1", 100)
	raw, err := os.ReadFile("../migrations/20260927_wechat_import_history_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = db.Exec(string(raw)).Error; err != nil {
			t.Fatal(err)
		}
	}
	var jobs []model.AnalyticsRebuildJob
	if err = db.Find(&jobs).Error; err != nil || len(jobs) != 1 {
		t.Fatalf("queued jobs = %+v, %v", jobs, err)
	}
	if _, err = s.LockProjectWrite(context.Background(), "p"); err != ErrAnalyticsRebuilding {
		t.Fatalf("writes not gated: %v", err)
	}
	if err = m.Run(context.Background(), jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(string(raw)).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err = db.Model(&model.AnalyticsRebuildJob{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("post-publication replay created new job: %d, %v", count, err)
	}
	v, err := s.Overview(context.Background(), "u", "p", AnalyticsQuery{From: "2026-09-25", To: "2026-09-27", Granularity: "day", MetricBasis: "cumulative"})
	if err != nil || v.Totals["read_users"] != int64(100) {
		t.Fatalf("SQL recovery = %+v, %v", v, err)
	}
}

func TestWechatRecoveryPreservesRevokedAndNullRowsAndReportsInvalid(t *testing.T) {
	s, m, db := wechatRecoveryFixture(t)
	url := "https://mp.weixin.qq.com/s?__biz=account&mid=100&idx=1"
	seedWechatRecoveryRow(t, db, "zero", "b-zero", "2026-09-25", url, 0)
	seedWechatRecoveryRow(t, db, "null", "b-null", "2026-09-26", url, 1)
	if err := db.Model(&model.WechatAnalyticsImportRow{}).Where("id = ?", "null").Update("read_users", nil).Error; err != nil {
		t.Fatal(err)
	}
	seedWechatRecoveryRow(t, db, "revoked", "b-revoked", "2026-09-27", url, 999)
	if err := db.Model(&model.WechatAnalyticsImportBatch{}).Where("id = ?", "b-revoked").Updates(map[string]any{"status": "revoked", "revoked_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	seedWechatRecoveryRow(t, db, "invalid", "b-invalid", "2026-09-27", url, -1)
	seedWechatRecoveryRow(t, db, "missing-url", "b-missing", "2026-09-27", "", 999)
	j := runWechatRecovery(t, m)
	var observations []model.AnalyticsObservation
	if err := db.Order("stat_date").Find(&observations).Error; err != nil {
		t.Fatal(err)
	}
	if len(observations) != 3 || observations[0].ReadUsers == nil || *observations[0].ReadUsers != 0 || observations[1].ReadUsers != nil || observations[2].RevokedAt == nil {
		t.Fatalf("raw audit lost zero/null/revocation: %+v", observations)
	}
	job, err := m.Status(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	var report AnalyticsMigrationReport
	if err = json.Unmarshal([]byte(job.ReportJSON), &report); err != nil {
		t.Fatal(err)
	}
	if report.Revoked != 1 || report.Reasons["wechat_import_invalid_metrics"] != 1 || report.Reasons["wechat_import_missing_article_identity"] != 1 {
		t.Fatalf("report = %+v", report)
	}
	q := AnalyticsQuery{From: "2026-09-25", To: "2026-09-27", Granularity: "day", MetricBasis: "cumulative"}
	d, err := s.Detail(context.Background(), "u", "p", observations[0].ContentID, q)
	if err != nil || len(d.Series) != 2 || d.Totals["read_users"] != nil {
		t.Fatalf("detail = %+v, %v", d, err)
	}
}

func TestWechatRecoveryLinksHistoryToExactReimportURL(t *testing.T) {
	s, m, db := wechatRecoveryFixture(t)
	url := "https://mp.weixin.qq.com/s?__biz=account&mid=100&idx=1"
	seedWechatRecoveryRow(t, db, "old", "b-old", "2026-09-25", url, 100)
	seedWechatRecoveryRow(t, db, "new", "b-new", "2026-09-27", url, 250)
	var row model.WechatAnalyticsImportRow
	var batch model.WechatAnalyticsImportBatch
	if err := db.First(&row, "id = ?", "new").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&batch, "id = ?", "b-new").Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "task1", ProjectID: "p", UserID: "u", Type: "article", Status: "completed", Title: "历史文章"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	in, err := analyticsWechatInput(AnalyticsCandidate{Task: &task, Title: task.Title, ContentType: "article"}, &row, &batch, batch.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Apply(context.Background(), AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{in}}); err != nil {
		t.Fatal(err)
	}
	row.TaskID = task.ID
	if err = db.Create(snapshotFromWechatImport(&batch, &row, batch.ReceivedAt)).Error; err != nil {
		t.Fatal(err)
	}
	runWechatRecovery(t, m)
	q := AnalyticsQuery{From: "2026-09-25", To: "2026-09-27", Granularity: "day", MetricBasis: "cumulative"}
	d, err := s.Detail(context.Background(), "u", "p", "task:task1", q)
	if err != nil || len(d.Series) != 2 || d.Series[0]["read_users"] != int64(100) || d.Totals["read_users"] != int64(250) {
		t.Fatalf("reimport history = %+v, %v", d, err)
	}
	var n int64
	if err = db.Model(&model.AnalyticsObservation{}).Count(&n).Error; err != nil || n != 2 {
		t.Fatalf("live import duplicated during rebuild: %d, %v", n, err)
	}
}

func TestWechatRecoverySameDayKeepsOriginalReceiptOrder(t *testing.T) {
	s, m, db := wechatRecoveryFixture(t)
	url := "https://mp.weixin.qq.com/s?__biz=account&mid=100&idx=1"
	seedWechatRecoveryRow(t, db, "z-old", "old", "2026-09-25", url, 100)
	seedWechatRecoveryRow(t, db, "a-new", "new", "2026-09-25", url, 250)
	if err := db.Model(&model.WechatAnalyticsImportBatch{}).Where("id = ?", "new").Update("received_at", time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC)).Error; err != nil {
		t.Fatal(err)
	}
	runWechatRecovery(t, m)
	v, err := s.Overview(context.Background(), "u", "p", AnalyticsQuery{From: "2026-09-25", To: "2026-09-25", Granularity: "day", MetricBasis: "cumulative"})
	if err != nil || v.Totals["read_users"] != int64(250) {
		t.Fatalf("recovery used UUID/backfill order instead of original receipts: %+v, %v", v, err)
	}
}

func TestWechatRecoveryUsesAlreadyMigratedSnapshotIdentity(t *testing.T) {
	s, m, db := wechatRecoveryFixture(t)
	url := "https://mp.weixin.qq.com/s?__biz=account&mid=100&idx=1"
	seedWechatRecoveryRow(t, db, "old", "b-old", "2026-09-25", url, 100)
	seedWechatRecoveryRow(t, db, "sqlrow", "b-sql", "2026-09-27", url, 250)
	var row model.WechatAnalyticsImportRow
	var batch model.WechatAnalyticsImportBatch
	if err := db.First(&row, "id = ?", "sqlrow").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&batch, "id = ?", "b-sql").Error; err != nil {
		t.Fatal(err)
	}
	row.PublicationID = "pub1"
	snapshot := snapshotFromWechatImport(&batch, &row, batch.ReceivedAt)
	if err := db.Create(snapshot).Error; err != nil {
		t.Fatal(err)
	}
	reads := int64(250)
	in := AnalyticsObservationInput{Content: model.AnalyticsContent{ID: "task:task1", TaskID: "task1", PublicationID: "pub1", ProjectID: "p", Platform: model.PlatformArticle, Title: "SQL migrated"}, Observation: model.AnalyticsObservation{ID: "legacy:wechat_import:" + snapshot.ID, ProjectID: "p", ContentID: "task:task1", BatchID: batch.ID, MetricBasis: "cumulative", StatDate: "2026-09-27", Source: "wechat_import", SourcePriority: 100, EffectiveAt: batch.DataAsOfAt, ReceivedAt: batch.ReceivedAt, AnalyticsMetrics: model.AnalyticsMetrics{ReadUsers: &reads}}, RawPayload: row.RawData}
	if _, err := s.Apply(context.Background(), AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{in}}); err != nil {
		t.Fatal(err)
	}
	runWechatRecovery(t, m)
	q := AnalyticsQuery{From: "2026-09-25", To: "2026-09-27", Granularity: "day", MetricBasis: "cumulative"}
	page, err := s.Contents(context.Background(), "u", "p", q)
	if err != nil || page.Total != 1 {
		t.Fatalf("recovery split existing identity: %+v, %v", page, err)
	}
	d, err := s.Detail(context.Background(), "u", "p", "task:task1", q)
	if err != nil || len(d.Series) != 2 {
		t.Fatalf("recovered timeline not joined to existing series: %+v, %v", d, err)
	}
}

func TestWechatRecoveryDoesNotBindByTitleOrAcrossProjects(t *testing.T) {
	s, m, db := wechatRecoveryFixture(t)
	url := "https://mp.weixin.qq.com/s?__biz=account&mid=100&idx=1"
	seedWechatRecoveryRow(t, db, "one", "b-one", "2026-09-25", url, 100)
	pub := model.WechatPublication{ID: "similar-title", TaskID: "other-url", ProjectID: "p", UserID: "u", Source: "wechat_console", Status: "published", DraftTitle: "历史文章", ArticleURL: "https://mp.weixin.qq.com/s?__biz=account&mid=999&idx=1"}
	if err := db.Create(&pub).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsContent{ID: "another-account", ProjectID: "other", Platform: model.PlatformArticle, URL: url, Title: "历史文章"}).Error; err != nil {
		t.Fatal(err)
	}
	runWechatRecovery(t, m)
	q := AnalyticsQuery{From: "2026-09-25", To: "2026-09-25", Granularity: "day", MetricBasis: "cumulative"}
	page, err := s.Contents(context.Background(), "u", "p", q)
	if err != nil || page.Total != 1 || page.Items[0].TaskID != "" || page.Items[0].PublicationID != "" {
		t.Fatalf("unsafe identity match: %+v, %v", page, err)
	}
	if _, err = s.Detail(context.Background(), "u", "p", "another-account", q); err == nil {
		t.Fatal("cross-account detail resolved")
	}
}

func seedWechatRecoveryRow(t *testing.T, db *gorm.DB, id, batch, date, url string, reads int64) {
	t.Helper()
	at, err := time.Parse(time.RFC3339, date+"T23:59:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	b := model.WechatAnalyticsImportBatch{ID: batch, ProjectID: "p", UserID: "u", DataAsOfAt: at.UTC(), ReceivedAt: at.Add(time.Hour).UTC(), Timezone: "Asia/Shanghai", Status: "needs_review"}
	if err = db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	r := model.WechatAnalyticsImportRow{ID: id, ProjectID: "p", BatchID: batch, SourceRow: 4, Title: "历史文章", ArticleURL: url, ReadUsers: &reads, MatchStatus: "unmatched", RawData: `{"阅读人数":"original"}`}
	if err = db.Create(&r).Error; err != nil {
		t.Fatal(err)
	}
}

func runWechatRecovery(t *testing.T, m *AnalyticsRebuildManager) *model.AnalyticsRebuildJob {
	t.Helper()
	j, err := m.Start(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Run(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestWechatRecoveryPreservesHistoricalDatesAndDistinctArticles(t *testing.T) {
	s, m, db := wechatRecoveryFixture(t)
	seedWechatRecoveryRow(t, db, "row1", "batch1", "2026-09-25", "http://mp.weixin.qq.com/s?__biz=account&mid=2247483766&idx=1&sn=signature", 100)
	seedWechatRecoveryRow(t, db, "row2", "batch2", "2026-09-27", "https://mp.weixin.qq.com/s?idx=1&mid=2247483766&__biz=account&scene=1&sn=signature#wechat_redirect", 250)
	seedWechatRecoveryRow(t, db, "row3", "batch3", "2026-09-27", "https://mp.weixin.qq.com/s?__biz=account&mid=2247483797&idx=1&sn=other", 40)
	runWechatRecovery(t, m)
	ctx := context.Background()
	q := AnalyticsQuery{From: "2026-09-25", To: "2026-09-27", MetricBasis: "cumulative", Granularity: "day"}
	page, err := s.Contents(ctx, "u", "p", q)
	if err != nil || page.Total != 2 {
		t.Fatalf("recovered contents = %+v, error = %v; want two distinct URL identities", page, err)
	}
	var content string
	for _, item := range page.Items {
		if item.Metrics["read_users"] == int64(250) {
			content = item.ID
		}
	}
	detail, err := s.Detail(ctx, "u", "p", content, q)
	if err != nil || len(detail.Series) != 2 || detail.Totals["read_users"] != int64(250) {
		t.Fatalf("historical detail = %+v, error = %v", detail, err)
	}
	if detail.Series[0]["date"] != "2026-09-25" || detail.Series[0]["read_users"] != int64(100) || detail.Series[1]["date"] != "2026-09-27" {
		t.Fatalf("original dates/values lost: %+v", detail.Series)
	}
	overview, err := s.Overview(ctx, "u", "p", q)
	if err != nil || overview.Totals["read_users"] != int64(290) {
		t.Fatalf("cumulative total = %+v, error = %v", overview, err)
	}
	runWechatRecovery(t, m)
	var n int64
	if err = db.Model(&model.AnalyticsObservation{}).Count(&n).Error; err != nil || n != 3 {
		t.Fatalf("rerun observations = %d, error = %v", n, err)
	}
	if _, err = s.RevokeBatch(ctx, "p", "batch2"); err != nil {
		t.Fatal(err)
	}
	detail, err = s.Detail(ctx, "u", "p", content, q)
	if err != nil || detail.Totals["read_users"] != int64(100) || len(detail.Series) != 1 {
		t.Fatalf("revoke latest must expose old batch: %+v, %v", detail, err)
	}
}
