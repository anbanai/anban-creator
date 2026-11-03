package model

import (
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAutoMigrateCreatesDesignerTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"designer_references", "image_generations", "image_generation_results"} {
		if !db.Migrator().HasTable(table) {
			t.Errorf("missing %s", table)
		}
	}
}

func TestAutoMigratePreservesDesignerGeneration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:designer-preserve?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	gen := ImageGeneration{ID: "generation-1", UserID: "user-1", Prompt: "city", Provider: "test", Model: "test", Status: ImageGenerationStatusCompleted, RequestFingerprint: "fingerprint"}
	if err := db.Create(&gen).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ImageGenerationResult{GenerationID: gen.ID, ImageURL: "https://example.test/image.png", Index: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	var persisted ImageGeneration
	if err := db.Preload("Results").First(&persisted, "id = ?", gen.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%s:%d", persisted.Prompt, len(persisted.Results)); got != "city:1" {
		t.Fatalf("generation after migration = %s, want city:1", got)
	}
}
