package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTopicDigestCurrentExecutionResearch(t *testing.T) {
	for _, tc := range []struct {
		name, scope, status string
		empty, wantOK       bool
	}{
		{"search", "current", "succeeded", false, true},
		{"fetch", "current", "succeeded", false, true},
		{"foreign task", "task", "succeeded", false, false},
		{"foreign project", "project", "succeeded", false, false},
		{"foreign execution", "foreign", "succeeded", false, false},
		{"failed provider", "current", "failed", false, false},
		{"empty success", "current", "succeeded", true, true},
		{"empty provider failure", "current", "failed", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := topicRadarTestRepository(t)
			sub := seedTopicRadarSubscription(t, repo)
			svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
			digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID, RequestKey: tc.name})
			if err != nil {
				t.Fatal(err)
			}
			raw := `{"schema_version":"1.0","suggestions":[{"title":"New","introduction":"Intro","source_summary":"Evidence","source_refs":[{"type":"search","ref_id":"receipt","url":"https://example.test/new","title":"Forged"}],"scores":{},"video_fit":"Video"}]}`
			if tc.empty {
				raw = `{"schema_version":"1.0","outcome":"no_new_candidates","suggestions":[]}`
			}
			body := topicDigestTestArtifact(t, repo, digest, []byte(raw))
			execution := *digest.AgentExecutionID
			if tc.scope == "foreign" {
				execution = "other"
			}
			receipt := &model.SearchOperation{ID: "receipt", Operation: "mcp.search_web", UserID: sub.UserID, ProjectID: sub.ProjectID, TaskID: *digest.AgentTaskID, ExecutionID: execution, RequestFingerprint: "receipt", Status: model.SearchOperationStatus(tc.status), ResponseSnapshot: []byte(`{"results":[{"title":"Verified","url":"https://example.test/new"}]}`)}
			if tc.scope == "task" {
				receipt.TaskID = "foreign-task"
			}
			if tc.scope == "project" {
				receipt.ProjectID = "foreign-project"
			}
			if tc.name == "fetch" {
				receipt.Operation = "mcp.fetch_web_page"
				receipt.ResponseSnapshot = []byte(`{"title":"Verified","url":"https://example.test/new","content_markdown":"Source"}`)
			}
			if err := repo.Analytics().DB().Create(receipt).Error; err != nil {
				t.Fatal(err)
			}
			err = svc.FinalizeDigest(ctx, digest.ID, body)
			if (err == nil) != tc.wantOK {
				t.Fatalf("finalize error=%v wantOK=%v", err, tc.wantOK)
			}
			if tc.wantOK {
				got, _ := repo.TopicDigests().FindByID(ctx, digest.ID)
				if got.Status != model.TopicDigestStatusSucceeded {
					t.Fatal(got.Status)
				}
				rows, _ := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
				if !tc.empty && (len(rows) != 1 || string(rows[0].SourceRefs) == "") {
					t.Fatal(rows)
				}
				if !tc.empty {
					var refs []model.TopicSourceRef
					if err := json.Unmarshal(rows[0].SourceRefs, &refs); err != nil {
						t.Fatal(err)
					}
					if refs[0].Title != "Verified" {
						t.Fatalf("untrusted metadata: %+v", refs)
					}
				}
			}
		})
	}
}
