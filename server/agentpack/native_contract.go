package agentpack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

type claudeAgentFrontmatter struct {
	Name     string `yaml:"name"`
	MaxTurns int    `yaml:"maxTurns"`
}

type codexAgentMetadata struct {
	Name string `toml:"name"`
}

func validateNativeAgentSources(manifest *Manifest) error {
	claudePath := filepath.Join(manifest.dir, manifest.Agent.ClaudeSource)
	claudeBody, err := os.ReadFile(claudePath)
	if err != nil {
		return fmt.Errorf("read Claude Agent source: %w", err)
	}
	frontmatter, err := parseClaudeAgentFrontmatter(claudeBody)
	if err != nil {
		return fmt.Errorf("Claude Agent source %q: %w", manifest.Agent.ClaudeSource, err)
	}
	if frontmatter.Name != manifest.Agent.Name {
		return fmt.Errorf("Claude Agent name %q does not match manifest agent.name %q", frontmatter.Name, manifest.Agent.Name)
	}
	if frontmatter.MaxTurns != manifest.Agent.MaxTurns {
		return fmt.Errorf("Claude Agent maxTurns %d does not match manifest agent.max_turns %d", frontmatter.MaxTurns, manifest.Agent.MaxTurns)
	}

	codexPath := filepath.Join(manifest.dir, manifest.Agent.CodexSource)
	codexBody, err := os.ReadFile(codexPath)
	if err != nil {
		return fmt.Errorf("read Codex Agent source: %w", err)
	}
	var metadata codexAgentMetadata
	if _, err := toml.Decode(string(codexBody), &metadata); err != nil {
		return fmt.Errorf("decode Codex Agent source %q: %w", manifest.Agent.CodexSource, err)
	}
	if metadata.Name != manifest.Agent.Name {
		return fmt.Errorf("Codex Agent name %q does not match manifest agent.name %q", metadata.Name, manifest.Agent.Name)
	}
	return nil
}

func parseClaudeAgentFrontmatter(body []byte) (claudeAgentFrontmatter, error) {
	normalized := strings.ReplaceAll(string(body), "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return claudeAgentFrontmatter{}, fmt.Errorf("frontmatter is required")
	}
	end := strings.Index(normalized[len("---\n"):], "\n---\n")
	if end < 0 {
		return claudeAgentFrontmatter{}, fmt.Errorf("frontmatter is not terminated")
	}
	raw := normalized[len("---\n") : len("---\n")+end]
	var frontmatter claudeAgentFrontmatter
	if err := yaml.Unmarshal([]byte(raw), &frontmatter); err != nil {
		return claudeAgentFrontmatter{}, fmt.Errorf("decode frontmatter: %w", err)
	}
	return frontmatter, nil
}
