package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

// Exercises the actual import mapping, persisted observations/projections and
// every public read shape. Seednote-only fixtures missed the platform/channel
// mismatch that silently removed all four WeChat summary fields.
func TestAnalyticsWechatImportedMetricsSurviveEveryRead(t *testing.T) {
	s, db := analyticsFixture(t)
	if err := db.Model(&model.Project{}).Where("id = ?", "p").Update("platform", "wechat").Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	date := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	read, zero, delivered := int64(42), int64(0), int64(100)
	input, err := analyticsWechatInput(AnalyticsCandidate{Title: "tea", ContentType: "wechat-article", Task: &model.Task{ID: "tea"}},
		&model.WechatAnalyticsImportRow{ID: "row", ReadUsers: &read, ShareUsers: &zero, DeliveredUsers: &delivered, RawData: `{}`},
		&model.WechatAnalyticsImportBatch{ID: "batch", ProjectID: "p", DataAsOfAt: date}, date)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{input}}); err != nil {
		t.Fatal(err)
	}
	q := AnalyticsQuery{From: "2026-09-28", To: "2026-10-04", Granularity: "day", MetricBasis: "cumulative", Limit: 25}
	assertMetrics := func(label string, got map[string]any) {
		t.Helper()
		for key, want := range map[string]any{"read_users": int64(42), "share_users": int64(0), "read_to_follow_users": nil, "delivered_users": int64(100)} {
			value, exists := got[key]
			if !exists || value != want {
				t.Errorf("%s %s = %v (exists %v), want %v", label, key, value, exists, want)
			}
		}
		if _, exists := got["view_count"]; exists {
			t.Errorf("%s leaked seednote metrics: %v", label, got)
		}
	}
	overview, err := s.Overview(ctx, "u", "p", q)
	if err != nil {
		t.Fatal(err)
	}
	assertMetrics("overview", overview.Totals)
	if len(overview.Series) != 1 {
		t.Fatalf("series = %v", overview.Series)
	}
	assertMetrics("trend", overview.Series[0])
	page, err := s.Contents(ctx, "u", "p", q)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	assertMetrics("contents", page.Items[0].Metrics)
	encoded, err := json.Marshal(page.Items[0])
	if err != nil {
		t.Fatal(err)
	}
	var view map[string]any
	if err := json.Unmarshal(encoded, &view); err != nil {
		t.Fatal(err)
	}
	if view["last_stat_date"] != "2026-10-01" {
		t.Errorf("lost statistical date: %s", encoded)
	}

	detail, err := s.Detail(ctx, "u", "p", "task:tea", q)
	if err != nil {
		t.Fatal(err)
	}
	assertMetrics("detail", detail.Content.Metrics)
	audit, err := s.Observations(ctx, "u", "p", "task:tea", q)
	if err != nil || len(audit.Items) != 1 {
		t.Fatalf("audit=%+v err=%v", audit, err)
	}
	assertMetrics("observations", audit.Items[0].Metrics)
	q.From, q.To = "2026-10-02", "2026-10-04"
	empty, err := s.Overview(ctx, "u", "p", q)
	if err != nil {
		t.Fatal(err)
	}
	if value, exists := empty.Totals["read_users"]; !exists || value != nil || len(empty.Series) != 0 {
		t.Fatalf("out-of-range = %+v", empty)
	}
}

func TestAnalyticsRejectsUnknownMetricPlatform(t *testing.T) {
	s, db := analyticsFixture(t)
	if err := db.Model(&model.Project{}).Where("id = ?", "p").Update("platform", "wechat-article").Error; err != nil {
		t.Fatal(err)
	}
	q := AnalyticsQuery{From: "2026-09-28", To: "2026-10-04", Granularity: "day", MetricBasis: "cumulative"}
	if _, err := s.Overview(context.Background(), "u", "p", q); err == nil {
		t.Fatal("channel passed as project platform was silently accepted")
	}
}

func TestWechatFeedbackMetricEligibilityUsesPlatformNotBasis(t *testing.T) {
	zero := int64(0)
	row := model.AnalyticsObservation{MetricBasis: "cumulative", AnalyticsMetrics: model.AnalyticsMetrics{ReadUsers: &zero}}
	if !hasValidFeedbackMetric(row, "wechat") || !hasValidFeedbackMetric(row, "wechat-article") || !hasValidFeedbackMetric(row, "wechat-picture") || !observationHasMetric(row) {
		t.Fatal("observed zero WeChat metric treated as missing")
	}
	if hasValidFeedbackMetric(row, "seednote") || hasValidFeedbackMetric(row, "cumulative") {
		t.Fatal("wrong vocabulary accepted")
	}
}

func TestAnalyticsContentTypeFilterUsesPublicationAndPostIdentity(t *testing.T) {
	s, db := analyticsFixture(t)
	if err := db.AutoMigrate(&model.Task{}, &model.WechatPublication{}, &model.SeednotePost{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.WechatPublication{ID: "pic", UserID: "u", ProjectID: "p", DraftTitle: "picture", DraftArticleType: "newspic", Source: "wechat_console", Status: "drafted"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SeednotePost{ID: "note", UserID: "u", ProjectID: "p", Genre: "image", Title: "note"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]model.AnalyticsContent{{ID: "wechat_publication:pic", ProjectID: "p", PublicationID: "pic", ContentType: "unknown"}, {ID: "seednote_post:note", ProjectID: "p", PostID: "note", ContentType: "unknown"}}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ typ, id, platform string }{{"wechat-picture", "wechat_publication:pic", "wechat"}, {"image_text", "seednote_post:note", "seednote"}} {
		q := AnalyticsQuery{From: "2026-09-28", To: "2026-10-04", MetricBasis: "cumulative", ContentType: tc.typ, Platform: tc.platform}
		page, err := s.Contents(context.Background(), "u", "p", q)
		if err != nil || len(page.Items) != 1 || page.Items[0].ContentType != tc.typ || page.Items[0].ID != tc.id {
			t.Errorf("filter %s: %+v %v", tc.typ, page, err)
		}
		detail, err := s.Detail(context.Background(), "u", "p", tc.id, q)
		if err != nil || detail.Content.ContentType != tc.typ {
			t.Errorf("detail %s: %+v %v", tc.typ, detail.Content, err)
		}
	}
}
