package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuizangSocialCardSkillIsNotDistributed(t *testing.T) {
	root := repoRoot(t)
	for _, plugin := range []string{"plugins"} {
		t.Run(plugin, func(t *testing.T) {
			dir := filepath.Join(root, plugin, "skills", "guizang-social-card")
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("%s should not be distributed", dir)
			}
		})
	}
}

func TestGuizangSocialCardRoutingIsRemoved(t *testing.T) {
	root := repoRoot(t)
	files := []string{
		filepath.Join(root, "plugins", "agents", "seednote.md"),
		filepath.Join(root, "plugins", "agents", "wechatarticle.md"),
		filepath.Join(root, "plugins", "agents", "moments.toml"),
		filepath.Join(root, "plugins", "agents", "seednote.toml"),
		filepath.Join(root, "plugins", "agents", "wechatarticle.toml"),
	}
	for _, plugin := range []string{"plugins"} {
		for _, skill := range []string{"moments", "seednote-visual-design", "article-visual-design", "article-cover-design"} {
			files = append(files, filepath.Join(root, plugin, "skills", skill, "SKILL.md"))
		}
		files = append(files, filepath.Join(root, plugin, "skills", "seednote-visual-design", "references", "content.md"))
	}

	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			body := readRepoFile(t, file)
			for _, forbidden := range []string{
				"guizang-social-card",
				"归藏",
				"Guizang",
				"social card",
				"小红书组图",
				"editorial card",
				"社交卡片",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s still contains removed social-card routing term %q", file, forbidden)
				}
			}
		})
	}
}

func TestGuizangSocialCardAssetsAreRemoved(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.Base(path) {
			case ".git", "node_modules", ".vite", "dist":
				return filepath.SkipDir
			}
			if filepath.Base(path) == "guizang-social-card" {
				t.Fatalf("removed skill directory still exists: %s", path)
			}
			return nil
		}
		base := filepath.Base(path)
		for _, forbidden := range []string{
			"validate-social-deck.mjs",
			"template-editorial-card.html",
			"template-swiss-card.html",
			"magazine-bg-webgl.js",
		} {
			if base == forbidden {
				t.Fatalf("removed social-card asset still exists: %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

func TestStudioProjectCreationDoesNotExposeGuizangCopy(t *testing.T) {
	root := repoRoot(t)
	files := []string{
		filepath.Join(root, "studio", "src", "pages", "ProjectsPage.tsx"),
		filepath.Join(root, "server", "model", "platform.go"),
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			body := readRepoFile(t, file)
			for _, forbidden := range []string{
				"归藏",
				"Guizang",
				"social card",
				"社交卡片",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s still exposes removed social-card copy %q", file, forbidden)
				}
			}
		})
	}
}
