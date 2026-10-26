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
	for _, required := range []string{"generation bigint NOT NULL", "status varchar(24) NOT NULL DEFAULT 'pending'", "idx_assistant_interaction_event_generation"} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration missing generation/attempt fencing contract %q", required)
		}
	}
}
