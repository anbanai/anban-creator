package agent

import (
	"encoding/json"
	"testing"
)

func TestBuildAutoMemorySettingsJSON(t *testing.T) {
	got, err := buildAutoMemorySettingsJSON("/workspace/task-1/.claude/memory")
	if err != nil {
		t.Fatalf("buildAutoMemorySettingsJSON() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("settings JSON is invalid: %v", err)
	}
	if decoded["autoMemoryDirectory"] != "/workspace/task-1/.claude/memory" {
		t.Fatalf("autoMemoryDirectory = %#v", decoded["autoMemoryDirectory"])
	}
}
