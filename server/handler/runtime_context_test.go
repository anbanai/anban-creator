package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
)

func TestBuildRuntimeContextSanitizesAndGroupsTaskState(t *testing.T) {
	now := time.Now()
	task := &model.Task{AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration,
		ID:                      "task-1",
		Type:                    model.TaskTypeWechatArticle,
		Status:                  model.TaskStatusFailed,
		CreatedAt:               now.Add(-time.Hour),
		AgentProfileFingerprint: strings.Repeat("a", 64),
		AgentProfileSnapshot:    model.AgentProfileSnapshot{ProfileID: "balanced", DisplayName: "均衡"},
		ProjectSnapshot:         datatypes.NewJSONType(model.ProjectSnapshot{ProjectName: "春日生活号", Platform: model.PlatformWechat}),
		Lifecycle:               datatypes.NewJSONType(model.TaskLifecycle{ExecutionID: "exec-1"}),
		Outcome: &model.TaskOutcome{Diagnostic: &model.ExecutionDiagnostic{
			Code: "artifact_upload_failed", Summary: "产物上传失败", Recoverable: true, ResumePoint: "image_generation",
		}},
	}
	files := []*model.TaskFile{
		{ID: "file-1", TaskID: task.ID, State: model.TaskFileStateDelivered, FileName: "article.md", FilePath: "output/article.md", Role: model.FileRoleMarkdown, ContentHash: strings.Repeat("b", 64)},
		{ID: "file-2", TaskID: task.ID, State: model.TaskFileStateRetained, FileName: "cover.png", FilePath: "output/cover.png", Role: model.FileRoleCover},
	}

	got := buildRuntimeContext(task, files, nil)
	profile := got["profile"].(map[string]any)
	if profile["status"] != "frozen" || profile["snapshot_id"] != strings.Repeat("a", 64) {
		t.Fatalf("profile = %#v", profile)
	}
	execution := got["execution"].(map[string]any)
	if execution["status"] != "recoverable" || execution["execution_id"] != "exec-1" || execution["recovery_stage"] != "image_generation" {
		t.Fatalf("execution = %#v", execution)
	}
	artifacts := got["artifacts"].(map[string]any)
	if artifacts["completed"] != 2 || artifacts["failed"] != 1 {
		t.Fatalf("artifacts = %#v", artifacts)
	}
	items := artifacts["items"].([]map[string]any)
	if len(items) != 2 || items[0]["path"] != nil || items[0]["content_hash"] != nil {
		t.Fatalf("artifact items leaked technical fields: %#v", items)
	}
	connectivity := got["connectivity"].(map[string]any)
	if connectivity["status"] != "healthy" || connectivity["diagnostic_code"] != nil {
		t.Fatalf("connectivity = %#v", connectivity)
	}
}

func TestBuildRuntimeContextUsesFrozenArtifactContractAndSanitizedRuntimeFacts(t *testing.T) {
	task := &model.Task{AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration, Type: model.TaskTypeWechatArticle, Status: model.TaskStatusFailed, Outcome: &model.TaskOutcome{Diagnostic: &model.ExecutionDiagnostic{Code: "endpoint_timeout", Summary: "图片服务暂时不可用", Recoverable: true}}}
	execution := &model.TaskExecution{
		ID: "exec-2", Status: model.TaskExecutionFailed, RuntimeProfile: "balanced", RuntimeImage: "creator-agent@sha256:" + strings.Repeat("c", 64),
		AgentPackRequiredArtifactContract: []byte(`[{"role":"markdown","path":"output/article.md","mime_type":"text/markdown","required":true},{"role":"cover","path":"output/cover.png","mime_type":"image/png","required":true}]`),
	}
	files := []*model.TaskFile{{ID: "file-1", FileName: "article.md", FilePath: "output/article.md", Role: model.FileRoleMarkdown, State: model.TaskFileStateDelivered, ContentHash: strings.Repeat("d", 64)}}
	files[0].ExecutionID = execution.ID
	got := buildRuntimeContextWithExecution(task, files, nil, execution)
	executionView := got["execution"].(map[string]any)
	if executionView["runtime_profile"] != "balanced" || executionView["runtime_image_digest"] != "sha256:"+strings.Repeat("c", 64) {
		t.Fatalf("execution = %#v", executionView)
	}
	artifacts := got["artifacts"].(map[string]any)
	if artifacts["completed"] != 1 || artifacts["required"] != 2 || artifacts["missing"] != 1 {
		t.Fatalf("artifacts = %#v", artifacts)
	}
	items := artifacts["items"].([]map[string]any)
	if len(items) != 2 || items[0]["label"] != "文章正文" || items[1]["label"] != "封面图" || items[1]["status"] != "missing" {
		t.Fatalf("items = %#v", items)
	}
	connectivity := got["connectivity"].(map[string]any)
	if connectivity["status"] != "degraded" || connectivity["diagnostic_code"] != "endpoint_timeout" {
		t.Fatalf("connectivity = %#v", connectivity)
	}
}

func TestBuildRuntimeContextMarksProfileConflictWhenProjectChanged(t *testing.T) {
	created := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	task := &model.Task{AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration,
		Type:      model.TaskTypeWechatArticle,
		Status:    model.TaskStatusCompleted,
		CreatedAt: created,
		ProjectID: "project-1",
		ProjectSnapshot: datatypes.NewJSONType(model.ProjectSnapshot{
			ProjectName: "旧项目", Platform: model.PlatformWechat, Profile: model.ProjectProfile{Version: 1},
		}),
	}
	project := &model.Project{ID: "project-1", Name: "新项目", Platform: model.PlatformWechat, UpdatedAt: created.Add(time.Hour)}
	project.Profile = datatypes.NewJSONType(model.ProjectProfile{Version: 2})
	got := buildRuntimeContext(task, nil, project)
	profile := got["profile"].(map[string]any)
	if profile["changed_since_snapshot"] != true || profile["status"] != "conflict" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestBuildRuntimeContextDoesNotCountArtifactsFromOlderExecution(t *testing.T) {
	task := &model.Task{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.TaskKindContentGeneration, Type: model.PlatformMontage, Status: model.TaskStatusFailed}
	execution := &model.TaskExecution{
		ID:     "current-execution",
		Status: model.TaskExecutionFailed,
		AgentPackRequiredArtifactContract: []byte(`[{
			"role":"export_video","path":"output/exports/*.mp4","mime_type":"video/mp4","required":true
		}]`),
	}
	files := []*model.TaskFile{{
		ID:          "old-file",
		TaskID:      task.ID,
		ExecutionID: "old-execution",
		State:       model.TaskFileStateRetained,
		Role:        model.FileRoleVideo,
		FileName:    "final.mp4",
		FilePath:    "output/exports/final.mp4",
	}}

	got := buildRuntimeContextWithExecution(task, files, nil, execution)
	artifacts := got["artifacts"].(map[string]any)
	if artifacts["completed"] != 0 || artifacts["required"] != 1 || artifacts["missing"] != 1 {
		t.Fatalf("artifacts = %#v", artifacts)
	}
	items := artifacts["items"].([]map[string]any)
	if len(items) != 2 || items[0]["status"] != "retained" || items[1]["status"] != "missing" {
		t.Fatalf("items = %#v", items)
	}
}

func TestBuildRuntimeContextRequiresArtifactRoleAndPath(t *testing.T) {
	task := &model.Task{AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration, Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted}
	execution := &model.TaskExecution{
		ID:                                "current-execution",
		Status:                            model.TaskExecutionSucceeded,
		AgentPackRequiredArtifactContract: []byte(`[{"role":"final_markdown","path":"output/article.md","required":true}]`),
	}
	files := []*model.TaskFile{{
		ID: "wrong-role", TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStateDelivered,
		Role: model.FileRoleCover, FileName: "article.md", FilePath: "output/article.md",
	}}

	got := buildRuntimeContextWithExecution(task, files, nil, execution)
	artifacts := got["artifacts"].(map[string]any)
	if artifacts["completed"] != 0 || artifacts["required"] != 1 || artifacts["missing"] != 1 {
		t.Fatalf("artifacts = %#v", artifacts)
	}
}
