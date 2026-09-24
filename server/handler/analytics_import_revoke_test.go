package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type revokeSeednoteAPI struct {
	SeednoteImportAPI
	call func(string, string, string) error
}

func (f revokeSeednoteAPI) Revoke(_ context.Context, user, project, batch string) (*service.SeednoteImportSummary, error) {
	return nil, f.call(user, project, batch)
}

type revokeWechatAPI struct {
	WechatAnalyticsImportAPI
	call func(string, string, string) error
}

func (f revokeWechatAPI) Revoke(_ context.Context, user, project, batch string) (*service.WechatAnalyticsImportSummary, error) {
	return nil, f.call(user, project, batch)
}

func TestAnalyticsImportRevocationHTTP(t *testing.T) {
	for _, platform := range []string{"seednote", "wechat"} {
		for _, tc := range []struct {
			name                                    string
			anonymous, invalidProject, invalidBatch bool
			serviceErr                              error
			status                                  int
		}{
			{name: "success", status: http.StatusOK},
			{name: "anonymous", anonymous: true, status: http.StatusUnauthorized},
			{name: "invalid project", invalidProject: true, status: http.StatusBadRequest},
			{name: "invalid batch", invalidBatch: true, status: http.StatusBadRequest},
			{name: "not owned", serviceErr: errors.New("batch does not belong to user"), status: http.StatusForbidden},
			{name: "not found", serviceErr: gorm.ErrRecordNotFound, status: http.StatusNotFound},
		} {
			t.Run(platform+"/"+tc.name, func(t *testing.T) {
				user, project, batch := uuid.NewString(), uuid.NewString(), uuid.NewString()
				called := false
				call := func(u, p, b string) error {
					called = true
					if u != user || p != project || b != batch {
						t.Fatalf("unexpected scope: %s %s %s", u, p, b)
					}
					return tc.serviceErr
				}
				var revoke fiber.Handler
				if platform == "seednote" {
					h := NewSeednoteImportHandler(revokeSeednoteAPI{call: call}, nil)
					revoke = h.Revoke
				} else {
					h := NewWechatAnalyticsImportHandler(revokeWechatAPI{call: call}, nil)
					revoke = h.Revoke
				}
				app := fiber.New()
				app.Use(func(c fiber.Ctx) error {
					if !tc.anonymous {
						c.Locals("user_id", user)
					}
					return c.Next()
				})
				app.Post("/projects/:id/imports/:batchId/revoke", revoke)
				if tc.invalidProject {
					project = "bad"
				}
				if tc.invalidBatch {
					batch = "bad"
				}
				req := httptest.NewRequest(http.MethodPost, "/projects/"+project+"/imports/"+batch+"/revoke", strings.NewReader(`{"actions":[]}`))
				req.Header.Set("Content-Type", "application/json")
				resp, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != tc.status {
					t.Fatalf("status=%d want=%d", resp.StatusCode, tc.status)
				}
				if (tc.anonymous || tc.invalidProject || tc.invalidBatch) && called {
					t.Fatal("invalid request reached service")
				}
			})
		}
	}
}
