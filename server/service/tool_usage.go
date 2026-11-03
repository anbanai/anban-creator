package service

import (
	"context"
	"strings"
	"sync"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

// ToolUsage accumulates provider-reported token usage for one MCP tool call.
// The MCP transport installs a tracker in the tool call context; shared model
// clients report into it, so a log line can be emitted when the call finishes.
// The zero value is ready to use, and a nil tracker is a no-op.
type ToolUsage struct {
	mu sync.Mutex

	// providerCalls counts provider results observed for this tool call. Failed
	// attempts do not reach the reporting path, so this is not an attempt count.
	providerCalls int
	usageReported bool
	models        []string
	modelsSeen    map[string]struct{}

	inputTokens              int64
	cachedInputTokens        int64
	cacheReadInputTokens     int64
	cacheCreationInputTokens int64
	outputTokens             int64
	totalTokens              int64
}

// ToolUsageSnapshot is an immutable view of a ToolUsage tracker.
type ToolUsageSnapshot struct {
	ProviderCalls            int
	UsageReported            bool
	Models                   []string
	InputTokens              int64
	CachedInputTokens        int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
	OutputTokens             int64
	TotalTokens              int64
}

type toolUsageContextKey struct{}

// NewToolUsage creates an empty tracker for one tool call.
func NewToolUsage() *ToolUsage { return &ToolUsage{} }

// WithToolUsage attaches a tracker to a tool call context.
func WithToolUsage(ctx context.Context, usage *ToolUsage) context.Context {
	if ctx == nil || usage == nil {
		return ctx
	}
	return context.WithValue(ctx, toolUsageContextKey{}, usage)
}

// ToolUsageFromContext returns the tracker installed by the MCP transport, or
// nil when the call did not originate from an instrumented MCP request.
func ToolUsageFromContext(ctx context.Context) *ToolUsage {
	if ctx == nil {
		return nil
	}
	usage, _ := ctx.Value(toolUsageContextKey{}).(*ToolUsage)
	return usage
}

// RecordModelUsage reports one provider completion to the tracker in ctx.
func RecordModelUsage(ctx context.Context, model string, usage srvconfig.TokenUsage) {
	if tracker := ToolUsageFromContext(ctx); tracker != nil {
		tracker.RecordModelUsage(model, usage)
	}
}

// RecordModelUsage adds one provider completion and its reported token usage.
func (u *ToolUsage) RecordModelUsage(model string, usage srvconfig.TokenUsage) {
	if u == nil {
		return
	}
	model = strings.TrimSpace(model)
	input := nonNegativeTokens(usage.InputTokens)
	cachedInput := nonNegativeTokens(usage.CachedInputTokens)
	cacheRead := nonNegativeTokens(usage.CacheReadInputTokens)
	cacheCreation := nonNegativeTokens(usage.CacheCreationInputTokens)
	output := nonNegativeTokens(usage.OutputTokens)
	total := nonNegativeTokens(usage.TotalTokens)
	usageReported := input+cachedInput+cacheRead+cacheCreation+output+total > 0
	if total == 0 {
		// InputTokens is uncached prompt usage, so this sum does not double count.
		total = input + cachedInput + output
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	u.providerCalls++
	if usageReported {
		u.usageReported = true
	}
	if model != "" {
		if u.modelsSeen == nil {
			u.modelsSeen = make(map[string]struct{})
		}
		if _, exists := u.modelsSeen[model]; !exists {
			u.modelsSeen[model] = struct{}{}
			u.models = append(u.models, model)
		}
	}
	u.inputTokens += input
	u.cachedInputTokens += cachedInput
	u.cacheReadInputTokens += cacheRead
	u.cacheCreationInputTokens += cacheCreation
	u.outputTokens += output
	u.totalTokens += total
}

// Snapshot returns a copy that is safe to use after the tool call completes.
func (u *ToolUsage) Snapshot() ToolUsageSnapshot {
	if u == nil {
		return ToolUsageSnapshot{}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return ToolUsageSnapshot{
		ProviderCalls:            u.providerCalls,
		UsageReported:            u.usageReported,
		Models:                   append([]string(nil), u.models...),
		InputTokens:              u.inputTokens,
		CachedInputTokens:        u.cachedInputTokens,
		CacheReadInputTokens:     u.cacheReadInputTokens,
		CacheCreationInputTokens: u.cacheCreationInputTokens,
		OutputTokens:             u.outputTokens,
		TotalTokens:              u.totalTokens,
	}
}

func nonNegativeTokens(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
