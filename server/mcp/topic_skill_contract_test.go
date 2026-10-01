package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTopicSkillsUseServerTrendContract(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(filepath.Join(root, "..", ".."))

	for _, skill := range []string{"trending-topics", "trend-rider", "topic-evaluator"} {
		path := filepath.Join(root, "harness", "skills", skill, "SKILL.md")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(raw)
		if !strings.Contains(body, "list_trends") {
			t.Errorf("%s must reference list_trends", path)
		}
		for _, banned := range []string{"web_fetch", "http://", "https://"} {
			if strings.Contains(body, banned) {
				t.Errorf("%s must not contain %q", path, banned)
			}
		}
		switch skill {
		case "trending-topics":
			if !strings.Contains(body, "stale=true") || !strings.Contains(body, "不能描述为实时") {
				t.Errorf("%s must distinguish stale snapshots from realtime trends", path)
			}
		case "trend-rider":
			if !strings.Contains(body, "不要在本 Skill 内发现热点") || !strings.Contains(body, "低或无必须建议不借势") {
				t.Errorf("%s must remain a judgment skill with a low-relevance refusal", path)
			}
		case "topic-evaluator":
			if !strings.Contains(body, "尚未制作的选题") || !strings.Contains(body, "不评估成稿") {
				t.Errorf("%s must not evaluate completed drafts", path)
			}
		}
	}

	for _, pack := range []string{"wechat-article", "seednote"} {
		path := filepath.Join(root, "harness", "packs", pack, "agent-pack.yaml")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(raw)
		for _, skill := range []string{"trending-topics", "trend-rider", "topic-evaluator"} {
			if !strings.Contains(body, skill) {
				t.Errorf("%s must declare %s", path, skill)
			}
		}
	}
}
