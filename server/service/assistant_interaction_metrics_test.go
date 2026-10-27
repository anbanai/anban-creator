package service

import (
	"expvar"
	"testing"
	"time"
)

func TestAssistantInteractionDurableLatencyMetricsExposePercentiles(t *testing.T) {
	metrics := &interactionMetrics{}
	for _, sample := range []int64{10, 20, 50, 100} {
		metrics.observeDraftStage("durable", time.Duration(sample)*time.Millisecond)
	}

	snapshot := metrics.snapshot()
	if snapshot.DraftDurableP50Millis != 20 || snapshot.DraftDurableP95Millis != 100 {
		t.Fatalf("durable latency percentiles = p50:%d p95:%d, want p50:20 p95:100", snapshot.DraftDurableP50Millis, snapshot.DraftDurableP95Millis)
	}
	for key, want := range map[string]int64{"p50_ms": 20, "p95_ms": 100} {
		metric, ok := assistantDraftDurableLatency.Get(key).(*expvar.Int)
		if !ok {
			t.Fatalf("expvar durable latency metric %q = %T, want *expvar.Int", key, assistantDraftDurableLatency.Get(key))
		}
		if got := metric.Value(); got != want {
			t.Fatalf("expvar durable latency metric %q = %d, want %d", key, got, want)
		}
	}
}
