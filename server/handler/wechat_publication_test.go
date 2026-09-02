package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type fakeWechatPublicationService struct {
	publication                       *model.WechatPublication
	err                               error
	method, userID, taskID, articleID string
}

func (f *fakeWechatPublicationService) Get(_ context.Context, userID, taskID string) (*model.WechatPublication, error) {
	f.method, f.userID, f.taskID = "get", userID, taskID
	return f.publication, f.err
}
func (f *fakeWechatPublicationService) Publish(_ context.Context, userID, taskID string) (*model.WechatPublication, error) {
	f.method, f.userID, f.taskID = "publish", userID, taskID
	return f.publication, f.err
}
func (f *fakeWechatPublicationService) RetryPublish(_ context.Context, userID, taskID string) (*model.WechatPublication, error) {
	f.method, f.userID, f.taskID = "retry-publish", userID, taskID
	return f.publication, f.err
}
func (f *fakeWechatPublicationService) Reconcile(_ context.Context, userID, taskID string) error {
	f.method, f.userID, f.taskID = "reconcile", userID, taskID
	return f.err
}
func (f *fakeWechatPublicationService) Select(_ context.Context, userID, taskID, articleID string) (*model.WechatPublication, error) {
	f.method, f.userID, f.taskID, f.articleID = "select", userID, taskID, articleID
	return f.publication, f.err
}

func publicationHandlerApp(fake WechatPublicationActions) *fiber.App {
	logger := zerolog.New(io.Discard)
	h := NewWechatPublicationHandler(fake, &logger)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-User-ID")); return c.Next() })
	app.Get("/tasks/:id/wechat-publication", h.Get)
	app.Post("/tasks/:id/wechat-publication/publish", h.Publish)
	app.Post("/tasks/:id/wechat-publication/retry-publish", h.RetryPublish)
	app.Post("/tasks/:id/wechat-publication/reconcile", h.Reconcile)
	app.Post("/tasks/:id/wechat-publication/select", h.Select)
	return app
}

func TestWechatPublicationHandlerActionsAreThinAndArticleIDOnly(t *testing.T) {
	taskID, userID := uuid.NewString(), uuid.NewString()
	for _, tc := range []struct{ name, method, path, body, called, articleID string }{
		{"get", http.MethodGet, "/tasks/" + taskID + "/wechat-publication", "", "get", ""},
		{"publish", http.MethodPost, "/tasks/" + taskID + "/wechat-publication/publish", "", "publish", ""},
		{"retry publish", http.MethodPost, "/tasks/" + taskID + "/wechat-publication/retry-publish", "", "retry-publish", ""},
		{"reconcile", http.MethodPost, "/tasks/" + taskID + "/wechat-publication/reconcile", "", "reconcile", ""},
		{"select", http.MethodPost, "/tasks/" + taskID + "/wechat-publication/select", `{"article_id":"article-1"}`, "select", "article-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeWechatPublicationService{publication: &model.WechatPublication{TaskID: taskID, Status: model.WechatPublicationStatusDrafted}}
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-User-ID", userID)
			resp, err := publicationHandlerApp(fake).Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK || fake.method != tc.called || fake.userID != userID || fake.taskID != taskID || fake.articleID != tc.articleID {
				t.Fatalf("status=%d call=%#v", resp.StatusCode, fake)
			}
		})
	}

	fake := &fakeWechatPublicationService{}
	req := httptest.NewRequest(http.MethodPost, "/tasks/"+taskID+"/wechat-publication/select", strings.NewReader(`{"article_url":"https://mp.weixin.qq.com/s/not-accepted"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	resp, err := publicationHandlerApp(fake).Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest || fake.method != "" {
		t.Fatalf("URL-only selection status=%d call=%q", resp.StatusCode, fake.method)
	}
}

func TestWechatPublicationHandlerMapsOwnershipStateAndRateLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"not found", service.ErrWechatPublicationNotFound, http.StatusNotFound},
		{"ownership", service.ErrWechatPublicationForbidden, http.StatusForbidden},
		{"state conflict", service.ErrWechatPublicationConflict, http.StatusConflict},
		{"mode conflict", service.ErrWechatPublicationModeConflict, http.StatusConflict},
		{"pending", service.ErrWechatPublicationPending, http.StatusConflict},
		{"rate limit", service.ErrWechatPublicationRateLimited, http.StatusTooManyRequests},
		{"article absent", service.ErrWechatPublicationArticleNotFound, http.StatusBadRequest},
		{"scheduler unavailable", service.ErrWechatPublicationSchedulerUnavailable, http.StatusServiceUnavailable},
		{"provider", errors.New("provider down"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeWechatPublicationService{err: tc.err}
			req := httptest.NewRequest(http.MethodPost, "/tasks/"+uuid.NewString()+"/wechat-publication/publish", nil)
			req.Header.Set("X-User-ID", uuid.NewString())
			resp, err := publicationHandlerApp(fake).Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				var body Response
				_ = json.NewDecoder(resp.Body).Decode(&body)
				t.Fatalf("status=%d want=%d body=%#v", resp.StatusCode, tc.want, body)
			}
		})
	}
}

func TestWechatPublicationHandlerDoesNotExposeWrappedLifecycleDetails(t *testing.T) {
	const internalDetail = "WeChat publication version changed"
	fake := &fakeWechatPublicationService{err: fmt.Errorf("%s: %w", internalDetail, service.ErrWechatPublicationConflict)}
	req := httptest.NewRequest(http.MethodPost, "/tasks/"+uuid.NewString()+"/wechat-publication/publish", nil)
	req.Header.Set("X-User-ID", uuid.NewString())
	resp, err := publicationHandlerApp(fake).Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d body=%#v", resp.StatusCode, body)
	}
	if body.Msg != service.ErrWechatPublicationConflict.Error() || strings.Contains(body.Msg, internalDetail) {
		t.Fatalf("handler exposed wrapped lifecycle detail: %#v", body)
	}
}
