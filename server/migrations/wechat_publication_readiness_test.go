package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestPublicationStatusReadinessChecksExactEnforcedConstraint(t *testing.T) {
	current := "(`status` in (_utf8mb4'drafting',_utf8mb4'drafted',_utf8mb4'awaiting_manual_publish',_utf8mb4'ambiguous',_utf8mb4'publishing',_utf8mb4'published',_utf8mb4'needs_selection',_utf8mb4'publish_failed',_utf8mb4'unsupported'))"
	for _, tc := range []struct {
		name, clause, enforced string
		valid                  bool
	}{
		{"current", current, "YES", true},
		{"escaped", strings.ReplaceAll(current, "'", `\'`), "YES", true},
		{"old", strings.Replace(current, "awaiting_manual_publish", "publish_submitting", 1), "YES", false},
		{"extra", strings.Replace(current, "'unsupported'", "'unsupported','bogus'", 1), "YES", false},
		{"not enforced", current, "NO", false},
		{"tautology", current + " OR 1=1", "YES", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()
			db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			mock.ExpectQuery("SELECT cc.CHECK_CLAUSE, tc.ENFORCED").WithArgs("wechat_publications", "chk_wechat_publication_status").WillReturnRows(sqlmock.NewRows([]string{"CHECK_CLAUSE", "ENFORCED"}).AddRow(tc.clause, tc.enforced))
			err = RequireWechatPublicationStatusConstraint(context.Background(), db)
			if (err == nil) != tc.valid {
				t.Fatalf("readiness=%v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublicationStatusForwardMigrationOnlyChangesConstraint(t *testing.T) {
	raw, err := os.ReadFile(WechatPublicationStatusMigration)
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, wanted := range []string{"information_schema.TABLE_CONSTRAINTS", "DROP CHECK `chk_wechat_publication_status`", "ADD CONSTRAINT `chk_wechat_publication_status`", "awaiting_manual_publish", "ambiguous", "PREPARE"} {
		if !strings.Contains(sql, wanted) {
			t.Errorf("missing %s", wanted)
		}
	}
	for _, forbidden := range []string{"UPDATE ", "ADD COLUMN", "DROP TABLE", "publish_submitting"} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("unsafe migration contains %s", forbidden)
		}
	}
}
