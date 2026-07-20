package router

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/handler"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

// setupTestApp creates a minimal Fiber app with the health endpoint
// backed by an in-memory SQLite database.
func setupTestApp(t *testing.T, withDB bool) (*fiber.App, func()) {
	t.Helper()

	var db *gorm.DB
	var closeFunc func()

	if withDB {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("failed to open sqlite: %v", err)
		}
		if err := model.AutoMigrate(db); err != nil {
			t.Fatalf("failed to auto-migrate: %v", err)
		}
		closeFunc = func() {
			sqlDB, _ := db.DB()
			if sqlDB != nil {
				sqlDB.Close()
			}
		}
	} else {
		closeFunc = func() {}
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 0, Host: "0.0.0.0"},
	}

	var svcs *Services
	if withDB && db != nil {
		repo := repository.New(db)

		jwtSvc, err := auth.NewJWTService("test-secret-key-for-testing", "24h", "168h")
		if err != nil {
			t.Fatalf("failed to create JWT service: %v", err)
		}

		planSvc := service.NewPlanService(repo, &logger)
		agentExecutor := agent.NewLocalExecutor(&logger, nil, nil, "", false, "", nil, nil, "", "", nil, nil)
		taskSvc := service.NewTaskService(repo, agentExecutor, nil, nil, nil, &logger, "", nil, "", nil, nil)
		seednoteTrackingSvc := service.NewSeednoteTrackingService(repo, nil, nil, nil, &logger)

		wsHub := handler.NewWebSocketHub(jwtSvc)
		authHandler := handler.NewAuthHandler(jwtSvc, nil, nil, repo, nil, &logger, wsHub, false, 3, nil, nil, nil)
		planHandler := handler.NewPlanHandler(planSvc, &logger)
		taskHandler := handler.NewTaskHandler(taskSvc, &logger)
		seednoteAnalyticsHandler := handler.NewSeednoteAnalyticsHandler(seednoteTrackingSvc, &logger)
		timelineHandler := handler.NewTimelineHandler(repo, &logger)

		svcs = &Services{
			Config:                   cfg,
			Logger:                   &logger,
			DB:                       db,
			Repo:                     repo,
			JWTService:               jwtSvc,
			WSHub:                    wsHub,
			AuthHandler:              authHandler,
			PlanService:              planSvc,
			TaskService:              taskSvc,
			PlanHandler:              planHandler,
			TaskHandler:              taskHandler,
			SeednoteAnalyticsHandler: seednoteAnalyticsHandler,
			TimelineHandler:          timelineHandler,
		}
	} else {
		svcs = &Services{
			Config: cfg,
			Logger: &logger,
		}
	}

	app := NewRouter(svcs)
	return app, closeFunc
}

// TestHealthCheck tests that the health endpoint returns 200 with a valid database.
func TestHealthCheck(t *testing.T) {
	app, closeFunc := setupTestApp(t, true)
	defer closeFunc()

	req := httptest.NewRequest("GET", "/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("health check request failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestHealthCheckNoDeps tests that the health endpoint returns 200 when
// no database or Redis is configured (graceful degradation).
func TestHealthCheckNoDeps(t *testing.T) {
	app, closeFunc := setupTestApp(t, false)
	defer closeFunc()

	req := httptest.NewRequest("GET", "/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("health check request failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("expected status 200 with no deps, got %d", resp.StatusCode)
	}
}

// TestProtectedEndpointsRequireAuth tests that protected API endpoints
// return 401 without an Authorization header.
func TestProtectedEndpointsRequireAuth(t *testing.T) {
	app, closeFunc := setupTestApp(t, true)
	defer closeFunc()

	protectedPaths := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/auth/me"},
		{"GET", "/api/v1/plans"},
		{"POST", "/api/v1/tasks"},
		{"GET", "/api/v1/tasks/00000000-0000-0000-0000-000000000001/seednote-analytics"},
		{"GET", "/api/v1/timeline?from=2025-01-01&to=2025-12-31"},
		{"GET", "/api/v1/credits/balance"},
		{"GET", "/api/v1/credits/sign-in/status"},
		{"POST", "/api/v1/credits/sign-in"},
		{"GET", "/api/v1/credits/transactions"},
	}

	for _, tc := range protectedPaths {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}

			if resp.StatusCode != fiber.StatusUnauthorized {
				t.Errorf("expected 401 for %s %s, got %d", tc.method, tc.path, resp.StatusCode)
			}
		})
	}
}

func TestLegacyFileUploadRouteIsNotRegistered(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	cfg := &config.Config{
		Server: config.ServerConfig{Port: 0, Host: "0.0.0.0"},
	}
	app := NewRouter(&Services{
		Config:      cfg,
		Logger:      &logger,
		FileHandler: handler.NewFileHandler(nil, &logger),
	})

	for _, route := range app.GetRoutes() {
		if route.Method == "POST" && route.Path == "/api/v1/files/"+"upload" {
			t.Fatalf("legacy upload route is still registered: %+v", route)
		}
	}
}

func TestResolveAssetRouteIsRegistered(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	app := NewRouter(&Services{
		Config: &config.Config{Server: config.ServerConfig{Port: 0, Host: "0.0.0.0"}},
		Logger: &logger,
		UploadHandler: handler.NewUploadHandler(
			nil,
			nil,
			service.DirectUploadConfig{},
			&logger,
		),
	})

	for _, route := range app.GetRoutes() {
		if route.Method == "POST" && route.Path == "/api/v1/uploads/resolve-asset-url" {
			return
		}
	}
	t.Fatal("POST /api/v1/uploads/resolve-asset-url is not registered")
}

// TestPublicAuthEndpoints tests that public auth endpoints are accessible
// without authentication.
func TestPublicAuthEndpoints(t *testing.T) {
	app, closeFunc := setupTestApp(t, true)
	defer closeFunc()

	// POST /api/v1/auth/register without body should return 400, not 401.
	req := httptest.NewRequest("POST", "/api/v1/auth/register", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode == fiber.StatusUnauthorized {
		t.Errorf("public endpoint should not require auth, got 401")
	}
}
