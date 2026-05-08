package service

import (
	"context"
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

func createWritingChannel(t *testing.T, repo repository.Repository, userID, platform, style string) string {
	t.Helper()
	ch := &model.Channel{
		ID:       uuid.NewString(),
		UserID:   userID,
		Platform: platform,
		Name:     "Writing Channel",
		Style:    style,
		Status:   model.ChannelStatusActive,
	}
	if err := repo.Channels().Create(context.Background(), ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return ch.ID
}

func TestWritingServiceWriteArticleDefaultsEmptyArticleStyle(t *testing.T) {
	llm := &fakeWritingLLM{response: "generated article"}
	svc, repo := setupTestWritingService(t, llm)
	channelID := createWritingChannel(t, repo, "user-1", model.PlatformArticle, "")

	result, err := svc.WriteArticle(context.Background(), "user-1", channelID, "写一篇关于专注力的文章", "idea", "", "")
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
	channelID := createWritingChannel(t, repo, "user-1", model.PlatformArticle, "missing-style")

	_, err := svc.WriteArticle(context.Background(), "user-1", channelID, "写一篇文章", "idea", "", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "STYLE_NOT_FOUND") {
		t.Fatalf("error = %q, want STYLE_NOT_FOUND", err.Error())
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
