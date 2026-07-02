package main

import (
	"context"
	"errors"
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
	err := newAgentCommand(nil, nil, func(_ context.Context, cfg *Config) error {
		if !cfg.ArticleWithCover || !cfg.ArticleWithContentImages {
			t.Fatalf("article image flags should default true, got cover=%v content=%v", cfg.ArticleWithCover, cfg.ArticleWithContentImages)
		}
		return nil
	}).Run(context.Background(), []string{
		"anban",
		"run",
		"--server-url", "http://localhost:18060",
		"--api-key", "key",
		"--task-id", "task-1",
		"--task-type", "article",
		"--topic", "时间管理",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseConfig_ArticleImageFlagsCanDisableAllImages(t *testing.T) {
	err := newAgentCommand(nil, nil, func(_ context.Context, cfg *Config) error {
		if cfg.ArticleWithCover || cfg.ArticleWithContentImages {
			t.Fatalf("article image flags should parse explicit false values, got cover=%v content=%v", cfg.ArticleWithCover, cfg.ArticleWithContentImages)
		}
		return nil
	}).Run(context.Background(), []string{
		"anban",
		"run",
		"--server-url", "http://localhost:18060",
		"--api-key", "key",
		"--task-id", "task-1",
		"--task-type", "article",
		"--article-with-cover=false",
		"--article-with-content-images=false",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentCommandRejectsBareRunFlags(t *testing.T) {
	called := false
	err := newAgentCommand(nil, nil, func(_ context.Context, _ *Config) error {
		called = true
		return nil
	}).Run(context.Background(), []string{
		"anban",
		"--server-url", "http://localhost:18060",
		"--api-key", "key",
		"--task-id", "task-1",
		"--task-type", "article",
	})
	if err == nil {
		t.Fatal("expected bare root flags to fail")
	}
	if called {
		t.Fatal("run action should not be called for bare root flags")
	}
}

func TestAgentCommandRequiresSubcommand(t *testing.T) {
	err := newAgentCommand(nil, nil, nil).Run(context.Background(), []string{"anban"})
	if err == nil {
		t.Fatal("expected root command without subcommand to fail")
	}
}

func TestAgentCommandPropagatesRunErrors(t *testing.T) {
	want := errors.New("boom")
	err := newAgentCommand(nil, nil, func(_ context.Context, _ *Config) error {
		return want
	}).Run(context.Background(), []string{
		"anban",
		"run",
		"--server-url", "http://localhost:18060",
		"--api-key", "key",
		"--task-id", "task-1",
		"--task-type", "article",
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected run error %v, got %v", want, err)
	}
}
