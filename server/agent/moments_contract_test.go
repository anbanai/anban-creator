package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
)

func TestMomentsAgentAndSkillContracts(t *testing.T) {
	root := repositoryRoot(t)

	claudeAgent := readRepoFile(t, filepath.Join(root, "plugins", "agents", "moments.md"))
	for _, want := range []string{
		"name: moments",
		"$TASK_ID",
		"ANBAN_DEFAULT_PROJECT",
		`list_projects(platform="moments")`,
		`get_project_profile(project_id="$PROJECT_ID", scope="moments", task_id="$TASK_ID")`,
		"output/material-analysis.md",
		"output/content.md",
		"output/image-prompts.md",
		"output/moments-image.png",
		"output/quality-review.md",
	} {
		if !strings.Contains(claudeAgent, want) {
			t.Fatalf("claudecode moments agent missing %q", want)
		}
	}
	for _, forbidden := range []string{"guizang-social-card", "社交卡片", "social card", "archive_workspace", "$ARCHIVE_DIR"} {
		if strings.Contains(claudeAgent, forbidden) {
			t.Fatalf("claudecode moments agent still contains removed visual handoff %q", forbidden)
		}
	}

	codexAgent := readRepoFile(t, filepath.Join(root, "plugins", "agents", "moments.toml"))
	for _, want := range []string{
		`name = "moments"`,
		"skills/moments/SKILL.md",
		"skills/humanizer/SKILL.md",
		"update_task_progress",
		`list_projects(platform="moments")`,
		`get_project_profile(project_id="$PROJECT_ID", scope="moments", task_id="$TASK_ID")`,
		"output/material-analysis.md",
		"output/content.md",
		"output/image-prompts.md",
		"output/moments-image.png",
		"output/quality-review.md",
	} {
		if !strings.Contains(codexAgent, want) {
			t.Fatalf("codex moments agent missing %q", want)
		}
	}
	for _, forbidden := range []string{"guizang-social-card", "社交卡片", "social card", "archive_workspace", "$ARCHIVE_DIR"} {
		if strings.Contains(codexAgent, forbidden) {
			t.Fatalf("codex moments agent still contains removed visual handoff %q", forbidden)
		}
	}

	reg := readRepoFile(t, filepath.Join(root, "plugins", "install", "agents-registration.toml"))
	if !strings.Contains(reg, "[agents.moments]") {
		t.Fatal("codex agents-registration.toml missing moments registration")
	}
}

func TestMomentsProgressHookContract(t *testing.T) {
	root := repositoryRoot(t)
	catalog, err := agentpack.LoadCatalog(filepath.Join(root, "plugins"))
	if err != nil {
		t.Fatalf("load Agent Pack catalog: %v", err)
	}
	pack, ok := catalog.Pack("moments")
	if !ok {
		t.Fatal("moments Agent Pack missing from catalog")
	}

	claudePaths := []string{
		filepath.Join(root, "plugins", "packs", "moments", "agent.claude.md"),
		filepath.Join(root, "plugins", "agents", "moments.md"),
	}
	for _, path := range claudePaths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			body := readRepoFile(t, path)
			if strings.Contains(body, "update_task_progress") {
				t.Fatalf("%s must derive managed progress from Task metadata, not update_task_progress", path)
			}
			for _, want := range []string{
				"TaskCreate",
				"TaskUpdate status=in_progress",
				"TaskUpdate status=completed",
				"anban_progress_stage",
				"Runner Hooks",
				"保存每次返回的 Task id",
				"对同一 Task id",
				"不得依赖任务标题推断阶段",
				"不得省略 TaskUpdate 的 metadata",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing Task metadata progress rule %q", path, want)
				}
			}
			for _, stage := range pack.Progress {
				metadata := `{"anban_progress_stage":"` + stage.ID + `"}`
				if !strings.Contains(body, metadata) {
					t.Fatalf("%s does not map declared progress stage %q through Task metadata %s", path, stage.ID, metadata)
				}
			}
		})
	}

	codexAgent := readRepoFile(t, filepath.Join(root, "plugins", "agents", "moments.toml"))
	if !strings.Contains(codexAgent, "update_task_progress") {
		t.Fatal("Codex moments agent must retain explicit update_task_progress")
	}
	for _, forbidden := range []string{"anban_progress_stage", "官方 Task Hook", "Runner Hook"} {
		if strings.Contains(codexAgent, forbidden) {
			t.Fatalf("Codex moments agent must not claim Claude Task Hook progress support: %q", forbidden)
		}
	}
}

func TestMomentsDeliveryOwnershipByPlatform(t *testing.T) {
	root := repositoryRoot(t)
	claudeAgent := readRepoFile(t, filepath.Join(root, "plugins", "agents", "moments.md"))
	if err := validateClaudeAgentFeedbackContract(
		claudeAgent,
		"moments",
		"最终摘要包含：",
		nil,
	); err != nil {
		t.Fatalf("claudecode moments agent must own delivery validation and feedback: %v", err)
	}
	finalSummaryAt := strings.Index(claudeAgent, "最终摘要包含：")
	feedbackAt := strings.Index(claudeAgent, "submit_agent_feedback(")
	if finalSummaryAt < 0 || feedbackAt <= finalSummaryAt {
		t.Fatal("claudecode moments final summary must precede feedback")
	}
	finalSummary := claudeAgent[finalSummaryAt:feedbackAt]
	for _, want := range []string{"output/material-analysis.md", "output/content.md", "output/image-prompts.md", "output/moments-image.png", "output/quality-review.md", "质量复盘状态", "证据不足", "人工复核点"} {
		if !strings.Contains(finalSummary, want) {
			t.Fatalf("claudecode moments delivery validation missing %q", want)
		}
	}

}

func TestMomentsSkillMirrorsAndMethodContract(t *testing.T) {
	root := repositoryRoot(t)
	claudeSkill := readRepoFile(t, filepath.Join(root, "plugins", "skills", "moments", "SKILL.md"))
	claudeExamples := readRepoFile(t, filepath.Join(root, "plugins", "skills", "moments", "references", "examples.md"))

	for _, plugin := range []string{"plugins"} {
		t.Run(plugin, func(t *testing.T) {
			skillPath := filepath.Join(root, plugin, "skills", "moments", "SKILL.md")
			body := readRepoFile(t, skillPath)
			if plugin != "claudecode" && body != claudeSkill {
				t.Fatalf("%s moments SKILL.md must match claudecode", skillPath)
			}
			for _, want := range []string{
				"name: moments",
				"references/examples.md",
				"Caihui0127/caihui-moments-skill",
				"发售",
				"人设",
				"产品",
				"案例",
				"生活",
				"认知",
				"观点层",
				"框架层",
				"风格层",
				"人设层",
				"output/material-analysis.md",
				"output/content.md",
				"output/image-prompts.md",
				"output/moments-image.png",
				"output/quality-review.md",
				"不默认使用“彩卉”人设",
				"不伪造客户案例、成交数据、用户反馈",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing moments contract term %q", skillPath, want)
				}
			}
			for _, forbidden := range []string{"guizang-social-card", "社交卡片", "social card"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s still contains removed visual handoff %q", skillPath, forbidden)
				}
			}

			examplesPath := filepath.Join(root, plugin, "skills", "moments", "references", "examples.md")
			examples := readRepoFile(t, examplesPath)
			if plugin != "claudecode" && examples != claudeExamples {
				t.Fatalf("%s must match claudecode moments examples", examplesPath)
			}
			for _, want := range []string{"### Case "} {
				if !strings.Contains(examples, want) {
					t.Fatalf("%s missing examples contract term %q", examplesPath, want)
				}
			}
			for _, boilerplate := range []string{"## Source Patterns", "Anthropic official", "GitHub high-star", "## How To Use These Cases"} {
				if strings.Contains(examples, boilerplate) {
					t.Fatalf("%s contains context-only template prose %q", examplesPath, boilerplate)
				}
			}
		})
	}
}

func TestMomentsSkillDoesNotVendorReferenceRepository(t *testing.T) {
	root := repositoryRoot(t)
	for _, plugin := range []string{"plugins"} {
		dir := filepath.Join(root, plugin, "skills", "moments")
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			base := filepath.Base(path)
			for _, forbidden := range []string{"素材库.md", "彩卉案例.md", "私域素材.md"} {
				if base == forbidden {
					t.Fatalf("%s vendors forbidden upstream/private asset %q", dir, forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}
