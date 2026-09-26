package service

import (
	"context"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
	"time"
)

func analyticsFixture(t *testing.T) (*AnalyticsService, *gorm.DB) {
	t.Helper()
	db, e := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&model.Project{}, &model.AnalyticsState{}, &model.AnalyticsContent{}, &model.AnalyticsObservation{}, &model.AnalyticsRawPayload{}, &model.AnalyticsBucket{}, &model.AnalyticsIdempotency{}); e != nil {
		t.Fatal(e)
	}
	db.Create(&model.Project{ID: "p", UserID: "u", Platform: "seednote"})
	return NewAnalyticsService(repository.New(db)), db
}
func analyticsWrite(t *testing.T, s *AnalyticsService, id, content, date, basis, batch string, priority int, count *int64) {
	t.Helper()
	at, _ := time.Parse(time.RFC3339, date+"T12:00:00+08:00")
	_, e := s.Apply(context.Background(), AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{{Content: model.AnalyticsContent{ID: content, ProjectID: "p", Platform: "seednote", Title: content}, Observation: model.AnalyticsObservation{ID: id, ContentID: content, ProjectID: "p", BatchID: batch, StatDate: date, MetricBasis: basis, Source: "import", SourcePriority: priority, EffectiveAt: at, ReceivedAt: at, AnalyticsMetrics: model.AnalyticsMetrics{ViewCount: count}}, RawPayload: `{"source":true}`}}})
	if e != nil {
		t.Fatal(e)
	}
}
func TestAnalyticsV2CumulativePriorityRevocationAndRange(t *testing.T) {
	s, _ := analyticsFixture(t)
	a, b, c := int64(10), int64(20), int64(30)
	analyticsWrite(t, s, "1", "a", "2026-01-31", "cumulative", "b1", 200, &a)
	analyticsWrite(t, s, "2", "a", "2026-02-01", "cumulative", "b2", 200, &b)
	analyticsWrite(t, s, "3", "a", "2026-02-01", "cumulative", "b3", 100, &c)
	analyticsWrite(t, s, "4", "b", "2026-02-01", "cumulative", "b4", 200, &a)
	q := AnalyticsQuery{From: "2026-01-31", To: "2026-02-01", Granularity: "month", MetricBasis: "cumulative"}
	v, e := s.Overview(context.Background(), "u", "p", q)
	if e != nil {
		t.Fatal(e)
	}
	if v.Totals["view_count"] != int64(30) {
		t.Fatalf("totals=%v", v.Totals)
	}
	if _, e = s.RevokeBatch(context.Background(), "p", "b2"); e != nil {
		t.Fatal(e)
	}
	v, e = s.Overview(context.Background(), "u", "p", q)
	if e != nil || v.Totals["view_count"] != int64(40) {
		t.Fatalf("revoke=%+v %v", v, e)
	}
	revision := v.Revision - 1
	q.ExpectedRevision = &revision
	if _, e = s.Overview(context.Background(), "u", "p", q); e != ErrAnalyticsRevisionConflict {
		t.Fatalf("revision error %v", e)
	}
}
func TestAnalyticsV2DailyNullAndRollback(t *testing.T) {
	s, db := analyticsFixture(t)
	zero, n := int64(0), int64(7)
	analyticsWrite(t, s, "1", "a", "2026-02-01", "daily", "one", 200, &zero)
	analyticsWrite(t, s, "2", "a", "2026-02-02", "daily", "two", 200, nil)
	analyticsWrite(t, s, "3", "a", "2026-02-03", "daily", "three", 200, &n)
	analyticsWrite(t, s, "4", "a", "2026-02-03", "cumulative", "four", 200, &n)
	q := AnalyticsQuery{From: "2026-02-01", To: "2026-02-02", Granularity: "week", MetricBasis: "daily"}
	v, e := s.Overview(context.Background(), "u", "p", q)
	if e != nil || v.Totals["view_count"] != int64(0) {
		t.Fatalf("clipped totals=%+v %v", v, e)
	}
	q.From = "2026-02-02"
	v, e = s.Overview(context.Background(), "u", "p", q)
	if e != nil || v.Totals["view_count"] != nil {
		t.Fatalf("null totals=%+v %v", v, e)
	}
	var raw int64
	db.Model(&model.AnalyticsRawPayload{}).Count(&raw)
	if raw != 4 {
		t.Fatalf("raw=%d", raw)
	}
	if _, e = s.Overview(context.Background(), "other", "p", q); e != ErrAnalyticsForbidden {
		t.Fatalf("ownership=%v", e)
	}
}

func TestAnalyticsV2ExactDecimalsNullOrderingAndAudit(t *testing.T) {
	s, db := analyticsFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 2, 3, 12, 0, 0, 0, time.UTC)
	n := int64(10)
	rate := model.AnalyticsDecimal("0.123456789012345678")
	in := AnalyticsObservationInput{Content: model.AnalyticsContent{ID: "a", ProjectID: "p", Platform: "seednote", Title: "a", TaskID: "task"}, Observation: model.AnalyticsObservation{ID: "exact", ProjectID: "p", ContentID: "a", StatDate: "2026-02-03", MetricBasis: "cumulative", Source: "import", EffectiveAt: at, ReceivedAt: at, AnalyticsMetrics: model.AnalyticsMetrics{ViewCount: &n, CoverClickRate: &rate}}, RawPayload: `{"large":"original payload"}`}
	rev, e := s.Apply(ctx, AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{in}})
	if e != nil {
		t.Fatal(e)
	}
	replay, e := s.Apply(ctx, AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{in}})
	if e != nil || replay != rev {
		t.Fatalf("replay %d %v", replay, e)
	}
	analyticsWrite(t, s, "null", "b", "2026-02-03", "cumulative", "null", 200, nil)
	q := AnalyticsQuery{From: "2026-02-01", To: "2026-02-28", MetricBasis: "cumulative", Granularity: "day", Sort: "view_count", Direction: "asc", Limit: 1}
	page, e := s.Contents(ctx, "u", "p", q)
	if e != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != "a" {
		t.Fatalf("page %+v %v", page, e)
	}
	detail, e := s.Detail(ctx, "u", "p", "task:task", q)
	if e != nil || detail.Totals["cover_click_rate"] != rate {
		t.Fatalf("exact decimal %+v %v", detail, e)
	}
	overview, e := s.Overview(ctx, "u", "p", q)
	if e != nil || overview.Totals["cover_click_rate"] != nil || overview.UnavailableMetrics["cover_click_rate"] == "" {
		t.Fatalf("unavailable %+v %v", overview, e)
	}
	audit, e := s.Observations(ctx, "u", "p", "a", q)
	if e != nil || len(audit.Items) != 1 || audit.Items[0].Metrics["cover_click_rate"] != rate {
		t.Fatalf("audit %+v %v", audit, e)
	}
	n = 11
	if _, e = s.Apply(ctx, AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{in}}); e != ErrAnalyticsIdempotencyConflict {
		t.Fatalf("mutating immutable observation: %v", e)
	}
	var count int64
	db.Model(&model.AnalyticsObservation{}).Count(&count)
	if count != 2 {
		t.Fatalf("facts %d", count)
	}
}
func TestAnalyticsV2OverflowAndInvalidDecimalRollBack(t *testing.T) {
	s, db := analyticsFixture(t)
	ctx := context.Background()
	max := int64(9223372036854775807)
	analyticsWrite(t, s, "max", "a", "2026-02-01", "daily", "max", 200, &max)
	at := time.Now()
	one := int64(1)
	in := AnalyticsObservationInput{Content: model.AnalyticsContent{ID: "b", ProjectID: "p", Platform: "seednote"}, Observation: model.AnalyticsObservation{ID: "overflow", ProjectID: "p", ContentID: "b", StatDate: "2026-02-01", MetricBasis: "daily", Source: "import", EffectiveAt: at, ReceivedAt: at, AnalyticsMetrics: model.AnalyticsMetrics{ViewCount: &one}}, RawPayload: "{}"}
	if _, e := s.Apply(ctx, AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{in}}); e == nil {
		t.Fatal("expected sum overflow")
	}
	var count int64
	db.Model(&model.AnalyticsObservation{}).Count(&count)
	if count != 1 {
		t.Fatal("overflow partially committed")
	}
	bad := model.AnalyticsDecimal("NaN")
	in.Observation.CoverClickRate = &bad
	if _, e := s.Apply(ctx, AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{in}}); e == nil {
		t.Fatal("NaN accepted")
	}
	state, _ := s.repo.Analytics().State(ctx, "p")
	if state.Revision != 1 {
		t.Fatal("failed write advanced revision")
	}
}
func TestAnalyticsV2BoundariesAndSparseCumulative(t *testing.T) {
	s, _ := analyticsFixture(t)
	a, b, c, d := int64(2), int64(100), int64(4), int64(1000)
	analyticsWrite(t, s, "a1", "a", "2026-01-01", "cumulative", "a1", 200, &a)
	analyticsWrite(t, s, "b1", "b", "2026-02-01", "cumulative", "b1", 200, &b)
	analyticsWrite(t, s, "a2", "a", "2026-03-01", "cumulative", "a2", 200, &c)
	analyticsWrite(t, s, "a3", "a", "2026-03-31", "cumulative", "a3", 200, &d)
	q := AnalyticsQuery{From: "2026-01-01", To: "2026-03-02", MetricBasis: "cumulative", Granularity: "month"}
	v, e := s.Overview(context.Background(), "u", "p", q)
	if e != nil || v.Totals["view_count"] != int64(104) || len(v.Series) != 3 || v.Series[2]["view_count"] != int64(4) {
		t.Fatalf("sparse %+v %v", v, e)
	}
	q.MetricBasis = "daily"
	v, e = s.Overview(context.Background(), "u", "p", q)
	if e != nil || v.Totals["view_count"] != nil || len(v.Series) != 0 {
		t.Fatalf("basis leaked %+v %v", v, e)
	}
}
func TestAnalyticsV2RebuildGateAndIdempotency(t *testing.T) {
	s, db := analyticsFixture(t)
	ctx := context.Background()
	q := AnalyticsQuery{From: "2026-01-01", To: "2026-01-31", MetricBasis: "daily"}
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	e := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		svc := NewAnalyticsService(tx)
		prior, _, e := svc.ClaimIdempotency(ctx, "p", "key", hash, "batch")
		if e != nil || prior != "" {
			t.Fatalf("claim %q %v", prior, e)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	prior, _, e := s.ClaimIdempotency(ctx, "p", "key", hash, "unused")
	if e != nil || prior != "batch" {
		t.Fatalf("replay %q %v", prior, e)
	}
	if _, _, e = s.ClaimIdempotency(ctx, "p", "key", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "unused"); e != ErrAnalyticsIdempotencyConflict {
		t.Fatalf("key conflict %v", e)
	}
	db.Model(&model.AnalyticsState{}).Where("project_id = ?", "p").Updates(map[string]any{"status": "rebuilding", "active_generation": 0})
	if _, e = s.Overview(ctx, "u", "p", q); e != ErrAnalyticsRebuilding {
		t.Fatalf("initial rebuild read %v", e)
	}
	if _, e = s.LockProjectWrite(ctx, "p"); e != ErrAnalyticsRebuilding {
		t.Fatalf("rebuild write %v", e)
	}
	db.Model(&model.AnalyticsState{}).Where("project_id = ?", "p").Update("active_generation", 1)
	if _, e = s.Overview(ctx, "u", "p", q); e != nil {
		t.Fatalf("old generation read %v", e)
	}
}
