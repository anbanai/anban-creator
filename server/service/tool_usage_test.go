package service

import (
	"context"
	"reflect"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

func TestToolUsageAggregatesProviderCalls(t *testing.T) {
	tracker := NewToolUsage()
	ctx := WithToolUsage(context.Background(), tracker)

	RecordModelUsage(ctx, "vision-route", srvconfig.TokenUsage{
		InputTokens:              10,
		CachedInputTokens:        2,
		CacheReadInputTokens:     3,
		CacheCreationInputTokens: 4,
		OutputTokens:             5,
		TotalTokens:              24,
	})
	RecordModelUsage(ctx, "vision-route", srvconfig.TokenUsage{
		InputTokens:  7,
		OutputTokens: 1,
		TotalTokens:  8,
	})
	RecordModelUsage(ctx, "text-route", srvconfig.TokenUsage{
		InputTokens:  1,
		OutputTokens: 1,
		TotalTokens:  2,
	})

	snapshot := tracker.Snapshot()
	if snapshot.ProviderCalls != 3 {
		t.Fatalf("provider calls = %d, want 3", snapshot.ProviderCalls)
	}
	if want := []string{"vision-route", "text-route"}; !reflect.DeepEqual(snapshot.Models, want) {
		t.Fatalf("models = %#v, want %#v", snapshot.Models, want)
	}
	if snapshot.InputTokens != 18 || snapshot.CachedInputTokens != 2 ||
		snapshot.CacheReadInputTokens != 3 || snapshot.CacheCreationInputTokens != 4 ||
		snapshot.OutputTokens != 7 || snapshot.TotalTokens != 34 {
		t.Fatalf("token snapshot = %#v, want input 18, cached 2, cache-read 3, cache-create 4, output 7, total 34", snapshot)
	}
}

func TestToolUsageDerivesTotalWhenProviderOmitsIt(t *testing.T) {
	tracker := NewToolUsage()
	tracker.RecordModelUsage("vision-route", srvconfig.TokenUsage{
		InputTokens:       10,
		CachedInputTokens: 5,
		OutputTokens:      2,
	})

	if got := tracker.Snapshot().TotalTokens; got != 17 {
		t.Fatalf("derived total tokens = %d, want 17", got)
	}
}

func TestToolUsageWithoutContextIsIgnored(t *testing.T) {
	if ToolUsageFromContext(context.Background()) != nil {
		t.Fatal("background context unexpectedly carries tool usage")
	}
	RecordModelUsage(context.Background(), "vision-route", srvconfig.TokenUsage{InputTokens: 1})
}

func TestToolUsageIsScopedToItsContext(t *testing.T) {
	first := NewToolUsage()
	second := NewToolUsage()
	firstCtx := WithToolUsage(context.Background(), first)
	secondCtx := WithToolUsage(context.Background(), second)

	RecordModelUsage(firstCtx, "first-model", srvconfig.TokenUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2})
	RecordModelUsage(secondCtx, "second-model", srvconfig.TokenUsage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7})

	if first.Snapshot().ProviderCalls != 1 || first.Snapshot().Models[0] != "first-model" {
		t.Fatalf("first tracker = %#v, want only first-model", first.Snapshot())
	}
	if second.Snapshot().ProviderCalls != 1 || second.Snapshot().Models[0] != "second-model" {
		t.Fatalf("second tracker = %#v, want only second-model", second.Snapshot())
	}
}

func TestToolUsageTracksWhetherProviderReportedUsage(t *testing.T) {
	tracker := NewToolUsage()
	tracker.RecordModelUsage("image-route", srvconfig.TokenUsage{})
	snapshot := tracker.Snapshot()
	if snapshot.ProviderCalls != 1 || snapshot.UsageReported {
		t.Fatalf("usage snapshot = %#v, want one call with no reported usage", snapshot)
	}

	tracker.RecordModelUsage("vision-route", srvconfig.TokenUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12})
	snapshot = tracker.Snapshot()
	if !snapshot.UsageReported || snapshot.TotalTokens != 12 {
		t.Fatalf("usage snapshot = %#v, want reported usage of 12 tokens", snapshot)
	}
}
