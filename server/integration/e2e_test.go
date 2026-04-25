package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/auth"
	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/handler"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/router"
	"github.com/royalrick/anbanwriter/server/service"
)

// ---------------------------------------------------------------------------
// Response type mirrors the standard JSON envelope from handler.Success/Error.
// ---------------------------------------------------------------------------

type apiResponse struct {
	Code int                    `json:"code"`
	Msg  string                 `json:"msg"`
	Data map[string]interface{} `json:"data"`
}

// ---------------------------------------------------------------------------
// No-op TaskEnqueuer that records tasks but doesn't execute them.
// Prevents the goroutine panic from a nil executor.
// ---------------------------------------------------------------------------

type noopEnqueuer struct{}

func (n *noopEnqueuer) Enqueue(taskType string, payload []byte) error      { return nil }
func (n *noopEnqueuer) EnqueueIn(taskType string, payload []byte, delay time.Duration) error {
	return nil
}

// ---------------------------------------------------------------------------
// setupTestRouter creates a full Fiber app with in-memory SQLite for E2E tests.
// ---------------------------------------------------------------------------

func setupTestRouter(t *testing.T) (*fiber.App, func(), repository.Repository) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	// Use AutoMigrate but skip the composite unique index DDL
	// (which uses MySQL-specific syntax). Manually create it for SQLite.
	if err := db.AutoMigrate(
		&model.User{},
		&model.LoginSession{},
		&model.Channel{},
		&model.Plan{},
		&model.Task{},
		&model.TaskFile{},
		&model.CreditTransaction{},
	); err != nil {
		t.Fatalf("failed to auto-migrate: %v", err)
	}


	closeFunc := func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	repo := repository.New(db)

	jwtSvc, err := auth.NewJWTService("test-secret-key-for-e2e-testing", "24h", "168h")
	if err != nil {
		t.Fatalf("failed to create JWT service: %v", err)
	}

	planSvc := service.NewPlanService(repo, &logger)
	taskSvc := service.NewTaskService(repo, nil, &noopEnqueuer{}, nil, nil, &logger, "", nil, "", nil)

	wsHub := handler.NewWebSocketHub(jwtSvc)
	authHandler := handler.NewAuthHandler(jwtSvc, nil, repo, nil, &logger, wsHub, false, 3)
	planHandler := handler.NewPlanHandler(planSvc, &logger)
	taskHandler := handler.NewTaskHandler(taskSvc, &logger)
	timelineHandler := handler.NewTimelineHandler(repo, &logger)

	svcs := &router.Services{
		Config:          &config.Config{Server: config.ServerConfig{Port: 0, Host: "0.0.0.0"}},
		Logger:          &logger,
		DB:              db,
		Repo:            repo,
		JWTService:      jwtSvc,
		WSHub:           wsHub,
		AuthHandler:     authHandler,
		PlanService:     planSvc,
		TaskService:     taskSvc,
		PlanHandler:     planHandler,
		TaskHandler:     taskHandler,
		TimelineHandler: timelineHandler,
		// Redis nil: rate limiter becomes pass-through.
		// Executor nil: TaskService won't execute tasks.
	}

	app := router.NewRouter(svcs)
	return app, closeFunc, repo
}

// parseJSONBody reads the response body and unmarshals it into a generic map.
func parseJSONBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse response JSON: %v\nbody: %s", err, string(body))
	}
	return result
}

// registerUser creates a user via the API and returns (token, userID).
// It calls t.Fatal on failure.
func registerUser(t *testing.T, app *fiber.App, email, password, nickname string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
		"nickname": nickname,
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, resp)
		t.Fatalf("register returned %d: %s", resp.StatusCode, result["msg"])
	}

	var result map[string]interface{}
	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	json.Unmarshal(bodyBytes, &result)

	data := result["data"].(map[string]interface{})
	token := data["token"].(string)
	userData := data["user"].(map[string]interface{})
	userID := userData["id"].(string)

	return token, userID
}

// ---------------------------------------------------------------------------
// E2E Tests
// ---------------------------------------------------------------------------

// TestE2E_FullUserFlow tests the complete lifecycle: register, create task,
// list tasks, get task by ID, verify ownership isolation, and timeline access.
func TestE2E_FullUserFlow(t *testing.T) {
	app, closeFunc, repo := setupTestRouter(t)
	defer closeFunc()

	// Step 1: Register user 1.
	token1, userID1 := registerUser(t, app, "test@example.com", "testpassword123", "Test User")

	// Step 1.5: Create a test channel for the user.
	testChannel := &model.Channel{
		ID:       "test-channel-rednote-1",
		UserID:   userID1,
		Platform: model.ScopeRednote,
		Name:     "Test Rednote Channel",
		Status:   model.ChannelStatusActive,
	}
	if err := repo.Channels().Create(context.Background(), testChannel); err != nil {
		t.Fatalf("create test channel: %v", err)
	}

	// Step 2: Create a manual task.
	taskBody, _ := json.Marshal(map[string]string{
		"channel_id": testChannel.ID,
		"prompt":      "TestTopic",
	})
	taskReq := httptest.NewRequest("POST", "/api/v1/tasks", strings.NewReader(string(taskBody)))
	taskReq.Header.Set("Content-Type", "application/json")
	taskReq.Header.Set("Authorization", "Bearer "+token1)
	taskResp, err := app.Test(taskReq)
	if err != nil {
		t.Fatalf("create task request failed: %v", err)
	}
	if taskResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, taskResp)
		t.Fatalf("create task returned %d: %s", taskResp.StatusCode, result["msg"])
	}

	taskResult := parseJSONBody(t, taskResp)
	taskData := taskResult["data"].(map[string]interface{})
	taskID := taskData["id"].(string)

	if taskData["type"].(string) != "rednote" {
		t.Errorf("expected task type 'rednote', got %s", taskData["type"])
	}
	if taskData["user_id"].(string) != userID1 {
		t.Errorf("expected task user_id %s, got %s", userID1, taskData["user_id"])
	}

	// Step 3: List tasks.
	listReq := httptest.NewRequest("GET", "/api/v1/tasks?offset=0&limit=20", nil)
	listReq.Header.Set("Authorization", "Bearer "+token1)
	listResp, err := app.Test(listReq)
	if err != nil {
		t.Fatalf("list tasks request failed: %v", err)
	}
	if listResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, listResp)
		t.Fatalf("list tasks returned %d: %s", listResp.StatusCode, result["msg"])
	}

	listResult := parseJSONBody(t, listResp)
	listData := listResult["data"].(map[string]interface{})
	items := listData["items"].([]interface{})
	if len(items) == 0 {
		t.Fatal("expected at least 1 task in list, got 0")
	}

	// Step 4: Get task by ID.
	getReq := httptest.NewRequest("GET", "/api/v1/tasks/"+taskID, nil)
	getReq.Header.Set("Authorization", "Bearer "+token1)
	getResp, err := app.Test(getReq)
	if err != nil {
		t.Fatalf("get task request failed: %v", err)
	}
	if getResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, getResp)
		t.Fatalf("get task returned %d: %s", getResp.StatusCode, result["msg"])
	}

	getResult := parseJSONBody(t, getResp)
	getData := getResult["data"].(map[string]interface{})
	if getData["id"].(string) != taskID {
		t.Errorf("expected task ID %s, got %s", taskID, getData["id"])
	}

	// Step 5: Verify ownership -- different user cannot access.
	token2, _ := registerUser(t, app, "other@example.com", "testpassword123", "Other User")

	forbiddenReq := httptest.NewRequest("GET", "/api/v1/tasks/"+taskID, nil)
	forbiddenReq.Header.Set("Authorization", "Bearer "+token2)
	forbiddenResp, err := app.Test(forbiddenReq)
	if err != nil {
		t.Fatalf("forbidden request failed: %v", err)
	}
	if forbiddenResp.StatusCode != fiber.StatusForbidden {
		result := parseJSONBody(t, forbiddenResp)
		t.Fatalf("expected 403 for cross-user access, got %d: %s", forbiddenResp.StatusCode, result["msg"])
	}

	// Step 6: Timeline.
	from := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	to := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	tlReq := httptest.NewRequest("GET", "/api/v1/timeline?from="+from+"&to="+to, nil)
	tlReq.Header.Set("Authorization", "Bearer "+token1)
	tlResp, err := app.Test(tlReq)
	if err != nil {
		t.Fatalf("timeline request failed: %v", err)
	}
	if tlResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, tlResp)
		t.Fatalf("timeline returned %d: %s", tlResp.StatusCode, result["msg"])
	}

	tlResult := parseJSONBody(t, tlResp)
	tlData := tlResult["data"].(map[string]interface{})
	tlItems := tlData["items"].([]interface{})
	found := false
	for _, item := range tlItems {
		itemMap := item.(map[string]interface{})
		if itemMap["id"].(string) == taskID {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find the created task in the timeline")
	}
}

// TestE2E_PlanLifecycle tests creating, reading, pausing, resuming, and
// deleting a plan.
func TestE2E_PlanLifecycle(t *testing.T) {
	app, closeFunc, repo := setupTestRouter(t)
	defer closeFunc()

	token, userID := registerUser(t, app, "plan@example.com", "testpassword123", "Plan User")

	// Step 0: Create a test channel for the user.
	testChannel := &model.Channel{
		ID:       "plan-lifecycle-channel",
		UserID:   userID,
		Platform: model.PlatformRednote,
		Name:     "Plan Lifecycle Channel",
		Status:   model.ChannelStatusActive,
	}
	if err := repo.Channels().Create(context.Background(), testChannel); err != nil {
		t.Fatalf("create test channel: %v", err)
	}

	// Step 1: Create a plan.
	planBody, _ := json.Marshal(map[string]string{
		"channel_id":  testChannel.ID,
		"cron_expr":   "0 9 * * *",
		"prompt":      "spring fashion",
	})
	createReq := httptest.NewRequest("POST", "/api/v1/plans", strings.NewReader(string(planBody)))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+token)
	createResp, err := app.Test(createReq)
	if err != nil {
		t.Fatalf("create plan request failed: %v", err)
	}
	if createResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, createResp)
		t.Fatalf("create plan returned %d: %s", createResp.StatusCode, result["msg"])
	}

	createResult := parseJSONBody(t, createResp)
	planData := createResult["data"].(map[string]interface{})
	planID := planData["id"].(string)

	if planData["status"].(string) != model.PlanStatusActive {
		t.Errorf("expected plan status 'active', got %s", planData["status"])
	}
	if planData["next_run_at"] == nil {
		t.Error("expected next_run_at to be set")
	}

	// Step 2: Get plan by ID.
	getReq := httptest.NewRequest("GET", "/api/v1/plans/"+planID, nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getResp, err := app.Test(getReq)
	if err != nil {
		t.Fatalf("get plan request failed: %v", err)
	}
	if getResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, getResp)
		t.Fatalf("get plan returned %d: %s", getResp.StatusCode, result["msg"])
	}

	// Step 3: Pause plan.
	pauseReq := httptest.NewRequest("POST", "/api/v1/plans/"+planID+"/pause", nil)
	pauseReq.Header.Set("Authorization", "Bearer "+token)
	pauseResp, err := app.Test(pauseReq)
	if err != nil {
		t.Fatalf("pause plan request failed: %v", err)
	}
	if pauseResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, pauseResp)
		t.Fatalf("pause plan returned %d: %s", pauseResp.StatusCode, result["msg"])
	}

	// Verify status is now paused.
	verifyReq := httptest.NewRequest("GET", "/api/v1/plans/"+planID, nil)
	verifyReq.Header.Set("Authorization", "Bearer "+token)
	verifyResp, err := app.Test(verifyReq)
	if err != nil {
		t.Fatalf("verify paused plan request failed: %v", err)
	}
	verifyResult := parseJSONBody(t, verifyResp)
	verifyData := verifyResult["data"].(map[string]interface{})
	if verifyData["status"].(string) != model.PlanStatusPaused {
		t.Errorf("expected plan status 'paused', got %s", verifyData["status"])
	}

	// Step 4: Resume plan.
	resumeReq := httptest.NewRequest("POST", "/api/v1/plans/"+planID+"/resume", nil)
	resumeReq.Header.Set("Authorization", "Bearer "+token)
	resumeResp, err := app.Test(resumeReq)
	if err != nil {
		t.Fatalf("resume plan request failed: %v", err)
	}
	if resumeResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, resumeResp)
		t.Fatalf("resume plan returned %d: %s", resumeResp.StatusCode, result["msg"])
	}

	// Verify status is back to active.
	verifyReq2 := httptest.NewRequest("GET", "/api/v1/plans/"+planID, nil)
	verifyReq2.Header.Set("Authorization", "Bearer "+token)
	verifyResp2, err := app.Test(verifyReq2)
	if err != nil {
		t.Fatalf("verify resumed plan request failed: %v", err)
	}
	verifyResult2 := parseJSONBody(t, verifyResp2)
	verifyData2 := verifyResult2["data"].(map[string]interface{})
	if verifyData2["status"].(string) != model.PlanStatusActive {
		t.Errorf("expected plan status 'active' after resume, got %s", verifyData2["status"])
	}

	// Step 5: Delete plan.
	deleteReq := httptest.NewRequest("DELETE", "/api/v1/plans/"+planID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteResp, err := app.Test(deleteReq)
	if err != nil {
		t.Fatalf("delete plan request failed: %v", err)
	}
	if deleteResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, deleteResp)
		t.Fatalf("delete plan returned %d: %s", deleteResp.StatusCode, result["msg"])
	}

	// Step 6: Verify plan is no longer accessible.
	deletedReq := httptest.NewRequest("GET", "/api/v1/plans/"+planID, nil)
	deletedReq.Header.Set("Authorization", "Bearer "+token)
	deletedResp, err := app.Test(deletedReq)
	if err != nil {
		t.Fatalf("get deleted plan request failed: %v", err)
	}
	if deletedResp.StatusCode != fiber.StatusNotFound {
		result := parseJSONBody(t, deletedResp)
		t.Fatalf("expected 404 for deleted plan, got %d: %s", deletedResp.StatusCode, result["msg"])
	}
}

// TestE2E_AuthLifecycle tests registration, login, token refresh, and logout.
func TestE2E_AuthLifecycle(t *testing.T) {
	app, closeFunc, _ := setupTestRouter(t)
	defer closeFunc()

	// Step 1: Register.
	token, _ := registerUser(t, app, "auth@example.com", "testpassword123", "Auth User")

	// Step 2: Verify /auth/me works.
	meReq := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	meResp, err := app.Test(meReq)
	if err != nil {
		t.Fatalf("me request failed: %v", err)
	}
	if meResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, meResp)
		t.Fatalf("me returned %d: %s", meResp.StatusCode, result["msg"])
	}

	meResult := parseJSONBody(t, meResp)
	meData := meResult["data"].(map[string]interface{})
	if meData["email"].(string) != "auth@example.com" {
		t.Errorf("expected email 'auth@example.com', got %s", meData["email"])
	}

	// Step 3: Login with the same credentials.
	loginBody, _ := json.Marshal(map[string]string{
		"email":    "auth@example.com",
		"password": "testpassword123",
	})
	loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(string(loginBody)))
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := app.Test(loginReq)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	if loginResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, loginResp)
		t.Fatalf("login returned %d: %s", loginResp.StatusCode, result["msg"])
	}

	loginResult := parseJSONBody(t, loginResp)
	loginData := loginResult["data"].(map[string]interface{})
	loginToken := loginData["token"].(string)
	refreshToken := loginData["refresh_token"].(string)

	if loginToken == "" {
		t.Error("expected login token to be non-empty")
	}
	if refreshToken == "" {
		t.Error("expected refresh token to be non-empty")
	}

	// Step 4: Access protected endpoint with login token.
	meReq2 := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	meReq2.Header.Set("Authorization", "Bearer "+loginToken)
	meResp2, err := app.Test(meReq2)
	if err != nil {
		t.Fatalf("me with login token request failed: %v", err)
	}
	if meResp2.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, meResp2)
		t.Fatalf("me with login token returned %d: %s", meResp2.StatusCode, result["msg"])
	}

	// Step 5: Unauthenticated access should fail.
	unauthReq := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	unauthResp, err := app.Test(unauthReq)
	if err != nil {
		t.Fatalf("unauth request failed: %v", err)
	}
	if unauthResp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated access, got %d", unauthResp.StatusCode)
	}

	// Step 6: Register with duplicate email should fail (HTTP 409).
	dupBody, _ := json.Marshal(map[string]string{
		"email":    "auth@example.com",
		"password": "testpassword123",
		"nickname": "Duplicate User",
	})
	dupReq := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(string(dupBody)))
	dupReq.Header.Set("Content-Type", "application/json")
	dupResp, err := app.Test(dupReq)
	if err != nil {
		t.Fatalf("duplicate register request failed: %v", err)
	}
	if dupResp.StatusCode != fiber.StatusConflict {
		result := parseJSONBody(t, dupResp)
		t.Fatalf("expected 409 for duplicate email, got %d: %s", dupResp.StatusCode, result["msg"])
	}
}

// TestE2E_TaskOwnershipIsolation verifies that a user cannot access another
// user's tasks via list or get-by-ID endpoints.
func TestE2E_TaskOwnershipIsolation(t *testing.T) {
	app, closeFunc, repo := setupTestRouter(t)
	defer closeFunc()

	token1, userID1 := registerUser(t, app, "user1@example.com", "password123", "User One")
	token2, _ := registerUser(t, app, "user2@example.com", "password123", "User Two")

	// Create a test channel for user 1.
	testChannel := &model.Channel{
		ID:       "test-channel-ownership-1",
		UserID:   userID1,
		Platform: model.ScopeArticle,
		Name:     "User1 Article Channel",
		Status:   model.ChannelStatusActive,
	}
	if err := repo.Channels().Create(context.Background(), testChannel); err != nil {
		t.Fatalf("create test channel: %v", err)
	}

	// User 1 creates a task.
	taskBody, _ := json.Marshal(map[string]string{
		"channel_id": testChannel.ID,
		"prompt":      "User1 Article",
	})
	taskReq := httptest.NewRequest("POST", "/api/v1/tasks", strings.NewReader(string(taskBody)))
	taskReq.Header.Set("Content-Type", "application/json")
	taskReq.Header.Set("Authorization", "Bearer "+token1)
	taskResp, err := app.Test(taskReq)
	if err != nil {
		t.Fatalf("create task request failed: %v", err)
	}
	if taskResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, taskResp)
		t.Fatalf("create task returned %d: %s", taskResp.StatusCode, result["msg"])
	}

	taskResult := parseJSONBody(t, taskResp)
	taskID := taskResult["data"].(map[string]interface{})["id"].(string)

	// User 2 lists tasks -- should not see User 1's task.
	listReq := httptest.NewRequest("GET", "/api/v1/tasks?offset=0&limit=20", nil)
	listReq.Header.Set("Authorization", "Bearer "+token2)
	listResp, err := app.Test(listReq)
	if err != nil {
		t.Fatalf("list tasks request failed: %v", err)
	}
	if listResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, listResp)
		t.Fatalf("list tasks returned %d: %s", listResp.StatusCode, result["msg"])
	}

	listResult := parseJSONBody(t, listResp)
	items := listResult["data"].(map[string]interface{})["items"].([]interface{})
	for _, item := range items {
		itemMap := item.(map[string]interface{})
		if itemMap["id"].(string) == taskID {
			t.Error("user2 should not see user1's task in list")
		}
	}

	// User 2 tries to get User 1's task by ID -- should be 403.
	forbiddenReq := httptest.NewRequest("GET", "/api/v1/tasks/"+taskID, nil)
	forbiddenReq.Header.Set("Authorization", "Bearer "+token2)
	forbiddenResp, err := app.Test(forbiddenReq)
	if err != nil {
		t.Fatalf("forbidden request failed: %v", err)
	}
	if forbiddenResp.StatusCode != fiber.StatusForbidden {
		result := parseJSONBody(t, forbiddenResp)
		t.Fatalf("expected 403 for cross-user task access, got %d: %s", forbiddenResp.StatusCode, result["msg"])
	}

	// User 2 tries to cancel User 1's task -- should be 403.
	cancelReq := httptest.NewRequest("POST", "/api/v1/tasks/"+taskID+"/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer "+token2)
	cancelResp, err := app.Test(cancelReq)
	if err != nil {
		t.Fatalf("cancel request failed: %v", err)
	}
	if cancelResp.StatusCode != fiber.StatusForbidden {
		result := parseJSONBody(t, cancelResp)
		t.Fatalf("expected 403 for cross-user task cancel, got %d: %s", cancelResp.StatusCode, result["msg"])
	}
}

// TestE2E_PlanOwnershipIsolation verifies that plan endpoints enforce ownership.
func TestE2E_PlanOwnershipIsolation(t *testing.T) {
	app, closeFunc, repo := setupTestRouter(t)
	defer closeFunc()

	token1, userID1 := registerUser(t, app, "planuser1@example.com", "password123", "Plan User One")
	token2, _ := registerUser(t, app, "planuser2@example.com", "password123", "Plan User Two")

	// Create a test channel for User 1.
	testChannel := &model.Channel{
		ID:       "plan-ownership-channel",
		UserID:   userID1,
		Platform: model.PlatformXLS,
		Name:     "User1 XLS Channel",
		Status:   model.ChannelStatusActive,
	}
	if err := repo.Channels().Create(context.Background(), testChannel); err != nil {
		t.Fatalf("create test channel: %v", err)
	}

	// User 1 creates a plan.
	planBody, _ := json.Marshal(map[string]string{
		"channel_id": testChannel.ID,
		"cron_expr":  "0 10 * * *",
		"prompt": "daily inspiration",
	})
	planReq := httptest.NewRequest("POST", "/api/v1/plans", strings.NewReader(string(planBody)))
	planReq.Header.Set("Content-Type", "application/json")
	planReq.Header.Set("Authorization", "Bearer "+token1)
	planResp, err := app.Test(planReq)
	if err != nil {
		t.Fatalf("create plan request failed: %v", err)
	}
	if planResp.StatusCode != fiber.StatusOK {
		result := parseJSONBody(t, planResp)
		t.Fatalf("create plan returned %d: %s", planResp.StatusCode, result["msg"])
	}

	planResult := parseJSONBody(t, planResp)
	planID := planResult["data"].(map[string]interface{})["id"].(string)

	// User 2 tries to get User 1's plan -- should be 403.
	forbiddenReq := httptest.NewRequest("GET", "/api/v1/plans/"+planID, nil)
	forbiddenReq.Header.Set("Authorization", "Bearer "+token2)
	forbiddenResp, err := app.Test(forbiddenReq)
	if err != nil {
		t.Fatalf("forbidden plan request failed: %v", err)
	}
	if forbiddenResp.StatusCode != fiber.StatusForbidden {
		result := parseJSONBody(t, forbiddenResp)
		t.Fatalf("expected 403 for cross-user plan access, got %d: %s", forbiddenResp.StatusCode, result["msg"])
	}

	// User 2 tries to pause User 1's plan -- should be 403.
	pauseReq := httptest.NewRequest("POST", "/api/v1/plans/"+planID+"/pause", nil)
	pauseReq.Header.Set("Authorization", "Bearer "+token2)
	pauseResp, err := app.Test(pauseReq)
	if err != nil {
		t.Fatalf("pause plan request failed: %v", err)
	}
	if pauseResp.StatusCode != fiber.StatusForbidden {
		result := parseJSONBody(t, pauseResp)
		t.Fatalf("expected 403 for cross-user plan pause, got %d: %s", pauseResp.StatusCode, result["msg"])
	}

	// User 2 tries to delete User 1's plan -- should be 403.
	deleteReq := httptest.NewRequest("DELETE", "/api/v1/plans/"+planID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token2)
	deleteResp, err := app.Test(deleteReq)
	if err != nil {
		t.Fatalf("delete plan request failed: %v", err)
	}
	if deleteResp.StatusCode != fiber.StatusForbidden {
		result := parseJSONBody(t, deleteResp)
		t.Fatalf("expected 403 for cross-user plan delete, got %d: %s", deleteResp.StatusCode, result["msg"])
	}
}

// TestE2E_InvalidInputs tests various invalid input scenarios.
func TestE2E_InvalidInputs(t *testing.T) {
	app, closeFunc, _ := setupTestRouter(t)
	defer closeFunc()

	token, _ := registerUser(t, app, "input@example.com", "password123", "Input User")

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		auth       bool
		wantStatus int
	}{
		{
			name:       "register without email",
			method:     "POST",
			path:       "/api/v1/auth/register",
			body:       `{"password":"test"}`,
			auth:       false,
			wantStatus: fiber.StatusBadRequest,
		},
		{
			name:       "register with short password",
			method:     "POST",
			path:       "/api/v1/auth/register",
			body:       `{"email":"short@pw.com","password":"ab"}`,
			auth:       false,
			wantStatus: fiber.StatusBadRequest,
		},
		{
			name:       "create task without channel_id",
			method:     "POST",
			path:       "/api/v1/tasks",
			body:       `{"prompt":"test"}`,
			auth:       true,
			wantStatus: fiber.StatusBadRequest,
		},
		{
			name:       "create task with non-existent channel_id",
			method:     "POST",
			path:       "/api/v1/tasks",
			body:       `{"channel_id":"nonexistent-id","prompt":"test"}`,
			auth:       true,
			wantStatus: fiber.StatusInternalServerError,
		},
		{
			name:       "create plan without channel_id",
			method:     "POST",
			path:       "/api/v1/plans",
			body:       `{"title":"test"}`,
			auth:       true,
			wantStatus: fiber.StatusBadRequest,
		},
		{
			name:       "timeline without date params",
			method:     "GET",
			path:       "/api/v1/timeline",
			body:       "",
			auth:       true,
			wantStatus: fiber.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			if tc.body != "" {
				req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			}
			if tc.auth {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != tc.wantStatus {
				result := parseJSONBody(t, resp)
				t.Errorf("expected status %d, got %d (msg: %s)", tc.wantStatus, resp.StatusCode, result["msg"])
			}
		})
	}
}

// TestE2E_FindRunningByUserDoesNotLeak verifies that the timeline endpoint
// does not leak running tasks from other users.
func TestE2E_FindRunningByUserDoesNotLeak(t *testing.T) {
	app, closeFunc, repo := setupTestRouter(t)
	defer closeFunc()

	token1, userID1 := registerUser(t, app, "runner1@example.com", "password123", "Runner One")
	token2, _ := registerUser(t, app, "runner2@example.com", "password123", "Runner Two")

	// Create a test channel for user 1.
	testChannel := &model.Channel{
		ID:       "test-channel-leak-1",
		UserID:   userID1,
		Platform: model.ScopeRednote,
		Name:     "Runner1 Rednote Channel",
		Status:   model.ChannelStatusActive,
	}
	if err := repo.Channels().Create(context.Background(), testChannel); err != nil {
		t.Fatalf("create test channel: %v", err)
	}

	// User 1 creates a task.
	taskBody, _ := json.Marshal(map[string]string{
		"channel_id": testChannel.ID,
		"prompt":      "Runner1 Task",
	})
	taskReq := httptest.NewRequest("POST", "/api/v1/tasks", strings.NewReader(string(taskBody)))
	taskReq.Header.Set("Content-Type", "application/json")
	taskReq.Header.Set("Authorization", "Bearer "+token1)
	taskResp, err := app.Test(taskReq)
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	if taskResp.StatusCode != fiber.StatusOK {
		t.Fatalf("create task returned %d", taskResp.StatusCode)
	}

	taskResult := parseJSONBody(t, taskResp)
	taskID := taskResult["data"].(map[string]interface{})["id"].(string)

	// Verify that User 2's timeline does not contain User 1's task.
	from := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	to := time.Now().Add(24 * time.Hour).Format("2006-01-02")

	// User 2 checks timeline -- should NOT see User 1's task.
	tlReq := httptest.NewRequest("GET", "/api/v1/timeline?from="+from+"&to="+to, nil)
	tlReq.Header.Set("Authorization", "Bearer "+token2)
	tlResp, err := app.Test(tlReq)
	if err != nil {
		t.Fatalf("timeline request failed: %v", err)
	}
	if tlResp.StatusCode != fiber.StatusOK {
		t.Fatalf("timeline returned %d", tlResp.StatusCode)
	}

	tlResult := parseJSONBody(t, tlResp)
	tlItems := tlResult["data"].(map[string]interface{})["items"].([]interface{})
	for _, item := range tlItems {
		itemMap := item.(map[string]interface{})
		if itemMap["id"].(string) == taskID {
			t.Error("user2 should not see user1's task in timeline (information leak)")
		}
	}

	// User 1 should see their own task in timeline.
	tlReq1 := httptest.NewRequest("GET", "/api/v1/timeline?from="+from+"&to="+to, nil)
	tlReq1.Header.Set("Authorization", "Bearer "+token1)
	tlResp1, err := app.Test(tlReq1)
	if err != nil {
		t.Fatalf("timeline request (user1) failed: %v", err)
	}
	if tlResp1.StatusCode != fiber.StatusOK {
		t.Fatalf("timeline (user1) returned %d", tlResp1.StatusCode)
	}

	tlResult1 := parseJSONBody(t, tlResp1)
	tlItems1 := tlResult1["data"].(map[string]interface{})["items"].([]interface{})
	found := false
	for _, item := range tlItems1 {
		itemMap := item.(map[string]interface{})
		if itemMap["id"].(string) == taskID {
			found = true
			break
		}
	}
	if !found {
		t.Error("user1 should see their own task in timeline")
	}
}
