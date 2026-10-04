package main

import (
	"context"
	"strings"
	"testing"
)

func TestReopenRequiresOperatorReviewBeforeDatabaseAccess(t *testing.T) {
	for _, args := range [][]string{{"--reopen"}, {"--reopen", "--reviewed-identity"}, {"--reopen", "--evidence", "review.json"}} {
		err := run(context.Background(), args)
		if err == nil || !strings.Contains(err.Error(), "reviewed-identity and evidence") {
			t.Fatalf("run(%v) = %v", args, err)
		}
	}
}

func TestReviewRequiresExplicitEmptyIdentityMembers(t *testing.T) {
	raw := []byte(`{"execution_id":"execution","task_id":"task","target":"docker","runtime_profile":"historical","runtime_image":"historical-image","scope":"","workload":"","instance_id":"","attempts":1,"diagnostic":"runtime_identity_conflict"}`)
	if _, err := parseReview(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"instance_id":"",`, "", 1), strings.Replace(string(raw), `"instance_id":""`, `"instance_id":null`, 1), string(raw) + " {}", strings.Replace(string(raw), `"attempts":1`, `"attempts":0`, 1)} {
		if _, err := parseReview([]byte(bad)); err == nil {
			t.Fatal("incomplete review accepted")
		}
	}
}
