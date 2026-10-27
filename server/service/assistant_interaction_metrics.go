package service

import (
	"expvar"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// InteractionMetricsSnapshot is intentionally limited to counters and timing
// buckets. It never contains user text, draft payloads, tokens, or runtime
// credentials.
type InteractionMetricsSnapshot struct {
	Recovery                        map[string]uint64
	DraftStage                      map[string]uint64
	DraftStageRetryAfterSessionLoss uint64
	CommitDuplicatePrevented        uint64
	RuntimeGenerationReplaced       uint64
	DraftDurableP50Millis           int64
	DraftDurableP95Millis           int64
}

type interactionMetrics struct {
	mu                              sync.Mutex
	recovery                        map[string]uint64
	draftStage                      map[string]uint64
	draftStageRetryAfterSessionLoss uint64
	commitDuplicatePrevented        uint64
	runtimeGenerationReplaced       uint64
	durableLatency                  []int64
}

var (
	assistantInteractionRecoveryTotal        = expvar.NewMap("assistant_interaction_recovery_total")
	assistantDraftStageTotal                 = expvar.NewMap("assistant_draft_stage_total")
	assistantDraftStageRetryAfterSessionLoss = expvar.NewInt("assistant_draft_stage_retry_after_session_loss_total")
	assistantCommitDuplicatePrevented        = expvar.NewInt("assistant_commit_duplicate_prevented_total")
	assistantRuntimeGenerationReplaced       = expvar.NewInt("assistant_runtime_generation_replaced_total")
	assistantDraftDurableLatency             = expvar.NewMap("assistant_draft_durable_latency_ms")
	assistantDraftDurableLatencyLast         expvar.Int
	assistantDraftDurableLatencyP50          expvar.Int
	assistantDraftDurableLatencyP95          expvar.Int
)

func init() {
	assistantDraftDurableLatency.Set("last_ms", &assistantDraftDurableLatencyLast)
	assistantDraftDurableLatency.Set("p50_ms", &assistantDraftDurableLatencyP50)
	assistantDraftDurableLatency.Set("p95_ms", &assistantDraftDurableLatencyP95)
}

func (m *interactionMetrics) observeRecovery(cause, outcome string) {
	if m == nil {
		return
	}
	key := "cause=" + metricLabel(cause) + ",outcome=" + metricLabel(outcome)
	m.mu.Lock()
	if m.recovery == nil {
		m.recovery = map[string]uint64{}
	}
	m.recovery[key]++
	m.mu.Unlock()
	assistantInteractionRecoveryTotal.Add(key, 1)
}

func metricLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, runeValue := range value {
		if unicode.IsLetter(runeValue) || unicode.IsDigit(runeValue) || runeValue == '_' || runeValue == '-' {
			builder.WriteRune(runeValue)
		} else {
			builder.WriteByte('_')
		}
		if builder.Len() >= 64 {
			break
		}
	}
	if builder.Len() == 0 {
		return "unknown"
	}
	return builder.String()
}

func (m *interactionMetrics) observeDraftStage(outcome string, latency time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.draftStage == nil {
		m.draftStage = map[string]uint64{}
	}
	m.draftStage[outcome]++
	if outcome == "durable" && latency >= 0 {
		if len(m.durableLatency) >= 4096 {
			copy(m.durableLatency, m.durableLatency[len(m.durableLatency)-2047:])
			m.durableLatency = m.durableLatency[:2048]
		}
		m.durableLatency = append(m.durableLatency, latency.Milliseconds())
		values := append([]int64(nil), m.durableLatency...)
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		assistantDraftDurableLatencyLast.Set(latency.Milliseconds())
		assistantDraftDurableLatencyP50.Set(nearestRankPercentile(values, 50))
		assistantDraftDurableLatencyP95.Set(nearestRankPercentile(values, 95))
	}
	m.mu.Unlock()
	assistantDraftStageTotal.Add(outcome, 1)
	if outcome == "durable" {
		assistantDraftDurableLatency.Add("count", 1)
	}
}

func (m *interactionMetrics) observeStageRetryAfterSessionLoss() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.draftStageRetryAfterSessionLoss++
	m.mu.Unlock()
	assistantDraftStageRetryAfterSessionLoss.Add(1)
}

func (m *interactionMetrics) observeCommitDuplicate() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.commitDuplicatePrevented++
	m.mu.Unlock()
	assistantCommitDuplicatePrevented.Add(1)
}

func (m *interactionMetrics) observeGenerationReplacement() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.runtimeGenerationReplaced++
	m.mu.Unlock()
	assistantRuntimeGenerationReplaced.Add(1)
}

func (m *interactionMetrics) snapshot() InteractionMetricsSnapshot {
	if m == nil {
		return InteractionMetricsSnapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := InteractionMetricsSnapshot{
		Recovery:                        map[string]uint64{},
		DraftStage:                      map[string]uint64{},
		DraftStageRetryAfterSessionLoss: m.draftStageRetryAfterSessionLoss,
		CommitDuplicatePrevented:        m.commitDuplicatePrevented,
		RuntimeGenerationReplaced:       m.runtimeGenerationReplaced,
	}
	for key, value := range m.recovery {
		snapshot.Recovery[key] = value
	}
	for key, value := range m.draftStage {
		snapshot.DraftStage[key] = value
	}
	if len(m.durableLatency) > 0 {
		values := append([]int64(nil), m.durableLatency...)
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		snapshot.DraftDurableP50Millis = nearestRankPercentile(values, 50)
		snapshot.DraftDurableP95Millis = nearestRankPercentile(values, 95)
	}
	return snapshot
}

func nearestRankPercentile(sortedValues []int64, percentile int) int64 {
	if len(sortedValues) == 0 {
		return 0
	}
	rank := (percentile*len(sortedValues) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return sortedValues[rank-1]
}
