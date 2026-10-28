package router

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/handler"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/rs/zerolog"
)

func TestTaskShareRoutesPublicAndOwnerAuthentication(t *testing.T) {
	logger := zerolog.New(io.Discard)
	repo := repository.New(nil)
	app := NewRouter(&Services{Config: &config.Config{}, Logger: &logger, Repo: repo, TaskShareHandler: handler.NewTaskShareHandler(service.NewTaskShareService(repo, nil))})
	for _, route := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/v1/public/shares/unknown", 404},
		{"GET", "/api/v1/public/shares/unknown/images/file", 404},
		{"GET", "/api/v1/tasks/task/share", 401},
		{"PUT", "/api/v1/tasks/task/share", 401},
		{"DELETE", "/api/v1/tasks/task/share", 401},
	} {
		resp, err := app.Test(httptest.NewRequest(route.method, route.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != route.status || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s %s: %d, %v", route.method, route.path, resp.StatusCode, resp.Header)
		}
	}
}
