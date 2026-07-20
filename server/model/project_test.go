package model

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProjectSchemaDoesNotCreateReferenceImageURL(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&Project{}); err != nil {
		t.Fatalf("migrate project: %v", err)
	}
	if db.Migrator().HasColumn(&Project{}, "reference_image_url") {
		t.Fatal("projects.reference_image_url exists, want asset-only project persistence")
	}
}
