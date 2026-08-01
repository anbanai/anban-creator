package agentpack

import (
	"fmt"
	"os"
	"path/filepath"
)

type ScaffoldOptions struct {
	ID             string
	Kind           string
	TaskType       string
	RuntimeProfile string
	Adapter        string
}

func Scaffold(pluginRoot string, options ScaffoldOptions) error {
	if !kebabIDPattern.MatchString(options.ID) {
		return fmt.Errorf("ID must use kebab-case")
	}
	if options.Kind != KindPlugin && options.Kind != KindManaged {
		return fmt.Errorf("kind must be %q or %q", KindPlugin, KindManaged)
	}
	if options.Kind == KindManaged {
		if options.TaskType == "" || options.RuntimeProfile == "" {
			return fmt.Errorf("managed Pack requires task type and runtime profile")
		}
		if options.Adapter != AdapterStandard && options.Adapter != AdapterOpenMontage {
			return fmt.Errorf("unsupported runtime adapter %q", options.Adapter)
		}
	}
	packDir := filepath.Join(pluginRoot, "packs", options.ID)
	if _, err := os.Stat(packDir); err == nil {
		return fmt.Errorf("Agent Pack %q already exists", options.ID)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		return err
	}

	manifest := fmt.Sprintf("id: %s\nversion: 1.0.0\nkind: %s\ndisplay_name: %s\ndescription: %s Agent Pack\nagent:\n  name: %s\n  claude_source: agent.claude.md\n  codex_source: agent.codex.toml\n  skills: []\n  max_turns: 60\n", options.ID, options.Kind, options.ID, options.ID, options.ID)
	if options.Kind == KindManaged {
		manifest += fmt.Sprintf("bindings:\n  task_types: [%s]\nruntime:\n  profile: %s\n  adapter: %s\n  max_turns: 40\nsurfaces: [plugin]\n", options.TaskType, options.RuntimeProfile, options.Adapter)
	} else {
		manifest += "bindings: {}\nruntime: {}\nsurfaces: [plugin]\n"
	}
	manifest += "features: []\nprogress: []\nartifacts: []\n"
	files := map[string]string{
		"agent-pack.yaml":  manifest,
		"agent.claude.md":  fmt.Sprintf("---\nname: %s\ndescription: %s Agent Pack\nmodel: inherit\nmaxTurns: 60\n---\n\n# %s\n", options.ID, options.ID, options.ID),
		"agent.codex.toml": fmt.Sprintf("# Codex subagent: %s\nname = %q\ndescription = %q\nmodel_reasoning_effort = \"medium\"\nsandbox_mode = \"workspace-write\"\ndeveloper_instructions = \"\"\"\n# %s\n\"\"\"\n", options.ID, options.ID, options.ID+" Agent Pack", options.ID),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(packDir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}
