package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func analyticsFixture(t *testing.T) (*AnalyticsService, *gorm.DB) {
	t.Helper()
	db, e := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&model.Project{}, &model.Task{}, &model.AnalyticsState{}, &model.AnalyticsContent{}, &model.AnalyticsObservation{}, &model.AnalyticsRawPayload{}, &model.AnalyticsBucket{}, &model.AnalyticsIdempotency{}, &model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}); e != nil {
		t.Fatal(e)
	}
	db.Create(&model.Project{ID: "p", UserID: "u", Platform: "seednote"})
	return NewAnalyticsService(repository.New(db)), db
}

func TestAnalyticsContentsExposeLatestContentTags(t *testing.T) {
	s, db := analyticsFixture(t)
	count := int64(12)
	analyticsWrite(t, s, "observation-1", "content-1", "2026-02-01", "cumulative", "batch-1", 200, &count)
	if err := db.Create(&model.ContentMetadataReport{ID: "report-1", TaskID: "task-1", ExecutionID: "execution-1", Status: model.ContentMetadataSucceeded, TaggingStatus: model.ContentMetadataSucceeded, FeedbackStatus: model.ContentMetadataSucceeded, TaxonomyVersion: model.ContentTaxonomyVersion, RawMetadata: datatypes.JSON([]byte(`{"tags":[]}`))}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AnalyticsContent{}).Where("id = ?", "content-1").Update("task_id", "task-1").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ContentTagAssignment{ID: "tag-1", ReportID: "report-1", Dimension: "source_relation", CanonicalValue: "hot_search", DisplayName: "热搜", LabelStatus: model.ContentTagCanonical}).Error; err != nil {
		t.Fatal(err)
	}

	page, err := s.Contents(context.Background(), "u", "p", AnalyticsQuery{From: "2026-02-01", To: "2026-02-28", MetricBasis: "cumulative", Granularity: "day"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Tags) != 1 || page.Items[0].Tags[0].DisplayName != "热搜" {
		t.Fatalf("content tags = %#v", page.Items)
	}
}

func TestAnalyticsContentsExposeHotSearchTagFromTaskOriginWithoutMetadataReport(t *testing.T) {
	s, db := analyticsFixture(t)
	if err := db.Create(&model.Task{ID: "task-hot", UserID: "u", ProjectID: "p", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted, ContentOrigin: model.ContentOriginHotSearch}).Error; err != nil {
		t.Fatal(err)
	}
	count := int64(18)
	analyticsWrite(t, s, "observation-hot", "content-hot", "2026-02-01", "cumulative", "batch-hot", 200, &count)
	if err := db.Model(&model.AnalyticsContent{}).Where("id = ?", "content-hot").Update("task_id", "task-hot").Error; err != nil {
		t.Fatal(err)
	}

	page, err := s.Contents(context.Background(), "u", "p", AnalyticsQuery{From: "2026-02-01", To: "2026-02-28", MetricBasis: "cumulative", Granularity: "day"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Tags) != 1 || page.Items[0].Tags[0].DisplayName != "热搜" {
		t.Fatalf("content tags = %#v", page.Items)
	}
}

func TestAnalyticsContentTagsReplaceLegacySourceTagForHotSearchTask(t *testing.T) {
	s, db := analyticsFixture(t)
	if err := db.Create(&model.Task{ID: "task-hot-legacy", UserID: "u", ProjectID: "p", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted, ContentOrigin: model.ContentOriginHotSearch}).Error; err != nil {
		t.Fatal(err)
	}
	count := int64(9)
	analyticsWrite(t, s, "observation-hot-legacy", "content-hot-legacy", "2026-02-01", "cumulative", "batch-hot-legacy", 200, &count)
	if err := db.Model(&model.AnalyticsContent{}).Where("id = ?", "content-hot-legacy").Update("task_id", "task-hot-legacy").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ContentMetadataReport{ID: "report-hot-legacy", TaskID: "task-hot-legacy", ExecutionID: "execution-hot-legacy", Status: model.ContentMetadataSucceeded, TaggingStatus: model.ContentMetadataSucceeded, FeedbackStatus: model.ContentMetadataSucceeded, TaxonomyVersion: model.ContentTaxonomyVersion, RawMetadata: datatypes.JSON([]byte(`{"tags":[]}`))}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ContentTagAssignment{ID: "tag-hot-legacy", ReportID: "report-hot-legacy", Dimension: "source_relation", CanonicalValue: "adapted", DisplayName: "改编", LabelStatus: model.ContentTagCanonical}).Error; err != nil {
		t.Fatal(err)
	}

	page, err := s.Contents(context.Background(), "u", "p", AnalyticsQuery{From: "2026-02-01", To: "2026-02-28", MetricBasis: "cumulative", Granularity: "day"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Tags) != 1 || page.Items[0].Tags[0].DisplayName != "热搜" {
		t.Fatalf("content tags = %#v", page.Items)
	}
}

func TestAnalyticsDetailExposesHotSearchTagFromTaskOrigin(t *testing.T) {
	s, db := analyticsFixture(t)
	if err := db.Create(&model.Task{ID: "task-hot-detail", UserID: "u", ProjectID: "p", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted, ContentOrigin: model.ContentOriginHotSearch}).Error; err != nil {
		t.Fatal(err)
	}
	count := int64(6)
	analyticsWrite(t, s, "observation-hot-detail", "content-hot-detail", "2026-02-01", "cumulative", "batch-hot-detail", 200, &count)
	if err := db.Model(&model.AnalyticsContent{}).Where("id = ?", "content-hot-detail").Update("task_id", "task-hot-detail").Error; err != nil {
		t.Fatal(err)
	}

	detail, err := s.Detail(context.Background(), "u", "p", "content-hot-detail", AnalyticsQuery{From: "2026-02-01", To: "2026-02-28", MetricBasis: "cumulative", Granularity: "day"})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Content.Tags) != 1 || detail.Content.Tags[0].DisplayName != "热搜" {
		t.Fatalf("detail tags = %#v", detail.Content.Tags)
	}
}

func TestAnalyticsContentsKeepLastSuccessfulTagsWhenLaterMetadataRetryFails(t *testing.T) {
	s, db := analyticsFixture(t)
	count := int64(4)
	analyticsWrite(t, s, "observation-retry", "content-retry", "2026-02-01", "cumulative", "batch-retry", 200, &count)
	if err := db.Model(&model.AnalyticsContent{}).Where("id = ?", "content-retry").Update("task_id", "task-retry").Error; err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := db.Create(&model.ContentMetadataReport{ID: "report-retry-ok", TaskID: "task-retry", ExecutionID: "execution-ok", Status: model.ContentMetadataSucceeded, TaggingStatus: model.ContentMetadataSucceeded, FeedbackStatus: model.ContentMetadataSucceeded, TaxonomyVersion: model.ContentTaxonomyVersion, RawMetadata: datatypes.JSON([]byte(`{"tags":[]}`)), CreatedAt: old, UpdatedAt: old}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ContentTagAssignment{ID: "tag-retry-ok", ReportID: "report-retry-ok", Dimension: "performance", CanonicalValue: "viral", DisplayName: "爆款", LabelStatus: model.ContentTagCanonical}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ContentMetadataReport{ID: "report-retry-failed", TaskID: "task-retry", ExecutionID: "execution-failed", Status: model.ContentMetadataFailed, TaggingStatus: model.ContentMetadataFailed, FeedbackStatus: model.ContentMetadataFailed, TaxonomyVersion: model.ContentTaxonomyVersion, RawMetadata: datatypes.JSON([]byte(`{"tags":[]}`)), CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}

	page, err := s.Contents(context.Background(), "u", "p", AnalyticsQuery{From: "2026-02-01", To: "2026-02-28", MetricBasis: "cumulative", Granularity: "day"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Tags) != 1 || page.Items[0].Tags[0].DisplayName != "爆款" {
		t.Fatalf("content tags = %#v", page.Items)
	}
}

func analyticsWrite(t *testing.T, s *AnalyticsService, id, content, date, basis, batch string, priority int, count *int64) {
	t.Helper()
	at, _ := time.Parse(time.RFC3339, date+"T12:00:00+08:00")
	_, e := s.Apply(context.Background(), AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{{Content: model.AnalyticsContent{ID: content, ProjectID: "p", Platform: "seednote", Title: content}, Observation: model.AnalyticsObservation{ID: id, ContentID: content, ProjectID: "p", BatchID: batch, StatDate: date, MetricBasis: basis, Source: "import", SourcePriority: priority, EffectiveAt: at, ReceivedAt: at, AnalyticsMetrics: model.AnalyticsMetrics{ViewCount: count}}, RawPayload: `{"source":true}`}}})
	if e != nil {
		t.Fatal(e)
	}
}
func TestAnalyticsCumulativePriorityRevocationAndRange(t *testing.T) {
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
func TestAnalyticsExactDecimalsNullOrderingAndAudit(t *testing.T) {
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
func TestAnalyticsOverflowAndInvalidDecimalRollBack(t *testing.T) {
	s, db := analyticsFixture(t)
	ctx := context.Background()
	max := int64(9223372036854775807)
	analyticsWrite(t, s, "max", "a", "2026-02-01", "cumulative", "max", 200, &max)
	at := time.Now()
	one := int64(1)
	in := AnalyticsObservationInput{Content: model.AnalyticsContent{ID: "b", ProjectID: "p", Platform: "seednote"}, Observation: model.AnalyticsObservation{ID: "overflow", ProjectID: "p", ContentID: "b", StatDate: "2026-02-01", MetricBasis: "cumulative", Source: "import", EffectiveAt: at, ReceivedAt: at, AnalyticsMetrics: model.AnalyticsMetrics{ViewCount: &one}}, RawPayload: "{}"}
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

func TestAnalyticsRejectsOversizedRawPayloadBeforeTransaction(t *testing.T) {
	s, db := analyticsFixture(t)
	at := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	count := int64(1)
	in := AnalyticsObservationInput{
		Content: model.AnalyticsContent{ID: "large", ProjectID: "p", Platform: "seednote"},
		Observation: model.AnalyticsObservation{
			ID: "large-observation", ProjectID: "p", ContentID: "large", StatDate: "2026-02-01",
			MetricBasis: "cumulative", Source: "import", EffectiveAt: at, ReceivedAt: at,
			AnalyticsMetrics: model.AnalyticsMetrics{ViewCount: &count},
		},
		RawPayload: strings.Repeat("x", maxAnalyticsRawPayloadBytes+1),
	}
	if _, err := s.Apply(context.Background(), AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{in}}); err == nil {
		t.Fatal("expected oversized payload to be rejected")
	}
	var observations, payloads int64
	db.Model(&model.AnalyticsObservation{}).Count(&observations)
	db.Model(&model.AnalyticsRawPayload{}).Count(&payloads)
	if observations != 0 || payloads != 0 {
		t.Fatalf("oversized payload partially committed: observations=%d payloads=%d", observations, payloads)
	}
}
func TestAnalyticsBoundariesAndSparseCumulative(t *testing.T) {
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
	if _, e = s.Overview(context.Background(), "u", "p", q); !errors.Is(e, ErrAnalyticsInvalidQuery) {
		t.Fatalf("daily query accepted: %v", e)
	}
}

func TestAnalyticsRejectsDailyObservationWrites(t *testing.T) {
	s, _ := analyticsFixture(t)
	count := int64(10)
	at := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	in := AnalyticsObservationInput{
		Content: model.AnalyticsContent{ID: "a", ProjectID: "p", Platform: "seednote"},
		Observation: model.AnalyticsObservation{
			ID: "daily", ProjectID: "p", ContentID: "a", StatDate: "2026-02-01",
			MetricBasis: "daily", Source: "import", EffectiveAt: at, ReceivedAt: at,
			AnalyticsMetrics: model.AnalyticsMetrics{ViewCount: &count},
		},
	}
	if _, err := s.Apply(context.Background(), AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{in}}); err == nil {
		t.Fatal("daily observation was accepted")
	}
}

func TestAnalyticsRebuildGateAndIdempotency(t *testing.T) {
	s, db := analyticsFixture(t)
	ctx := context.Background()
	q := AnalyticsQuery{From: "2026-01-01", To: "2026-01-31", MetricBasis: "cumulative"}
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
