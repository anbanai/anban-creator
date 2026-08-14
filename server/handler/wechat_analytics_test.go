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

type fakeWechatAnalyticsService struct {
	analytics *service.WechatAnalytics
	err       error
	userID    string
	taskID    string
	url       string
}

func (f *fakeWechatAnalyticsService) GetTaskAnalytics(context.Context, string, string) (*service.WechatAnalytics, error) {
	return f.analytics, f.err
}

func (f *fakeWechatAnalyticsService) BindTask(_ context.Context, userID, taskID, articleURL string) error {
	f.userID, f.taskID, f.url = userID, taskID, articleURL
	return f.err
}

func setupWechatAnalyticsHandlerTest(svc WechatAnalyticsService) *fiber.App {
	logger := zerolog.New(io.Discard)
	h := NewWechatAnalyticsHandler(svc, &logger)
	app := fiber.New()
	app.Post("/tasks/:id/wechat-analytics/bind", func(c fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-User-ID"))
		return h.BindTask(c)
	})
	return app
}

func TestWechatAnalyticsHandler_BindTask(t *testing.T) {
	svc := &fakeWechatAnalyticsService{}
	app := setupWechatAnalyticsHandlerTest(svc)
	taskID, userID := uuid.NewString(), uuid.NewString()
	articleURL := "https://mp.weixin.qq.com/s/article"
	req := httptest.NewRequest(http.MethodPost, "/tasks/"+taskID+"/wechat-analytics/bind", strings.NewReader(`{"article_url":"`+articleURL+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if svc.userID != userID || svc.taskID != taskID || svc.url != articleURL {
		t.Fatalf("bind args = user:%q task:%q url:%q", svc.userID, svc.taskID, svc.url)
	}
}

func TestWechatAnalyticsHandler_BindErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid URL", err: service.ErrWechatArticleURLInvalid, want: fiber.StatusBadRequest},
		{name: "article not found", err: service.ErrWechatArticleNotFound, want: fiber.StatusBadRequest},
		{name: "upstream failure", err: errors.New("upstream timeout"), want: fiber.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupWechatAnalyticsHandlerTest(&fakeWechatAnalyticsService{err: tt.err})
			req := httptest.NewRequest(http.MethodPost, "/tasks/"+uuid.NewString()+"/wechat-analytics/bind", strings.NewReader(`{"article_url":"https://mp.weixin.qq.com/s/article"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-User-ID", uuid.NewString())
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}
