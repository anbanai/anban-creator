package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMomentsAgentAndSkillContracts(t *testing.T) {
	root := repositoryRoot(t)

	claudeAgent := readRepoFile(t, filepath.Join(root, "claudecode", "agents", "moments.md"))
	for _, want := range []string{
		"name: moments",
		"$TASK_ID",
		"update_task_progress",
		"ANBAN_DEFAULT_PROJECT",
		`list_projects(platform="moments")`,
		`get_project_profile(project_id="$PROJECT_ID", scope="moments", task_id="$TASK_ID")`,
		`prepare_workspace(content_type="moments", task_id=$TASK_ID)`,
		`archive_workspace(content_type="moments"`,
		"material-analysis.md",
		"content.md",
		"quality-review.md",
		"guizang-social-card",
	} {
		if !strings.Contains(claudeAgent, want) {
			t.Fatalf("claudecode moments agent missing %q", want)
		}
	}

	codexAgent := readRepoFile(t, filepath.Join(root, "codex", "agents", "moments.toml"))
	for _, want := range []string{
		`name = "moments"`,
		"skills/moments/SKILL.md",
		"skills/humanizer/SKILL.md",
		"skills/guizang-social-card/SKILL.md",
		`list_projects(platform="moments")`,
		`get_project_profile(project_id="$PROJECT_ID", scope="moments", task_id="$TASK_ID")`,
		`prepare_workspace(content_type="moments", task_id=$TASK_ID)`,
		`archive_workspace(content_type="moments"`,
	} {
		if !strings.Contains(codexAgent, want) {
			t.Fatalf("codex moments agent missing %q", want)
		}
	}

	reg := readRepoFile(t, filepath.Join(root, "codex", "install", "agents-registration.toml"))
	if !strings.Contains(reg, "[agents.moments]") {
		t.Fatal("codex agents-registration.toml missing moments registration")
	}
}

func TestMomentsSkillMirrorsAndMethodContract(t *testing.T) {
	root := repositoryRoot(t)
	claudeSkill := readRepoFile(t, filepath.Join(root, "claudecode", "skills", "moments", "SKILL.md"))
	claudeExamples := readRepoFile(t, filepath.Join(root, "claudecode", "skills", "moments", "references", "examples.md"))

	for _, plugin := range []string{"claudecode", "openclaw", "codex"} {
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
				"material-analysis.md",
				"content.md",
				"quality-review.md",
				"guizang-social-card",
				"不默认使用“彩卉”人设",
				"不伪造客户案例、成交数据、用户反馈",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing moments contract term %q", skillPath, want)
				}
			}

			examplesPath := filepath.Join(root, plugin, "skills", "moments", "references", "examples.md")
			examples := readRepoFile(t, examplesPath)
			if plugin != "claudecode" && examples != claudeExamples {
				t.Fatalf("%s must match claudecode moments examples", examplesPath)
			}
			for _, want := range []string{"## Source Patterns", "Anthropic official", "GitHub high-star", "## How To Use These Cases", "### Case "} {
				if !strings.Contains(examples, want) {
					t.Fatalf("%s missing examples contract term %q", examplesPath, want)
				}
			}
		})
	}
}

func TestMomentsSkillDoesNotVendorReferenceRepository(t *testing.T) {
	root := repositoryRoot(t)
	for _, plugin := range []string{"claudecode", "openclaw", "codex"} {
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
