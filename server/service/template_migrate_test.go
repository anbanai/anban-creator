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

func TestMigrateTemplatePromptBackfillsCanonicalFieldsWithoutContractingSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE templates (id TEXT PRIMARY KEY, style_prompt TEXT, prompt TEXT, visibility TEXT)`,
		`CREATE TABLE projects (id TEXT PRIMARY KEY, template_id TEXT, style TEXT)`,
		`INSERT INTO templates (id, style_prompt, prompt, visibility) VALUES ('legacy', '旧视觉版式', '', 'private')`,
		`INSERT INTO templates (id, style_prompt, prompt, visibility) VALUES ('whitespace-canonical', '空格值应回填', '   ', 'public')`,
		`INSERT INTO templates (id, style_prompt, prompt, visibility) VALUES ('canonical', '不应覆盖', '新提示词', 'public')`,
		`INSERT INTO projects (id, template_id, style) VALUES ('blank-style', 'legacy', '')`,
		`INSERT INTO projects (id, template_id, style) VALUES ('whitespace-style', 'whitespace-canonical', '   ')`,
		`INSERT INTO projects (id, template_id, style) VALUES ('kept-style', 'legacy', '项目已有视觉')`,
		`INSERT INTO projects (id, template_id, style) VALUES ('canonical-source', 'canonical', '')`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}
	log := zerolog.Nop()
	if err := MigrateTemplatePrompt(context.Background(), db, &log); err != nil {
		t.Fatalf("migration pass 1: %v", err)
	}

	templateRows := map[string]struct {
		Prompt     string
		Visibility string
	}{}
	var rawTemplates []struct {
		ID         string
		Prompt     string
		Visibility string
	}
	if err := db.Table("templates").Select("id, prompt, visibility").Scan(&rawTemplates).Error; err != nil {
		t.Fatalf("read templates: %v", err)
	}
	for _, row := range rawTemplates {
		templateRows[row.ID] = struct {
			Prompt     string
			Visibility string
		}{row.Prompt, row.Visibility}
	}
	if got := templateRows["legacy"]; got.Prompt != "旧视觉版式" || got.Visibility != "private" {
		t.Fatalf("legacy template = %+v, want prompt backfill and unchanged private visibility", got)
	}
	if got := templateRows["canonical"]; got.Prompt != "新提示词" || got.Visibility != "public" {
		t.Fatalf("canonical template = %+v, want unchanged", got)
	}
	if got := templateRows["whitespace-canonical"]; got.Prompt != "空格值应回填" || got.Visibility != "public" {
		t.Fatalf("whitespace canonical template = %+v, want legacy prompt backfill", got)
	}

	var projects []struct {
		ID    string
		Style string
	}
	if err := db.Table("projects").Select("id, style").Order("id").Scan(&projects).Error; err != nil {
		t.Fatalf("read projects: %v", err)
	}
	want := map[string]string{
		"blank-style": "旧视觉版式", "whitespace-style": "空格值应回填",
		"kept-style": "项目已有视觉", "canonical-source": "新提示词",
	}
	for _, project := range projects {
		if project.Style != want[project.ID] {
			t.Errorf("project %s style = %q, want %q", project.ID, project.Style, want[project.ID])
		}
	}

	if !db.Migrator().HasColumn("templates", "style_prompt") {
		t.Error("templates.style_prompt was dropped during expand/backfill migration")
	}
	if !db.Migrator().HasColumn("projects", "template_id") {
		t.Error("projects.template_id was dropped during expand/backfill migration")
	}
	if err := MigrateTemplatePrompt(context.Background(), db, &log); err != nil {
		t.Fatalf("migration pass 2: %v", err)
	}
}

func TestMigrateTemplatePromptMySQLBackfillIsIdempotentAndNonDestructive(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open mysql mock: %v", err)
	}

	expectMySQLCurrentSchema := func() {
		mock.ExpectQuery(regexp.QuoteMeta("SELECT DATABASE()")).
			WillReturnRows(sqlmock.NewRows([]string{"database"}).AddRow("testdb"))
		mock.ExpectQuery(regexp.QuoteMeta(
			"SELECT SCHEMA_NAME from Information_schema.SCHEMATA where SCHEMA_NAME LIKE ? ORDER BY SCHEMA_NAME=? DESC,SCHEMA_NAME limit 1",
		)).WithArgs("testdb%", "testdb").
			WillReturnRows(sqlmock.NewRows([]string{"SCHEMA_NAME"}).AddRow("testdb"))
	}
	expectMySQLHasTable := func(table string) {
		expectMySQLCurrentSchema()
		mock.ExpectQuery(regexp.QuoteMeta(
			"SELECT count(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ? AND table_type = ?",
		)).WithArgs("testdb", table, "BASE TABLE").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	}
	expectMySQLHasColumn := func(table, column string, exists bool) {
		expectMySQLCurrentSchema()
		count := 0
		if exists {
			count = 1
		}
		mock.ExpectQuery(regexp.QuoteMeta(
			"SELECT count(*) FROM INFORMATION_SCHEMA.columns WHERE table_schema = ? AND table_name = ? AND column_name = ?",
		)).WithArgs("testdb", table, column).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
	}

	expectMySQLHasTable("templates")
	expectMySQLHasColumn("templates", "prompt", true)
	expectMySQLHasColumn("templates", "style_prompt", true)
	mock.ExpectExec("UPDATE templates[[:space:]]+SET prompt = style_prompt").
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectMySQLHasTable("projects")
	expectMySQLHasColumn("projects", "template_id", true)
	expectMySQLHasColumn("projects", "style", true)
	mock.ExpectExec("UPDATE projects[[:space:]]+SET style =").
		WillReturnResult(sqlmock.NewResult(0, 1))
	log := zerolog.Nop()
	if err := MigrateTemplatePrompt(context.Background(), db, &log); err != nil {
		t.Fatalf("migrate mysql schema: %v", err)
	}

	// A second startup repeats guarded backfills while legacy columns remain.
	expectMySQLHasTable("templates")
	expectMySQLHasColumn("templates", "prompt", true)
	expectMySQLHasColumn("templates", "style_prompt", true)
	mock.ExpectExec("UPDATE templates[[:space:]]+SET prompt = style_prompt").
		WillReturnResult(sqlmock.NewResult(0, 0))
	expectMySQLHasTable("projects")
	expectMySQLHasColumn("projects", "template_id", true)
	expectMySQLHasColumn("projects", "style", true)
	mock.ExpectExec("UPDATE projects[[:space:]]+SET style =").
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := MigrateTemplatePrompt(context.Background(), db, &log); err != nil {
		t.Fatalf("migrate mysql schema pass 2: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mysql migration expectations: %v", err)
	}
}

func TestMigrateTemplatePromptFreshSchemaIsNoOp(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Exec(`CREATE TABLE templates (id TEXT PRIMARY KEY, prompt TEXT, visibility TEXT)`).Error; err != nil {
		t.Fatalf("create templates: %v", err)
	}
	if err := db.Exec(`CREATE TABLE projects (id TEXT PRIMARY KEY, style TEXT)`).Error; err != nil {
		t.Fatalf("create projects: %v", err)
	}
	log := zerolog.Nop()
	if err := MigrateTemplatePrompt(context.Background(), db, &log); err != nil {
		t.Fatalf("fresh migration: %v", err)
	}
}
