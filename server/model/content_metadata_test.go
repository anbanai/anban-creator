package model

import (
	"testing"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestContentMetadataModelsMigrateAndPersist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:content-metadata-model?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&ContentMetadataReport{}, &ContentTagAssignment{}, &ContentTagVocabulary{}, &AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	report := &ContentMetadataReport{ID: "report-1", TaskID: "task-1", ExecutionID: "exec-1", Status: ContentMetadataPending, TaxonomyVersion: ContentTaxonomyVersion, RawMetadata: datatypes.JSON([]byte(`{"tags":[]}`))}
	if err := db.Create(report).Error; err != nil {
		t.Fatal(err)
	}
	tag := &ContentTagAssignment{ID: "tag-1", ReportID: report.ID, Dimension: "industry", Value: "茶叶", CanonicalValue: "tea", LabelStatus: ContentTagCanonical, Confidence: 0.91, Evidence: datatypes.JSON([]byte(`{"file":"output/content.md"}`))}
	if err := db.Create(tag).Error; err != nil {
		t.Fatal(err)
	}
	var got ContentTagAssignment
	if err := db.First(&got, "id = ?", tag.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.CanonicalValue != "tea" || got.LabelStatus != ContentTagCanonical {
		t.Fatalf("got %#v", got)
	}
}
