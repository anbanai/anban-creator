package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func managedRequiredMCPTools(taskType string) []string {
	switch strings.TrimSpace(taskType) {
	case model.PlatformSeednote:
		return []string{"analyze_image", "claim_topic", "finalize_task_title", "generate_image", "get_project_profile", "list_project_titles", "submit_agent_feedback"}
	case model.TaskTypeViralAnalysis:
		return []string{"get_project_profile", "list_project_titles", "submit_agent_feedback"}
	case model.TaskTypeLiveSlicer:
		return []string{"analyze_video", "build_live_clip_manifest", "build_live_clip_plan", "build_live_subject_clip_plan", "create_live_analysis_task", "get_media_pipeline_status", "prepare_file_upload", "query_live_analysis_task", "submit_agent_feedback"}
	case model.PlatformMontage:
		return []string{"analyze_image", "analyze_video", "generate_image", "get_project_profile", "submit_agent_feedback"}
	default:
		return nil
	}
}

func frontmatterBlock(t *testing.T, body string) string {
	t.Helper()
	if !strings.HasPrefix(body, "---\n") {
		t.Fatal("markdown file missing frontmatter start")
	}
	rest := strings.TrimPrefix(body, "---\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		t.Fatal("markdown file missing frontmatter end")
	}
	return rest[:idx]
}
