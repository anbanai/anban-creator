package handler

import (
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestDecorateProfileDraftWithServerMetadata(t *testing.T) {
	current := model.NewProjectProfile()
	current.Version = 3
	current.AnalysisTaskID = "analysis-task"
	draft := model.NewProjectProfile()
	draft.Status = model.ProfileStatusDraft

	got := decorateProfileDraft(draft, current)
	if got.Version != current.Version || got.AnalysisTaskID != current.AnalysisTaskID {
		t.Fatalf("draft metadata = version %d task %q, want version %d task %q", got.Version, got.AnalysisTaskID, current.Version, current.AnalysisTaskID)
	}
	if got.Status != model.ProfileStatusDraft {
		t.Fatalf("draft status = %q, want draft", got.Status)
	}
}
