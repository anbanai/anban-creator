package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

func TestAnalyticsMixedNeutralProjectRequiresAndScopesPlatform(t *testing.T) {
	s, db := analyticsFixture(t)
	ctx := context.Background()
	if err := db.Model(&model.Project{}).Where("id = ?", "p").Update("platform", "").Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		id, channel, date string
		count             int64
	}{
		{"wechat", model.ChannelArticle, "2026-02-01", 3},
		{"seednote", model.ChannelSeednote, "2026-02-02", 7},
		{"picture", model.ChannelWechatPicture, "2026-02-01", 5},
	} {
		at, _ := time.Parse("2006-01-02", row.date)
		_, err := s.Apply(ctx, AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{{
			Content:     model.AnalyticsContent{ID: row.id, ProjectID: "p", Channel: row.channel, ContentType: row.channel},
			Observation: model.AnalyticsObservation{ID: row.id, ContentID: row.id, ProjectID: "p", StatDate: row.date, MetricBasis: "cumulative", Source: "import", EffectiveAt: at, ReceivedAt: at, AnalyticsMetrics: model.AnalyticsMetrics{CommentCount: &row.count}},
		}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	q := AnalyticsQuery{From: "2026-02-01", To: "2026-02-28", MetricBasis: "cumulative"}
	if _, err := s.Overview(ctx, "u", "p", q); !errors.Is(err, ErrAnalyticsInvalidQuery) {
		t.Fatalf("missing platform: %v", err)
	}
	for _, tc := range []struct {
		platform, id, other, date string
		total, coverage           int64
	}{
		{"wechat", "wechat", "seednote", "2026-02-01", 8, 2},
		{"seednote", "seednote", "wechat", "2026-02-02", 7, 1},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			q.Platform = tc.platform
			for _, gran := range []string{"day", "week", "month"} {
				q.Granularity = gran
				overview, err := s.Overview(ctx, "u", "p", q)
				if err != nil || overview.Totals["comment_count"] != tc.total || overview.Coverage.Contents != tc.coverage || len(overview.Series) != 1 || overview.Series[0]["comment_count"] != tc.total {
					t.Fatalf("overview %s: %+v %v", gran, overview, err)
				}
			}
			page, err := s.Contents(ctx, "u", "p", q)
			if err != nil || page.Total != tc.coverage {
				t.Fatalf("contents: %+v %v", page, err)
			}
			dates, err := s.Dates(ctx, "u", "p", 2026, q)
			if err != nil || len(dates.Dates) != 1 || dates.Dates[0] != tc.date {
				t.Fatalf("dates: %+v %v", dates, err)
			}
			if _, err := s.Detail(ctx, "u", "p", tc.id, q); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Observations(ctx, "u", "p", tc.id, q); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Detail(ctx, "u", "p", tc.other, q); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("cross-platform detail: %v", err)
			}
			if _, err := s.Observations(ctx, "u", "p", tc.other, q); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("cross-platform observations: %v", err)
			}
		})
	}
	if err := db.Model(&model.Project{}).Where("id = ?", "p").Update("platform", "wechat").Error; err != nil {
		t.Fatal(err)
	}
	q.Platform = "seednote"
	if got, err := s.Overview(ctx, "u", "p", q); err != nil || got.Totals["comment_count"] != int64(7) {
		t.Fatalf("explicit selector must override legacy project: %+v %v", got, err)
	}
	q.Platform = "unknown"
	if _, err := s.Overview(ctx, "u", "p", q); !errors.Is(err, ErrAnalyticsInvalidQuery) {
		t.Fatalf("invalid platform: %v", err)
	}
}

func TestAnalyticsOverviewScopesWechatAgentContentType(t *testing.T) {
	s, _ := analyticsFixture(t)
	ctx := context.Background()
	for _, row := range []struct {
		id, channel string
		count       int64
	}{
		{"article", model.ChannelArticle, 3},
		{"picture", model.ChannelWechatPicture, 5},
	} {
		at, _ := time.Parse("2006-01-02", "2026-02-01")
		_, err := s.Apply(ctx, AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{{
			Content:     model.AnalyticsContent{ID: row.id, ProjectID: "p", Channel: row.channel, ContentType: row.channel},
			Observation: model.AnalyticsObservation{ID: row.id, ContentID: row.id, ProjectID: "p", StatDate: "2026-02-01", MetricBasis: "cumulative", Source: "import", EffectiveAt: at, ReceivedAt: at, AnalyticsMetrics: model.AnalyticsMetrics{CommentCount: &row.count}},
		}}})
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		contentType, contentID string
		count                  int64
	}{
		{"wechat-article", "article", 3},
		{"wechat-picture", "picture", 5},
	} {
		t.Run(tc.contentType, func(t *testing.T) {
			q := AnalyticsQuery{Platform: "wechat", ContentType: tc.contentType, From: "2026-02-01", To: "2026-02-28", Granularity: "month", MetricBasis: "cumulative"}
			overview, err := s.Overview(ctx, "u", "p", q)
			if err != nil || overview.Totals["comment_count"] != tc.count || overview.Coverage.Contents != 1 || len(overview.Series) != 1 || overview.Series[0]["comment_count"] != tc.count {
				t.Fatalf("overview: %+v %v", overview, err)
			}
			page, err := s.Contents(ctx, "u", "p", q)
			if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != tc.contentID {
				t.Fatalf("contents: %+v %v", page, err)
			}
		})
	}
}

func TestAnalyticsDetailUsesCanonicalContentDespiteConflictingAgentFilter(t *testing.T) {
	s, _ := analyticsFixture(t)
	ctx := context.Background()
	for _, row := range []struct {
		id, channel string
		count       int64
	}{
		{"article", model.ChannelArticle, 3},
		{"picture", model.ChannelWechatPicture, 5},
	} {
		at, _ := time.Parse("2006-01-02", "2026-02-01")
		_, err := s.Apply(ctx, AnalyticsWriteRequest{ProjectID: "p", Observations: []AnalyticsObservationInput{{
			Content:     model.AnalyticsContent{ID: row.id, ProjectID: "p", Channel: row.channel, ContentType: row.channel},
			Observation: model.AnalyticsObservation{ID: "detail-" + row.id, ContentID: row.id, ProjectID: "p", StatDate: "2026-02-01", MetricBasis: "cumulative", Source: "import", EffectiveAt: at, ReceivedAt: at, AnalyticsMetrics: model.AnalyticsMetrics{CommentCount: &row.count}},
		}}})
		if err != nil {
			t.Fatal(err)
		}
	}

	detail, err := s.Detail(ctx, "u", "p", "picture", AnalyticsQuery{
		Platform: "wechat", ContentType: model.TaskTypeWechatArticle,
		From: "2026-02-01", To: "2026-02-28", Granularity: "month", MetricBasis: "cumulative",
	})
	if err != nil || detail.Content.ContentType != model.TaskTypeWechatPicture || detail.Totals["comment_count"] != int64(5) {
		t.Fatalf("conflicting detail = %+v, error %v", detail, err)
	}
}

func TestSeednoteImportAcceptsNeutralProjectAndIsolatesCandidates(t *testing.T) {
	ctx := context.Background()
	svc, repo, req, taskID := seednoteSelectionFixture(t)
	project, err := repo.Projects().FindByID(ctx, req.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	project.Platform = ""
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: "wechat-other", ProjectID: req.ProjectID, UserID: req.UserID, Channel: model.ChannelArticle, Type: model.TaskTypeWechatArticle, Title: "早起效率翻倍的方法"}); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(ctx, req)
	if err != nil || preview.Rows[0].Target == nil || preview.Rows[0].Target.ID != taskID {
		t.Fatalf("neutral preview: %+v %v", preview, err)
	}
	req.Selections = []AnalyticsSelection{{SourceRow: 2, Target: AnalyticsTarget{Kind: "task", ID: "wechat-other"}}}
	if _, err := svc.Import(ctx, req); err == nil {
		t.Fatal("cross-channel manual selection accepted")
	}
	req.Selections = []AnalyticsSelection{{SourceRow: 2, Target: AnalyticsTarget{Kind: "task", ID: taskID}}}
	if _, err := svc.Import(ctx, req); err != nil {
		t.Fatal(err)
	}
	candidates := NewContentAnalyticsService(repo)
	for _, tc := range []struct{ platform, id string }{{"wechat", "wechat-other"}, {"seednote", taskID}} {
		items, total, err := candidates.Candidates(ctx, req.UserID, req.ProjectID, tc.platform, "", 0, 100)
		if err != nil || total != 1 || len(items) != 1 {
			t.Fatalf("%s candidates: %+v %d %v", tc.platform, items, total, err)
		}
		if tc.platform == "wechat" && items[0].Target.ID != tc.id {
			t.Fatalf("wrong candidate: %+v", items)
		}
	}
	if _, _, err := candidates.Candidates(ctx, req.UserID, req.ProjectID, "", "", 0, 25); !errors.Is(err, ErrAnalyticsInvalidQuery) {
		t.Fatalf("unspecified candidates: %v", err)
	}
	if err := svc.checkProject(ctx, "foreign", req.ProjectID); err == nil {
		t.Fatal("foreign user accepted")
	}
}
