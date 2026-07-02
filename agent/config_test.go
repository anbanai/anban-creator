package main

import (
	"flag"
	"os"
	"strings"
	"testing"
)

func TestConfigUserPrompt_ArticleImageFlagsDefaultOn(t *testing.T) {
	cfg := &Config{
		TaskID:                   "task-1",
		TaskType:                 "article",
		Topic:                    "时间管理",
		ArticleWithCover:         true,
		ArticleWithContentImages: true,
	}

	got := cfg.UserPrompt()
	if !strings.Contains(got, "article_image_mode=cover_and_content") {
		t.Fatalf("expected article default image mode in prompt, got %q", got)
	}
}

func TestConfigUserPrompt_ArticleImageFlagsCanDisableAllImages(t *testing.T) {
	cfg := &Config{
		TaskID:                   "task-1",
		TaskType:                 "article",
		Topic:                    "时间管理",
		ArticleWithCover:         false,
		ArticleWithContentImages: false,
	}

	got := cfg.UserPrompt()
	if !strings.Contains(got, "article_image_mode=text_only") {
		t.Fatalf("expected article text_only image mode in prompt, got %q", got)
	}
}

func TestParseConfig_ArticleImageFlagsDefaultTrue(t *testing.T) {
	withArgs(t,
		"anban-creator-agent",
		"--server-url", "http://localhost:18060",
		"--api-key", "key",
		"--task-id", "task-1",
		"--task-type", "article",
		"--topic", "时间管理",
	)

	cfg, err := ParseConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ArticleWithCover || !cfg.ArticleWithContentImages {
		t.Fatalf("article image flags should default true, got cover=%v content=%v", cfg.ArticleWithCover, cfg.ArticleWithContentImages)
	}
}

func withArgs(t *testing.T, args ...string) {
	t.Helper()
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
	})
	os.Args = args
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
}
