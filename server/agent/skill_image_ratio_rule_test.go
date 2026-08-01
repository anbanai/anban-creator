package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllSkillsDeclareEffectiveImageRatioRule(t *testing.T) {
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
				legacyRule := []string{
					"图片比例固定规则",
					"用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先",
					"项目/频道默认比例次之",
					"业务默认比例只作兜底",
					"不得从工具缺省值反推业务比例",
					"比例只由用户、任务、项目或业务场景决定",
					"微信文章封面/正文图默认 `16:9`",
					"Seednote/XLS/移动信息流默认 `3:4`",
				}
				effectiveRule := []string{
					"图像参数合同",
					"resolved_profile.image_ratio",
					"resolved_profile.supported_sizes",
					"用户明确比例",
					"智能适配",
					"显式传 `size`",
					"image_capability_ratio_unsupported",
				}
				required := legacyRule
				if strings.Contains(body, "generate_image(") {
					required = effectiveRule
				}
				for _, want := range required {
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
