package model

import (
	"encoding/json"
	"testing"
)

func TestProjectProfileContract(t *testing.T) {
	profile := NewProjectProfile()
	profile.Dimensions.Identity.Content["name"] = "示例账号"
	profile.Dimensions.Identity.Sources = []string{"[用户确认]"}
	profile.Dimensions.Style.Evidence = []string{"样本标题与正文节奏"}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ProjectProfile
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != 1 || decoded.Status != ProfileStatusDraft || decoded.Dimensions.Identity.Content["name"] != "示例账号" {
		t.Fatalf("profile contract mismatch: %#v", decoded)
	}
	if got := ProfileDimensions(); len(got) != 6 || got[0] != "identity" || got[5] != "memory" {
		t.Fatalf("dimensions = %#v", got)
	}
}

func TestProjectProfileValidateRequiresCompleteDimensionsAndKnownSources(t *testing.T) {
	profile := NewProjectProfile()
	if err := profile.Validate(); err != nil {
		t.Fatalf("new profile should validate: %v", err)
	}
	profile.Dimensions.Identity.Sources = []string{"model guessed"}
	if err := profile.Validate(); err == nil {
		t.Fatal("unknown source tag should be rejected")
	}
	profile = NewProjectProfile()
	profile.Dimensions.Style.Content = nil
	if err := profile.Validate(); err == nil {
		t.Fatal("nil dimension content should be rejected")
	}
	profile = NewProjectProfile()
	profile.SchemaVersion = 2
	if err := profile.Validate(); err == nil {
		t.Fatal("unknown schema should be rejected")
	}
}

func TestProjectProfileSupportsLifecycleStateAndUserEdits(t *testing.T) {
	profile := NewProjectProfile()
	if profile.InitializationStatus != ProfileInitializationNotStarted {
		t.Fatalf("initialization status = %q, want %q", profile.InitializationStatus, ProfileInitializationNotStarted)
	}
	profile.InitializationStatus = ProfileInitializationReady
	profile.Dimensions.Identity.Sources = []string{"[用户编辑]"}
	if err := profile.Validate(); err != nil {
		t.Fatalf("user-edited ready profile should validate: %v", err)
	}
}
