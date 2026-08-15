package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
)

type fakeChannelsAnalyticsService struct {
	analytics *service.ChannelsAnalytics
	err       error
	userID    string
	taskID    string
	videoURL  string
}

func (f *fakeChannelsAnalyticsService) GetTaskAnalytics(context.Context, string, string) (*service.ChannelsAnalytics, error) {
	return f.analytics, f.err
}

func (f *fakeChannelsAnalyticsService) BindTask(_ context.Context, userID, taskID, videoURL string) error {
	f.userID, f.taskID, f.videoURL = userID, taskID, videoURL
	return f.err
}

func setupChannelsAnalyticsHandlerTest(svc ChannelsAnalyticsService) *fiber.App {
	logger := zerolog.New(io.Discard)
	h := NewChannelsAnalyticsHandler(svc, &logger)
	app := fiber.New()
	app.Post("/tasks/:id/channels-analytics/bind", func(c fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-User-ID"))
		return h.BindTask(c)
	})
	return app
}

func TestChannelsAnalyticsHandlerBindTask(t *testing.T) {
	svc := &fakeChannelsAnalyticsService{}
	app := setupChannelsAnalyticsHandlerTest(svc)
	taskID, userID := uuid.NewString(), uuid.NewString()
	videoURL := "https://weixin.qq.com/sph/video"
	req := httptest.NewRequest(http.MethodPost, "/tasks/"+taskID+"/channels-analytics/bind", strings.NewReader(`{"video_url":"`+videoURL+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if svc.userID != userID || svc.taskID != taskID || svc.videoURL != videoURL {
		t.Fatalf("bind args = user:%q task:%q url:%q", svc.userID, svc.taskID, svc.videoURL)
	}
}

func TestChannelsAnalyticsHandlerErrorMapping(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{service.ErrChannelsVideoURLInvalid, fiber.StatusBadRequest},
		{service.ErrChannelsProviderDisabled, fiber.StatusServiceUnavailable},
		{errors.New("upstream timeout"), fiber.StatusBadGateway},
	}
	for _, tt := range tests {
		app := setupChannelsAnalyticsHandlerTest(&fakeChannelsAnalyticsService{err: tt.err})
		req := httptest.NewRequest(http.MethodPost, "/tasks/"+uuid.NewString()+"/channels-analytics/bind", strings.NewReader(`{"video_url":"https://weixin.qq.com/sph/video"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", uuid.NewString())
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != tt.want {
			t.Fatalf("error %v: status = %d, want %d", tt.err, resp.StatusCode, tt.want)
		}
	}
}
