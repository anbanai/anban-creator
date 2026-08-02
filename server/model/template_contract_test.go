package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestTemplateJSONUsesCanonicalPromptOnly(t *testing.T) {
	raw, err := json.Marshal(Template{Prompt: "暖色晨光，留白排版"})
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal template JSON: %v", err)
	}
	if payload["prompt"] != "暖色晨光，留白排版" {
		t.Fatalf("prompt = %v, want canonical prompt", payload["prompt"])
	}
	for _, legacy := range []string{"style_prompt", "visual_style"} {
		if _, exists := payload[legacy]; exists {
			t.Fatalf("template JSON retained legacy alias %q: %s", legacy, raw)
		}
	}
}

func TestSeednoteTemplateCategoriesAreFixed(t *testing.T) {
	want := [...]string{"好物种草", "美妆护肤", "健康养生", "美食生活", "家居家装", "知识科普"}
	if !reflect.DeepEqual(SeednoteTemplateCategories, want) {
		t.Fatalf("SeednoteTemplateCategories = %v, want %v", SeednoteTemplateCategories, want)
	}
	for _, category := range want {
		if !IsSeednoteTemplateCategory(category) {
			t.Errorf("IsSeednoteTemplateCategory(%q) = false", category)
		}
	}
	if IsSeednoteTemplateCategory("其他") {
		t.Fatal("unlisted category 其他 was accepted")
	}
}

func TestUserJSONIncludesIsAdmin(t *testing.T) {
	raw, err := json.Marshal(User{IsAdmin: true})
	if err != nil {
		t.Fatalf("marshal user: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal user JSON: %v", err)
	}
	if payload["is_admin"] != true {
		t.Fatalf("is_admin = %v, want true", payload["is_admin"])
	}
}

func TestProjectJSONDoesNotBindLegacyTemplateID(t *testing.T) {
	raw, err := json.Marshal(Project{})
	if err != nil {
		t.Fatalf("marshal project: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal project: %v", err)
	}
	for _, field := range []string{"template_id", "created_from_template_id"} {
		if _, exists := payload[field]; exists {
			t.Fatalf("project JSON retained legacy template binding %q: %s", field, raw)
		}
	}
}
