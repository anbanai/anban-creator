package mcp

import (
	"os"
	"strings"
	"testing"
)

func TestRepositoryInstructionsDefineMCPAsCapabilityTransport(t *testing.T) {
	for _, path := range []string{"../../CLAUDE.md", "../../AGENTS.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, want := range []string{
			"MCP is a stateless capability transport",
			"Agents and Skills own business workflow orchestration",
			"one application capability",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
	}
}
