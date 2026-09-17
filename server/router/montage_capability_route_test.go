package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMontageCapabilityRoute(t *testing.T) {
	app, cleanup := setupTestApp(t, true)
	defer cleanup()

	for _, route := range app.GetRoutes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/montage-capabilities" {
			return
		}
	}
	t.Fatal("GET /api/v1/montage-capabilities is not registered")
}

func TestMontageCapabilityRouteRequiresAuthentication(t *testing.T) {
	app, cleanup := setupTestApp(t, true)
	defer cleanup()

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/montage-capabilities", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}
