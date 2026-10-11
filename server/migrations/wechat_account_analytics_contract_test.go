package migrations

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestWechatAccountAnalyticsMigrationContract(t *testing.T) {
	b, err := os.ReadFile("20261009_wechat_account_analytics.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, required := range []string{
		"wechat_account_analytics_batches",
		"`user_id` CHAR(36) NOT NULL",
		"wechat_account_analytics_days",
		"wechat_traffic_source_days",
		"idx_wechat_account_analytics_days_project_date",
		"idx_wechat_traffic_source_days_project_date",
		"uq_wechat_traffic_source_days_batch_date_source",
		"revoked_at",
	} {
		if !strings.Contains(s, required) {
			t.Errorf("migration is missing %q", required)
		}
	}
}

func TestWechatAccountAnalyticsAutoMigrateAndNullableMetrics(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	for _, table := range []string{
		"wechat_account_analytics_batches",
		"wechat_account_analytics_days",
		"wechat_traffic_source_days",
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("AutoMigrate did not register %s", table)
		}
	}

	zero := int64(0)
	batch := &model.WechatAccountAnalyticsBatch{
		ID:         "batch-1",
		ProjectID:  "project-1",
		ReceivedAt: time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC),
		EffectiveAt: time.Date(2026, 10, 8, 0, 0, 0, 0,
			time.UTC),
		Status: model.WechatAccountAnalyticsBatchCompleted,
	}
	if err := db.Create(batch).Error; err != nil {
		t.Fatalf("create batch: %v", err)
	}
	day := &model.WechatAccountAnalyticsDay{
		ID:                "day-1",
		ProjectID:         batch.ProjectID,
		BatchID:           batch.ID,
		StatDate:          "2026-10-08",
		ReadUsers:         &zero,
		ShareUsers:        nil,
		PublishedArticles: nil,
	}
	if err := db.Create(day).Error; err != nil {
		t.Fatalf("create day: %v", err)
	}
	var got model.WechatAccountAnalyticsDay
	if err := db.First(&got, "id = ?", day.ID).Error; err != nil {
		t.Fatalf("read day: %v", err)
	}
	if got.ReadUsers == nil || *got.ReadUsers != 0 {
		t.Fatalf("explicit zero was not preserved: %#v", got.ReadUsers)
	}
	if got.ShareUsers != nil || got.PublishedArticles != nil {
		t.Fatalf("missing metrics must remain null: share=%#v published=%#v", got.ShareUsers, got.PublishedArticles)
	}
}

func TestWechatTrafficSourceNormalizationAndUniqueFacts(t *testing.T) {
	cases := map[string]model.WechatTrafficSource{
		"消息":                    model.WechatTrafficSourceMessage,
		"朋友在看":                  model.WechatTrafficSourceRecommendation,
		"朋友圈":                   model.WechatTrafficSourceMoments,
		"搜一搜":                   model.WechatTrafficSourceSearch,
		"其他":                    model.WechatTrafficSourceOther,
		"official account home": model.WechatTrafficSourceOfficialAccountHome,
	}
	for input, want := range cases {
		got, err := model.NormalizeWechatTrafficSource(input)
		if err != nil || got != want {
			t.Errorf("NormalizeWechatTrafficSource(%q) = %q, %v; want %q", input, got, err, want)
		}
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.WechatAccountAnalyticsBatch{}, &model.WechatTrafficSourceDay{}); err != nil {
		t.Fatal(err)
	}
	batch := model.WechatAccountAnalyticsBatch{ID: "batch-unique", ProjectID: "project-unique", Status: model.WechatAccountAnalyticsBatchCompleted, ReceivedAt: time.Now().UTC(), EffectiveAt: time.Now().UTC()}
	if err := db.Create(&batch).Error; err != nil {
		t.Fatal(err)
	}
	row := model.WechatTrafficSourceDay{ID: "source-1", ProjectID: batch.ProjectID, BatchID: batch.ID, StatDate: "2026-10-08", Source: model.WechatTrafficSourceMessage, ReadUsers: ptrInt64(3)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := row
	duplicate.ID = "source-2"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate project/date/batch/source fact should be rejected")
	}
}

func TestWechatAccountAnalyticsRepositoryWinnerRevocationAndRevision(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	old := &model.WechatAccountAnalyticsBatch{ID: "batch-old", ProjectID: "project-repo", Status: model.WechatAccountAnalyticsBatchCompleted, ReceivedAt: time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}
	newer := &model.WechatAccountAnalyticsBatch{ID: "batch-new", ProjectID: old.ProjectID, Status: model.WechatAccountAnalyticsBatchCompleted, ReceivedAt: time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}
	active := &model.WechatAccountAnalyticsBatch{ID: "batch-active", ProjectID: old.ProjectID, Status: model.WechatAccountAnalyticsBatchActive, ReceivedAt: time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)}
	for _, batch := range []*model.WechatAccountAnalyticsBatch{old, newer, active} {
		if err := repo.WechatAccountAnalytics().CreateBatch(ctx, batch); err != nil {
			t.Fatal(err)
		}
	}
	for _, day := range []*model.WechatAccountAnalyticsDay{
		{ID: "day-old", ProjectID: old.ProjectID, BatchID: old.ID, StatDate: "2026-10-08", ReadUsers: ptrInt64(10)},
		{ID: "day-new", ProjectID: newer.ProjectID, BatchID: newer.ID, StatDate: "2026-10-08", ReadUsers: ptrInt64(20)},
		{ID: "day-active", ProjectID: active.ProjectID, BatchID: active.ID, StatDate: "2026-10-08", ReadUsers: ptrInt64(30)},
	} {
		if err := repo.WechatAccountAnalytics().CreateDays(ctx, []*model.WechatAccountAnalyticsDay{day}); err != nil {
			t.Fatal(err)
		}
	}
	winners, err := repo.WechatAccountAnalytics().ListWinningDays(ctx, old.ProjectID, "2026-10-08", "2026-10-08")
	if err != nil {
		t.Fatal(err)
	}
	if len(winners) != 1 || winners[0].ReadUsers == nil || *winners[0].ReadUsers != 30 {
		t.Fatalf("winner = %#v, want active fact", winners)
	}
	if err := repo.WechatAccountAnalytics().RevokeBatch(ctx, old.ProjectID, old.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := repo.WechatAccountAnalytics().RevokeBatch(ctx, old.ProjectID, "missing", time.Now().UTC()); err == nil {
		t.Fatal("revoking a missing batch should fail")
	}
	if err := repo.WechatAccountAnalytics().BumpRevision(ctx, old.ProjectID); err != nil {
		t.Fatal(err)
	}
	revision, err := repo.WechatAccountAnalytics().CurrentRevision(ctx, old.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if revision != 1 {
		t.Fatalf("revision = %d, want 1", revision)
	}
}

func ptrInt64(v int64) *int64 { return &v }
