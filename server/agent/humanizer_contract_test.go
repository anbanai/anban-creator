package agent

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const pinnedHumanizerRevision = "9862685f575c65a8247f90369951df1b3416e3d6"

func TestHumanizerSkillUsesOfficialNestedSubmodule(t *testing.T) {
	root := repoRoot(t)
	pluginRoot := filepath.Join(root, "harness")
	cmd := exec.Command("git", "ls-tree", "HEAD", "--", "skills/humanizer")
	cmd.Dir = pluginRoot
	entry, err := cmd.Output()
	if err != nil {
		t.Fatalf("read Humanizer gitlink: %v", err)
	}
	wantEntry := "160000 commit " + pinnedHumanizerRevision + "\tskills/humanizer\n"
	if got := string(entry); got != wantEntry {
		t.Fatalf("Humanizer gitlink = %q, want %q", got, wantEntry)
	}

	upstreamPath := filepath.Join(root, "harness", "skills", "humanizer", "SKILL.md")
	upstream := readRepoFile(t, upstreamPath)
	frontmatter := parseSkillFrontmatter(t, upstreamPath, upstream)
	if got := frontmatterStringValue(frontmatter["name"]); got != "humanizer" {
		t.Fatalf("upstream Humanizer name = %q, want humanizer", got)
	}
	metadata, ok := frontmatter["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("upstream Humanizer metadata = %#v, want a mapping", frontmatter["metadata"])
	}
	if got := frontmatterStringValue(metadata["version"]); got != "3.0.0" {
		t.Fatalf("upstream Humanizer version = %q, want pinned 3.0.0", got)
	}
	for _, want := range []string{
		"name: humanizer",
		"metadata:",
		`version: "3.0.0"`,
		"license: MIT",
	} {
		if !strings.Contains(upstream, want) {
			t.Fatalf("upstream Humanizer source missing %q", want)
		}
	}
}

func TestHumanizerSourceAndUpdateCommandAreDeclared(t *testing.T) {
	root := repoRoot(t)
	rootModules := readRepoFile(t, filepath.Join(root, ".gitmodules"))
	if strings.Contains(rootModules, `submodule "third_party/Humanizer"`) {
		t.Fatal("Anban Writer must not retain the old root Humanizer submodule")
	}

	pluginModules := readRepoFile(t, filepath.Join(root, "harness", ".gitmodules"))
	for _, want := range []string{
		`[submodule "skills/humanizer"]`,
		"path = skills/humanizer",
		"url = https://github.com/blader/humanizer.git",
		"branch = main",
	} {
		if !strings.Contains(pluginModules, want) {
			t.Fatalf("Creator Skills .gitmodules missing upstream Humanizer contract %q", want)
		}
	}

	script := readRepoFile(t, filepath.Join(root, "harness", "scripts", "update-humanizer.sh"))
	for _, want := range []string{
		"submodule_name=skills/humanizer",
		`submodule_root=$(git -C "$submodule_path" rev-parse --show-toplevel 2>/dev/null || true)`,
		`if [ "$submodule_root" != "$repo_root/$submodule_path" ]; then`,
		`git submodule update --init -- "$submodule_path"`,
		`git -C "$submodule_path" fetch --unshallow origin`,
		"git -C \"$submodule_path\" fetch --prune origin \"$branch\"",
		"git -C \"$submodule_path\" checkout --detach \"origin/$branch\"",
		"git diff --submodule=log",
		"make humanizer-check",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Creator Skills updater missing %q", want)
		}
	}
	if strings.Contains(script, `if ! git -C "$submodule_path" rev-parse --git-dir`) {
		t.Fatal("Creator Skills updater must not mistake the parent repository for an initialized nested submodule")
	}

	checkScript := readRepoFile(t, filepath.Join(root, "harness", "scripts", "check-humanizer.sh"))
	if !strings.Contains(checkScript, `submodule_root=$(git -C "$submodule_path" rev-parse --show-toplevel 2>/dev/null || true)`) ||
		!strings.Contains(checkScript, `if [ "$submodule_root" != "$repo_root/$submodule_path" ]; then`) {
		t.Fatal("Creator Skills checker must verify the nested repository boundary")
	}

	makefile := readRepoFile(t, filepath.Join(root, "harness", "Makefile"))
	if !strings.Contains(makefile, "humanizer-update:") ||
		!strings.Contains(makefile, "scripts/update-humanizer.sh") ||
		!strings.Contains(makefile, "humanizer-check:") ||
		!strings.Contains(makefile, "scripts/check-humanizer.sh") {
		t.Fatal("Creator Skills Makefile must expose the upstream Humanizer update and check commands")
	}

	rootScript := readRepoFile(t, filepath.Join(root, "scripts", "update-humanizer.sh"))
	for _, want := range []string{
		`git -C "$repo_root" submodule update --init --depth 1 -- harness`,
		`exec "$plugin_script"`,
	} {
		if !strings.Contains(rootScript, want) {
			t.Fatalf("Anban Writer updater missing Creator Skills delegation boundary %q", want)
		}
	}

	codexInstall := readRepoFile(t, filepath.Join(root, "harness", "docs", "codex-installation.md"))
	for _, want := range []string{
		"Codex marketplace Git sources do not initialize nested submodules",
		"git submodule update --init --recursive harness",
		"test -f harness/skills/humanizer/SKILL.md",
		"codex plugin add anban@anbanai",
	} {
		if !strings.Contains(codexInstall, want) {
			t.Fatalf("Codex installation guide missing nested Humanizer boundary %q", want)
		}
	}
	if strings.Contains(codexInstall, "codex plugin install") {
		t.Fatal("Codex installation guide must not use the nonexistent plugin install command")
	}
}

func TestHumanizerIsPreloadedOnlyByAgentsThatUseIt(t *testing.T) {
	root := repoRoot(t)
	for _, relPath := range []string{
		"harness/agents/article.md",
		"harness/agents/ecommerce.md",
		"harness/agents/moments.md",
		"harness/agents/seednote.md",
	} {
		body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relPath)))
		frontmatter := frontmatterBlock(t, body)
		if !strings.Contains(frontmatter, "\n  - humanizer") {
			t.Fatalf("%s must preload its Humanizer capability", relPath)
		}
	}

	for _, relPath := range []string{
		"harness/agents/article.toml",
		"harness/agents/ecommerce.toml",
		"harness/agents/moments.toml",
		"harness/agents/seednote.toml",
	} {
		body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relPath)))
		if !strings.Contains(body, `path = "__PLUGIN_ROOT__/skills/humanizer/SKILL.md"`) {
			t.Fatalf("%s must inject the bundled humanizer skill", relPath)
		}
	}

	for _, relPath := range []string{
		"harness/agents/seednote.md",
		"harness/agents/seednote.toml",
		"harness/skills/seednote-writing/SKILL.md",
	} {
		body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relPath)))
		for _, banned := range []string{"内置去 AI", "不要再调用 `humanizer` Skill", "额外加载通用 33 类规则会浪费上下文"} {
			if strings.Contains(body, banned) {
				t.Fatalf("%s still contains duplicate Seednote de-AI contract %q", relPath, banned)
			}
		}
	}
	for _, name := range []string{"Dockerfile.agent-article", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		dockerfile := readRepoFile(t, filepath.Join(root, "deploy", "docker", name))
		for _, want := range []string{
			"COPY harness/ /anbanai/",
			"ENV CLAUDE_PLUGIN_ROOT=/anbanai",
			"@anthropic-ai/claude-agent-sdk",
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
				"harness/skills/seednote-writing/SKILL.md",
				"harness/skills/seednote-writing/SKILL.md",
			},
			wants: []string{"不得调用 `AskUserQuestion`", "仍 ≤1000 字", "不得在改写中引入新的违禁词"},
		},
		{
			name: "article",
			relPaths: []string{
				"harness/skills/article/SKILL.md",
				"harness/skills/article/SKILL.md",
			},
			wants: []string{"不得调用 `AskUserQuestion`", "覆盖原文全部信息点", "不得引入新的违禁词或导流风险"},
		},
		{
			name: "ecommerce",
			relPaths: []string{
				"harness/skills/ecommerce-copywriting/SKILL.md",
				"harness/skills/ecommerce-copywriting/SKILL.md",
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
