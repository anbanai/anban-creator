package model

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProjectConfigModelsAllowMultipleScopesAndEnforceUniqueScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:project-config-models?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectAgentConfig{}, &ProjectChannelConfig{}); err != nil {
		t.Fatal(err)
	}
	projectID := uuid.NewString()
	if err := db.Create(&ProjectAgentConfig{ID: uuid.NewString(), ProjectID: projectID, AgentID: AgentIDArticle}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProjectAgentConfig{ID: uuid.NewString(), ProjectID: projectID, AgentID: AgentIDSeednote}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProjectChannelConfig{ID: uuid.NewString(), ProjectID: projectID, Channel: ChannelArticle}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProjectChannelConfig{ID: uuid.NewString(), ProjectID: projectID, Channel: ChannelSeednote}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProjectAgentConfig{ID: uuid.NewString(), ProjectID: projectID, AgentID: AgentIDArticle}).Error; err == nil {
		t.Fatal("duplicate project+agent should be rejected")
	}
	if err := db.Create(&ProjectChannelConfig{ID: uuid.NewString(), ProjectID: projectID, Channel: ChannelArticle}).Error; err == nil {
		t.Fatal("duplicate project+channel should be rejected")
	}
}
