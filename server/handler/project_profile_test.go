package handler

import (
	"encoding/json"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
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

func TestProfileAgentInputIncludesSetupAndConfirmedProfileProvenance(t *testing.T) {
	profile := model.NewProjectProfile()
	profile.Status = model.ProfileStatusConfirmed
	profile.InitializationStatus = model.ProfileInitializationReady
	profile.Version = 7
	profile.AnalysisTaskID = "previous-analysis"
	profile.Dimensions.Identity.Content["summary"] = "账号定位"
	profile.Dimensions.Identity.Sources = []string{"[链接分析]", "[推断待确认]"}
	profile.Dimensions.Identity.Evidence = []string{"公开主页简介"}
	project := &model.Project{
		ID: "project-1", Name: "示例项目", Platform: model.PlatformWechat,
		ProfileURL: "https://mp.weixin.qq.com/s/example", Profile: datatypes.NewJSONType(profile),
	}
	input := profileAgentInput(project, nil, nil)
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["current_profile"] == nil {
		t.Fatalf("current_profile = %#v, want confirmed project profile", decoded["current_profile"])
	}
	if decoded["expected_revision"] != float64(7) {
		t.Fatalf("expected_revision = %#v, want frozen revision 7", decoded["expected_revision"])
	}
	current := decoded["current_profile"].(map[string]any)
	if current["version"] != float64(7) || current["status"] != model.ProfileStatusConfirmed {
		t.Fatalf("current profile metadata = %#v, want confirmed version 7", current)
	}
	identity := current["dimensions"].(map[string]any)["identity"].(map[string]any)
	if len(identity["sources"].([]any)) != 2 || identity["evidence"].([]any)[0] != "公开主页简介" {
		t.Fatalf("current profile provenance = %#v", identity)
	}
	answers := decoded["answers"].(map[string]any)
	if answers["intent"] == nil || answers["content"] == nil || answers["boundaries"] == nil || answers["basic"] == nil {
		t.Fatalf("analysis answers = %#v, want all four Easel groups", answers)
	}
	if decoded["profile_setup"] == nil || decoded["profile_url"] != project.ProfileURL {
		t.Fatalf("analysis setup and compatibility URL = %#v/%#v", decoded["profile_setup"], decoded["profile_url"])
	}
}
