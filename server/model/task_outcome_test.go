package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskPublicJSONHidesInternalResultAndOmitsEmptyOutcome(t *testing.T) {
	result := `{"secret":"internal evidence"}`
	task := Task{ID: "task-1", Result: &result}
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "result") || strings.Contains(string(raw), "internal evidence") {
		t.Fatalf("public task JSON exposed internal result: %s", raw)
	}
	if strings.Contains(string(raw), "outcome") {
		t.Fatalf("public task JSON exposed an empty outcome: %s", raw)
	}
}

func TestTaskPublicJSONIncludesTypedOutcome(t *testing.T) {
	task := Task{ID: "task-1", Outcome: &TaskOutcome{
		CoreDelivery: TaskCoreDeliveryOutcome{Status: TaskCoreDeliveryComplete},
		Visual:       TaskVisualOutcome{Status: TaskVisualPartial},
		Review:       TaskReviewOutcome{Status: TaskReviewWarning},
		Publication:  TaskPublicationOutcome{Status: TaskPublicationSkipped},
		Warnings:     []TaskOutcomeWarning{},
	}}
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"outcome"`, `"core_delivery":{"status":"complete"}`, `"visual":{"status":"partial"}`, `"publication":{"status":"skipped","attempted":false}`} {
		if !strings.Contains(string(raw), fragment) {
			t.Fatalf("public task JSON missing %s: %s", fragment, raw)
		}
	}
}
