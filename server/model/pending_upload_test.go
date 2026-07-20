package model

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAutoMigrateAddsPendingUploadFinalizedKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Exec(`CREATE TABLE pending_uploads (id TEXT PRIMARY KEY, key TEXT NOT NULL)`).Error; err != nil {
		t.Fatalf("create legacy pending_uploads: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	if !db.Migrator().HasColumn(&PendingUpload{}, "FinalizedKey") {
		t.Fatal("pending_uploads.finalized_key was not migrated")
	}
}
