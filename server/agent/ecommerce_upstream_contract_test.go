package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
)

const ecommerceUpstreamPath = "skills/ecommerce-visual-copywriting-skill"

func TestEcommerceUpstreamSubmoduleIsPinnedReadOnly(t *testing.T) {
	root := repoRoot(t)
	gitmodules := readRepoFile(t, filepath.Join(root, "harness", ".gitmodules"))
	block := submoduleBlock(t, gitmodules, ecommerceUpstreamPath)
	for _, want := range []string{
		"url = https://github.com/feichanggege/ecommerce-visual-copywriting-skill.git",
		"branch = main",
		"path = " + ecommerceUpstreamPath,
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("harness/.gitmodules submodule %s missing %q", ecommerceUpstreamPath, want)
		}
	}

	upstreamRoot := filepath.Join(root, "harness", filepath.FromSlash(ecommerceUpstreamPath))
	for _, required := range []string{
		"SKILL.md",
		"LICENSE",
		"references/compliance-rules.md",
		"references/copy-quality-rules.md",
		"references/output-contracts.md",
		"references/platform-playbooks.md",
		"references/source-notes.md",
		"references/category-notes.md",
		"scripts/count-copy.py",
		"scripts/scan-final-copy.py",
	} {
		if _, err := os.Stat(filepath.Join(upstreamRoot, filepath.FromSlash(required))); err != nil {
			t.Fatalf("ecommerce upstream submodule missing %s; initialize it with git submodule update --init --recursive harness: %v", required, err)
		}
	}

	body := readRepoFile(t, filepath.Join(upstreamRoot, "SKILL.md"))
	for _, want := range []string{"证据账本", "KV1识别自检", "五维 Pass/Fail", "自审记录", "图内文案规格"} {
		if !strings.Contains(body, want) {
			t.Fatalf("pinned ecommerce upstream SKILL.md missing contract marker %q", want)
		}
	}

	info, err := os.Stat(filepath.Join(root, "harness", "scripts", "check-ecommerce-upstream.sh"))
	if err != nil {
		t.Fatalf("missing upstream check script: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("check-ecommerce-upstream.sh must be executable")
	}
}

func TestEcommerceUpstreamSubmodulePinIsRecordedAsGitlink(t *testing.T) {
	root := repoRoot(t)
	const pinned = "7fd4abaa5a280dddf414ccc1372d886bef702cba"

	cmd := exec.Command("git", "-C", filepath.Join(root, "harness"), "ls-files", "--stage", "--", ecommerceUpstreamPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git ls-files harness/%s: %v: %s", ecommerceUpstreamPath, err, output)
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 || fields[0] != "160000" || fields[1] != pinned {
		t.Fatalf("harness/%s must be a 160000 gitlink pinned at %s; got %q", ecommerceUpstreamPath, pinned, strings.TrimSpace(string(output)))
	}

	cmd = exec.Command("git", "-C", filepath.Join(root, "harness", filepath.FromSlash(ecommerceUpstreamPath)), "rev-parse", "HEAD")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse ecommerce upstream HEAD: %v: %s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != pinned {
		t.Fatalf("ecommerce upstream checkout = %s, want pinned %s", got, pinned)
	}
}

func TestEcommerceEntrypointPersistsGateABeforeGeneration(t *testing.T) {
	root := repoRoot(t)
	body := readRepoFile(t, filepath.Join(root, "harness", "skills", "ecommerce", "SKILL.md"))
	for _, want := range []string{
		"Gate A 写入 `output/quality-gate.md`",
		"追加 Gate B",
		"若宿主未注入该变量",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("ecommerce entrypoint missing quality-gate/upstream fallback contract %q", want)
		}
	}
}

func TestEcommercePackRequiresQualityGateArtifact(t *testing.T) {
	root := repoRoot(t)
	catalog, err := agentpack.LoadCatalog(filepath.Join(root, "harness"))
	if err != nil {
		t.Fatalf("load Agent Pack catalog: %v", err)
	}
	pack, ok := catalog.Pack("ecommerce")
	if !ok {
		t.Fatal("ecommerce Pack missing")
	}

	var artifact *agentpack.ArtifactSpec
	for index := range pack.Artifacts {
		if pack.Artifacts[index].Path == "output/quality-gate.md" {
			artifact = &pack.Artifacts[index]
			break
		}
	}
	if artifact == nil {
		t.Fatal("ecommerce Pack must declare output/quality-gate.md as an artifact")
	}
	if !artifact.Required {
		t.Fatal("output/quality-gate.md must be a required artifact so the completion gate enforces it")
	}
	if artifact.Role != "quality_gate" || artifact.MIMEType != "text/markdown" {
		t.Fatalf("quality gate artifact = %#v, want role quality_gate with text/markdown", *artifact)
	}

	delivered := false
	for _, delivery := range pack.Delivery {
		if delivery.Path == "output/quality-gate.md" && delivery.Role == "quality_gate" {
			delivered = true
			break
		}
	}
	if !delivered {
		t.Fatal("output/quality-gate.md must be part of the delivery contract")
	}
}

func TestEcommerceAgentsReusePinnedUpstreamSOP(t *testing.T) {
	root := repoRoot(t)
	tests := []struct {
		path       string
		pluginRoot string
	}{
		{path: "harness/packs/ecommerce/agent.claude.md", pluginRoot: "$CLAUDE_PLUGIN_ROOT"},
		{path: "harness/packs/ecommerce/agent.codex.toml", pluginRoot: "__PLUGIN_ROOT__"},
		{path: "harness/agents/ecommerce.md", pluginRoot: "$CLAUDE_PLUGIN_ROOT"},
		{path: "harness/agents/ecommerce.toml", pluginRoot: "__PLUGIN_ROOT__"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(tc.path)))
			for _, want := range []string{
				tc.pluginRoot + "/" + ecommerceUpstreamPath,
				"UPSTREAM_ECOMMERCE_SKILL=",
				"upstream_skill_unavailable",
				"resume_from=bootstrap",
				"output/quality-gate.md",
				"证据账本",
				"Campaign Style Lock",
				"KV1",
				"80 分",
				"2 轮",
				"text_rendering=degraded",
				"upstream-copy-mapping.md",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing pinned upstream contract term %q", tc.path, want)
				}
			}
			for _, forbidden := range []string{"openai.yaml", "留白排版为默认", "先发开工问卷"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s must not adopt interactive upstream behavior %q", tc.path, forbidden)
				}
			}
		})
	}
}

func TestEcommerceSkillsDelegateToUpstreamAdapters(t *testing.T) {
	root := repoRoot(t)
	tests := []struct {
		skill    string
		adapter  string
		contains []string
	}{
		{
			skill:    "ecommerce-product-analysis/SKILL.md",
			adapter:  "references/upstream-evidence-mapping.md",
			contains: []string{"商品名称与命名来源", "证据", "product-extraction-prompt.md"},
		},
		{
			skill:    "ecommerce-copywriting/SKILL.md",
			adapter:  "references/upstream-copy-mapping.md",
			contains: []string{"证据账本", "成交驱动力", "count-copy", "先去 AI，后合规"},
		},
		{
			skill:    "ecommerce-visual-design/SKILL.md",
			adapter:  "references/upstream-visual-mapping.md",
			contains: []string{"Campaign Style Lock", "Storyboard", "KV1", "显式传 `aspect_ratio`", "text_verbatim", "kv1_identifiable", "text_rendering_status"},
		},
		{
			skill:    "ecommerce-platform-specs/SKILL.md",
			adapter:  "references/upstream-compliance-mapping.md",
			contains: []string{"宣称", "compliance-rules.md", "平台规则"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.skill, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join(root, "harness", "skills", filepath.FromSlash(tc.skill)))
			if !strings.Contains(body, tc.adapter) {
				t.Fatalf("%s must link its upstream adapter %s", tc.skill, tc.adapter)
			}
			for _, want := range tc.contains {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing adapted contract term %q", tc.skill, want)
				}
			}
		})
	}

	mainImage := readRepoFile(t, filepath.Join(root, "harness", "skills", "ecommerce-visual-design", "references", "main-image.md"))
	for _, want := range []string{"KV1 识别自检", "商品名/品类词", "≤12 汉字"} {
		if !strings.Contains(mainImage, want) {
			t.Fatalf("main-image.md missing KV1 identification rule %q", want)
		}
	}
}

func TestEcommerceRuntimeProvidesPythonForUpstreamScripts(t *testing.T) {
	root := repoRoot(t)
	body := readRepoFile(t, filepath.Join(root, "deploy", "docker", "Dockerfile.agent-ecommerce"))
	for _, want := range []string{
		"python3",
		"apt-get -o Acquire::Retries=5",
		"ln -sf /usr/bin/python3 /usr/local/bin/python",
		"COPY harness/ /anbanai/",
		"test -f /anbanai/skills/ecommerce-visual-copywriting-skill/SKILL.md",
		"ENV CLAUDE_PLUGIN_ROOT=/anbanai",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Dockerfile.agent-ecommerce missing python/upstream runtime term %q", want)
		}
	}
	if strings.Contains(body, "pip install") {
		t.Fatal("Dockerfile.agent-ecommerce must not provision pip dependencies; the pinned upstream scripts are stdlib-only")
	}
}

func submoduleBlock(t *testing.T, gitmodules, path string) string {
	t.Helper()
	lines := strings.Split(gitmodules, "\n")
	start := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == `[submodule "`+path+`"]` {
			start = index
			break
		}
	}
	if start < 0 {
		t.Fatalf("harness/.gitmodules missing submodule %q", path)
	}
	end := len(lines)
	for index := start + 1; index < len(lines); index++ {
		if strings.HasPrefix(strings.TrimSpace(lines[index]), "[submodule ") {
			end = index
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}
