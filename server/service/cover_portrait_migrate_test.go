package service

import (
	"fmt"
	"testing"
)

func TestMigrateCoverPortraitPreservesSelectionsAndIsIdempotent(t *testing.T) {
	db := newMigrateTestDB(t)
	for _, table := range []string{"tasks", "plans"} {
		if err := db.Exec(fmt.Sprintf("CREATE TABLE `%s` (id text primary key, article_cover_use_portrait numeric NOT NULL DEFAULT 0)", table)).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(fmt.Sprintf("INSERT INTO `%s` VALUES ('selected', 1), ('ordinary', 0)", table)).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := MigrateCoverPortrait(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"tasks", "plans"} {
		var rows []struct {
			ID               string
			CoverUsePortrait bool
		}
		if err := db.Table(table).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || rows[0].CoverUsePortrait || !rows[1].CoverUsePortrait {
			t.Fatalf("%s selections=%+v", table, rows)
		}
		if db.Migrator().HasColumn(table, "article_cover_use_portrait") {
			t.Fatalf("%s still has obsolete column", table)
		}
		// A later user opt-out must stay false after a repeated server startup.
		if err := db.Table(table).Where("id = ?", "selected").Update("cover_use_portrait", false).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := MigrateCoverPortrait(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"tasks", "plans"} {
		var count int64
		if err := db.Table(table).Where("cover_use_portrait = ?", true).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s opt-out overwritten", table)
		}
	}
}

func TestMigrateCoverPortraitAllowsFreshSchema(t *testing.T) {
	if err := MigrateCoverPortrait(t.Context(), newMigrateTestDB(t)); err != nil {
		t.Fatal(err)
	}
}
