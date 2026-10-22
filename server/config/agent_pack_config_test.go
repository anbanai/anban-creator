package config

import (
	"strings"
	"testing"
)

func TestClaudeConfigRejectsServerOwnedMaxTurns(t *testing.T) {
	_, err := loadClaudeConfigYAML(t, strings.Replace(validClaudeConfigYAML, "claude:\n", "claude:\n  max_turns: {article: 42}\n", 1))
	if err == nil || !strings.Contains(err.Error(), `unknown claude config field "max_turns"`) {
		t.Fatalf("loading server max_turns override error = %v, want unknown claude config field", err)
	}
}
