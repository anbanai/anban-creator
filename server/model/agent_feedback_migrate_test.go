package model

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type legacyAgentFeedback struct {
	ID            string `gorm:"type:char(36);primaryKey"`
	TaskID        string `gorm:"type:varchar(128);index;not null"`
	AgentName     string `gorm:"type:varchar(30);not null"`
	Scores        string `gorm:"type:json"`
	Errors        string `gorm:"type:text"`
	Optimizations string `gorm:"type:text"`
	Summary       string `gorm:"type:varchar(500)"`
	CreatedAt     time.Time
}

func TestMigrateAgentFeedbackIdempotencySchemaReplacesMalformedNamedIndex(t *testing.T) {
	tests := []struct {
		name       string
		createSQL  string
		insertRows []legacyAgentFeedback
		wantRows   int64
	}{
		{
			name:      "non-unique business columns",
			createSQL: "CREATE INDEX idx_agent_feedback_task_agent ON agent_feedbacks(task_id, agent_name)",
			insertRows: []legacyAgentFeedback{
				{ID: uuid.NewString(), TaskID: "task-1", AgentName: "seednote", Summary: "older", CreatedAt: time.Now().Add(-time.Minute)},
				{ID: uuid.NewString(), TaskID: "task-1", AgentName: "seednote", Summary: "latest", CreatedAt: time.Now()},
			},
			wantRows: 1,
		},
		{
			name:      "unique wrong columns",
			createSQL: "CREATE UNIQUE INDEX idx_agent_feedback_task_agent ON agent_feedbacks(task_id)",
			insertRows: []legacyAgentFeedback{
				{ID: uuid.NewString(), TaskID: "task-1", AgentName: "seednote", CreatedAt: time.Now()},
				{ID: uuid.NewString(), TaskID: "task-2", AgentName: "seednote", CreatedAt: time.Now()},
			},
			wantRows: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&legacyAgentFeedback{}); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(tt.createSQL).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&tt.insertRows).Error; err != nil {
				t.Fatal(err)
			}

			if err := MigrateAgentFeedbackIdempotencySchema(db); err != nil {
				t.Fatalf("migrate malformed index: %v", err)
			}
			assertExactAgentFeedbackBusinessKeyIndex(t, db)
			var count int64
			if err := db.Model(&legacyAgentFeedback{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != tt.wantRows {
				t.Fatalf("row count = %d, want %d", count, tt.wantRows)
			}
			duplicate := AgentFeedback{ID: uuid.NewString(), TaskID: "task-1", AgentName: "seednote"}
			if err := db.Create(&duplicate).Error; err == nil {
				t.Fatal("database accepted duplicate business key after migration")
			}
		})
	}
}

func assertExactAgentFeedbackBusinessKeyIndex(t *testing.T, db *gorm.DB) {
	t.Helper()
	indexes, err := db.Migrator().GetIndexes(&AgentFeedback{})
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		if !strings.EqualFold(index.Name(), agentFeedbackBusinessKeyIndex) {
			continue
		}
		unique, ok := index.Unique()
		columns := index.Columns()
		for i := range columns {
			columns[i] = strings.ToLower(strings.TrimSpace(columns[i]))
		}
		if !ok || !unique || len(columns) != 2 || columns[0] != "task_id" || columns[1] != "agent_name" {
			t.Fatalf("index %s unique=(%v,%v) columns=%v, want unique [task_id agent_name]", index.Name(), unique, ok, columns)
		}
		return
	}
	t.Fatalf("missing index %s", agentFeedbackBusinessKeyIndex)
}

func (legacyAgentFeedback) TableName() string { return "agent_feedbacks" }

func TestMigrateAgentFeedbackIdempotencySchemaDeduplicatesLegacyRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyAgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	rows := []legacyAgentFeedback{
		{ID: uuid.NewString(), TaskID: "task-1", AgentName: "seednote", Scores: `{"quality":4}`, Summary: "older", CreatedAt: base},
		{ID: uuid.NewString(), TaskID: "task-1", AgentName: "seednote", Scores: `{"quality":9}`, Summary: "latest", CreatedAt: base.Add(time.Minute)},
		{ID: uuid.NewString(), TaskID: "task-1", AgentName: "designer", Scores: `{"quality":7}`, Summary: "other agent", CreatedAt: base},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateAgentFeedbackIdempotencySchema(db); err != nil {
		t.Fatalf("deduplicate legacy rows: %v", err)
	}

	var migrated []AgentFeedback
	if err := db.Order("agent_name").Find(&migrated).Error; err != nil {
		t.Fatal(err)
	}
	if len(migrated) != 2 {
		t.Fatalf("row count = %d, want 2", len(migrated))
	}
	var seednote AgentFeedback
	if err := db.Where("task_id = ? AND agent_name = ?", "task-1", "seednote").First(&seednote).Error; err != nil {
		t.Fatal(err)
	}
	if seednote.Summary != "latest" || seednote.Scores != `{"quality":9}` {
		t.Fatalf("retained row = %#v, want latest complete legacy row", seednote)
	}
	assertExactAgentFeedbackBusinessKeyIndex(t, db)
	duplicate := AgentFeedback{ID: uuid.NewString(), TaskID: "task-1", AgentName: "seednote"}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("database accepted duplicate business key after migration")
	}
}
