package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

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
		taskSvc := service.NewTaskService(repo, nil, nil, &logger, "", nil, nil)
		seednoteTrackingSvc := service.NewSeednoteTrackingService(repo, nil, nil, &logger)
		wechatTrackingSvc := service.NewWechatTrackingService(repo, nil, nil, &logger)
		channelsTrackingSvc := service.NewChannelsTrackingService(repo, nil, nil, &logger)

		wsHub := handler.NewWebSocketHub(jwtSvc)
		authHandler := handler.NewAuthHandler(jwtSvc, nil, nil, repo, nil, &logger, wsHub, false, 3, nil)
		planHandler := handler.NewPlanHandler(planSvc, &logger)
		taskHandler := handler.NewTaskHandler(taskSvc, &logger)
		seednoteAnalyticsHandler := handler.NewSeednoteAnalyticsHandler(seednoteTrackingSvc, &logger)
		wechatAnalyticsHandler := handler.NewWechatAnalyticsHandler(wechatTrackingSvc, &logger)
		channelsAnalyticsHandler := handler.NewChannelsAnalyticsHandler(channelsTrackingSvc, &logger)
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
			WechatAnalyticsHandler:   wechatAnalyticsHandler,
			ChannelsAnalyticsHandler: channelsAnalyticsHandler,
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

func TestWechatAnalyticsURLBindRouteIsNotRegistered(t *testing.T) {
	app, cleanup := setupTestApp(t, true)
	defer cleanup()

	foundReadRoute := false
	for _, route := range app.GetRoutes() {
		key := route.Method + " " + route.Path
		if key == "POST /api/v1/tasks/:id/wechat-analytics/bind" {
			t.Fatalf("legacy WeChat analytics URL bind route is still registered: %s", key)
		}
		if key == "GET /api/v1/tasks/:id/wechat-analytics" {
			foundReadRoute = true
		}
	}
	if !foundReadRoute {
		t.Fatal("WeChat analytics read route is not registered")
	}
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
		{"GET", "/api/v1/tasks/00000000-0000-0000-0000-000000000001/channels-analytics"},
		{"POST", "/api/v1/tasks/00000000-0000-0000-0000-000000000001/wechat-publication/retry-publish"},
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

func TestWechatPublicationRoutesReplaceLegacyTaskPublicationRoutes(t *testing.T) {
	logger := zerolog.New(io.Discard)
	app := NewRouter(&Services{
		Config: &config.Config{}, Logger: &logger,
		WechatPublicationHandler: handler.NewWechatPublicationHandler(nil, &logger),
	})
	want := map[string]bool{
		"GET /api/v1/tasks/:id/wechat-publication":                false,
		"POST /api/v1/tasks/:id/wechat-publication/publish":       false,
		"POST /api/v1/tasks/:id/wechat-publication/retry-publish": false,
		"POST /api/v1/tasks/:id/wechat-publication/reconcile":     false,
		"POST /api/v1/tasks/:id/wechat-publication/select":        false,
	}
	for _, route := range app.GetRoutes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
		if key == "POST /api/v1/tasks/:id/publish-approve" || key == "POST /api/v1/tasks/:id/publish-reject" || key == "PATCH /api/v1/tasks/:id/published" {
			t.Fatalf("legacy publication route is still registered: %s", key)
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("missing route %s", route)
		}
	}
}

func TestRouterEnablesRequestBodyStreaming(t *testing.T) {
	app := NewRouter(&Services{Config: &config.Config{}})
	if !app.Config().StreamRequestBody {
		t.Fatal("StreamRequestBody must be enabled")
	}
}

func TestBillingRoutes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	t.Cleanup(func() { _ = repo.Close() })
	jwtSvc, err := auth.NewJWTService("billing-router-secret", "24h", "168h")
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	app := NewRouter(&Services{
		Config: &config.Config{Server: config.ServerConfig{Host: "0.0.0.0"}}, Logger: &logger,
		Repo: repo, JWTService: jwtSvc, BillingHandler: &handler.BillingHandler{},
	})
	want := map[string]string{
		"GET /api/v1/billing/wallet":       "",
		"GET /api/v1/billing/transactions": "",
		"POST /api/v1/billing/quotes":      "",
		"GET /api/v1/billing/referral":     "",
		"POST /api/admin/billing/topups":   "",
	}
	for _, route := range app.GetRoutes() {
		delete(want, route.Method+" "+route.Path)
	}
	if len(want) != 0 {
		t.Fatalf("billing routes missing: %#v", want)
	}
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/billing/wallet"},
		{http.MethodGet, "/api/v1/billing/transactions"},
		{http.MethodPost, "/api/v1/billing/quotes"},
		{http.MethodGet, "/api/v1/billing/referral"},
	} {
		resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", tc.method, tc.path, resp.StatusCode)
		}
	}
	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/admin/billing/topups", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("admin billing route status = %d, want 401", resp.StatusCode)
	}
}

func TestViralAnalysisRoutesAreHistoryReadOnly(t *testing.T) {
	logger := zerolog.New(io.Discard)
	app := NewRouter(&Services{
		Config: &config.Config{Server: config.ServerConfig{Host: "0.0.0.0"}}, Logger: &logger,
		ViralAnalysisHandler: handler.NewViralAnalysisHandler(nil, &logger),
	})
	want := map[string]bool{
		http.MethodGet + " /api/v1/viral-analyses/":    false,
		http.MethodGet + " /api/v1/viral-analyses/:id": false,
	}
	for _, route := range app.GetRoutes() {
		key := route.Method + " " + route.Path
		if route.Method == http.MethodPost && route.Path == "/api/v1/viral-analyses/" {
			t.Fatalf("legacy write route is still registered: %s", key)
		}
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("missing history route %s", route)
		}
	}
}

func TestAgentExecutionProfileRoute(t *testing.T) {
	app := NewRouter(&Services{
		Config:              &config.Config{},
		AgentProfileHandler: &handler.AgentProfileHandler{},
	})
	for _, route := range app.GetRoutes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/agent/execution-profiles" {
			return
		}
	}
	t.Fatal("GET /api/v1/agent/execution-profiles is not registered")
}

func TestAgentPackCatalogRoute(t *testing.T) {
	app := NewRouter(&Services{
		Config:           &config.Config{},
		AgentPackHandler: handler.NewAgentPackHandler(),
	})
	for _, route := range app.GetRoutes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/agent-packs" {
			return
		}
	}
	t.Fatal("GET /api/v1/agent-packs is not registered")
}

func TestSeednoteLoginRoutesAreAdminOnlySurface(t *testing.T) {
	logger := zerolog.New(io.Discard)
	app := NewRouter(&Services{
		Config:         &config.Config{},
		Logger:         &logger,
		ProjectHandler: handler.NewProjectHandler(nil, &logger),
	})
	want := map[string]bool{
		http.MethodGet + " /api/v1/seednote/account/login-status": false,
		http.MethodGet + " /api/v1/seednote/account/login-qrcode": false,
		http.MethodDelete + " /api/v1/seednote/account/login":     false,
	}
	for _, route := range app.GetRoutes() {
		key := route.Method + " " + route.Path
		if route.Path == "/api/v1/seednote/login-status" || strings.HasPrefix(route.Path, "/api/v1/admin/seednote/") {
			t.Fatalf("legacy user-facing Seednote login route remains registered: %s", key)
		}
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("missing Seednote admin route %s", route)
		}
	}
}

func TestSeednoteAdminRoutesUseJWTAndAdminAuthorization(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	t.Cleanup(func() { _ = repo.Close() })

	adminID, regularID := uuid.NewString(), uuid.NewString()
	for _, user := range []*model.User{
		{ID: adminID, Email: "admin@example.com", Tier: model.TierPro, InviteCode: "ADMIN123", IsAdmin: true},
		{ID: regularID, Email: "user@example.com", Tier: model.TierFree, InviteCode: "USER123"},
	} {
		if err := repo.Users().Create(t.Context(), user); err != nil {
			t.Fatal(err)
		}
	}
	jwtSvc, err := auth.NewJWTService("seednote-admin-router-secret", "24h", "168h")
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	app := NewRouter(&Services{
		Config:         &config.Config{Server: config.ServerConfig{Host: "0.0.0.0"}},
		Logger:         &logger,
		Repo:           repo,
		JWTService:     jwtSvc,
		ProjectHandler: handler.NewProjectHandler(nil, &logger),
	})

	adminToken, err := jwtSvc.GenerateAccessToken(adminID)
	if err != nil {
		t.Fatal(err)
	}
	regularToken, err := jwtSvc.GenerateAccessToken(regularID)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{name: "missing token", wantStatus: http.StatusUnauthorized},
		{name: "regular user", token: regularToken, wantStatus: http.StatusForbidden},
		{name: "administrator", token: adminToken, wantStatus: http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/seednote/account/login-status", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

func TestIlinkRoutesRemainJWTProtected(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	t.Cleanup(func() { _ = repo.Close() })
	jwtSvc, err := auth.NewJWTService("ilink-router-secret", "24h", "168h")
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	app := NewRouter(&Services{
		Config:       &config.Config{Server: config.ServerConfig{Host: "0.0.0.0"}},
		Logger:       &logger,
		Repo:         repo,
		JWTService:   jwtSvc,
		IlinkHandler: handler.NewIlinkHandler(nil, &logger),
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/ilink/status", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAgentExecutionProfileRouteAuthenticatesStudioUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	t.Cleanup(func() { _ = repo.Close() })

	userID := uuid.NewString()
	if err := repo.Users().Create(t.Context(), &model.User{ID: userID, Tier: model.TierPro}); err != nil {
		t.Fatal(err)
	}
	jwtSvc, err := auth.NewJWTService("agent-profile-router-secret", "24h", "168h")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := service.NewAgentProfileRegistry([]service.AgentExecutionProfile{
		routerTestAgentProfile("effective", "性价比", "deepseek", "deepseek-v4-flash", model.TierFree),
		routerTestAgentProfile("balanced", "平衡型", "volcengine_ark", "doubao-seed-evolving", model.TierPro),
		routerTestAgentProfile("quality", "极致效果", "moonshot", "kimi-k3", model.TierEnterprise),
	})
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	agentHandler := handler.NewAgentHandler(nil, nil, nil, "", &logger)
	executionTokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	agentHandler.SetExecutionTokenService(executionTokens)
	app := NewRouter(&Services{
		Config:              &config.Config{Server: config.ServerConfig{Host: "0.0.0.0"}},
		Logger:              &logger,
		Repo:                repo,
		JWTService:          jwtSvc,
		AgentHandler:        agentHandler,
		AgentProfileHandler: handler.NewAgentProfileHandler(repo, profiles, &logger),
	})

	token, err := jwtSvc.GenerateAccessToken(userID)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/execution-profiles", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authenticated profile request status=%d body=%s", resp.StatusCode, body)
	}
}

func routerTestAgentProfile(id, displayName, provider, modelName string, minTier model.Tier) service.AgentExecutionProfile {
	return service.AgentExecutionProfile{
		ID: id, DisplayName: displayName, Provider: provider, Protocol: "anthropic", MinTier: minTier, Available: true,
		Envs: map[string]string{
			model.ClaudeEnvBaseURL:           "https://" + provider + ".example/anthropic",
			model.ClaudeEnvAuthToken:         "test-secret",
			model.ClaudeEnvModel:             modelName,
			"ANTHROPIC_DEFAULT_OPUS_MODEL":   modelName,
			"ANTHROPIC_DEFAULT_FABLE_MODEL":  modelName,
			"ANTHROPIC_DEFAULT_SONNET_MODEL": modelName,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":  modelName,
		},
		ModelUsageAliases: map[string]string{modelName: modelName},
	}
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
