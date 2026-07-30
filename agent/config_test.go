package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	serveragent "github.com/anbanai/anban-creator/server/agent"
)

func TestParseConfigModelUsageAliases(t *testing.T) {
	err := newAgentCommand(nil, nil, func(_ context.Context, cfg *Config) error {
		want := map[string]serveragent.ModelUsageIdentity{
			"doubao-seed-evolving":                {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
			"doubao-seed-evolving-latest-version": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
		}
		if !reflect.DeepEqual(cfg.ModelUsageAliases, want) {
			t.Fatalf("ModelUsageAliases = %#v, want %#v", cfg.ModelUsageAliases, want)
		}
		return nil
	}).Run(context.Background(), []string{
		"anban", "run", "--server-url", "http://localhost:18060", "--api-key", "key",
		"--task-id", "task-1", "--task-type", "article",
		"--model-usage-alias", "doubao-seed-evolving=volcengine_ark/doubao-seed-evolving",
		"--model-usage-alias", "doubao-seed-evolving-latest-version=volcengine_ark/doubao-seed-evolving",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseConfigRejectsInvalidModelUsageAlias(t *testing.T) {
	err := newAgentCommand(nil, nil, func(_ context.Context, _ *Config) error { return nil }).Run(context.Background(), []string{
		"anban", "run", "--server-url", "http://localhost:18060", "--api-key", "key",
		"--task-id", "task-1", "--task-type", "article", "--model-usage-alias", "raw=missing-provider",
	})
	if err == nil || !strings.Contains(err.Error(), "model-usage-alias") {
		t.Fatalf("error = %v, want model-usage-alias validation error", err)
	}
}

func TestRunCommandDoesNotExposeLegacyModelOverride(t *testing.T) {
	cmd := newAgentCommand(nil, nil, func(context.Context, *Config) error { return nil })
	run := cmd.Command("run")
	if run == nil {
		t.Fatal("run command is missing")
	}
	for _, name := range run.FlagNames() {
		if name == "model" {
			t.Fatal("run command exposes legacy model override")
		}
	}
}

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

func TestParseConfig_AutoMemoryDirectory(t *testing.T) {
	err := newAgentCommand(nil, nil, func(_ context.Context, cfg *Config) error {
		if got, want := cfg.AutoMemoryDirectory, "/workspace/task-1/.claude/memory"; got != want {
			t.Fatalf("AutoMemoryDirectory = %q, want %q", got, want)
		}
		return nil
	}).Run(context.Background(), []string{
		"anban",
		"run",
		"--server-url", "http://localhost:18060",
		"--api-key", "key",
		"--task-id", "task-1",
		"--task-type", "article",
		"--auto-memory-directory", "/workspace/task-1/.claude/memory",
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
