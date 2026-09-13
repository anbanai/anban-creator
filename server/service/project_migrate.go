package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// MigrateChannelsToProjects is a one-time, idempotent schema rename that moves
// the legacy `channels` concept to `projects` while preserving all data.
//
// Why this exists: the `channel` (WeChat account) concept was renamed to
// `project`. GORM AutoMigrate cannot rename tables or columns — it only adds
// new ones — so on an existing database AutoMigrate creates a fresh empty
// `projects` table and empty `project_id` columns alongside the legacy
// `channels` table / `channel_id` columns. This migration detects that state
// and reconciles it via GORM's Migrator API (dialect-agnostic, so it runs
// unchanged on MySQL in production and SQLite in tests).
//
// Safe to run on every startup:
//   - Fresh install: `channels` never exists → no-op.
//   - Already migrated: `channels` and `channel_id` are gone → no-op.
//   - Each table/column step is independently guarded, so a mid-migration crash
//     converges on the next startup.
//
// Failures are logged but do not block startup (mirrors MigrateArticleStyleOverload).
func MigrateChannelsToProjects(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil {
		return nil
	}
	m := db.Migrator()

	// --- 1. Main table: channels → projects ---
	if m.HasTable("channels") {
		if m.HasTable("projects") {
			// AutoMigrate just created an empty `projects` artifact alongside the
			// data-bearing `channels`. Drop the artifact so we can rename `channels`
			// into place. If it is NOT empty, something unexpected happened — refuse
			// to destroy data and leave it for manual resolution.
			n, err := rowCount(ctx, db, "projects")
			if err != nil {
				log.Error().Err(err).Msg("project migrate: failed to count projects")
				return nil
			}
			switch {
			case n > 0:
				log.Warn().Int64("rows", n).
					Msg("project migrate: both channels and projects exist and projects is non-empty; skipping rename to avoid data loss (resolve manually)")
			case m.DropTable("projects") != nil:
				log.Error().Err(err).Msg("project migrate: failed to drop empty projects artifact")
				return nil
			case m.RenameTable("channels", "projects") != nil:
				log.Error().Err(err).Msg("project migrate: failed to rename channels to projects")
				return nil
			default:
				log.Info().Msg("project migrate: renamed channels → projects (dropped empty AutoMigrate artifact first)")
			}
		} else if err := m.RenameTable("channels", "projects"); err != nil {
			log.Error().Err(err).Msg("project migrate: failed to rename channels to projects")
			return nil
		} else {
			log.Info().Msg("project migrate: renamed channels → projects")
		}
	}

	// --- 2. Foreign-key columns: channel_id → project_id ---
	// notNull mirrors the new model's gorm tag for each column.
	fkTables := []struct {
		name    string
		notNull bool
	}{
		{"tasks", false},
		{"plans", false},
		{"topic_pool", true},
		{"image_generations", true},
		{"seednote_post_trackings", true},
	}
	for _, t := range fkTables {
		if err := migrateProjectFKColumn(ctx, db, log, t.name, t.notNull); err != nil {
			log.Error().Err(err).Str("table", t.name).Msg("project migrate: foreign-key column step failed")
		}
	}

	return nil
}

// migrateProjectFKColumn reconciles a single channel_id → project_id column.
// AutoMigrate has already created an empty project_id column when the table
// pre-existed, so the common path copies channel_id values into it and drops
// channel_id (dropping the column also removes its dedicated index; AutoMigrate
// has already created the new project_id index). If project_id does not yet
// exist we rename the column in place, preserving its definition.
func migrateProjectFKColumn(ctx context.Context, db *gorm.DB, log *zerolog.Logger, table string, notNull bool) error {
	m := db.Migrator()
	if !m.HasTable(table) {
		return nil
	}

	hasChannelID, err := columnExists(m, table, "channel_id")
	if err != nil {
		return fmt.Errorf("probe channel_id on %s: %w", table, err)
	}
	if !hasChannelID {
		return nil // already migrated (or fresh install)
	}

	hasProjectID, err := columnExists(m, table, "project_id")
	if err != nil {
		return fmt.Errorf("probe project_id on %s: %w", table, err)
	}

	if hasProjectID {
		// AutoMigrate created an empty project_id; backfill it from channel_id,
		// then drop the legacy column (its index is dropped with it). DropColumn
		// uses raw SQL because the GORM migrator's DropColumn needs a model
		// schema when given a bare table name (panics on the SQLite driver);
		// `ALTER TABLE ... DROP COLUMN` is valid on both MySQL 8 and SQLite 3.35+.
		if err := db.WithContext(ctx).Exec(
			fmt.Sprintf("UPDATE `%s` SET `project_id` = `channel_id`", table),
		).Error; err != nil {
			return fmt.Errorf("copy channel_id→project_id on %s: %w", table, err)
		}
		if err := db.WithContext(ctx).Exec(
			fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `channel_id`", table),
		).Error; err != nil {
			return fmt.Errorf("drop channel_id on %s: %w", table, err)
		}
		log.Info().Str("table", table).Msg("project migrate: copied channel_id → project_id and dropped channel_id")
		return nil
	}

	if err := m.RenameColumn(table, "channel_id", "project_id"); err != nil {
		return fmt.Errorf("rename channel_id→project_id on %s: %w", table, err)
	}
	log.Info().Str("table", table).Msg("project migrate: renamed channel_id → project_id")
	return nil
}

// columnExists reports whether a column exists on the given table, using the
// Migrator's ColumnTypes so it works across MySQL and SQLite.
func columnExists(m gorm.Migrator, table, column string) (bool, error) {
	cols, err := m.ColumnTypes(table)
	if err != nil {
		return false, err
	}
	for _, c := range cols {
		if c.Name() == column {
			return true, nil
		}
	}
	return false, nil
}

func rowCount(ctx context.Context, db *gorm.DB, table string) (int64, error) {
	var n int64
	err := db.WithContext(ctx).Table(table).Count(&n).Error
	return n, err
}
