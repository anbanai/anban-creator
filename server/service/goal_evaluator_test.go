package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// stubLLM is a minimal LLMClient that returns a canned response.
type stubLLM struct {
	resp string
	err  error
}

func (s *stubLLM) Complete(_ context.Context, _, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.resp, nil
}

func (s *stubLLM) CompleteWithImage(_ context.Context, _, _, _ string) (string, error) {
	return "", errors.New("not implemented")
}

func TestGoalEvaluator_ParseCleanJSON(t *testing.T) {
	ev := NewGoalEvaluator(&stubLLM{resp: `{"achieved": true, "reason": "文章包含 3 个案例，达成目标"}`}, nil)
	out, err := ev.Evaluate(context.Background(), "必须包含 3 个案例", "写一篇关于 X 的文章", "正文……")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Achieved {
		t.Errorf("expected achieved=true, got false")
	}
	if !strings.Contains(out.Reason, "案例") {
		t.Errorf("unexpected reason: %s", out.Reason)
	}
}

func TestGoalEvaluator_ParseMarkdownFencedJSON(t *testing.T) {
	resp := "```json\n{\"achieved\": false, \"reason\": \"字数不足 1500\"}\n```"
	ev := NewGoalEvaluator(&stubLLM{resp: resp}, nil)
	out, err := ev.Evaluate(context.Background(), "字数 ≥ 1500", "prompt", "content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Achieved {
		t.Errorf("expected achieved=false")
	}
}

func TestGoalEvaluator_ParseJSONWithSurroundingProse(t *testing.T) {
	resp := `好的，我来判断：
{"achieved": true, "reason": "满足所有条件"}
希望这能帮到你。`
	ev := NewGoalEvaluator(&stubLLM{resp: resp}, nil)
	out, err := ev.Evaluate(context.Background(), "goal", "p", "c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Achieved {
		t.Errorf("expected achieved=true")
	}
}

func TestGoalEvaluator_UnparseableOutputDefaultsToAchieved(t *testing.T) {
	ev := NewGoalEvaluator(&stubLLM{resp: "抱歉，我无法判断。"}, nil)
	out, err := ev.Evaluate(context.Background(), "goal", "p", "c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Achieved {
		t.Errorf("expected default achieved=true on unparseable output")
	}
}

func TestGoalEvaluator_LLMErrorReturnsError(t *testing.T) {
	ev := NewGoalEvaluator(&stubLLM{err: errors.New("upstream timeout")}, nil)
	_, err := ev.Evaluate(context.Background(), "goal", "p", "c")
	if err == nil {
		t.Errorf("expected error when LLM call fails")
	}
}

func TestGoalEvaluator_EmptyGoalIsAchieved(t *testing.T) {
	ev := NewGoalEvaluator(&stubLLM{}, nil)
	out, err := ev.Evaluate(context.Background(), "  ", "p", "c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Achieved {
		t.Errorf("empty goal should default to achieved=true")
	}
}

func TestGoalEvaluator_NilLoggerDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("constructor panicked: %v", r)
		}
	}()
	_ = NewGoalEvaluator(&stubLLM{}, nil)
}

func TestGoalEvaluator_ContentTruncatedInPrompt(t *testing.T) {
	var captured string
	llm := &capturingLLM{capture: &captured}
	ev := NewGoalEvaluator(llm, nil)
	long := strings.Repeat("a", 32*1024)
	_, _ = ev.Evaluate(context.Background(), "goal", "p", long)
	if !strings.Contains(captured, "[... 内容已截断 ...]") {
		t.Errorf("expected truncation marker in prompt, got len=%d", len(captured))
	}
}

type capturingLLM struct {
	capture *string
}

func (c *capturingLLM) Complete(_ context.Context, _, userPrompt string) (string, error) {
	*c.capture = userPrompt
	return `{"achieved": true, "reason": "ok"}`, nil
}

func (c *capturingLLM) CompleteWithImage(_ context.Context, _, _, _ string) (string, error) {
	return "", errors.New("not implemented")
}

func TestParseGoalEvaluation_EmptyReasonGetsFallback(t *testing.T) {
	out := parseGoalEvaluation(`{"achieved": false, "reason": ""}`)
	if out == nil {
		t.Fatal("expected non-nil")
	}
	if out.Reason == "" {
		t.Errorf("expected fallback reason, got empty")
	}
}

func TestParseGoalEvaluation_NoAchievedKey(t *testing.T) {
	// "achieved" appears as a substring of the value here, but the JSON does
	// not actually have an "achieved" key — the parser must reject it.
	out := parseGoalEvaluation(`{"reason": "no goal met"}`)
	if out != nil {
		t.Errorf("expected nil when achieved key missing, got %+v", out)
	}
}

// Silence zerolog when no logger is provided.
var _ = zerolog.Nop
