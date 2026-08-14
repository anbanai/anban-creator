package service

import (
	"context"
	"testing"
)

func TestMigrateDesignerRemovalDropsLegacyTablesAndIsIdempotent(t *testing.T) {
	db := newMigrateTestDB(t)
	for _, statement := range []string{
		`CREATE TABLE designer_references (id text primary key)`,
		`CREATE TABLE image_generations (id text primary key)`,
		`CREATE TABLE image_generation_results (id integer primary key, generation_id text)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 2; i++ {
		if err := MigrateDesignerRemoval(context.Background(), db, nil); err != nil {
			t.Fatalf("migration run %d: %v", i+1, err)
		}
	}
	for _, table := range []string{"designer_references", "image_generation_results", "image_generations"} {
		if db.Migrator().HasTable(table) {
			t.Fatalf("%s still exists", table)
		}
	}
}

func TestMigrateDesignerRemovalAllowsFreshSchema(t *testing.T) {
	db := newMigrateTestDB(t)
	if err := MigrateDesignerRemoval(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
}
