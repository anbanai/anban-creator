package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// GoalEvaluator — assesses whether a task's content satisfies a user-defined goal
// ---------------------------------------------------------------------------

// GoalEvaluation is the outcome of one evaluation run.
type GoalEvaluation struct {
	Achieved bool   `json:"achieved"`
	Reason   string `json:"reason"`
}

// GoalEvaluator calls an LLM (Haiku-class) to decide whether the produced
// content meets the user-defined goal condition. Used by TaskService in
// "strong goal mode" to drive automatic retries up to GoalMaxAttempts.
//
// Reuses the OpenAI-compatible LLMClient abstraction so it can be wired to
// any configured provider (Kimi / BigModel / OpenAI / Anthropic-compatible).
type GoalEvaluator struct {
	llm    LLMClient
	logger *zerolog.Logger
}

// NewGoalEvaluator constructs an evaluator backed by the given LLMClient.
// The caller is responsible for picking a small / fast model when constructing
// the client (e.g. a Haiku-equivalent tier).
func NewGoalEvaluator(llm LLMClient, logger *zerolog.Logger) *GoalEvaluator {
	if logger == nil {
		nop := zerolog.Nop()
		logger = &nop
	}
	return &GoalEvaluator{llm: llm, logger: logger}
}

// Evaluate asks the LLM whether `content` (produced for `prompt`) satisfies `goal`.
//
// Contract:
//   - Returns a non-nil Evaluation even on partial parse failure (defaults to achieved=true).
//   - Returns an error only when the LLM cannot be reached at all; callers should
//     treat such errors as "achieved=true" to protect users from wasted credits.
//   - The output is constrained to JSON: {"achieved": bool, "reason": string}.
func (e *GoalEvaluator) Evaluate(ctx context.Context, goal, prompt, content string) (*GoalEvaluation, error) {
	if strings.TrimSpace(goal) == "" {
		// No goal defined — nothing to evaluate. Treat as achieved.
		return &GoalEvaluation{Achieved: true, Reason: "未定义目标条件，默认达成"}, nil
	}

	if e.llm == nil {
		return nil, fmt.Errorf("goal evaluator LLM client is not configured")
	}

	systemPrompt := `你是一个内容质量评估器。你的唯一任务是判断一篇内容产出是否满足给定的「目标条件」。

判断要求：
- 仅根据「目标条件」做 yes/no 判断，不要引入目标之外的主观标准。
- 「目标条件」中的具体数字（如字数、案例数量、关键词出现次数）按字面值判断。
- 忽略产出中的小瑕疵（如个别错字），只看是否满足核心目标。

输出格式：严格输出一行 JSON，不要包含 markdown 代码块、注释或多余文字：
{"achieved": <true|false>, "reason": "<不超过 120 字的简短说明，解释为何达成或未达成>"}`

	userPrompt := buildGoalUserPrompt(goal, prompt, content)

	// Bound the evaluation so a slow model can't stall the task pipeline.
	evalCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	raw, err := e.llm.Complete(evalCtx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("goal evaluator LLM call: %w", err)
	}

	eval := parseGoalEvaluation(raw)
	if eval == nil {
		// LLM returned unparseable output. Default to achieved=true to avoid
		// burning a retry credit on a parser bug rather than a real failure.
		e.logger.Warn().
			Str("raw_output", truncateEvalOutput(raw, 400)).
			Msg("goal evaluator output unparseable, defaulting to achieved=true")
		return &GoalEvaluation{
			Achieved: true,
			Reason:   "评估输出无法解析，默认达成",
		}, nil
	}
	return eval, nil
}

// buildGoalUserPrompt assembles the user-side prompt for the evaluator.
// Truncates content to a sane upper bound (16KB) to keep the call fast and cheap.
func buildGoalUserPrompt(goal, prompt, content string) string {
	const maxContent = 16 * 1024
	if len(content) > maxContent {
		content = content[:maxContent] + "\n\n[... 内容已截断 ...]"
	}

	var b strings.Builder
	b.WriteString("【目标条件】\n")
	b.WriteString(strings.TrimSpace(goal))
	b.WriteString("\n\n【原始写作要求】\n")
	if strings.TrimSpace(prompt) == "" {
		b.WriteString("(无)")
	} else {
		b.WriteString(strings.TrimSpace(prompt))
	}
	b.WriteString("\n\n【待评估的内容产出】\n---\n")
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n---\n\n请判断上述产出是否满足「目标条件」。严格输出 JSON。")
	return b.String()
}

// goalJSONRegex extracts the first {...} block from the model output.
// Tolerates ```json fenced blocks and surrounding prose.
var goalJSONRegex = regexp.MustCompile(`(?s)\{[^{}]*"achieved"[^{}]*\}`)

// parseGoalEvaluation extracts an Evaluation from raw LLM output. Returns nil
// when no parseable JSON object is found.
func parseGoalEvaluation(raw string) *GoalEvaluation {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	// Fast path: output is already a clean JSON object.
	if strings.HasPrefix(trimmed, "{") {
		if ev := tryParseGoalJSON(trimmed); ev != nil {
			return ev
		}
	}

	// Slow path: extract the first JSON object that contains an "achieved" key.
	match := goalJSONRegex.FindString(trimmed)
	if match == "" {
		return nil
	}
	return tryParseGoalJSON(match)
}

func tryParseGoalJSON(s string) *GoalEvaluation {
	// First verify the JSON has an explicit "achieved" key. Without this guard,
	// `{"reason": "..."}` would parse with Achieved defaulting to false (zero
	// value) — which we'd misinterpret as a definitive "not achieved" verdict.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &probe); err != nil {
		return nil
	}
	if _, ok := probe["achieved"]; !ok {
		return nil
	}

	var out struct {
		Achieved bool   `json:"achieved"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	out.Reason = strings.TrimSpace(out.Reason)
	if out.Reason == "" {
		out.Reason = "(评估器未提供理由)"
	}
	return &GoalEvaluation{Achieved: out.Achieved, Reason: out.Reason}
}

func truncateEvalOutput(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
