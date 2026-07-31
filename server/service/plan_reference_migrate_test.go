package service

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestMigratePlanReferenceAttachments(t *testing.T) {
	t.Run("reference only", func(t *testing.T) {
		db := newPlanReferenceMigrateDB(t)
		asset := createPlanReferenceMigrateAsset(t, db, "asset-reference", "user-1")
		createPlanReferenceMigratePlan(t, db, model.Plan{ID: "plan-reference", UserID: asset.UserID, Status: model.PlanStatusActive, ReferenceImageAssetID: asset.ID})

		runPlanReferenceMigration(t, db)

		got := findPlanReferenceMigratePlan(t, db, "plan-reference")
		want := []model.EntryAttachment{{
			AssetID: asset.ID, Type: "image", FileName: asset.FileName,
			ContentType: asset.ContentType, Size: asset.Size,
		}}
		if !reflect.DeepEqual(got.InputAttachments.Data(), want) {
			t.Fatalf("attachments = %#v, want %#v", got.InputAttachments.Data(), want)
		}
		if got.ReferenceImageAssetID != "" {
			t.Fatalf("reference = %q, want cleared", got.ReferenceImageAssetID)
		}
	})

	t.Run("reference plus existing attachments", func(t *testing.T) {
		db := newPlanReferenceMigrateDB(t)
		asset := createPlanReferenceMigrateAsset(t, db, "asset-reference", "user-1")
		existing := []model.EntryAttachment{
			{Type: "text", Text: "brief", FileName: "brief.txt"},
			{Type: "image", URL: "https://example.com/existing.png", FileName: "existing.png"},
		}
		createPlanReferenceMigratePlan(t, db, model.Plan{
			ID: "plan-with-existing", UserID: asset.UserID, Status: model.PlanStatusActive,
			ReferenceImageAssetID: asset.ID, InputAttachments: datatypes.NewJSONType(existing),
		})

		runPlanReferenceMigration(t, db)

		got := findPlanReferenceMigratePlan(t, db, "plan-with-existing")
		attachments := got.InputAttachments.Data()
		if len(attachments) != 3 || attachments[0].AssetID != asset.ID || !reflect.DeepEqual(attachments[1:], existing) {
			t.Fatalf("attachments = %#v, want migrated asset followed by %#v", attachments, existing)
		}
		if got.ReferenceImageAssetID != "" {
			t.Fatalf("reference = %q, want cleared", got.ReferenceImageAssetID)
		}
	})

	t.Run("asset already present", func(t *testing.T) {
		db := newPlanReferenceMigrateDB(t)
		asset := createPlanReferenceMigrateAsset(t, db, "asset-reference", "user-1")
		existing := []model.EntryAttachment{
			{Type: "text", Text: "keep first"},
			{AssetID: asset.ID, Type: "image", FileName: "existing-name.png", Instruction: "keep metadata"},
		}
		createPlanReferenceMigratePlan(t, db, model.Plan{
			ID: "plan-existing-asset", UserID: asset.UserID, Status: model.PlanStatusActive,
			ReferenceImageAssetID: asset.ID, InputAttachments: datatypes.NewJSONType(existing),
		})

		runPlanReferenceMigration(t, db)

		got := findPlanReferenceMigratePlan(t, db, "plan-existing-asset")
		if !reflect.DeepEqual(got.InputAttachments.Data(), existing) {
			t.Fatalf("attachments = %#v, want unchanged %#v", got.InputAttachments.Data(), existing)
		}
		if got.ReferenceImageAssetID != "" {
			t.Fatalf("reference = %q, want cleared", got.ReferenceImageAssetID)
		}
	})

	t.Run("inactive plans", func(t *testing.T) {
		for _, status := range []string{model.PlanStatusPaused, model.PlanStatusCompleted} {
			t.Run(status, func(t *testing.T) {
				db := newPlanReferenceMigrateDB(t)
				asset := createPlanReferenceMigrateAsset(t, db, "asset-reference", "user-1")
				existing := []model.EntryAttachment{{Type: "text", Text: "unchanged"}}
				createPlanReferenceMigratePlan(t, db, model.Plan{
					ID: "plan-inactive", UserID: asset.UserID, Status: status,
					ReferenceImageAssetID: asset.ID, InputAttachments: datatypes.NewJSONType(existing),
				})

				runPlanReferenceMigration(t, db)

				got := findPlanReferenceMigratePlan(t, db, "plan-inactive")
				if got.ReferenceImageAssetID != asset.ID || !reflect.DeepEqual(got.InputAttachments.Data(), existing) {
					t.Fatalf("inactive plan changed: reference=%q attachments=%#v", got.ReferenceImageAssetID, got.InputAttachments.Data())
				}
			})
		}
	})

	t.Run("invalid missing or foreign asset", func(t *testing.T) {
		tests := []struct {
			name    string
			assetID string
			seed    func(*testing.T, *gorm.DB)
		}{
			{
				name: "invalid purpose", assetID: "asset-invalid-purpose",
				seed: func(t *testing.T, db *gorm.DB) {
					asset := createPlanReferenceMigrateAsset(t, db, "asset-invalid-purpose", "user-1")
					if err := db.Model(&asset).Update("purpose", DirectUploadPurposeProjectReference).Error; err != nil {
						t.Fatal(err)
					}
				},
			},
			{
				name: "invalid image metadata", assetID: "asset-invalid-image",
				seed: func(t *testing.T, db *gorm.DB) {
					asset := createPlanReferenceMigrateAsset(t, db, "asset-invalid-image", "user-1")
					if err := db.Model(&asset).Update("content_type", "application/pdf").Error; err != nil {
						t.Fatal(err)
					}
				},
			},
			{name: "missing", assetID: "asset-missing", seed: func(*testing.T, *gorm.DB) {}},
			{
				name: "foreign", assetID: "asset-foreign",
				seed: func(t *testing.T, db *gorm.DB) {
					createPlanReferenceMigrateAsset(t, db, "asset-foreign", "user-2")
				},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				db := newPlanReferenceMigrateDB(t)
				tt.seed(t, db)
				existing := []model.EntryAttachment{{Type: "text", Text: "unchanged"}}
				createPlanReferenceMigratePlan(t, db, model.Plan{
					ID: "plan-invalid", UserID: "user-1", Status: model.PlanStatusActive,
					ReferenceImageAssetID: tt.assetID, InputAttachments: datatypes.NewJSONType(existing),
				})

				err := MigratePlanReferenceAttachments(context.Background(), db, nil)
				if err == nil {
					t.Fatal("migration error = nil, want validation failure")
				}
				got := findPlanReferenceMigratePlan(t, db, "plan-invalid")
				if got.ReferenceImageAssetID != tt.assetID || !reflect.DeepEqual(got.InputAttachments.Data(), existing) {
					t.Fatalf("invalid plan changed: reference=%q attachments=%#v", got.ReferenceImageAssetID, got.InputAttachments.Data())
				}
			})
		}
	})

	t.Run("rollback", func(t *testing.T) {
		db := newPlanReferenceMigrateDB(t)
		firstAsset := createPlanReferenceMigrateAsset(t, db, "asset-first", "user-1")
		secondAsset := createPlanReferenceMigrateAsset(t, db, "asset-second", "user-1")
		createPlanReferenceMigratePlan(t, db, model.Plan{ID: "a-first", UserID: "user-1", Status: model.PlanStatusActive, ReferenceImageAssetID: firstAsset.ID})
		createPlanReferenceMigratePlan(t, db, model.Plan{ID: "b-fail", UserID: "user-1", Status: model.PlanStatusActive, ReferenceImageAssetID: secondAsset.ID})
		if err := db.Exec(`CREATE TRIGGER fail_plan_reference_migration BEFORE UPDATE ON plans WHEN OLD.id = 'b-fail' BEGIN SELECT RAISE(FAIL, 'forced plan update failure'); END`).Error; err != nil {
			t.Fatalf("create failure trigger: %v", err)
		}

		err := MigratePlanReferenceAttachments(context.Background(), db, nil)
		if err == nil || !strings.Contains(err.Error(), "forced plan update failure") {
			t.Fatalf("migration error = %v, want forced persistence failure", err)
		}
		for _, planID := range []string{"a-first", "b-fail"} {
			got := findPlanReferenceMigratePlan(t, db, planID)
			if got.ReferenceImageAssetID == "" || len(got.InputAttachments.Data()) != 0 {
				t.Fatalf("plan %s was not rolled back: reference=%q attachments=%#v", planID, got.ReferenceImageAssetID, got.InputAttachments.Data())
			}
		}
	})

	t.Run("idempotent second run", func(t *testing.T) {
		db := newPlanReferenceMigrateDB(t)
		asset := createPlanReferenceMigrateAsset(t, db, "asset-reference", "user-1")
		createPlanReferenceMigratePlan(t, db, model.Plan{ID: "plan-idempotent", UserID: asset.UserID, Status: model.PlanStatusActive, ReferenceImageAssetID: asset.ID})

		runPlanReferenceMigration(t, db)
		first := findPlanReferenceMigratePlan(t, db, "plan-idempotent")
		runPlanReferenceMigration(t, db)
		second := findPlanReferenceMigratePlan(t, db, "plan-idempotent")

		if !reflect.DeepEqual(second.InputAttachments.Data(), first.InputAttachments.Data()) || second.ReferenceImageAssetID != first.ReferenceImageAssetID {
			t.Fatalf("second run changed plan: first=%#v second=%#v", first, second)
		}
	})
}

func newPlanReferenceMigrateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newMigrateTestDB(t)
	if err := db.AutoMigrate(&model.Plan{}, &model.Asset{}); err != nil {
		t.Fatalf("auto-migrate plan reference fixtures: %v", err)
	}
	return db
}

func createPlanReferenceMigrateAsset(t *testing.T, db *gorm.DB, id, userID string) model.Asset {
	t.Helper()
	asset := model.Asset{
		ID: id, UserID: userID, Purpose: DirectUploadPurposeTaskReference,
		StorageKey: "assets/users/" + userID + "/" + id + "/reference.png",
		FileName:   "reference.png", ContentType: "image/png", Size: 1234, ETag: "etag-" + id,
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatalf("create asset: %v", err)
	}
	return asset
}

func createPlanReferenceMigratePlan(t *testing.T, db *gorm.DB, plan model.Plan) {
	t.Helper()
	if err := db.Create(&plan).Error; err != nil {
		t.Fatalf("create plan: %v", err)
	}
}

func findPlanReferenceMigratePlan(t *testing.T, db *gorm.DB, id string) model.Plan {
	t.Helper()
	var plan model.Plan
	if err := db.First(&plan, "id = ?", id).Error; err != nil {
		t.Fatalf("find plan: %v", err)
	}
	return plan
}

func runPlanReferenceMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	log := zerolog.Nop()
	if err := MigratePlanReferenceAttachments(context.Background(), db, &log); err != nil {
		t.Fatalf("migrate plan references: %v", err)
	}
}
