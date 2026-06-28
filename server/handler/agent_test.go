package handler

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

// setupAgentClaimApp wires an AgentHandler (real API-key auth via APIKeyService)
// over a fresh sqlite DB seeded with a user + project, and returns the app, the
// repo handle (so callers can seed tasks against the same DB), the user's raw
// API key, and the user/project IDs.
func setupAgentClaimApp(t *testing.T) (app *fiber.App, repo repository.Repository, rawKey, userID, projectID string) {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo = repository.New(db)
	ctx := context.Background()

	userID = uuid.New().String()
	projectID = uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "claim@example.com", Password: "x", InviteCode: "ic"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "P", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	taskSvc.SetExecutorDefaults("claude-test", nil)
	apiKeySvc := service.NewAPIKeyService(repo, &logger)
	_, rawKey, err := apiKeySvc.Create(ctx, userID, "test")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	h := NewAgentHandler(taskSvc, apiKeySvc, nil, "", &logger)
	app = fiber.New()
	app.Post("/agent/claim", h.AuthMiddleware, h.Claim)
	return app, repo, rawKey, userID, projectID
}

// seedClaimableLocalTask persists a pending local-target task and returns its ID.
func seedClaimableLocalTask(t *testing.T, repo repository.Repository, userID, projectID string) string {
	t.Helper()
	task := &model.Task{
		ID:                 uuid.New().String(),
		UserID:             userID,
		ProjectID:          projectID,
		Type:               model.PlatformSeednote,
		Status:             model.TaskStatusPending,
		Prompt:             "测试选题",
		ExecutionTarget:    model.ExecutionTargetLocal,
		LocalClaimDeadline: ptrTime(time.Now().Add(service.LocalClaimWindow)),
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create seed task: %v", err)
	}
	return task.ID
}

func ptrTime(t time.Time) *time.Time { return &t }

// TestAgentClaim_RequiresAuth confirms a missing token is rejected.
func TestAgentClaim_RequiresAuth(t *testing.T) {
	app, _, _, _, _ := setupAgentClaimApp(t)
	req := httptest.NewRequest("POST", "/agent/claim", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestAgentClaim_RejectsInvalidToken confirms an unknown token is rejected.
func TestAgentClaim_RejectsInvalidToken(t *testing.T) {
	app, _, _, _, _ := setupAgentClaimApp(t)
	req := httptest.NewRequest("POST", "/agent/claim", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-key")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestAgentClaim_ReturnsConfigThenNoContent confirms a valid claim returns the
// full task config, and a follow-up claim returns 204 (nothing left).
func TestAgentClaim_ReturnsConfigThenNoContent(t *testing.T) {
	app, repo, rawKey, userID, projectID := setupAgentClaimApp(t)
	taskID := seedClaimableLocalTask(t, repo, userID, projectID)

	// First claim: should return 200 + config carrying the task id.
	req := httptest.NewRequest("POST", "/agent/claim", strings.NewReader(`{"executor_info":{"hostname":"mbp"}}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), taskID) {
		t.Fatalf("response body does not contain task id %q: %s", taskID, body)
	}
	if !strings.Contains(string(body), `"agent_flag"`) {
		t.Fatalf("response body missing agent_flag field: %s", body)
	}

	// Second claim: nothing claimable → 204 No Content.
	req2 := httptest.NewRequest("POST", "/agent/claim", nil)
	req2.Header.Set("Authorization", "Bearer "+rawKey)
	resp2, err := app.Test(req2)
	if err != nil {
		t.Fatalf("second claim failed: %v", err)
	}
	if resp2.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp2.StatusCode)
	}
}

// TestAgentClaim_NoContentWhenEmpty confirms an unauthenticated-of-tasks claim
// returns 204 even with a valid token.
func TestAgentClaim_NoContentWhenEmpty(t *testing.T) {
	app, _, rawKey, _, _ := setupAgentClaimApp(t)
	// No task seeded.
	req := httptest.NewRequest("POST", "/agent/claim", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}
