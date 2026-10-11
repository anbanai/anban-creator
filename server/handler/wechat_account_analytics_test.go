package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type wechatAccountAnalyticsHandlerStub struct {
	overview      *service.WechatAccountOverview
	overviewErr   error
	traffic       *service.WechatTrafficSourcesResponse
	trafficErr    error
	userID        string
	projectID     string
	trafficMetric string
}

func (s *wechatAccountAnalyticsHandlerStub) Overview(_ context.Context, userID, projectID, _, _ string) (*service.WechatAccountOverview, error) {
	s.userID, s.projectID = userID, projectID
	return s.overview, s.overviewErr
}

func (s *wechatAccountAnalyticsHandlerStub) TrafficSources(_ context.Context, userID, projectID, _, _, metric, _ string) (*service.WechatTrafficSourcesResponse, error) {
	s.userID, s.projectID, s.trafficMetric = userID, projectID, metric
	return s.traffic, s.trafficErr
}

func TestWechatAccountAnalyticsHandlerOverview(t *testing.T) {
	stub := &wechatAccountAnalyticsHandlerStub{overview: &service.WechatAccountOverview{Revision: 4, From: "2026-10-08", To: "2026-10-08", Totals: map[string]*int64{"read_users": ptrHandlerInt64(12)}}}
	h := NewWechatAccountAnalyticsHandler(stub, nil)
	app := fiber.New()
	app.Get("/projects/:id/wechat-analytics/overview", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Overview(c)
	})
	projectID := uuid.NewString()
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/projects/"+projectID+"/wechat-analytics/overview?from=2026-10-08&to=2026-10-08", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || stub.userID != "user-1" || stub.projectID != projectID {
		t.Fatalf("status=%d user=%q project=%q", resp.StatusCode, stub.userID, stub.projectID)
	}
	var envelope struct {
		Data service.WechatAccountOverview `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Revision != 4 || envelope.Data.Totals["read_users"] == nil || *envelope.Data.Totals["read_users"] != 12 {
		t.Fatalf("response = %#v", envelope.Data)
	}
}

func TestWechatAccountAnalyticsHandlerMapsErrorsAndDefaults(t *testing.T) {
	projectID := uuid.NewString()
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "forbidden", err: service.ErrAnalyticsForbidden, status: http.StatusForbidden},
		{name: "invalid query", err: service.ErrAnalyticsInvalidQuery, status: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &wechatAccountAnalyticsHandlerStub{overviewErr: test.err}
			h := NewWechatAccountAnalyticsHandler(stub, nil)
			app := fiber.New()
			app.Get("/projects/:id/wechat-analytics/overview", func(c fiber.Ctx) error {
				c.Locals("user_id", "user-1")
				return h.Overview(c)
			})
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/projects/"+projectID+"/wechat-analytics/overview?from=2026-10-08&to=2026-10-08", nil))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != test.status {
				t.Fatalf("status=%d, want %d", resp.StatusCode, test.status)
			}
		})
	}

	stub := &wechatAccountAnalyticsHandlerStub{traffic: &service.WechatTrafficSourcesResponse{Metric: "read_users"}}
	h := NewWechatAccountAnalyticsHandler(stub, nil)
	app := fiber.New()
	app.Get("/projects/:id/wechat-analytics/traffic-sources", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.TrafficSources(c)
	})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/projects/"+projectID+"/wechat-analytics/traffic-sources?from=2026-10-08&to=2026-10-08&metric=read_users", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || stub.trafficMetric != "read_users" {
		t.Fatalf("traffic status=%d metric=%q", resp.StatusCode, stub.trafficMetric)
	}
}

func ptrHandlerInt64(value int64) *int64 { return &value }
