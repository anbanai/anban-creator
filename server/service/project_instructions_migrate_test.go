package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestMigrateProjectPositioningToInstructionsBackfillsOnlyBlankInstructions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Project{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	ctx := context.Background()
	log := zerolog.New(zerolog.NewTestWriter(t))

	rows := []*model.Project{
		{
			ID:          "legacy-positioning-project",
			UserID:      "user-1",
			Platform:    model.PlatformSeednote,
			Name:        "Legacy",
			Positioning: "旧定位",
			Status:      model.ProjectStatusActive,
		},
		{
			ID:           "canonical-instructions-project",
			UserID:       "user-1",
			Platform:     model.PlatformSeednote,
			Name:         "Canonical",
			Positioning:  "旧定位不应覆盖",
			Instructions: "新定位",
			Status:       model.ProjectStatusActive,
		},
	}
	for _, row := range rows {
		if err := repo.Projects().Create(ctx, row); err != nil {
			t.Fatalf("create project %s: %v", row.ID, err)
		}
	}

	if err := MigrateProjectPositioningToInstructions(ctx, db, &log); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	legacy, err := repo.Projects().FindByID(ctx, "legacy-positioning-project")
	if err != nil {
		t.Fatalf("find legacy: %v", err)
	}
	if legacy.Instructions != "旧定位" {
		t.Fatalf("legacy Instructions = %q, want 旧定位", legacy.Instructions)
	}
	canonical, err := repo.Projects().FindByID(ctx, "canonical-instructions-project")
	if err != nil {
		t.Fatalf("find canonical: %v", err)
	}
	if canonical.Instructions != "新定位" {
		t.Fatalf("canonical Instructions = %q, want 新定位", canonical.Instructions)
	}
}
