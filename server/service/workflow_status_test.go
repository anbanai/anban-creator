package service

import (
	"encoding/json"
	"testing"

	"github.com/royalrick/anbanwriter/server/model"
)

func TestDetermineWorkflowArtifactRole(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		mimeType string
		want     string
	}{
		{"topic json", "01-topic.json", "application/json", model.FileRoleTopic},
		{"outline markdown", "02-outline.md", "text/markdown", model.FileRoleOutline},
		{"draft markdown", "03-draft.md", "text/markdown", model.FileRoleDraft},
		{"final markdown", "04-final.md", "text/markdown", model.FileRoleFinalMarkdown},
		{"article html", "05-article.html", "text/html", model.FileRoleHTML},
		{"cover image", "cover.png", "image/png", model.FileRoleCover},
		{"images manifest", "images.json", "application/json", model.FileRoleImageManifest},
		{"draft package", "draft.json", "application/json", model.FileRoleDraftPackage},
		{"review", "review.json", "application/json", model.FileRoleReview},
		{"video", "clip.mp4", "video/mp4", model.FileRoleVideo},
		{"fallback image", "img_01.png", "image/png", model.FileRoleImage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineWorkflowArtifactRole(tt.fileName, tt.mimeType)
			if got != tt.want {
				t.Fatalf("DetermineWorkflowArtifactRole(%q, %q) = %q, want %q", tt.fileName, tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestDetermineTaskFileRoleUsesWorkflowRoles(t *testing.T) {
	got := DetermineTaskFileRole("review.json", "application/json")
	if got != model.FileRoleReview {
		t.Fatalf("DetermineTaskFileRole(review.json) = %q, want %q", got, model.FileRoleReview)
	}
}

func TestBuildWorkflowStatusArticleWithReview(t *testing.T) {
	files := []*model.TaskFile{
		{FileName: "01-topic.json", MimeType: "application/json", FilePath: "01-topic.json"},
		{FileName: "02-outline.md", MimeType: "text/markdown", FilePath: "02-outline.md"},
		{FileName: "03-draft.md", MimeType: "text/markdown", FilePath: "03-draft.md"},
		{FileName: "04-final.md", MimeType: "text/markdown", FilePath: "04-final.md"},
		{FileName: "05-article.html", MimeType: "text/html", FilePath: "05-article.html"},
		{FileName: "cover.png", MimeType: "image/png", FilePath: "cover.png"},
		{FileName: "draft.json", MimeType: "application/json", FilePath: "draft.json"},
		{FileName: "review.json", MimeType: "application/json", FilePath: "review.json"},
	}
	review := []byte(`{
		"overall_score": 82,
		"readiness": "ready_with_minor_edits",
		"strengths": ["账号匹配"],
		"risks": ["标题还可以更具体"],
		"next_actions": ["微调标题"]
	}`)

	status, err := BuildWorkflowStatus(model.ScopeArticle, files, review)
	if err != nil {
		t.Fatalf("BuildWorkflowStatus() unexpected error: %v", err)
	}

	if status.Version != WorkflowVersionCreationV1 {
		t.Fatalf("Version = %q, want %q", status.Version, WorkflowVersionCreationV1)
	}
	if status.CurrentStage != WorkflowStageReview {
		t.Fatalf("CurrentStage = %q, want %q", status.CurrentStage, WorkflowStageReview)
	}
	if status.Review == nil {
		t.Fatal("Review is nil")
	}
	if status.Review.OverallScore != 82 {
		t.Fatalf("Review.OverallScore = %d, want 82", status.Review.OverallScore)
	}

	stageByKey := make(map[string]WorkflowStage, len(status.Stages))
	for _, stage := range status.Stages {
		stageByKey[stage.Key] = stage
	}
	if stageByKey[WorkflowStageDraft].Status != WorkflowStageStatusCompleted {
		t.Fatalf("draft status = %q, want completed", stageByKey[WorkflowStageDraft].Status)
	}
	if len(stageByKey[WorkflowStageReview].ArtifactPaths) != 1 || stageByKey[WorkflowStageReview].ArtifactPaths[0] != "review.json" {
		t.Fatalf("review artifacts = %#v, want [review.json]", stageByKey[WorkflowStageReview].ArtifactPaths)
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("marshal status returned empty JSON")
	}
}

func TestBuildWorkflowStatusWarnsOnInvalidReview(t *testing.T) {
	files := []*model.TaskFile{
		{FileName: "04-final.md", MimeType: "text/markdown", FilePath: "04-final.md"},
		{FileName: "review.json", MimeType: "application/json", FilePath: "review.json"},
	}

	status, err := BuildWorkflowStatus(model.ScopeArticle, files, []byte(`{"overall_score":`))
	if err != nil {
		t.Fatalf("BuildWorkflowStatus() unexpected error: %v", err)
	}
	if status.Review != nil {
		t.Fatal("Review should be nil for invalid review JSON")
	}
	if len(status.Warnings) != 1 {
		t.Fatalf("Warnings len = %d, want 1", len(status.Warnings))
	}
	if status.Warnings[0].Code != "invalid_review_json" {
		t.Fatalf("warning code = %q, want invalid_review_json", status.Warnings[0].Code)
	}
}
