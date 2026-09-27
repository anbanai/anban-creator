package service

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// MigrateCoverPortrait runs before AutoMigrate so renaming the persisted option
// preserves existing selections without introducing a second public contract.
// A successful rename is also the migration marker: subsequent startups cannot
// restore an old true value after the user has disabled portrait covers.
func MigrateCoverPortrait(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return nil
	}
	db = db.WithContext(ctx)
	for _, table := range []string{"tasks", "plans"} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		columns, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			return fmt.Errorf("inspect %s portrait cover columns: %w", table, err)
		}
		hasOld, hasNew := false, false
		for _, column := range columns {
			hasOld = hasOld || column.Name() == "article_cover_use_portrait"
			hasNew = hasNew || column.Name() == "cover_use_portrait"
		}
		if !hasOld {
			continue
		}
		if hasNew {
			return fmt.Errorf("cannot migrate %s portrait cover: both old and new columns exist; reconcile selections before restarting", table)
		}
		if err := db.Migrator().RenameColumn(table, "article_cover_use_portrait", "cover_use_portrait"); err != nil {
			return fmt.Errorf("rename %s portrait cover option: %w", table, err)
		}
	}
	return nil
}
