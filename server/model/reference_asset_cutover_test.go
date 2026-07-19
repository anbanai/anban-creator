package model

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPlanAndTaskSchemasDoNotCreateReferenceImageURL(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&Plan{}, &Task{}); err != nil {
		t.Fatalf("migrate models: %v", err)
	}
	for _, value := range []any{&Plan{}, &Task{}} {
		if db.Migrator().HasColumn(value, "reference_image_url") {
			t.Fatalf("%T.reference_image_url exists, want asset-only persistence", value)
		}
	}
}

func TestPlanTaskAndSnapshotSerializeOnlyTransientReferenceView(t *testing.T) {
	view := &AssetView{AssetID: "asset-1", FileName: "ref.png", ContentType: "image/png", Size: 3}
	for name, value := range map[string]any{
		"plan":     &Plan{ReferenceImageAssetID: "asset-1", ReferenceImage: view, ReferenceImageURL: "legacy"},
		"task":     &Task{ReferenceImageAssetID: "asset-1", ReferenceImage: view, ReferenceImageURL: "legacy"},
		"snapshot": ProjectSnapshot{ReferenceImageAssetID: "asset-1", ReferenceImageURL: "legacy"},
	} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		if _, exists := got["reference_image_url"]; exists {
			t.Fatalf("%s exposed legacy reference_image_url: %s", name, raw)
		}
		if name == "snapshot" {
			if got["reference_image_asset_id"] != "asset-1" {
				t.Fatalf("snapshot asset id = %#v", got["reference_image_asset_id"])
			}
		} else if _, exists := got["reference_image"]; !exists {
			t.Fatalf("%s omitted transient reference view: %s", name, raw)
		}
	}
}

func TestSnapshotProjectCopiesReferenceImageAssetID(t *testing.T) {
	project := &Project{Platform: PlatformArticle, ReferenceImageAssetID: "asset-project", ReferenceImageURL: "legacy"}
	snapshot := SnapshotProject(project)
	if snapshot.ReferenceImageAssetID != "asset-project" || snapshot.ReferenceImageURL != "" {
		t.Fatalf("snapshot reference = %#v, want asset-project only", snapshot)
	}
	restored := ProjectFromSnapshot(&Project{}, snapshot)
	if restored.ReferenceImageAssetID != "asset-project" || restored.ReferenceImageURL != "" {
		t.Fatalf("restored project reference = %#v, want asset-project only", restored)
	}
}
