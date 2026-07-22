package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHumanizerSkillNormalizesPinnedUpstream(t *testing.T) {
	root := repoRoot(t)
	upstreamPath := filepath.Join(root, "third_party", "Humanizer", "SKILL.md")
	upstream := readRepoFile(t, upstreamPath)
	frontmatter := parseSkillFrontmatter(t, upstreamPath, upstream)
	if got := frontmatterStringValue(frontmatter["name"]); got != "humanizer" {
		t.Fatalf("upstream Humanizer name = %q, want humanizer", got)
	}
	if got := frontmatterStringValue(frontmatter["version"]); got != "2.8.2" {
		t.Fatalf("upstream Humanizer version = %q, want pinned 2.8.2", got)
	}
	for _, want := range []string{
		"name: humanizer",
		"version: 2.8.2",
		"license: MIT",
		"compatibility: any-agent",
		"  - AskUserQuestion",
	} {
		if !strings.Contains(upstream, want) {
			t.Fatalf("upstream Humanizer source missing %q", want)
		}
	}

	normalized := strings.ReplaceAll(upstream, "version: 2.8.2\n", "")
	normalized = strings.ReplaceAll(normalized, "compatibility: any-agent\n", "")
	for _, distro := range []string{"plugins"} {
		skillDir := filepath.Join(root, distro, "skills", "humanizer")
		path := filepath.Join(skillDir, "SKILL.md")
		if got := readRepoFile(t, path); got != normalized {
			t.Fatalf("%s must equal the cross-host normalized form of %s", path, upstreamPath)
		}
		referencesDir := filepath.Join(skillDir, "references")
		entries, err := os.ReadDir(referencesDir)
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read %s: %v", referencesDir, err)
		}
		if len(entries) != 0 {
			t.Fatalf("%s must not retain Anban-specific references", skillDir)
		}
	}
}

func TestHumanizerSourceAndUpdateCommandAreDeclared(t *testing.T) {
	root := repoRoot(t)
	gitmodules := readRepoFile(t, filepath.Join(root, ".gitmodules"))
	for _, want := range []string{
		`[submodule "third_party/Humanizer"]`,
		"path = third_party/Humanizer",
		"url = https://github.com/blader/humanizer.git",
		"branch = main",
		"shallow = true",
	} {
		if !strings.Contains(gitmodules, want) {
			t.Fatalf(".gitmodules missing upstream Humanizer contract %q", want)
		}
	}

	script := readRepoFile(t, filepath.Join(root, "scripts", "update-humanizer.sh"))
	for _, want := range []string{
		"git -C \"$submodule_path\" fetch --prune origin \"$branch\"",
		"git -C \"$submodule_path\" checkout --detach \"origin/$branch\"",
		"skill_dir=plugins/skills/humanizer",
		"rm -rf \"$skill_dir/references\"",
		"sed '/^version:[[:space:]]*/d; /^compatibility:[[:space:]]*/d' \"$source_skill\" > \"$destination\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("update-humanizer.sh missing %q", want)
		}
	}

	makefile := readRepoFile(t, filepath.Join(root, "Makefile"))
	if !strings.Contains(makefile, "humanizer-update:") ||
		!strings.Contains(makefile, "scripts/update-humanizer.sh") {
		t.Fatal("Makefile must expose the upstream Humanizer update command")
	}
}

func TestHumanizerIsPreloadedOnlyByAgentsThatUseIt(t *testing.T) {
	root := repoRoot(t)
	for _, relPath := range []string{
		"plugins/agents/wechatarticle.md",
		"plugins/agents/ecommerce.md",
		"plugins/agents/moments.md",
	} {
		body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relPath)))
		frontmatter := frontmatterBlock(t, body)
		if !strings.Contains(frontmatter, "\n  - humanizer") {
			t.Fatalf("%s must preload its Humanizer capability", relPath)
		}
	}

	seednoteAgent := readRepoFile(t, filepath.Join(root, "plugins", "agents", "seednote.md"))
	if strings.Contains(seednoteAgent, "anban:humanizer") || strings.Contains(seednoteAgent, "using the `humanizer` skill") {
		t.Fatal("Claude Seednote must use its compact built-in de-AI pass instead of loading the general Humanizer Skill")
	}

	for _, relPath := range []string{
		"plugins/agents/wechatarticle.toml",
		"plugins/agents/ecommerce.toml",
		"plugins/agents/moments.toml",
	} {
		body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relPath)))
		if !strings.Contains(body, `path = "__PLUGIN_ROOT__/skills/humanizer/SKILL.md"`) {
			t.Fatalf("%s must inject the bundled humanizer skill", relPath)
		}
	}
	codexSeednote := readRepoFile(t, filepath.Join(root, "plugins", "agents", "seednote.toml"))
	if strings.Contains(codexSeednote, `skills/humanizer/SKILL.md`) {
		t.Fatal("Codex Seednote must use seednote-writing's built-in de-AI pass instead of preloading Humanizer")
	}

	for _, name := range []string{"Dockerfile.agent-article", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		dockerfile := readRepoFile(t, filepath.Join(root, "deploy", "docker", name))
		for _, want := range []string{
			"COPY plugins/ /anbanai/",
			"claude plugin install --scope user anban@anbanai",
			`cp -a "$HOME/.claude/plugins/cache/anbanai" "$ANBAN_HOME_TEMPLATE/.claude/plugins/cache/anbanai"`,
		} {
			if !strings.Contains(dockerfile, want) {
				t.Fatalf("%s missing Humanizer injection boundary %q", name, want)
			}
		}
	}
}

func TestHumanizerBusinessConstraintsStayInOwningWorkflows(t *testing.T) {
	root := repoRoot(t)
	tests := []struct {
		name     string
		relPaths []string
		wants    []string
	}{
		{
			name: "seednote",
			relPaths: []string{
				"plugins/skills/seednote-writing/SKILL.md",
				"plugins/skills/seednote-writing/SKILL.md",
			},
			wants: []string{"不得调用 `AskUserQuestion`", "仍 ≤1000 字", "不得在改写中引入新的违禁词"},
		},
		{
			name: "article",
			relPaths: []string{
				"plugins/skills/article/SKILL.md",
				"plugins/skills/article/SKILL.md",
			},
			wants: []string{"不得调用 `AskUserQuestion`", "覆盖原文全部信息点", "不得引入新的违禁词或导流风险"},
		},
		{
			name: "ecommerce",
			relPaths: []string{
				"plugins/skills/ecommerce-copywriting/SKILL.md",
				"plugins/skills/ecommerce-copywriting/SKILL.md",
			},
			wants: []string{"不得调用 `AskUserQuestion`", "FABE 信息点", "先去 AI，后合规"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, relPath := range tc.relPaths {
				body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relPath)))
				for _, want := range tc.wants {
					if !strings.Contains(body, want) {
						t.Fatalf("%s missing owning workflow constraint %q", relPath, want)
					}
				}
			}
		})
	}
}
