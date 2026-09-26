package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestAnalyticsV2MigrationContract(t *testing.T) {
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
