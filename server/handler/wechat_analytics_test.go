package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type fakeWechatAnalyticsService struct {
	analytics *service.WechatAnalytics
	err       error
	userID    string
	taskID    string
}

func (f *fakeWechatAnalyticsService) GetTaskAnalytics(_ context.Context, userID, taskID string) (*service.WechatAnalytics, error) {
	f.userID, f.taskID = userID, taskID
	return f.analytics, f.err
}

func TestWechatAnalyticsHandlerGetTaskAnalytics(t *testing.T) {
	want := &service.WechatAnalytics{Trend: []*service.WechatMetricTrendItem{{StatDate: "2026-08-01", ReadUsers: 9}}}
	svc := &fakeWechatAnalyticsService{analytics: want}
	logger := zerolog.New(io.Discard)
	h := NewWechatAnalyticsHandler(svc, &logger)
	app := fiber.New()
	app.Get("/tasks/:id/wechat-analytics", func(c fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-User-ID"))
		return h.GetTaskAnalytics(c)
	})
	userID, taskID := uuid.NewString(), uuid.NewString()
	req := httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/wechat-analytics", nil)
	req.Header.Set("X-User-ID", userID)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK || svc.userID != userID || svc.taskID != taskID {
		t.Fatalf("status=%d args=user:%q task:%q", resp.StatusCode, svc.userID, svc.taskID)
	}
	var envelope struct {
		Data service.WechatAnalytics `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Trend) != 1 || envelope.Data.Trend[0].ReadUsers != 9 {
		t.Fatalf("analytics response = %#v", envelope.Data)
	}
}
