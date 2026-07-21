package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllSkillsDeclareImageRatioRule(t *testing.T) {
	root := articleContractRepoRoot(t)
	for _, plugin := range []string{"claudecode", "codex"} {
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
				for _, want := range []string{
					"图片比例固定规则",
					"用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先",
					"项目/频道默认比例次之",
					"业务默认比例只作兜底",
					"不得从模型路由、供应商默认 `size` 或模型能力反推业务比例",
					"微信文章封面/正文图默认 `16:9`",
					"Seednote/XLS/移动信息流默认 `3:4`",
				} {
					if !strings.Contains(body, want) {
						t.Fatalf("%s missing global image ratio rule term %q", path, want)
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
