package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeednoteVisualDesignSkillKeepsImageRelevanceContract(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "claudecode", "skills", "seednote-visual-design", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "seednote-visual-design", "SKILL.md"),
	}

	required := []string{
		"image-prompts.md",
		"image-review.md",
		"简体中文",
		"禁止英文",
		"伪词",
		"春日饮茶指南",
		"茉莉花茶",
		"白牡丹白茶",
		"85-90°C",
		"10秒出汤",
		"焖泡10秒",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read skill: %v", err)
			}
			body := string(data)
			for _, term := range required {
				if !strings.Contains(body, term) {
					t.Fatalf("%s missing required term %q", path, term)
				}
			}
		})
	}
}

func TestSeednoteWritingSkillKeepsUserInputLocking(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "claudecode", "skills", "seednote-writing", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "seednote-writing", "SKILL.md"),
	}

	required := []string{
		"用户输入锁定规则",
		"封面标题",
		"笔记正文",
		"话题标签",
		"不能覆盖用户指定标题",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read skill: %v", err)
			}
			body := string(data)
			for _, term := range required {
				if !strings.Contains(body, term) {
					t.Fatalf("%s missing required term %q", path, term)
				}
			}
		})
	}
}

func TestSeednoteSkillContracts_RuntimeImageMode(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "claudecode", "agents", "seednote.md"),
		filepath.Join(root, "claudecode", "skills", "seednote", "SKILL.md"),
		filepath.Join(root, "claudecode", "skills", "seednote-visual-design", "SKILL.md"),
		filepath.Join(root, "claudecode", "skills", "seednote-visual-design", "references", "content.md"),
		filepath.Join(root, "openclaw", "skills", "seednote", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "seednote-visual-design", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "seednote-visual-design", "references", "content.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(data)
			for _, term := range []string{
				"seednote_image_mode",
				"cover_only",
				"cover_content",
				"cover_tail",
				"full",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing seednote runtime image mode term %q", path, term)
				}
			}
			for _, stale := range []string{
				"图片构成要求",
				"user prompt 指令",
				"禁止生成尾图",
			} {
				if strings.Contains(text, stale) {
					t.Fatalf("%s still contains stale seednote image-control phrase %q", path, stale)
				}
			}
		})
	}
}

func TestSeednoteAgentUsesAgentReachForExternalXHSData(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "claudecode", "agents", "seednote.md"),
		filepath.Join(root, "codex", "agents", "seednote.toml"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			body := string(data)

			for _, want := range []string{
				"agent-reach",
				"Agent-Reach",
				"agent-reach doctor --json",
				"唯一外部数据入口",
				"backend 顺序和可用性完全由 Agent-Reach 决定",
				"不生成虚构热门数据",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing Agent-Reach contract term %q", path, want)
				}
			}

			for _, forbidden := range []string{
				"opencli xiaohongshu publish",
				"opencli xiaohongshu delete-note",
				"opencli xiaohongshu follow",
				"opencli xiaohongshu unfollow",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s must not include write-operation command %q", path, forbidden)
				}
			}
		})
	}
}

func TestSeednoteResearchSkillsUseAgentReachOnlyForExternalXHSData(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "claudecode", "skills", "seednote-research", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "seednote-research", "SKILL.md"),
		filepath.Join(root, "codex", "skills", "seednote-research", "SKILL.md"),
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			body := string(data)
			for _, want := range []string{
				"Agent-Reach",
				"agent-reach doctor --json",
				"active_backend",
				"data_source=agent-reach",
				"backend_command_family",
				"token_source",
				"missing_fields",
				"fallback_reason",
				"不能凭空构造",
				"只读",
				"不要在 Anban 内自行判断",
				"实际可用性、安装、登录和 fallback 顺序由 Agent-Reach 决定",
				"只作为 legacy/server/internal fallback，不进入新 seednote 研究主路径",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing Agent-Reach research contract term %q", path, want)
				}
			}
			for _, forbidden := range []string{
				"opencli xiaohongshu publish",
				"opencli xiaohongshu delete-note",
				"opencli xiaohongshu follow",
				"opencli xiaohongshu unfollow",
				"opencli xiaohongshu like",
				"opencli xiaohongshu favorite",
				"mcporter call 'xiaohongshu.publish",
				"mcporter call 'xiaohongshu.delete",
				"mcporter call 'xiaohongshu.follow",
				"mcporter call 'xiaohongshu.like",
				"mcporter call 'xiaohongshu.collect",
				"xhs publish",
				"xhs delete",
				"xhs follow",
				"xhs like",
				"xhs favorite",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s must not include write-operation command %q", path, forbidden)
				}
			}
		})
	}
}

func TestSeednoteSkillsDoNotUseLegacyXHSMCPAsMainPath(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "claudecode", "skills", "seednote", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "seednote", "SKILL.md"),
		filepath.Join(root, "codex", "skills", "seednote", "SKILL.md"),
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			body := string(data)
			for _, want := range []string{
				"seednote-research",
				"Agent-Reach",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing seednote Agent-Reach handoff term %q", path, want)
				}
			}
			for _, forbidden := range []string{
				"list_project_topics(",
				"MCP `get_feed_detail",
				"先获取 xsec_token，再调用 MCP",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s still uses legacy XHS MCP main path %q", path, forbidden)
				}
			}
		})
	}
}
