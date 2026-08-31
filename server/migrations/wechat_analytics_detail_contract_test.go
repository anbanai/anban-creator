package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestWechatAnalyticsDetailMigrationIsCleanAndUsesExactSnapshotIdentity(t *testing.T) {
	raw, err := os.ReadFile("20260831_wechat_analytics_detail_contract.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	wantInOrder := []string{
		"DROP TABLE IF EXISTS `wechat_metric_snapshots`",
		"DROP TABLE IF EXISTS `wechat_article_trackings`",
		"CREATE TABLE `wechat_article_trackings`",
		"CREATE TABLE `wechat_metric_snapshots`",
		"UNIQUE KEY `idx_wechat_tracking_stat_date` (`tracking_id`, `stat_date`)",
	}
	previous := -1
	for _, fragment := range wantInOrder {
		offset := strings.Index(sql, fragment)
		if offset < 0 || offset <= previous {
			t.Fatalf("migration fragment %q missing or out of order", fragment)
		}
		previous = offset
	}
	for _, obsolete := range []string{"captured_date", "int_page_read", "ori_page_read", "add_to_fav", "getarticletotal`"} {
		if strings.Contains(sql, obsolete) {
			t.Fatalf("migration retains obsolete analytics contract %q", obsolete)
		}
	}
}
