package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHypitCapabilityRoute(t *testing.T) {
	app, cleanup := setupTestApp(t, true)
	defer cleanup()

	for _, route := range app.GetRoutes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/hypit-capabilities" {
			return
		}
	}
	t.Fatal("GET /api/v1/hypit-capabilities is not registered")
}

func TestHypitCapabilityRouteRequiresAuthentication(t *testing.T) {
	app, cleanup := setupTestApp(t, true)
	defer cleanup()

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/hypit-capabilities", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}
