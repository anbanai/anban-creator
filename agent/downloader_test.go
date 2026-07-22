package main

import (
	"path/filepath"
	"testing"
)

func TestToolBaseNameHandlesPluginMCPNames(t *testing.T) {
	cases := map[string]string{
		"generate_image": "generate_image",
		"mcp__plugin_anban_creator__generate_image":        "generate_image",
		"mcp__plugin_anban_creator__create_video_asr_task": "create_video_asr_task",
	}

	for name, want := range cases {
		if got := toolBaseName(name); got != want {
			t.Fatalf("toolBaseName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestDownloaderResolvesRelativePathsFromTaskRuntimeCwd(t *testing.T) {
	workspace := t.TempDir()
	montage := NewDownloader(&Config{Workspace: workspace, TaskType: "montage"})
	got, err := montage.resolveWorkspacePath("output/final.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(workspace, "montage", "output", "final.mp4"); got != want {
		t.Fatalf("Montage download path = %q, want %q", got, want)
	}

	article := NewDownloader(&Config{Workspace: workspace, TaskType: "article"})
	got, err = article.resolveWorkspacePath("output/article.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(workspace, "output", "article.md"); got != want {
		t.Fatalf("Article download path = %q, want %q", got, want)
	}
}
