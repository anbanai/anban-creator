package model

import (
	"testing"
	"time"
)

func TestTopicRadarOccurrenceKeyNormalizesScheduledTimeToUTC(t *testing.T) {
	planned := time.Date(2026, 10, 31, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	got := ScheduledTopicOccurrenceKey("sub-1", planned)
	want := "sched:sub-1:2026-10-31T01:30:00Z"
	if got != want {
		t.Fatalf("ScheduledTopicOccurrenceKey() = %q, want %q", got, want)
	}
}

func TestTopicPoolDedupeKeyCollapsesWhitespaceAndCase(t *testing.T) {
	got := NormalizeTopicDedupeKey("  低成本 直播 选题  ")
	want := "低成本 直播 选题"
	if got != want {
		t.Fatalf("NormalizeTopicDedupeKey() = %q, want %q", got, want)
	}
	if got != NormalizeTopicDedupeKey("低成本 直播 选题") {
		t.Fatal("equivalent topic titles must share a dedupe key")
	}
}

func TestTopicRadarSourceRefRequiresResolvableReference(t *testing.T) {
	cases := []struct {
		name string
		ref  TopicSourceRef
		want bool
	}{
		{name: "url", ref: TopicSourceRef{Type: "external", URL: "https://example.com/a"}, want: true},
		{name: "id", ref: TopicSourceRef{Type: "trend", RefID: "trend-1"}, want: true},
		{name: "label only", ref: TopicSourceRef{Type: "trend", Label: "hot"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ref.Resolvable(); got != tc.want {
				t.Fatalf("Resolvable() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTopicDigestStatesAllowOnlyDeclaredTransitions(t *testing.T) {
	valid := [][2]string{
		{TopicDigestStatusQueued, TopicDigestStatusPreparing},
		{TopicDigestStatusPreparing, TopicDigestStatusDispatched},
		{TopicDigestStatusDispatched, TopicDigestStatusRunning},
		{TopicDigestStatusRunning, TopicDigestStatusFinalizing},
		{TopicDigestStatusFinalizing, TopicDigestStatusSucceeded},
		{TopicDigestStatusFinalizing, TopicDigestStatusFailed},
	}
	for _, transition := range valid {
		if !CanTransitionTopicDigest(transition[0], transition[1]) {
			t.Fatalf("transition %q -> %q should be allowed", transition[0], transition[1])
		}
	}
	if CanTransitionTopicDigest(TopicDigestStatusSucceeded, TopicDigestStatusRunning) {
		t.Fatal("terminal digest must not transition back to running")
	}
}

func TestTopicRadarDefaultWeightsSumToOne(t *testing.T) {
	weights := DefaultTopicRadarWeights()
	var total float64
	for _, weight := range weights {
		total += weight
	}
	if total < 0.999 || total > 1.001 {
		t.Fatalf("default score weights sum to %v, want 1", total)
	}
}
