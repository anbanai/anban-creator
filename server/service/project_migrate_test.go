package service

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newMigrateTestDB opens an isolated in-memory SQLite database (unique name per
// call so sub-tests do not share state) for exercising the rename migration.
func newMigrateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := "migrate_" + uuid.NewString()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

// createLegacySchema builds the PRE-rename schema directly with raw SQL: a
// `channels` table and the five foreign-key tables each carrying `channel_id`.
// (We cannot AutoMigrate the old `Channel` model — it no longer exists.)
func createLegacySchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE channels (id TEXT PRIMARY KEY, name TEXT, platform TEXT)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, title TEXT, channel_id TEXT)`,
		`CREATE TABLE plans (id TEXT PRIMARY KEY, channel_id TEXT)`,
		`CREATE TABLE topic_pool (id INTEGER PRIMARY KEY AUTOINCREMENT, topic TEXT, channel_id TEXT NOT NULL)`,
		`CREATE TABLE image_generations (id TEXT PRIMARY KEY, channel_id TEXT NOT NULL)`,
		`CREATE TABLE seednote_post_trackings (id TEXT PRIMARY KEY, channel_id TEXT NOT NULL)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}
	// Seed data referencing a single channel so we can verify preservation.
	seed := []string{
		`INSERT INTO channels (id, name, platform) VALUES ('ch-1', 'My Account', 'article')`,
		`INSERT INTO tasks (id, title, channel_id) VALUES ('t-1', 'Hello', 'ch-1')`,
		`INSERT INTO plans (id, channel_id) VALUES ('p-1', 'ch-1')`,
		`INSERT INTO topic_pool (id, topic, channel_id) VALUES (1, 'a topic', 'ch-1')`,
		`INSERT INTO image_generations (id, channel_id) VALUES ('ig-1', 'ch-1')`,
		`INSERT INTO seednote_post_trackings (id, channel_id) VALUES ('spt-1', 'ch-1')`,
	}
	for _, s := range seed {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("seed legacy data: %v", err)
		}
	}
}

func TestMigrateChannelsToProjects_RenamesAndPreservesData(t *testing.T) {
	db := newMigrateTestDB(t)
	createLegacySchema(t, db)
	log := zerolog.Nop()

	if err := MigrateChannelsToProjects(context.Background(), db, &log); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Main table renamed.
	if db.Migrator().HasTable("channels") {
		t.Error("channels table should be gone after migration")
	}
	if !db.Migrator().HasTable("projects") {
		t.Fatal("projects table should exist after migration")
	}
	var projName string
	if err := db.Raw(`SELECT name FROM projects WHERE id = 'ch-1'`).Scan(&projName).Error; err != nil {
		t.Fatalf("read migrated project: %v", err)
	}
	if projName != "My Account" {
		t.Errorf("project name = %q, want %q (data must be preserved)", projName, "My Account")
	}

	// Every FK table: channel_id gone, project_id present and carrying the value.
	fkTables := []string{"tasks", "plans", "topic_pool", "image_generations", "seednote_post_trackings"}
	for _, table := range fkTables {
		hasCh, _ := columnExists(db.Migrator(), table, "channel_id")
		if hasCh {
			t.Errorf("%s: channel_id should be gone", table)
		}
		hasProj, _ := columnExists(db.Migrator(), table, "project_id")
		if !hasProj {
			t.Errorf("%s: project_id should exist", table)
			continue
		}
		var got string
		// Each seeded row carries 'ch-1' under whatever its primary key is.
		if err := db.Raw(`SELECT project_id FROM ` + table + ` LIMIT 1`).Scan(&got).Error; err != nil {
			t.Errorf("%s: read project_id: %v", table, err)
			continue
		}
		if got != "ch-1" {
			t.Errorf("%s: project_id = %q, want %q (FK value must be preserved)", table, got, "ch-1")
		}
	}
}

func TestMigrateChannelsToProjects_IsIdempotent(t *testing.T) {
	db := newMigrateTestDB(t)
	createLegacySchema(t, db)
	log := zerolog.Nop()

	if err := MigrateChannelsToProjects(context.Background(), db, &log); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Snapshot the data we expect to remain stable across a second run.
	var projCount int64
	db.Raw(`SELECT COUNT(*) FROM projects`).Scan(&projCount)

	// Second run must be a clean no-op.
	if err := MigrateChannelsToProjects(context.Background(), db, &log); err != nil {
		t.Fatalf("second run (should be no-op): %v", err)
	}

	var projCount2 int64
	db.Raw(`SELECT COUNT(*) FROM projects`).Scan(&projCount2)
	if projCount2 != projCount {
		t.Errorf("idempotency: project row count changed %d → %d on second run", projCount, projCount2)
	}
	// Legacy names must still be absent (not recreated).
	if db.Migrator().HasTable("channels") {
		t.Error("idempotency: channels table reappeared after second run")
	}
}

// TestMigrateChannelsToProjects_PreExistingProjectID simulates the production
// reality where AutoMigrate has ALREADY created an empty `project_id` column
// alongside `channel_id`. The migration must backfill project_id from
// channel_id and then drop channel_id (the copy + drop branch).
func TestMigrateChannelsToProjects_PreExistingProjectID(t *testing.T) {
	db := newMigrateTestDB(t)
	log := zerolog.Nop()
	stmts := []string{
		`CREATE TABLE channels (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, channel_id TEXT, project_id TEXT)`,
		`INSERT INTO channels (id, name) VALUES ('ch-9', 'Nine')`,
		`INSERT INTO tasks (id, channel_id, project_id) VALUES ('t-9', 'ch-9', '')`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	if err := MigrateChannelsToProjects(context.Background(), db, &log); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if hasCh, _ := columnExists(db.Migrator(), "tasks", "channel_id"); hasCh {
		t.Error("tasks.channel_id should be dropped")
	}
	var got string
	if err := db.Raw(`SELECT project_id FROM tasks WHERE id = 't-9'`).Scan(&got).Error; err != nil {
		t.Fatalf("read task project_id: %v", err)
	}
	if got != "ch-9" {
		t.Errorf("project_id = %q, want %q (must be backfilled from channel_id)", got, "ch-9")
	}
}

// TestMigrateChannelsToProjects_FreshInstallIsNoOp verifies that a database
// with no legacy `channels` table (fresh install or already migrated) is an
// error-free no-op.
func TestMigrateChannelsToProjects_FreshInstallIsNoOp(t *testing.T) {
	db := newMigrateTestDB(t)
	log := zerolog.Nop()
	// Intentionally empty — no channels, no FK tables.
	if err := MigrateChannelsToProjects(context.Background(), db, &log); err != nil {
		t.Fatalf("fresh no-op run: %v", err)
	}
	if db.Migrator().HasTable("channels") || db.Migrator().HasTable("projects") {
		t.Error("fresh install should not create projects or leave channels")
	}
}

func TestMigrateProjectFKColumn_MySQLMissingTableIsNoOp(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open mysql mock: %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT DATABASE()")).
		WillReturnRows(sqlmock.NewRows([]string{"database"}).AddRow("creator"))
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT SCHEMA_NAME from Information_schema.SCHEMATA where SCHEMA_NAME LIKE ? ORDER BY SCHEMA_NAME=? DESC,SCHEMA_NAME limit 1",
	)).WithArgs("creator%", "creator").
		WillReturnRows(sqlmock.NewRows([]string{"SCHEMA_NAME"}).AddRow("creator"))
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ? AND table_type = ?",
	)).WithArgs("creator", "image_generations", "BASE TABLE").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	log := zerolog.Nop()
	if err := migrateProjectFKColumn(context.Background(), db, &log, "image_generations", true); err != nil {
		t.Fatalf("missing legacy table should be ignored: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mysql migration expectations: %v", err)
	}
}
