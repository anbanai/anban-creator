package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTopicRadarRoutesAreRegistered(t *testing.T) {
	app, cleanup := setupTestApp(t, true)
	defer cleanup()

	want := map[string]bool{
		"GET /api/v1/projects/:project_id/topic-radar/subscriptions":         false,
		"POST /api/v1/projects/:project_id/topic-radar/subscriptions":        false,
		"GET /api/v1/projects/:project_id/topic-radar/notification-channels": false,
		"POST /api/v1/topic-radar/subscriptions/:subscription_id/run":        false,
		"GET /api/v1/topic-radar/digests/:digest_id":                         false,
		"GET /api/v1/topic-radar/digests/:digest_id/suggestions":             false,
		"POST /api/v1/topic-radar/suggestions/:suggestion_id/script":         false,
		"GET /api/v1/topic-radar/scripts/:script_id":                         false,
		"POST /api/v1/topic-radar/notification-channels/:channel_id/test":    false,
	}
	for _, route := range app.GetRoutes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("%s is not registered", route)
		}
	}
}

func TestTopicRadarRoutesRequireAuthentication(t *testing.T) {
	app, cleanup := setupTestApp(t, true)
	defer cleanup()

	for _, path := range []string{
		"/api/v1/projects/project-1/topic-radar/subscriptions",
		"/api/v1/projects/project-1/topic-radar/notification-channels",
		"/api/v1/topic-radar/digests/digest-1",
	} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d", path, resp.StatusCode, http.StatusUnauthorized)
		}
	}
}
