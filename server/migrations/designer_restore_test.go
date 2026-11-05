package migrations

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm/schema"
)

func TestDesignerRestoreMigrationMatchesModels(t *testing.T) {
	raw, err := os.ReadFile("20261103_restore_designer.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	if strings.Contains(strings.ToUpper(sql), "DROP TABLE") || strings.Contains(strings.ToUpper(sql), "TRUNCATE ") {
		t.Fatal("Designer restore migration must preserve existing data")
	}
	if strings.Contains(strings.ToUpper(sql), "ADD COLUMN IF NOT EXISTS") {
		t.Fatal("MySQL 8 does not support ADD COLUMN IF NOT EXISTS")
	}
	for _, value := range []any{&model.DesignerReference{}, &model.ImageGeneration{}, &model.ImageGenerationResult{}} {
		parsed, err := schema.Parse(value, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		marker := "CREATE TABLE IF NOT EXISTS `" + parsed.Table + "` ("
		start := strings.Index(sql, marker)
		if start < 0 {
			t.Errorf("missing idempotent CREATE TABLE for %s", parsed.Table)
			continue
		}
		body := sql[start+len(marker):]
		end := strings.Index(body, ") ENGINE=")
		if end < 0 {
			t.Errorf("missing ENGINE terminator for %s", parsed.Table)
			continue
		}
		body = body[:end]
		columns := map[string]bool{}
		indexes := map[string]bool{}
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "`") {
				name, _, ok := strings.Cut(strings.TrimPrefix(line, "`"), "`")
				if ok {
					columns[name] = true
				}
			}
			for _, prefix := range []string{"KEY `", "UNIQUE KEY `"} {
				if strings.HasPrefix(line, prefix) {
					name, _, ok := strings.Cut(strings.TrimPrefix(line, prefix), "`")
					if ok {
						indexes[name] = true
					}
				}
			}
		}
		for _, field := range parsed.Fields {
			if field.DBName != "" && !columns[field.DBName] {
				t.Errorf("%s missing model column %s", parsed.Table, field.DBName)
			}
		}
		for _, index := range parsed.ParseIndexes() {
			if !indexes[index.Name] {
				t.Errorf("%s missing model index %s", parsed.Table, index.Name)
			}
		}
	}
}
