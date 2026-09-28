package service

import "testing"

func TestResolveSeednotePostIdentityRejectsDuplicateExternalIdentity(t *testing.T) {
	postsByNoteID := map[string]string{"note-1": "post-new", "note-2": "post-old"}
	noteIDCounts := map[string]int{"note-1": 2, "note-2": 1}
	postsByURL := map[string]string{"https://example.test/note/1": "post-new"}
	urlCounts := map[string]int{"https://example.test/note/1": 1}
	postsByTask := map[string]string{"task-1": "post-new"}
	taskCounts := map[string]int{"task-1": 2}

	postID, ambiguous := resolveSeednotePostIdentity("note-1", "https://example.test/note/1", "task-1", postsByNoteID, noteIDCounts, postsByURL, urlCounts, postsByTask, taskCounts)
	if postID != "" || !ambiguous {
		t.Fatalf("resolveSeednotePostIdentity() = (%q, %t), want empty ID and ambiguity", postID, ambiguous)
	}
}

func TestResolveSeednotePostIdentityRejectsConflictingUniqueIdentities(t *testing.T) {
	postsByNoteID := map[string]string{"note-1": "post-1"}
	noteIDCounts := map[string]int{"note-1": 1}
	postsByURL := map[string]string{"https://example.test/note/1": "post-2"}
	urlCounts := map[string]int{"https://example.test/note/1": 1}

	postID, ambiguous := resolveSeednotePostIdentity("note-1", "https://example.test/note/1", "", postsByNoteID, noteIDCounts, postsByURL, urlCounts, nil, nil)
	if postID != "" || !ambiguous {
		t.Fatalf("resolveSeednotePostIdentity() = (%q, %t), want empty ID and ambiguity", postID, ambiguous)
	}
}
