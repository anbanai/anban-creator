package repository

import (
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWechatAnalyticsSnapshotIsIdempotentPerImportRow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.WechatAnalyticsSnapshot{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	t.Cleanup(func() { _ = repo.Close() })
	now := time.Now()
	first := &model.WechatAnalyticsSnapshot{ID: uuid.NewString(), ProjectID: "project-1", PublicationID: "publication-1", BatchID: "batch-1", ImportRowID: "row-1", Source: "import", DataAsOfAt: now, ImportedAt: now, RawData: `{}`}
	second := *first
	second.ID = uuid.NewString()
	if err := repo.WechatAnalyticsImports().CreateSnapshot(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := repo.WechatAnalyticsImports().CreateSnapshot(context.Background(), &second); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.WechatAnalyticsSnapshot{}).Where("import_row_id = ?", "row-1").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("snapshot count = %d, want 1", count)
	}
}
