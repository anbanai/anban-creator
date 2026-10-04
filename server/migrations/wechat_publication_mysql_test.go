package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Opt-in: a dedicated empty database named anban_test_* is mandatory. Never
// accepts the application DSN or creates/drops a database.
func TestWechatPublicationStatusMySQL8(t *testing.T) {
	dsn := os.Getenv("ANBAN_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("ANBAN_MYSQL_TEST_DSN unset; requires dedicated empty MySQL 8 database anban_test_*")
	}
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	if !strings.HasPrefix(cfg.DBName, "anban_test_") {
		t.Fatal("refusing non-test database; name must start anban_test_")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(1)
	var version string
	if err := db.Raw("SELECT VERSION()").Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, "8.") {
		t.Fatalf("requires MySQL 8, got %s", version)
	}
	var tables int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE()").Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("refusing nonempty test database")
	}
	if err := db.Exec("CREATE TABLE wechat_publications (id varchar(36) PRIMARY KEY, status varchar(32) NOT NULL, draft_add_attempts int DEFAULT 0, CONSTRAINT chk_wechat_publication_status CHECK (status IN ('drafting','drafted','publish_submitting','publishing','published','needs_selection','publish_failed','unsupported')))").Error; err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE wechat_publications")
	ctx := context.Background()
	if err := RequireWechatPublicationStatusConstraint(ctx, db); err == nil {
		t.Fatal("stale guard was accepted")
	}
	if err := db.Exec("INSERT INTO wechat_publications(id,status) VALUES ('manual','awaiting_manual_publish')").Error; err == nil {
		t.Fatal("stale check unexpectedly accepted manual status")
	}
	raw, err := os.ReadFile(WechatPublicationStatusMigration)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	migrate := func() {
		t.Helper()
		for _, stmt := range strings.Split(strings.Join(lines, "\n"), ";") {
			if strings.TrimSpace(stmt) != "" {
				if err := db.Exec(stmt).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for i := 0; i < 3; i++ {
		migrate()
		if err := RequireWechatPublicationStatusConstraint(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{"drafting", "drafted", "awaiting_manual_publish", "ambiguous", "publishing", "published", "needs_selection", "publish_failed", "unsupported"} {
		if err := db.Exec("INSERT INTO wechat_publications(id,status) VALUES (?,?)", status, status).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{"publish_submitting", "bogus"} {
		if err := db.Exec("INSERT INTO wechat_publications(id,status) VALUES (?,?)", status, status).Error; err == nil {
			t.Fatalf("invalid status accepted: %s", status)
		}
	}
	// MySQL must reject the migration atomically when old rows require reviewed
	// lifecycle evidence. The migration must not translate them to ambiguous.
	db.Exec("DELETE FROM wechat_publications")
	if err := db.Exec("ALTER TABLE wechat_publications DROP CHECK chk_wechat_publication_status, ADD CONSTRAINT chk_wechat_publication_status CHECK (status IN ('drafted','publish_submitting'))").Error; err != nil {
		t.Fatal(err)
	}
	db.Exec("INSERT INTO wechat_publications(id,status) VALUES ('legacy','publish_submitting')")
	for _, stmt := range strings.Split(strings.Join(lines, "\n"), ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		err = db.Exec(stmt).Error
		if strings.HasPrefix(strings.TrimSpace(stmt), "EXECUTE ") {
			if err == nil {
				t.Fatal("invalid existing lifecycle row silently accepted")
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	db.Exec("DEALLOCATE PREPARE wechat_status_check_stmt")
	var status string
	db.Raw("SELECT status FROM wechat_publications WHERE id='legacy'").Scan(&status)
	if status != "publish_submitting" {
		t.Fatalf("migration rewrote evidence: %s", status)
	}
	// The unsuccessful atomic ALTER must leave the previous enforced guard in place.
	if err := db.Exec("INSERT INTO wechat_publications(id,status) VALUES ('must-reject','awaiting_manual_publish')").Error; err == nil {
		t.Fatal("failed migration removed the original constraint")
	}
	if err := db.Exec("INSERT INTO wechat_publications(id,status) VALUES ('still-valid','drafted')").Error; err != nil {
		t.Fatalf("original constraint no longer accepts valid state: %v", err)
	}
}
