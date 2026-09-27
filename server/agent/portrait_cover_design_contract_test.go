package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortraitCoverDesignUsesManagedMCPWorkflow(t *testing.T) {
	root := repoRoot(t)
	skillRoot := filepath.Join(root, "harness", "skills", "portrait-cover-design")
	body := readRepoFile(t, filepath.Join(skillRoot, "SKILL.md"))

	for _, want := range []string{
		"output/cover.png",
		"output/cover-plan.md",
		"output/cover-prompt.md",
		"output/cover-quality.json",
		"output/failure-diagnosis.md",
		"$COVER_ASPECT_RATIO",
		"$PORTRAIT_REFERENCE_PATH",
		"不询问用户",
		"最多 3 次",
		"generate_image(",
		"project_id=$PROJECT_ID",
		"task_id=$TASK_ID",
		`image_type="cover"`,
		`output_path="output/cover.png"`,
		"aspect_ratio=$COVER_ASPECT_RATIO",
		"analyze_image(",
		`file_path="output/cover.png"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("portrait-cover-design SKILL.md missing managed workflow term %q", want)
		}
	}

	for _, forbidden := range []string{
		"只产出中文提示词",
		"不直接调用任何模型",
		"每次生成时上传",
		"三轮提问",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("portrait-cover-design SKILL.md retains interactive or prompt-only contract %q", forbidden)
		}
	}
}

func TestPortraitCoverDesignReferencesUseRuntimeRatioAndCleanPackaging(t *testing.T) {
	root := repoRoot(t)
	skillRoot := filepath.Join(root, "harness", "skills", "portrait-cover-design")

	for _, forbiddenPath := range []string{"README.md", ".git", ".gitignore", "assets/.gitkeep"} {
		if _, err := os.Stat(filepath.Join(skillRoot, forbiddenPath)); !os.IsNotExist(err) {
			t.Fatalf("portrait-cover-design must not ship %s", forbiddenPath)
		}
	}
	if _, err := os.Stat(filepath.Join(skillRoot, "LICENSE")); err != nil {
		t.Fatalf("portrait-cover-design must preserve LICENSE: %v", err)
	}

	err := filepath.WalkDir(skillRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := string(content)
		for _, forbidden := range []string{"3:4 竖版构图", "assets/my-face.png", "config.md"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s contains forbidden vendored workflow term %q", path, forbidden)
			}
		}
		if strings.HasPrefix(entry.Name(), "style-") || entry.Name() == "examples.md" {
			if !strings.Contains(body, "$VIDEO_ASPECT_RATIO") {
				t.Fatalf("%s does not parameterize the runtime video ratio", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk portrait-cover-design: %v", err)
	}
}
