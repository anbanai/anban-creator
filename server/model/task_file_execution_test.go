package model

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type legacyTaskFileCollectionState struct {
	ID          string `gorm:"primaryKey"`
	TaskID      string
	ExecutionID string
	State       string `gorm:"check:chk_task_file_state,state IN ('pending','published','superseded')"`
	Role        string
	FilePath    string
	FileName    string
}

func (legacyTaskFileCollectionState) TableName() string { return "task_files" }

type legacyTaskExecutionCollectionState struct {
	ID             string `gorm:"primaryKey"`
	TaskID         string
	Attempt        int
	Target         string
	Status         string
	ManifestStatus string `gorm:"check:chk_task_execution_manifest_status,manifest_status IN ('','pending','published','discarded','rejected')"`
}

func (legacyTaskExecutionCollectionState) TableName() string { return "task_executions" }

func TestTaskFileExecutionScopedUniqueIndexMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	type legacyTaskFile struct {
		ID       string `gorm:"primaryKey"`
		TaskID   string `gorm:"uniqueIndex:idx_task_file_task_path"`
		FilePath string `gorm:"uniqueIndex:idx_task_file_task_path"`
		Role     string
		FileName string
	}
	if err := db.Table("task_files").AutoMigrate(&legacyTaskFile{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("task_files").Create(&legacyTaskFile{ID: "legacy", TaskID: "legacy-task", FilePath: "output/legacy.md", Role: FileRoleOther, FileName: "legacy.md"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("second AutoMigrate: %v", err)
	}
	var legacy TaskFile
	if err := db.First(&legacy, "id = ?", "legacy").Error; err != nil {
		t.Fatal(err)
	}
	if legacy.ExecutionID != "" || legacy.State != TaskFileStatePublished {
		t.Fatalf("migrated legacy row = %#v", legacy)
	}
	if db.Migrator().HasIndex(&TaskFile{}, "idx_task_file_task_path") {
		t.Fatal("legacy unique index still exists")
	}
	if !db.Migrator().HasIndex(&TaskFile{}, "idx_task_file_execution_path") {
		t.Fatal("execution unique index missing")
	}

	rows := []*TaskFile{
		{ID: "f1", TaskID: "t1", ExecutionID: "e1", State: TaskFileStatePending, Role: FileRoleOther, FilePath: "output/a.md", FileName: "a.md"},
		{ID: "f2", TaskID: "t1", ExecutionID: "e2", State: TaskFileStatePending, Role: FileRoleOther, FilePath: "output/a.md", FileName: "a.md"},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create %s: %v", row.ID, err)
		}
	}
	duplicate := *rows[1]
	duplicate.ID = "f3"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate task/execution/path unexpectedly succeeded")
	}
}

func TestTaskFileMySQLMigrationSQLContract(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DryRun: true,
		Logger: logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropIndex(&TaskFile{}, "idx_task_file_task_path"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateIndex(&TaskFile{}, "idx_task_file_execution_path"); err != nil {
		t.Fatal(err)
	}
	sql := logs.String()
	dropAt, createAt := strings.Index(sql, "DROP INDEX `idx_task_file_task_path`"), strings.Index(sql, "CREATE UNIQUE INDEX `idx_task_file_execution_path`")
	if dropAt < 0 || createAt < 0 || dropAt >= createAt {
		t.Fatalf("migration SQL order/shape invalid:\n%s", sql)
	}
	for _, column := range []string{"`task_id`", "`execution_id`", "`file_path`"} {
		if !strings.Contains(sql[createAt:], column) {
			t.Fatalf("new index missing %s:\n%s", column, sql)
		}
	}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&TaskFile{}); err != nil {
		t.Fatal(err)
	}
	state, execution := stmt.Schema.LookUpField("State"), stmt.Schema.LookUpField("ExecutionID")
	if state == nil || !state.HasDefaultValue || state.DefaultValue != TaskFileStatePublished {
		t.Fatalf("state default contract = %#v", state)
	}
	if execution == nil || !execution.HasDefaultValue || execution.DefaultValue != "" {
		t.Fatalf("execution default contract = %#v", execution)
	}
}

func TestTaskFileDefaultsToPublishedForLegacyWriters(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	row := &TaskFile{ID: "f1", TaskID: "t1", Role: FileRoleOther, FilePath: "a.md", FileName: "a.md"}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != TaskFileStatePublished {
		t.Fatalf("state = %q, want published", row.State)
	}
	invalid := &TaskFile{ID: "f2", TaskID: "t1", State: "invalid", Role: FileRoleOther, FilePath: "b.md", FileName: "b.md"}
	if err := db.Create(invalid).Error; err == nil {
		t.Fatal("invalid task file state unexpectedly persisted")
	}
}

func TestTaskArtifactCollectionStateMigrationReplacesLegacyConstraints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	if err := db.AutoMigrate(&legacyTaskFileCollectionState{}, &legacyTaskExecutionCollectionState{}); err != nil {
		t.Fatalf("create legacy artifact schema: %v", err)
	}
	if err := MigrateTaskArtifactCollectionSchema(db); err != nil {
		t.Fatalf("migrate artifact collection states: %v", err)
	}
	if err := db.AutoMigrate(&TaskFile{}, &TaskExecution{}); err != nil {
		t.Fatalf("auto migrate current artifact schema: %v", err)
	}
	if err := db.Create(&TaskExecution{
		ID: "e-collected", TaskID: "t-collected", Attempt: 1, Target: "kubernetes",
		Status: TaskExecutionFailed, ManifestStatus: "collected",
	}).Error; err != nil {
		t.Fatalf("create collected execution: %v", err)
	}
	if err := db.Create(&TaskFile{
		ID: "f-collected", TaskID: "t-collected", ExecutionID: "e-collected",
		State: "collected", Role: FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json",
	}).Error; err != nil {
		t.Fatalf("create collected task file: %v", err)
	}
}
