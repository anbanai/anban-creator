package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratingSkillsDeclareSemanticAspectRatioRule(t *testing.T) {
	root := articleContractRepoRoot(t)
	for _, plugin := range []string{"plugins"} {
		skillsRoot := filepath.Join(root, plugin, "skills")
		err := filepath.WalkDir(skillsRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(path) != "SKILL.md" {
				return nil
			}
			if isUpstreamHumanizerSkillPath(path) {
				return nil
			}
			t.Run(plugin+"/"+filepath.Base(filepath.Dir(path)), func(t *testing.T) {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read %s: %v", path, err)
				}
				body := string(raw)
				effectiveRule := []string{
					"图像参数合同",
					"resolved_profile.image_ratio",
					"resolved_profile.allowed_image_ratios",
					"用户明确比例",
					"智能适配",
					"显式传 `aspect_ratio`",
				}
				if !strings.Contains(body, "generate_image(") {
					return
				}
				for _, want := range effectiveRule {
					if !strings.Contains(body, want) {
						t.Fatalf("%s missing image ratio contract term %q", path, want)
					}
				}
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", skillsRoot, err)
		}
	}
}
