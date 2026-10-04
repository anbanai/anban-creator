package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAnalyticsHTTPScopesNeutralProjectToRequestedPlatform(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.AnalyticsState{}, &model.AnalyticsContent{}, &model.AnalyticsObservation{}, &model.AnalyticsRawPayload{}, &model.AnalyticsBucket{}); err != nil {
		t.Fatal(err)
	}
	projectID := uuid.NewString()
	if err := db.Create(&model.Project{ID: projectID, UserID: "user"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.NewAnalyticsService(repository.New(db))
	at := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		channel string
		count   int64
	}{{model.ChannelArticle, 3}, {model.ChannelSeednote, 7}} {
		_, err := svc.Apply(context.Background(), service.AnalyticsWriteRequest{ProjectID: projectID, Observations: []service.AnalyticsObservationInput{{Content: model.AnalyticsContent{ID: row.channel, ProjectID: projectID, Channel: row.channel}, Observation: model.AnalyticsObservation{ID: row.channel, ContentID: row.channel, ProjectID: projectID, MetricBasis: "cumulative", StatDate: "2026-02-01", Source: "import", EffectiveAt: at, ReceivedAt: at, AnalyticsMetrics: model.AnalyticsMetrics{CommentCount: &row.count}}}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	app := fiber.New()
	h := NewAnalyticsHandler(svc)
	app.Get("/projects/:id/overview", func(c fiber.Ctx) error { c.Locals("user_id", "user"); return h.Overview(c) })
	for _, tc := range []struct {
		platform string
		status   int
		count    float64
	}{{"wechat", 200, 3}, {"seednote", 200, 7}, {"", 400, 0}, {"bogus", 400, 0}} {
		res, err := app.Test(httptest.NewRequest("GET", "/projects/"+projectID+"/overview?platform="+tc.platform+"&from=2026-02-01&to=2026-02-28&metric_basis=cumulative", nil))
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Data struct {
				Totals map[string]any `json:"totals"`
			} `json:"data"`
		}
		err = json.NewDecoder(res.Body).Decode(&result)
		res.Body.Close()
		if err != nil || res.StatusCode != tc.status || (tc.status == 200 && result.Data.Totals["comment_count"] != tc.count) {
			t.Fatalf("platform %q: status=%d totals=%v err=%v", tc.platform, res.StatusCode, result.Data.Totals, err)
		}
	}
}
