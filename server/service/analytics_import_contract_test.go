package service

import (
	"testing"
	"time"
)

func TestAnalyticsImportRequiresExplicitSemantics(t *testing.T) {
	now := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, platform, basis, key string
		at                         *time.Time
		valid                      bool
	}{
		{"wechat cumulative", "article", "cumulative", "key", &now, true},
		{"wechat fixed default", "article", "", "key", &now, true},
		{"seednote cumulative", "seednote", "cumulative", "key", &now, true},
		{"seednote daily", "seednote", "daily", "key", &now, true},
		{"seednote missing basis", "seednote", "", "key", &now, false},
		{"wechat daily forbidden", "article", "daily", "key", &now, false},
		{"missing date", "article", "cumulative", "key", nil, false},
		{"missing request key", "article", "cumulative", "", &now, false},
		{"interval forbidden", "seednote", "interval", "key", &now, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAnalyticsImport(tc.platform, tc.basis, tc.key, tc.at)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
func TestAnalyticsRequestFingerprintIncludesMeaningButNotSelectionOrder(t *testing.T) {
	now := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	selections := []AnalyticsSelection{{2, AnalyticsTarget{"task", "a"}}, {3, AnalyticsTarget{"task", "b"}}}
	a := analyticsRequestFingerprint("file", "cumulative", now, selections)
	b := analyticsRequestFingerprint("file", "cumulative", now, []AnalyticsSelection{selections[1], selections[0]})
	if a != b {
		t.Fatal("same selections reordered must be same request")
	}
	if a == analyticsRequestFingerprint("file", "daily", now, selections) {
		t.Fatal("basis not fingerprinted")
	}
	if a == analyticsRequestFingerprint("file", "cumulative", now.Add(time.Hour), selections) {
		t.Fatal("date not fingerprinted")
	}
}
