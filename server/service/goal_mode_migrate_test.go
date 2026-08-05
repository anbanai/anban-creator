package service

import (
	"context"
	"testing"
)

func TestMigrateGoalModeRemovalDropsLegacyColumnsAndIsIdempotent(t *testing.T) {
	db := newMigrateTestDB(t)
	for _, statement := range []string{
		`CREATE TABLE tasks (id text primary key, prompt text, goal text, goal_mode numeric)`,
		`CREATE TABLE plans (id text primary key, prompt text, goal text, goal_mode numeric)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 2; i++ {
		if err := MigrateGoalModeRemoval(context.Background(), db, nil); err != nil {
			t.Fatalf("migration run %d: %v", i+1, err)
		}
	}
	for _, table := range []string{"tasks", "plans"} {
		if !db.Migrator().HasColumn(table, "prompt") {
			t.Fatalf("%s.prompt was removed", table)
		}
		for _, column := range []string{"goal", "goal_mode"} {
			if db.Migrator().HasColumn(table, column) {
				t.Fatalf("%s.%s still exists", table, column)
			}
		}
	}
}

func TestMigrateGoalModeRemovalAllowsFreshSchema(t *testing.T) {
	db := newMigrateTestDB(t)
	if err := MigrateGoalModeRemoval(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
}
