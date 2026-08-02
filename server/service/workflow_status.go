package service

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

const (
	WorkflowVersionCreationV1 = "creation_workflow_v1"

	WorkflowStageTopic             = "topic"
	WorkflowStageOutline           = "outline"
	WorkflowStageDraft             = "draft"
	WorkflowStageHumanize          = "humanize"
	WorkflowStageSEO               = "seo"
	WorkflowStageCover             = "cover"
	WorkflowStageHTML              = "html"
	WorkflowStageDraftPackage      = "draft_package"
	WorkflowStagePublishOptional   = "publish_optional"
	WorkflowStageReview            = "review"
	WorkflowStageContentScript     = "content_script"
	WorkflowStageVisualPlan        = "visual_plan"
	WorkflowStageImages            = "images"
	WorkflowStageSourceNote        = "source_note"
	WorkflowStageEvidenceAnalysis  = "evidence_analysis"
	WorkflowStageTemplateArtifacts = "template_artifacts"

	WorkflowStageStatusPending   = "pending"
	WorkflowStageStatusCompleted = "completed"
	WorkflowStageStatusWarning   = "warning"
)

// WorkflowStatus is stored as JSON on a task and rendered by Studio.
type WorkflowStatus struct {
	Version      string            `json:"version"`
	CurrentStage string            `json:"current_stage"`
	Stages       []WorkflowStage   `json:"stages"`
	Warnings     []WorkflowWarning `json:"warnings,omitempty"`
	Review       *WorkflowReview   `json:"review,omitempty"`
}

type WorkflowStage struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	Status        string   `json:"status"`
	ArtifactPaths []string `json:"artifact_paths,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type WorkflowWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type WorkflowReview struct {
	OverallScore int               `json:"overall_score"`
	Readiness    string            `json:"readiness"`
	Scores       map[string]int    `json:"scores,omitempty"`
	Strengths    []string          `json:"strengths,omitempty"`
	Risks        []string          `json:"risks,omitempty"`
	NextActions  []string          `json:"next_actions,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

func DetermineWorkflowArtifactRole(fileName, mimeType string) string {
	base := strings.ToLower(filepath.Base(fileName))
	ext := strings.ToLower(filepath.Ext(base))

	switch base {
	case "01-topic.json", "topic.json":
		return model.FileRoleTopic
	case "02-outline.md", "outline.md":
		return model.FileRoleOutline
	case "03-draft.md", "draft.md":
		return model.FileRoleDraft
	case "04-final.md", "final.md", "article-final.md":
		return model.FileRoleFinalMarkdown
	case "images.json":
		return model.FileRoleImageManifest
	case "draft.json":
		return model.FileRoleDraftPackage
	case "review.json":
		return model.FileRoleReview
	}

	if strings.HasPrefix(base, "cover.") {
		return model.FileRoleCover
	}
	if ext == ".html" || ext == ".htm" || mimeType == "text/html" {
		return model.FileRoleHTML
	}
	if strings.HasPrefix(mimeType, "image/") {
		return model.FileRoleImage
	}
	if ext == ".md" || ext == ".markdown" {
		return model.FileRoleMarkdown
	}
	return model.FileRoleOther
}

func BuildWorkflowStatus(taskType string, files []*model.TaskFile, reviewJSON []byte) (*WorkflowStatus, error) {
	if taskType == model.TaskTypeViralAnalysis {
		return buildViralAnalysisWorkflowStatus(files), nil
	}
	artifactPathsByRole := make(map[string][]string)
	for _, file := range files {
		role := DetermineWorkflowArtifactRole(file.FileName, file.MimeType)
		path := file.FilePath
		if path == "" {
			path = file.FileName
		}
		artifactPathsByRole[role] = append(artifactPathsByRole[role], path)
	}
	for role := range artifactPathsByRole {
		sort.Strings(artifactPathsByRole[role])
	}

	status := &WorkflowStatus{
		Version: WorkflowVersionCreationV1,
		Stages:  buildWorkflowStages(taskType, artifactPathsByRole),
	}
	status.CurrentStage = currentWorkflowStage(status.Stages)

	if len(reviewJSON) > 0 {
		var review WorkflowReview
		if err := json.Unmarshal(reviewJSON, &review); err != nil {
			status.Warnings = append(status.Warnings, WorkflowWarning{
				Code:    "invalid_review_json",
				Message: fmt.Sprintf("review.json could not be parsed: %v", err),
			})
			markStageWarning(status.Stages, WorkflowStageReview, "review.json could not be parsed")
		} else {
			status.Review = &review
		}
	}

	return status, nil
}

func buildViralAnalysisWorkflowStatus(files []*model.TaskFile) *WorkflowStatus {
	pathsByName := make(map[string][]string)
	for _, file := range files {
		if file == nil {
			continue
		}
		name := strings.ToLower(filepath.Base(file.FileName))
		path := file.FilePath
		if path == "" {
			path = file.FileName
		}
		pathsByName[name] = append(pathsByName[name], path)
	}
	for name := range pathsByName {
		sort.Strings(pathsByName[name])
	}
	templatePaths := copyStrings(pathsByName["viral-template.json"])
	sort.Strings(templatePaths)
	stages := []WorkflowStage{
		buildStage(WorkflowStageSourceNote, "源笔记", pathsByName["source-analysis.md"]),
		buildStage(WorkflowStageEvidenceAnalysis, "证据分析", pathsByName["source-analysis.md"]),
		buildStage(WorkflowStageTemplateArtifacts, "模板产物", templatePaths),
	}
	return &WorkflowStatus{Version: WorkflowVersionCreationV1, CurrentStage: currentWorkflowStage(stages), Stages: stages}
}

func buildWorkflowStages(taskType string, artifactPathsByRole map[string][]string) []WorkflowStage {
	return []WorkflowStage{
		buildStage(WorkflowStageTopic, "选题", artifactPathsByRole[model.FileRoleTopic]),
		buildStage(WorkflowStageOutline, "大纲", artifactPathsByRole[model.FileRoleOutline]),
		buildStage(WorkflowStageDraft, "初稿", artifactPathsByRole[model.FileRoleDraft]),
		buildStage(WorkflowStageHumanize, "去 AI 味", artifactPathsByRole[model.FileRoleFinalMarkdown]),
		buildStage(WorkflowStageSEO, "标题与 SEO", nil),
		buildStage(WorkflowStageCover, "封面", artifactPathsByRole[model.FileRoleCover]),
		buildStage(WorkflowStageHTML, "排版 HTML", artifactPathsByRole[model.FileRoleHTML]),
		buildStage(WorkflowStageDraftPackage, "草稿包", artifactPathsByRole[model.FileRoleDraftPackage]),
		buildStage(WorkflowStagePublishOptional, "发布草稿", nil),
		buildStage(WorkflowStageReview, "质量复盘", artifactPathsByRole[model.FileRoleReview]),
	}
}

func buildStage(key, label string, artifactPaths []string) WorkflowStage {
	stage := WorkflowStage{
		Key:    key,
		Label:  label,
		Status: WorkflowStageStatusPending,
	}
	if len(artifactPaths) > 0 {
		stage.ArtifactPaths = copyStrings(artifactPaths)
		sort.Strings(stage.ArtifactPaths)
		stage.Status = WorkflowStageStatusCompleted
	}
	return stage
}

func currentWorkflowStage(stages []WorkflowStage) string {
	for i := len(stages) - 1; i >= 0; i-- {
		if stages[i].Status != WorkflowStageStatusPending {
			return stages[i].Key
		}
	}
	if len(stages) == 0 {
		return ""
	}
	return stages[0].Key
}

func markStageWarning(stages []WorkflowStage, key, message string) {
	for i := range stages {
		if stages[i].Key == key {
			stages[i].Status = WorkflowStageStatusWarning
			stages[i].Error = message
			return
		}
	}
}

func copyStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
