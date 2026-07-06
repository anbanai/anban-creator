package mcp

import (
	"context"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/service"
)

type fakeMCPWritingLLM struct {
	response string
	usage    srvconfig.TokenUsage
	calls    int
}

func (f *fakeMCPWritingLLM) Complete(context.Context, string, string) (string, error) {
	f.calls++
	return f.response, nil
}

func (f *fakeMCPWritingLLM) CompleteWithImage(context.Context, string, string, string) (string, error) {
	f.calls++
	return f.response, nil
}

func (f *fakeMCPWritingLLM) CompleteWithImageResult(context.Context, string, string, string) (*service.LLMResult, error) {
	f.calls++
	return &service.LLMResult{Text: f.response, Usage: f.usage}, nil
}
