package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/anbanai/anban-creator/server/model"
)

func TestAgentProgressPlanAndStageUpdateUseLifecycleContract(t *testing.T) {
	app, repo, task, _, token, _, _ := setupExecutionScopedAgentAppForPack(t, model.PlatformMoments, "")
	planBody := `{"task_id":"` + task.ID + `","stages":[{"id":"research","title":"研究素材","goal":"核验事实"},{"id":"writing","title":"撰写内容"}]}`
	planReq := agentJSONRequest("/agent/progress-plan", planBody)
	planReq.Header.Set("Authorization", "Bearer "+token)
	planResp, err := app.Test(planReq)
	if err != nil {
		t.Fatal(err)
	}
	if planResp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(planResp.Body)
		t.Fatalf("plan status/body = %d/%s", planResp.StatusCode, body)
	}

	updateBody := `{"task_id":"` + task.ID + `","stage":"research","state":"active","description":"正在核验来源"}`
	updateReq := agentJSONRequest("/agent/progress", updateBody)
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateResp, err := app.Test(updateReq)
	if err != nil {
		t.Fatal(err)
	}
	if updateResp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(updateResp.Body)
		t.Fatalf("update status/body = %d/%s", updateResp.StatusCode, body)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := persisted.Lifecycle.Data()
	if lifecycle.Revision != 2 || lifecycle.Stages[0].State != model.TaskLifecycleStateActive || lifecycle.Stages[0].LatestUpdate != "正在核验来源" {
		t.Fatalf("persisted lifecycle = %#v", lifecycle)
	}
}

func TestAgentProgressRejectsRemovedPercentageAndTitleFields(t *testing.T) {
	for _, removed := range []string{`"title":"客户端标题"`, `"progress_percent":50`} {
		t.Run(removed, func(t *testing.T) {
			app, repo, task, _, token, _, _ := setupExecutionScopedAgentAppForPack(t, model.PlatformMoments, "")
			body := `{"task_id":"` + task.ID + `","stage":"research","state":"active",` + removed + `}`
			req := agentJSONRequest("/agent/progress", body)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusBadRequest {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("status/body = %d/%s, want 400", resp.StatusCode, responseBody)
			}
			persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Lifecycle.Data().Revision != 0 || strings.TrimSpace(persisted.ProgressLog) != "" {
				t.Fatalf("rejected legacy payload caused side effects: %#v / %q", persisted.Lifecycle.Data(), persisted.ProgressLog)
			}
		})
	}
}

func TestAgentProgressPlanRejectsLegacyExecutionIDWithoutMutation(t *testing.T) {
	app, repo, task, _, token, _, _ := setupExecutionScopedAgentAppForPack(t, model.PlatformMoments, "")
	body := `{"task_id":"` + task.ID + `","execution_id":"wrong","stages":[{"id":"research","title":"研究素材"},{"id":"writing","title":"撰写内容"}]}`
	req := agentJSONRequest("/agent/progress-plan", body)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Lifecycle.Data().Revision != 0 {
		t.Fatalf("mismatched identity mutated lifecycle: %#v", persisted.Lifecycle.Data())
	}
}
