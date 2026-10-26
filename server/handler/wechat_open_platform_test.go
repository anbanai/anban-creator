package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

type fakeWechatOpenPlatformHandlerService struct {
	userID       string
	projectID    string
	state        string
	code         string
	startErr     error
	callbackErr  error
	urlSignature bool
}

func (f *fakeWechatOpenPlatformHandlerService) StartAuthorization(_ context.Context, userID, projectID string) (*service.WechatAuthorizationStart, error) {
	f.userID, f.projectID = userID, projectID
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &service.WechatAuthorizationStart{AuthorizationURL: "https://mp.weixin.qq.com/authorize", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (*fakeWechatOpenPlatformHandlerService) Disconnect(context.Context, string, string) error {
	return nil
}

func (f *fakeWechatOpenPlatformHandlerService) CompleteAuthorization(_ context.Context, state, code string) (string, error) {
	f.state, f.code = state, code
	if f.callbackErr != nil {
		return "", f.callbackErr
	}
	return "project-1", nil
}

func (*fakeWechatOpenPlatformHandlerService) AuthorizationReturnURL(projectID, outcome string) string {
	return "https://creator.example.com/projects?project_id=" + projectID + "&wechat_authorization=" + outcome
}

func (f *fakeWechatOpenPlatformHandlerService) VerifyCallbackURL(string, string, string) bool {
	return f.urlSignature
}

func (*fakeWechatOpenPlatformHandlerService) HandleEvent(context.Context, string, string, string, string, []byte) error {
	return nil
}

func TestWechatOpenPlatformStartAuthorizationRequiresAuthenticatedProjectOwner(t *testing.T) {
	service := &fakeWechatOpenPlatformHandlerService{}
	handler := NewWechatOpenPlatformHandler(service, nil)
	app := fiber.New()
	app.Post("/projects/:id/wechat/authorization", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return handler.StartAuthorization(c)
	})
	response, err := app.Test(httptest.NewRequest(http.MethodPost, "/projects/project-1/wechat/authorization", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != fiber.StatusOK || service.userID != "user-1" || service.projectID != "project-1" || !strings.Contains(string(body), "https://mp.weixin.qq.com/authorize") {
		t.Fatalf("status=%d service=%#v body=%s", response.StatusCode, service, string(body))
	}
}

func TestWechatOpenPlatformCallbackConsumesStateAndRedirectsToProject(t *testing.T) {
	service := &fakeWechatOpenPlatformHandlerService{}
	handler := NewWechatOpenPlatformHandler(service, nil)
	app := fiber.New()
	app.Get("/authorization/callback", handler.AuthorizationCallback)
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/authorization/callback?state=opaque&auth_code=one-time-code", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusSeeOther || service.state != "opaque" || service.code != "one-time-code" || response.Header.Get(fiber.HeaderLocation) != "https://creator.example.com/projects?project_id=project-1&wechat_authorization=connected" {
		t.Fatalf("status=%d state=%q code=%q location=%q", response.StatusCode, service.state, service.code, response.Header.Get(fiber.HeaderLocation))
	}
}

func TestWechatOpenPlatformCallbackDoesNotExposeAuthorizationErrors(t *testing.T) {
	service := &fakeWechatOpenPlatformHandlerService{callbackErr: errors.New("provider token secret")}
	handler := NewWechatOpenPlatformHandler(service, nil)
	app := fiber.New()
	app.Get("/authorization/callback", handler.AuthorizationCallback)
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/authorization/callback?state=opaque&auth_code=bad", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusSeeOther || response.Header.Get(fiber.HeaderLocation) != "https://creator.example.com/projects?project_id=&wechat_authorization=failed" {
		t.Fatalf("status=%d location=%q", response.StatusCode, response.Header.Get(fiber.HeaderLocation))
	}
}
