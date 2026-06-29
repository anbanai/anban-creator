package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

type fakeWritingLLM struct {
	response string
	prompt   string
}

func (f *fakeWritingLLM) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	f.prompt = userPrompt
	return f.response, nil
}

func (f *fakeWritingLLM) CompleteWithImage(_ context.Context, _, _, _ string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func setupTestWritingService(t *testing.T, llm *fakeWritingLLM) (*WritingService, repository.Repository) {
	t.Helper()
	db := setupTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	writersDir, err := filepath.Abs("../../claudecode/writers")
	if err != nil {
		t.Fatalf("resolve writers dir: %v", err)
	}
	svc := NewWritingService(repo, llm, writersDir, 0, &logger)
	return svc, repo
}

// createWritingProject seeds a project with the given 写作风格 (writer key).
// WriteArticle resolves the writer from Writer, never from ch.VisualStyle (the
// visual dimension), so the helper writes the writer key into Writer.
func createWritingProject(t *testing.T, repo repository.Repository, userID, platform, writingStyle string) string {
	t.Helper()
	ch := &model.Project{
		ID:       uuid.NewString(),
		UserID:   userID,
		Platform: platform,
		Name:     "Writing Project",
		Writer:   writingStyle,
		Status:   model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(context.Background(), ch); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return ch.ID
}

func TestWritingServiceWriteArticleDefaultsEmptyArticleStyle(t *testing.T) {
	llm := &fakeWritingLLM{response: "generated article"}
	svc, repo := setupTestWritingService(t, llm)
	projectID := createWritingProject(t, repo, "user-1", model.PlatformArticle, "")

	result, err := svc.WriteArticle(context.Background(), "user-1", projectID, "写一篇关于专注力的文章", "idea", "", "", "")
	if err != nil {
		t.Fatalf("WriteArticle: %v", err)
	}
	if result.Article != "generated article" {
		t.Fatalf("Article = %q", result.Article)
	}
	if result.WordCount != utf8.RuneCountInString("generated article") {
		t.Fatalf("WordCount = %d", result.WordCount)
	}
	if llm.prompt == "" || !strings.Contains(llm.prompt, "Dan Koe") {
		t.Fatalf("prompt did not use default dan-koe style: %q", llm.prompt)
	}
}

func TestWritingServiceWriteArticleKeepsMissingStyleError(t *testing.T) {
	llm := &fakeWritingLLM{response: "generated article"}
	svc, repo := setupTestWritingService(t, llm)
	projectID := createWritingProject(t, repo, "user-1", model.PlatformArticle, "missing-style")

	_, err := svc.WriteArticle(context.Background(), "user-1", projectID, "写一篇文章", "idea", "", "", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "STYLE_NOT_FOUND") {
		t.Fatalf("error = %q, want STYLE_NOT_FOUND", err.Error())
	}
}

func TestWritingServiceResearchTopicsUsesProjectInstructionsAsPositioning(t *testing.T) {
	llm := &fakeWritingLLM{response: `[{"topic":"选题","angle":"角度","keywords":["效率"],"viral_score":80}]`}
	svc, repo := setupTestWritingService(t, llm)
	projectID := uuid.NewString()
	if err := repo.Projects().Create(context.Background(), &model.Project{
		ID:           projectID,
		UserID:       "user-1",
		Platform:     model.PlatformSeednote,
		Name:         "Research Project",
		Positioning:  "旧定位不应进入选题 prompt",
		Instructions: "面向独立开发者的效率工具项目",
		Status:       model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	if _, err := svc.ResearchTopics(context.Background(), "user-1", projectID, nil, "", 1); err != nil {
		t.Fatalf("ResearchTopics: %v", err)
	}

	if !strings.Contains(llm.prompt, "面向独立开发者的效率工具项目") {
		t.Fatalf("prompt = %q, want project instructions positioning", llm.prompt)
	}
	if strings.Contains(llm.prompt, "旧定位不应进入选题 prompt") {
		t.Fatalf("prompt leaked legacy positioning: %q", llm.prompt)
	}
}

func TestExtractJSONArray(t *testing.T) {
	t.Run("pure JSON array", func(t *testing.T) {
		input := `[{"topic":"a","angle":"b"}]`
		out, err := extractJSONArray(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != input {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("markdown fences", func(t *testing.T) {
		input := "```json\n[{\"topic\":\"a\"}]\n```"
		out, err := extractJSONArray(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != `[{"topic":"a"}]` {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("preamble text", func(t *testing.T) {
		input := "Here are the topics:\n[{\"topic\":\"a\"}]"
		out, err := extractJSONArray(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != `[{"topic":"a"}]` {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("Chinese preamble with å-like chars", func(t *testing.T) {
		input := "好的，以下是为您生成的话题：\n[{\"topic\":\"测试话题\"}]"
		out, err := extractJSONArray(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != `[{"topic":"测试话题"}]` {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("UTF-8 BOM prefix", func(t *testing.T) {
		input := "\xEF\xBB\xBF[{\"topic\":\"a\"}]"
		out, err := extractJSONArray(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != `[{"topic":"a"}]` {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("trailing commas", func(t *testing.T) {
		input := `[{"topic":"a",},]`
		out, err := extractJSONArray(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != `[{"topic":"a"}]` {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("nested objects inside array", func(t *testing.T) {
		input := `[{\"topic\":\"a\",\"keywords\":[\"k1\",\"k2\"]}]`
		out, err := extractJSONArray(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != input {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("brackets inside strings are ignored", func(t *testing.T) {
		input := `{"text":"[not an array]"}` + `[{"topic":"a"}]`
		out, err := extractJSONArray(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != `[{"topic":"a"}]` {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("no JSON array", func(t *testing.T) {
		_, err := extractJSONArray("no array here")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("unmatched brackets", func(t *testing.T) {
		_, err := extractJSONArray("[{\"topic\":\"a\"}")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("malformed extra quote repaired by lenient fallback", func(t *testing.T) {
		// Production case: "angle"":value desynchronizes bracket matcher,
		// but lenient fallback repairs the extra quote and parses successfully.
		input := `[{"topic":"a","angle":"b"},{"topic":"c","angle"":"malformed"}]`
		out, err := extractJSONArray(input)
		if err != nil {
			t.Fatalf("expected lenient fallback to succeed, got: %v", err)
		}
		if !strings.HasPrefix(out, "[") {
			t.Fatalf("expected array, got: %s", out[:20])
		}
	})

	t.Run("lenient fallback with preamble and trailing text", func(t *testing.T) {
		input := "好的，以下是话题：\n[{\"topic\":\"a\"},{\"topic\":\"b\"}]\n更多内容"
		out, err := extractJSONArray(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != `[{"topic":"a"},{"topic":"b"}]` {
			t.Fatalf("got: %s", out)
		}
	})

	t.Run("unmatched with no valid JSON still fails", func(t *testing.T) {
		_, err := extractJSONArray("[{\"topic\":\"a\"}")
		if err == nil {
			t.Fatal("expected error for truly broken JSON")
		}
	})
}

func TestExtractJSONObject(t *testing.T) {
	t.Run("pure JSON object", func(t *testing.T) {
		input := `{"title":"a"}`
		out, err := extractJSONObject(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != input {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("preamble text", func(t *testing.T) {
		input := "Result:\n```json\n{\"title\":\"a\"}\n```"
		out, err := extractJSONObject(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != `{"title":"a"}` {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("nested objects", func(t *testing.T) {
		input := "some text\n{\"outer\":{\"inner\":1}}"
		out, err := extractJSONObject(input)
		if err != nil {
			t.Fatal(err)
		}
		if out != `{"outer":{"inner":1}}` {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("no JSON object", func(t *testing.T) {
		_, err := extractJSONObject("no object here")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
