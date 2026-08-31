package migrations

import (
	"bufio"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type migrationColumn struct {
	declaration  string
	notNull      bool
	defaultValid bool
	defaultValue string
	indexes      []string
}

type modelColumn struct {
	notNull      bool
	defaultValid bool
	defaultValue string
}

func TestWechatPublicationContractMigrationIsACleanCutover(t *testing.T) {
	raw := readPublicationMigration(t)
	if got, want := parseTaskOperations(t, raw), []string{
		"DROP COLUMN `published`",
		"DROP COLUMN `published_at`",
		"DROP COLUMN `publish_approval_state`",
		"DROP COLUMN `pending_draft_articles`",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("task migration operations = %q, want %q", got, want)
	}
	operations := parseTaskExecutionOperations(t, raw)
	wantOperations := []string{
		"DROP INDEX `idx_task_executions_publishing_status`",
		"CHANGE COLUMN `publishing_status` `draft_delivery_status` varchar(20) NOT NULL DEFAULT ''",
		"CHANGE COLUMN `publishing_result` `draft_delivery_result` json NULL",
		"ADD INDEX `idx_task_executions_draft_delivery_status` (`draft_delivery_status`)",
	}
	if strings.Join(operations, "\n") != strings.Join(wantOperations, "\n") {
		t.Fatalf("task execution migration operations = %q, want %q", operations, wantOperations)
	}

	contract := parsePublicationTableContract(t, raw)
	if contract.name != "wechat_publications" {
		t.Fatalf("created table = %q, want wechat_publications", contract.name)
	}
	if contract.primaryKey != "id" {
		t.Fatalf("primary key = %q, want id", contract.primaryKey)
	}
	if got := strings.Join(contract.checks, "\n"); got != strings.Join([]string{
		"CONSTRAINT `chk_wechat_publication_source` CHECK (`source` IN ('anban_api','wechat_console'))",
		"CONSTRAINT `chk_wechat_publication_status` CHECK (`status` IN ('drafting','drafted','publish_submitting','publishing','published','needs_selection','publish_failed','unsupported'))",
	}, "\n") {
		t.Fatalf("checks = %q", got)
	}

	for _, forbidden := range []string{"UPDATE `tasks`", "INSERT INTO `wechat_publications`", "enable_publishing", "require_publish_approval"} {
		if strings.Contains(strings.ToLower(raw), strings.ToLower(forbidden)) {
			t.Errorf("migration contains obsolete compatibility path %q", forbidden)
		}
	}
}

func TestWechatPublicationMigrationCreatesProjectLeaseAndBindingGuards(t *testing.T) {
	raw := readPublicationMigration(t)
	for _, fragment := range []string{
		"CREATE TABLE `wechat_project_reconcile_leases`",
		"PRIMARY KEY (`project_id`)",
		"CREATE TABLE `wechat_publication_bindings`",
		"PRIMARY KEY (`project_id`, `article_id`)",
		"UNIQUE KEY `idx_wechat_publication_bindings_publication_id` (`publication_id`)",
	} {
		if !strings.Contains(raw, fragment) {
			t.Errorf("migration missing %q", fragment)
		}
	}
}

func TestWechatPublicationMigrationMatchesCanonicalColumnAndIndexContract(t *testing.T) {
	contract := parsePublicationTableContract(t, readPublicationMigration(t))
	want := publicationColumnContract()
	if len(contract.columns) != len(want) {
		t.Fatalf("migration has %d columns, want %d", len(contract.columns), len(want))
	}
	for name, expected := range want {
		got, ok := contract.columns[name]
		if !ok || !migrationColumnMatches(got, expected) {
			t.Errorf("migration column %s = %#v, want %#v", name, got, expected)
		}
	}
	if got, wantIndexes := contract.indexes, publicationIndexes(); strings.Join(got, "\n") != strings.Join(wantIndexes, "\n") {
		t.Fatalf("migration indexes = %q, want %q", got, wantIndexes)
	}
}

func TestWechatPublicationModelMatchesCanonicalNullabilityAndDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.WechatPublication{}); err != nil {
		t.Fatal(err)
	}
	columns := sqlitePublicationColumns(t, db)
	for name, expected := range publicationColumnContract() {
		got, ok := columns[name]
		if !ok || !modelColumnMatches(got, expected) {
			t.Errorf("model column %s = %#v, want notNull=%v default valid=%v value=%q", name, got, expected.notNull, expected.defaultValid, expected.defaultValue)
		}
	}
}

func TestPublicationColumnContractDistinguishesAbsentAndEmptyDefault(t *testing.T) {
	if modelColumnMatches(modelColumn{notNull: true}, migrationColumn{notNull: true, defaultValid: true, defaultValue: ""}) {
		t.Fatal("an absent default must not satisfy DEFAULT ''")
	}
}

func readPublicationMigration(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("20260831_wechat_publication_lifecycle_contract.sql")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func parseTaskExecutionOperations(t *testing.T, raw string) []string {
	t.Helper()
	section := sqlSection(t, raw, "ALTER TABLE `task_executions`", "CREATE TABLE `wechat_publications`")
	return parseAlterOperations(t, section, "DROP INDEX ", "CHANGE COLUMN ", "ADD INDEX ")
}

func parseTaskOperations(t *testing.T, raw string) []string {
	t.Helper()
	section := sqlSection(t, raw, "ALTER TABLE `tasks`", "ALTER TABLE `task_executions`")
	return parseAlterOperations(t, section, "DROP COLUMN ")
}

func parseAlterOperations(t *testing.T, section string, prefixes ...string) []string {
	t.Helper()
	var operations []string
	scanner := bufio.NewScanner(strings.NewReader(section))
	for scanner.Scan() {
		line := strings.TrimRight(strings.TrimSpace(scanner.Text()), ",;")
		for _, prefix := range prefixes {
			if strings.HasPrefix(line, prefix) {
				operations = append(operations, line)
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return operations
}

type publicationTableContract struct {
	name       string
	columns    map[string]migrationColumn
	primaryKey string
	indexes    []string
	checks     []string
}

func parsePublicationTableContract(t *testing.T, raw string) publicationTableContract {
	t.Helper()
	section := sqlSection(t, raw, "CREATE TABLE `wechat_publications`", ") ENGINE=InnoDB")
	contract := publicationTableContract{columns: map[string]migrationColumn{}}
	scanner := bufio.NewScanner(strings.NewReader(section))
	for scanner.Scan() {
		line := strings.TrimSuffix(strings.TrimSpace(scanner.Text()), ",")
		switch {
		case strings.HasPrefix(line, "CREATE TABLE `"):
			contract.name = betweenBackticks(t, line)
		case strings.HasPrefix(line, "`"):
			name := betweenBackticks(t, line)
			declaration := strings.TrimSpace(strings.TrimPrefix(line, "`"+name+"`"))
			contract.columns[name] = parseMigrationColumn(declaration)
		case strings.HasPrefix(line, "PRIMARY KEY "):
			contract.primaryKey = betweenBackticks(t, line)
			column := contract.columns[contract.primaryKey]
			column.indexes = append(column.indexes, "PRIMARY")
			contract.columns[contract.primaryKey] = column
		case strings.HasPrefix(line, "UNIQUE KEY ") || strings.HasPrefix(line, "KEY "):
			contract.indexes = append(contract.indexes, line)
			identifiers := backtickIdentifiers(t, line)
			if len(identifiers) != 2 {
				t.Fatalf("index declaration = %q, want index and column identifiers", line)
			}
			column := contract.columns[identifiers[1]]
			column.indexes = append(column.indexes, identifiers[0])
			contract.columns[identifiers[1]] = column
		case strings.HasPrefix(line, "CONSTRAINT "):
			contract.checks = append(contract.checks, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return contract
}

func parseMigrationColumn(declaration string) migrationColumn {
	column := migrationColumn{declaration: declaration, notNull: strings.Contains(declaration, " NOT NULL")}
	const defaultMarker = " DEFAULT "
	if offset := strings.Index(declaration, defaultMarker); offset >= 0 {
		column.defaultValid = true
		column.defaultValue = strings.TrimSpace(declaration[offset+len(defaultMarker):])
	}
	return column
}

func sqlSection(t *testing.T, raw, start, end string) string {
	t.Helper()
	startOffset := strings.Index(raw, start)
	if startOffset < 0 {
		t.Fatalf("SQL section start %q not found", start)
	}
	endOffset := strings.Index(raw[startOffset:], end)
	if endOffset < 0 {
		t.Fatalf("SQL section end %q not found", end)
	}
	return raw[startOffset : startOffset+endOffset+len(end)]
}

func betweenBackticks(t *testing.T, value string) string {
	t.Helper()
	identifiers := backtickIdentifiers(t, value)
	if len(identifiers) == 0 {
		t.Fatalf("missing backtick-delimited identifier in %q", value)
	}
	return identifiers[0]
}

func backtickIdentifiers(t *testing.T, value string) []string {
	t.Helper()
	var identifiers []string
	for remaining := value; ; {
		start := strings.Index(remaining, "`")
		if start < 0 {
			return identifiers
		}
		end := strings.Index(remaining[start+1:], "`")
		if end < 0 {
			t.Fatalf("unclosed backtick-delimited identifier in %q", value)
		}
		identifiers = append(identifiers, remaining[start+1:start+1+end])
		remaining = remaining[start+2+end:]
	}
}

func migrationColumnMatches(got, want migrationColumn) bool {
	return got.declaration == want.declaration && got.notNull == want.notNull && got.defaultValid == want.defaultValid && got.defaultValue == want.defaultValue && strings.Join(got.indexes, "\n") == strings.Join(want.indexes, "\n")
}

func modelColumnMatches(got modelColumn, want migrationColumn) bool {
	return got.notNull == want.notNull && got.defaultValid == want.defaultValid && normalizeDefault(got.defaultValue) == normalizeDefault(want.defaultValue)
}

func sqlitePublicationColumns(t *testing.T, db *gorm.DB) map[string]modelColumn {
	t.Helper()
	rows, err := db.Raw("PRAGMA table_info(wechat_publications)").Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]modelColumn{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = modelColumn{notNull: notNull == 1, defaultValid: defaultValue.Valid, defaultValue: defaultValue.String}
	}
	return columns
}

func normalizeDefault(value string) string {
	return strings.Trim(value, "'\"")
}

func publicationColumnContract() map[string]migrationColumn {
	columns := []struct {
		name, declaration string
	}{
		{"id", "char(36) NOT NULL"},
		{"task_id", "char(36) NOT NULL"},
		{"user_id", "char(36) NOT NULL"},
		{"project_id", "char(36) NOT NULL"},
		{"draft_media_id", "varchar(191) NOT NULL DEFAULT ''"},
		{"draft_title", "varchar(500) NOT NULL DEFAULT ''"},
		{"draft_author", "varchar(500) NOT NULL DEFAULT ''"},
		{"draft_digest", "text NOT NULL"},
		{"draft_thumb_media_id", "varchar(191) NOT NULL DEFAULT ''"},
		{"draft_content_fingerprint", "char(64) NOT NULL DEFAULT ''"},
		{"source", "varchar(32) NOT NULL"},
		{"status", "varchar(32) NOT NULL"},
		{"publish_id", "varchar(191) NOT NULL DEFAULT ''"},
		{"msg_data_id", "varchar(191) NOT NULL DEFAULT ''"},
		{"msg_id", "varchar(191) NOT NULL DEFAULT ''"},
		{"article_id", "varchar(191) NOT NULL DEFAULT ''"},
		{"article_url", "varchar(1000) NOT NULL DEFAULT ''"},
		{"article_index", "int NOT NULL DEFAULT 1"},
		{"wechat_status_code", "int NOT NULL DEFAULT 0"},
		{"draft_created_at", "datetime(3) NULL"},
		{"published_at", "datetime(3) NULL"},
		{"next_check_at", "datetime(3) NULL"},
		{"last_checked_at", "datetime(3) NULL"},
		{"check_attempts", "int NOT NULL DEFAULT 0"},
		{"last_error", "text NOT NULL"},
		{"candidates", "json NULL"},
		{"claim_token", "char(36) NOT NULL DEFAULT ''"},
		{"claimed_at", "datetime(3) NULL"},
		{"submit_attempted_at", "datetime(3) NULL"},
		{"created_at", "datetime(3) NOT NULL"},
		{"updated_at", "datetime(3) NOT NULL"},
	}
	contract := make(map[string]migrationColumn, len(columns))
	for _, column := range columns {
		contract[column.name] = parseMigrationColumn(column.declaration)
	}
	for _, index := range []struct{ name, column string }{
		{"PRIMARY", "id"},
		{"idx_wechat_publications_task_id", "task_id"},
		{"idx_wechat_publications_user_id", "user_id"},
		{"idx_wechat_publications_project_id", "project_id"},
		{"idx_wechat_publications_draft_media_id", "draft_media_id"},
		{"idx_wechat_publications_draft_content_fingerprint", "draft_content_fingerprint"},
		{"idx_wechat_publications_source", "source"},
		{"idx_wechat_publications_status", "status"},
		{"idx_wechat_publications_publish_id", "publish_id"},
		{"idx_wechat_publications_msg_data_id", "msg_data_id"},
		{"idx_wechat_publications_msg_id", "msg_id"},
		{"idx_wechat_publications_article_id", "article_id"},
		{"idx_wechat_publications_draft_created_at", "draft_created_at"},
		{"idx_wechat_publications_published_at", "published_at"},
		{"idx_wechat_publications_next_check_at", "next_check_at"},
		{"idx_wechat_publications_last_checked_at", "last_checked_at"},
		{"idx_wechat_publications_claim_token", "claim_token"},
		{"idx_wechat_publications_claimed_at", "claimed_at"},
	} {
		column := contract[index.column]
		column.indexes = append(column.indexes, index.name)
		contract[index.column] = column
	}
	return contract
}

func publicationIndexes() []string {
	return []string{
		"UNIQUE KEY `idx_wechat_publications_task_id` (`task_id`)",
		"KEY `idx_wechat_publications_user_id` (`user_id`)",
		"KEY `idx_wechat_publications_project_id` (`project_id`)",
		"KEY `idx_wechat_publications_draft_media_id` (`draft_media_id`)",
		"KEY `idx_wechat_publications_draft_content_fingerprint` (`draft_content_fingerprint`)",
		"KEY `idx_wechat_publications_source` (`source`)",
		"KEY `idx_wechat_publications_status` (`status`)",
		"KEY `idx_wechat_publications_publish_id` (`publish_id`)",
		"KEY `idx_wechat_publications_msg_data_id` (`msg_data_id`)",
		"KEY `idx_wechat_publications_msg_id` (`msg_id`)",
		"KEY `idx_wechat_publications_article_id` (`article_id`)",
		"KEY `idx_wechat_publications_draft_created_at` (`draft_created_at`)",
		"KEY `idx_wechat_publications_published_at` (`published_at`)",
		"KEY `idx_wechat_publications_next_check_at` (`next_check_at`)",
		"KEY `idx_wechat_publications_last_checked_at` (`last_checked_at`)",
		"KEY `idx_wechat_publications_claim_token` (`claim_token`)",
		"KEY `idx_wechat_publications_claimed_at` (`claimed_at`)",
	}
}
