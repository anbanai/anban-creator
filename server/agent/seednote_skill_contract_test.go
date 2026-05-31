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

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root with go.mod not found")
		}
		dir = parent
	}
}
