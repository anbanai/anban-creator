package model

import "testing"

func TestContentAssetDefaultsAreStable(t *testing.T) {
	asset := NewContentAsset("user-1", "为什么努力没有结果")
	if asset.UserID != "user-1" || asset.Title != "为什么努力没有结果" {
		t.Fatalf("asset identity = %#v", asset)
	}
	if asset.Status != ContentAssetStatusCandidate || asset.Lifecycle != ContentAssetLifecycleEvergreen {
		t.Fatalf("asset defaults = status %q lifecycle %q", asset.Status, asset.Lifecycle)
	}
}

func TestContentReuseRunAllowsPlatformSpecificLineage(t *testing.T) {
	run := NewContentReuseRun("user-1", "asset-1", "project-1", "seednote")
	if run.Status != ContentReuseRunStatusQueued || run.Stage != ContentReuseStageResearch {
		t.Fatalf("run defaults = status %q stage %q", run.Status, run.Stage)
	}
	if run.TargetPlatform != "seednote" || run.IdempotencyKey == "" {
		t.Fatalf("run target/idempotency = %#v", run)
	}
}

func TestContentAutopilotPolicyDefaultsDoNotPublish(t *testing.T) {
	policy := NewContentAutopilotPolicy("user-1")
	if policy.Enabled || policy.PublishEnabled || policy.DraftEnabled {
		t.Fatalf("policy must be opt-in: %#v", policy)
	}
	if policy.FailureMode != ContentAutopilotFailurePause {
		t.Fatalf("failure mode = %q", policy.FailureMode)
	}
}
