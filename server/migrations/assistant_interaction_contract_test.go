package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestAssistantInteractionMigrationDeclaresDurableBoundary(t *testing.T) {
	raw, err := os.ReadFile("20261026_assistant_interactions.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, table := range []string{"assistant_interactions", "assistant_interaction_runtimes", "assistant_interaction_drafts", "assistant_interaction_events", "assistant_interaction_attempts"} {
		if !strings.Contains(text, table) {
			t.Fatalf("migration missing %s", table)
		}
	}
	if !strings.Contains(text, "abandoned") || !strings.Contains(text, "24 HOUR") || !strings.Contains(text, "30 DAY") {
		t.Fatal("migration does not document retention and old-session cutover")
	}
	for _, required := range []string{"assistant_interaction_legacy_migrations", "homepage_interaction_cutover", "UPDATE assistant_sessions", "workspace_id <> ''"} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration missing legacy cutover contract %q", required)
		}
	}
	for _, required := range []string{"UPDATE assistant_workspaces", "runtime_status = 'closed'", "closed_at = COALESCE(closed_at"} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration does not release legacy workspace %q", required)
		}
	}
	if strings.Contains(text, "SET s.status") || strings.Contains(text, "SET s.closed_at") || strings.Contains(text, "SET s.updated_at") {
		t.Fatal("migration uses a qualified assignment target that is rejected by some MySQL versions")
	}
	for _, required := range []string{
		"generation bigint NOT NULL",
		"status varchar(24) NOT NULL DEFAULT 'pending'",
		"idx_assistant_interaction_event_generation",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration missing generation/attempt fencing contract %q", required)
		}
	}
}

func TestAssistantInteractionCommitClaimLeaseMigrationIsAdditive(t *testing.T) {
	raw, err := os.ReadFile("20261027_assistant_interaction_commit_claim_lease.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"UPDATE assistant_workspaces w",
		"JOIN assistant_sessions s ON s.workspace_id = w.id",
		"SET runtime_status = 'closed'",
		"closed_at = COALESCE(w.closed_at",
		"ALTER TABLE assistant_interaction_attempts",
		"ADD COLUMN user_id char(36) NOT NULL DEFAULT ''",
		"ADD COLUMN claim_token char(36) NOT NULL",
		"ADD COLUMN claim_expires_at datetime(6) NULL",
		"UPDATE assistant_interaction_attempts",
		"JOIN assistant_interactions",
		"idx_assistant_interaction_attempt_user_key (user_id, operation, attempt_id)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("commit claim lease migration missing %q", required)
		}
	}
}
