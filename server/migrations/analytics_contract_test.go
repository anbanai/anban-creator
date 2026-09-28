package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestAnalyticsMigrationContract(t *testing.T) {
	b, e := os.ReadFile("20260926_analytics_v2.sql")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, n := range []string{"analytics_project_states", "analytics_contents", "analytics_observations", "analytics_observation_payloads", "analytics_buckets", "analytics_idempotencies", "analytics_rebuild_jobs", "DECIMAL(38,18)", "idx_analytics_observations_winner", "idx_analytics_buckets_range"} {
		if !strings.Contains(s, n) {
			t.Errorf("missing %s", n)
		}
	}
}

func TestAnalyticsLegacyBackfillIncludesOfficialSnapshots(t *testing.T) {
	b, err := os.ReadFile("20260927_analytics_legacy_sql_backfill.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, required := range []string{
		"FROM wechat_analytics_import_batches",
		"FROM seednote_import_batches",
		"wechat_metric_snapshots",
		"wechat_article_trackings",
		"seednote_metric_snapshots",
		"seednote_post_trackings",
		"legacy:wechat_official:",
		"legacy:seednote_official:",
		"wechat_api",
		"seednote_public",
		"seednote_ambiguous",
		"wechat_official",
		"seednote_official",
		"INSERT INTO analytics_observations(",
		"CONCAT('legacy:wechat_official:', legacy_id)",
		"CONCAT('legacy:seednote_official:', legacy_id)",
		"FROM tmp_analytics_wechat_official",
		"FROM tmp_analytics_seednote_official",
		"wechat_publication:",
		"CONCAT('wechat_import:', w.import_row_id)",
		"existing.tracking_id = w.tracking_id",
		"existing.metric_basis = 'cumulative'",
		"existing.stat_date = w.stat_date",
		"tmp_analytics_wechat_identity_conflicts",
		"INSERT INTO analytics_observation_payloads",
		"identity_conflicts",
		"basis_unknown",
		"JSON_OBJECT(",
	} {
		if !strings.Contains(s, required) {
			t.Errorf("legacy backfill is missing official-source migration contract %q", required)
		}
	}
}
