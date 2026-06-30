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
