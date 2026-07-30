package service

import (
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestChatCompletionUsageSeparatesCachedPromptTokens(t *testing.T) {
	usage := chatCompletionUsage(openai.CompletionUsage{
		PromptTokens:     100,
		CompletionTokens: 7,
		TotalTokens:      107,
		PromptTokensDetails: openai.CompletionUsagePromptTokensDetails{
			CachedTokens: 40,
		},
	})

	if usage.InputTokens != 60 || usage.CachedInputTokens != 40 || usage.OutputTokens != 7 || usage.TotalTokens != 107 {
		t.Fatalf("usage = %#v, want uncached input 60, cached input 40, output 7, total 107", usage)
	}
}
