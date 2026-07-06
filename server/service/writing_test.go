package service

import (
	"strings"
	"testing"
)

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
