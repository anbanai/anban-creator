package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/handler"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/rs/zerolog"
)

func TestDesignerRoutesAreRegisteredBehindAuthentication(t *testing.T) {
	logger := zerolog.New(io.Discard)
	app := NewRouter(&Services{Config: &config.Config{}, Logger: &logger, Repo: repository.New(nil), DesignerHandler: handler.NewDesignerHandler(nil, &logger)})
	want := map[string]bool{
		"POST /api/v1/designer/quote":                     false,
		"POST /api/v1/designer/generate":                  false,
		"POST /api/v1/designer/register-reference":        false,
		"POST /api/v1/designer/upload-reference":          false,
		"POST /api/v1/designer/upload-reference-from-url": false,
		"GET /api/v1/designer/history":                    false,
		"GET /api/v1/designer/generations/:id":            false,
	}
	for _, route := range app.GetRoutes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("missing %s", route)
		}
	}
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/designer/history", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", resp.StatusCode)
	}
}
