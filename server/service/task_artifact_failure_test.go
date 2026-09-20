package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
)

func TestSeednoteCompletionDistinguishesUploadFailureFromMissingGeneration(t *testing.T) {
	for _, tc := range []struct {
		name, missing, failedPath, wantReason string
		success                               bool
	}{
		{name: "required upload failed", missing: "image-review.md", failedPath: "output/image-review.md", wantReason: "artifact_upload_failed"},
		{name: "not generated", missing: "image-review.md", wantReason: "deliverable_validation_failed"},
		{name: "unrelated upload failure", missing: "image-review.md", failedPath: "output/optional.md", wantReason: "deliverable_validation_failed"},
		{name: "optional upload failed", failedPath: "output/optional.md", success: true, wantReason: "completed"},
		{name: "verified object wins", failedPath: "output/image-review.md", success: true, wantReason: "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, repo, db, task, execution := setupCloudCompletionTestWithDB(t, false)
			task.Type = model.PlatformSeednote
			if err := repo.Tasks().Update(ctx, task); err != nil {
				t.Fatal(err)
			}
			if err := applyAgentPackIdentity(execution, task.Type); err != nil {
				t.Fatal(err)
			}
			if err := db.Save(execution).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateActive, "已开始"); err != nil {
				t.Fatal(err)
			}
			for _, name := range seednoteCompletionArtifactNamesForTest(true, false) {
				if name == tc.missing {
					continue
				}
				mime := "text/markdown"
				if strings.HasSuffix(name, ".json") {
					mime = "application/json"
				}
				if strings.HasSuffix(name, ".png") {
					mime = "image/png"
				}
				path := "output/" + name
				body := validTaskDeliveryFixtureBody(path, mime)
				if mime == "image/png" {
					body = tinyImagePNG(t)
				}
				addCloudOutcomeArtifact(t, svc, repo, task, execution, path, mime, body)
			}
			if err := db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).Updates(map[string]any{"manifest_sealed": true, "manifest_status": model.TaskExecutionManifestPending}).Error; err != nil {
				t.Fatal(err)
			}
			result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
			if tc.failedPath != "" {
				payload, _ := json.Marshal(map[string]any{"success": true, "remote_artifacts": true, "artifact_upload_failures": []map[string]any{{"path": tc.failedPath, "operation": "prepare", "code": "service_unavailable", "http_status": 503, "attempts": 4, "retryable": true}}})
				if err := json.Unmarshal(payload, result); err != nil {
					t.Fatal(err)
				}
			}
			if err := svc.CompleteCloudExecution(ctx, execution.ID, result); err != nil {
				t.Fatal(err)
			}
			if err := svc.CompleteCloudExecution(ctx, execution.ID, result); err != nil {
				t.Fatalf("identical completion retry: %v", err)
			}
			got, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			ex, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (got.Status == model.TaskStatusCompleted) != tc.success || ex.TerminalReason != tc.wantReason {
				t.Fatalf("task=%s reason=%s error=%s", got.Status, ex.TerminalReason, got.ErrorMessage)
			}
			if tc.wantReason == "artifact_upload_failed" {
				if got.BillingTerminalReason != model.TaskBillingTerminalPlatformError {
					t.Fatalf("billing=%s", got.BillingTerminalReason)
				}
				if got.Outcome == nil || got.Outcome.Diagnostic == nil || got.Outcome.Diagnostic.Stage != "artifact_upload" || got.Outcome.Diagnostic.Provider != "" {
					t.Fatalf("outcome=%#v", got.Outcome)
				}
				for _, stage := range got.Lifecycle.Data().Stages {
					if stage.State == model.TaskLifecycleStateFailed {
						t.Fatalf("upload falsely blamed stage: %#v", stage)
					}
				}
				files, err := repo.TaskFiles().FindByExecutionID(ctx, execution.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(files) != 10 {
					t.Fatalf("retained=%d", len(files))
				}
				for _, f := range files {
					if f.State != model.TaskFileStateRetained {
						t.Fatalf("file state=%s", f.State)
					}
				}
			}
		})
	}
}

func TestRepeatedLifecycleCompletionCanUpdateDescription(t *testing.T) {
	ctx := context.Background()
	svc, _, task, execution := setupTaskLifecycleTest(t, model.PlatformSeednote)
	if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateActive, ""); err != nil {
		t.Fatal(err)
	}
	first, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateComplete, "完成")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateComplete, "补充说明")
	if err != nil {
		t.Fatal(err)
	}
	if !first.Stages[0].CompletedAt.Equal(*second.Stages[0].CompletedAt) || second.Stages[0].LatestUpdate != "补充说明" {
		t.Fatalf("completion moved or update lost: %#v", second.Stages[0])
	}
}

func TestCompletionReplayDoesNotRevalidateRetainedInvalidArtifact(t *testing.T) {
	ctx := context.Background()
	svc, repo, db, task, execution := setupCloudCompletionTestWithDB(t, false)
	task.Type = model.PlatformSeednote
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := applyAgentPackIdentity(execution, task.Type); err != nil {
		t.Fatal(err)
	}
	if err := db.Save(execution).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range seednoteCompletionArtifactNamesForTest(true, false) {
		mime := "text/markdown"
		if strings.HasSuffix(name, ".json") {
			mime = "application/json"
		}
		if strings.HasSuffix(name, ".png") {
			mime = "image/png"
		}
		path := "output/" + name
		body := validTaskDeliveryFixtureBody(path, mime)
		if mime == "image/png" && name != "cover.png" {
			body = tinyImagePNG(t)
		}
		addCloudOutcomeArtifact(t, svc, repo, task, execution, path, mime, body)
	}
	if err := db.Model(execution).Updates(map[string]any{"manifest_sealed": true, "manifest_status": model.TaskExecutionManifestPending}).Error; err != nil {
		t.Fatal(err)
	}
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	if err := svc.CompleteCloudExecution(ctx, execution.ID, result); err != nil {
		t.Fatal(err)
	}
	first, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != model.TaskExecutionFailed || !strings.Contains(string(first.Result), "decodable") {
		t.Fatalf("expected invalid image failure: %s", first.Result)
	}
	if err := svc.CompleteCloudExecution(ctx, execution.ID, result); err != nil {
		t.Fatalf("identical retry after file retention: %v", err)
	}
	second, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Result) != string(second.Result) {
		t.Fatal("retry changed durable outcome")
	}
	changed := *result
	changed.LogText = "different completion"
	if err := svc.CompleteCloudExecution(ctx, execution.ID, &changed); !errors.Is(err, ErrTaskCompletionConflict) {
		t.Fatalf("changed retry: %v", err)
	}
}

func TestManifestFailurePreservesTransferDiagnostic(t *testing.T) {
	ctx := context.Background()
	svc, repo, db, task, execution := setupCloudCompletionTestWithDB(t, false)
	if err := db.Model(execution).UpdateColumn("manifest_sealed", false).Error; err != nil {
		t.Fatal(err)
	}
	var result agent.ExecutionResult
	if err := json.Unmarshal([]byte(`{"success":false,"remote_artifacts":true,"root_error_code":"artifact_manifest_failed","artifact_finalization_failure":{"operation":"manifest","code":"service_unavailable","http_status":503,"attempts":4,"retryable":true,"request_id":"request-123"}}`), &result); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteCloudExecution(ctx, execution.ID, &result); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.TaskStatusFailed || got.BillingTerminalReason != model.TaskBillingTerminalPlatformError || got.Outcome == nil || got.Outcome.Diagnostic == nil {
		t.Fatalf("task=%+v", got)
	}
	diagnostic := got.Outcome.Diagnostic
	if diagnostic.Code != "artifact_manifest_failed" || diagnostic.HTTPStatus != 503 || !diagnostic.Recoverable || diagnostic.RequestID != safePublicRequestID("request-123") || diagnostic.Provider != "" {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	stored, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored.Result), `"attempts":4`) {
		t.Fatalf("lost structured transfer evidence: %s", stored.Result)
	}
	if err := svc.CompleteCloudExecution(ctx, execution.ID, &result); err != nil {
		t.Fatal(err)
	}
}
