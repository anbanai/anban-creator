package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLegacyProgressCompatibilityToolRemainsRegistered(t *testing.T) {
	tools := listToolNames(t, RegisterTools)
	if !tools["update_task_progress"] {
		t.Fatal("update_task_progress must remain registered for Codex, DSH, and other hosts without SDK Task lifecycle hooks")
	}
}

func TestLegacyProgressCompatibilityHandlerUsesStageFallback(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	userID := uuid.NewString()
	project := createAccountInfoProject(t, repo, userID, "")
	task := createAccountInfoTask(t, repo, userID, project.ID, "")
	arguments, err := json.Marshal(map[string]any{
		"task_id": task.ID,
		"stage":   "writing",
		"title":   "Writing",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := progressUpdateHandler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: arguments},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("progressUpdateHandler returned tool error: %#v", result.Content)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Progress != 40 || persisted.LatestProgress.Data().Percent != 40 {
		t.Fatalf("legacy fallback progress = %d/%#v, want seednote writing percent 40", persisted.Progress, persisted.LatestProgress.Data())
	}
}
