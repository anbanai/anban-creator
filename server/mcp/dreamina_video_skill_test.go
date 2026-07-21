package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestObsoleteVideoSkillsAreNotDistributed(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	for _, plugin := range []string{"claudecode", "codex"} {
		for _, skill := range []string{"dreamina-video", "seedance-20"} {
			skillDir := filepath.Join(root, plugin, "skills", skill)
			if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
				t.Fatalf("%s must not distribute obsolete %s Skill, stat err = %v", plugin, skill, err)
			}
		}
	}
}
