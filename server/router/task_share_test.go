package router

import (
	"bytes"
	"io"
	"net/http/httptest"
	"strings"
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

func TestTaskShareEncodedRouteLogsNeverExposeTokens(t *testing.T) {
	for _, path := range []string{
		"/api/v1/public/%73hares/secret-token",
		"/api/v1/public/shares%2fsecret-token",
		"/api/v1/public%2f%73hares%2fsecret-token/images/image-id",
	} {
		t.Run(path, func(t *testing.T) {
			var logs bytes.Buffer
			logger := zerolog.New(&logs)
			repo := repository.New(nil)
			app := NewRouter(&Services{Config: &config.Config{}, Logger: &logger, Repo: repo, TaskShareHandler: handler.NewTaskShareHandler(service.NewTaskShareService(repo, nil))})
			resp, err := app.Test(httptest.NewRequest("GET", path, nil))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if strings.Contains(logs.String(), "secret-token") || !strings.Contains(logs.String(), "/api/v1/public/shares/[redacted]") {
				t.Fatalf("encoded request logged a credential: %s", logs.String())
			}
		})
	}
}

func TestTaskShareOwnerCORSPreflightAllowsClientHeaders(t *testing.T) {
	logger := zerolog.New(io.Discard)
	repo := repository.New(nil)
	app := NewRouter(&Services{Config: &config.Config{}, Logger: &logger, Repo: repo, TaskShareHandler: handler.NewTaskShareHandler(service.NewTaskShareService(repo, nil))})
	request := httptest.NewRequest("OPTIONS", "/api/v1/tasks/task/share", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	request.Header.Set("Access-Control-Request-Method", "GET")
	request.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 || response.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("owner preflight failed: %d %v", response.StatusCode, response.Header)
	}
	allowed := "," + strings.ToLower(strings.ReplaceAll(response.Header.Get("Access-Control-Allow-Headers"), " ", "")) + ","
	for _, header := range []string{"authorization", "content-type"} {
		if !strings.Contains(allowed, ","+header+",") {
			t.Fatalf("owner request header %s is not allowed: %s", header, allowed)
		}
	}
}
