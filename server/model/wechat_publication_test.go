package model

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWechatPublishModeContract(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "zero defaults manual", want: WechatPublishModeManual},
		{name: "disabled", mode: WechatPublishModeDisabled, want: WechatPublishModeDisabled},
		{name: "manual", mode: WechatPublishModeManual, want: WechatPublishModeManual},
		{name: "api confirmed", mode: WechatPublishModeAPIConfirmed, want: WechatPublishModeAPIConfirmed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := Project{Config: ProjectConfig{WechatPublishMode: tt.mode}}
			if got := project.GetWechatPublishMode(); got != tt.want {
				t.Fatalf("publish mode = %q, want %q", got, tt.want)
			}
		})
	}

	raw, err := json.Marshal(ProjectConfig{WechatPublishMode: WechatPublishModeAPIConfirmed})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"enable_publishing", "require_publish_approval"} {
		if string(raw) == forbidden || containsJSONKey(raw, forbidden) {
			t.Fatalf("project config still serializes obsolete %q: %s", forbidden, raw)
		}
	}
}

func TestWechatPublicationSchemaIsOnePerTaskAndHasLifecycleContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&WechatPublication{}); err != nil {
		t.Fatalf("migrate publication: %v", err)
	}
	if !db.Migrator().HasTable(&WechatPublication{}) || !db.Migrator().HasIndex(&WechatPublication{}, "idx_wechat_publications_task_id") {
		t.Fatal("wechat publication task uniqueness is missing")
	}
	first := &WechatPublication{ID: uuid.NewString(), TaskID: "task-1", UserID: "user-1", ProjectID: "project-1", Source: WechatPublicationSourceAnbanAPI, Status: WechatPublicationStatusDrafted}
	if err := db.Create(first).Error; err != nil {
		t.Fatalf("create publication: %v", err)
	}
	if first.ArticleIndex != 1 {
		t.Fatalf("article index = %d, want default 1", first.ArticleIndex)
	}
	if err := db.Create(&WechatPublication{ID: uuid.NewString(), TaskID: first.TaskID, UserID: "user-1", ProjectID: "project-1", Source: WechatPublicationSourceAnbanAPI, Status: WechatPublicationStatusDrafted}).Error; err == nil {
		t.Fatal("duplicate task publication succeeded")
	}
	for _, invalid := range []WechatPublication{
		{ID: uuid.NewString(), TaskID: "task-invalid-status", UserID: "user-1", ProjectID: "project-1", Source: WechatPublicationSourceAnbanAPI, Status: "completed"},
		{ID: uuid.NewString(), TaskID: "task-invalid-source", UserID: "user-1", ProjectID: "project-1", Source: "manual", Status: WechatPublicationStatusDrafted},
	} {
		if err := db.Create(&invalid).Error; err == nil {
			t.Fatalf("invalid publication %#v persisted", invalid)
		}
	}

	typeOfPublication := reflect.TypeOf(WechatPublication{})
	for _, field := range []string{"DraftMediaID", "DraftTitle", "DraftAuthor", "DraftDigest", "DraftThumbMediaID", "DraftContentFingerprint", "DraftRequestFingerprint", "Source", "Status", "PublishID", "MsgDataID", "MsgID", "ArticleID", "ArticleURL", "ArticleIndex", "WechatStatusCode", "DraftCreatedAt", "PublishedAt", "NextCheckAt", "LastCheckedAt", "CheckAttempts", "LastError", "Candidates", "ClaimToken", "ClaimedAt", "SubmitAttemptedAt"} {
		if _, ok := typeOfPublication.FieldByName(field); !ok {
			t.Errorf("WechatPublication missing %s", field)
		}
	}
	for _, source := range []string{WechatPublicationSourceAnbanAPI, WechatPublicationSourceWechatConsole} {
		if !IsWechatPublicationSource(source) {
			t.Errorf("source %q not recognized", source)
		}
	}
	for _, status := range []string{
		WechatPublicationStatusDrafting, WechatPublicationStatusDrafted, WechatPublicationStatusPublishSubmitting,
		WechatPublicationStatusPublishing, WechatPublicationStatusPublished, WechatPublicationStatusNeedsSelection,
		WechatPublicationStatusPublishFailed, WechatPublicationStatusUnsupported,
	} {
		if !IsWechatPublicationStatus(status) {
			t.Errorf("status %q not recognized", status)
		}
	}
}

func TestWechatPublicationLeaseAndBindingSchemaEnforceProjectScopedUniqueness(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&WechatProjectReconcileLease{}, &WechatPublicationBinding{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&WechatProjectReconcileLease{ProjectID: "project-1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&WechatProjectReconcileLease{ProjectID: "project-1"}).Error; err == nil {
		t.Fatal("duplicate project reconcile lease succeeded")
	}
	first := &WechatPublicationBinding{ProjectID: "project-1", ArticleID: "article-1", PublicationID: "publication-1"}
	if err := db.Create(first).Error; err != nil {
		t.Fatal(err)
	}
	for _, duplicate := range []WechatPublicationBinding{
		{ProjectID: "project-1", ArticleID: "article-1", PublicationID: "publication-2"},
		{ProjectID: "project-1", ArticleID: "article-2", PublicationID: "publication-1"},
	} {
		if err := db.Create(&duplicate).Error; err == nil {
			t.Fatalf("duplicate binding %#v succeeded", duplicate)
		}
	}
	if err := db.Create(&WechatPublicationBinding{ProjectID: "project-2", ArticleID: "article-1", PublicationID: "publication-2"}).Error; err != nil {
		t.Fatalf("same article ID in another project must be allowed: %v", err)
	}
}

func TestPublicationCutoverRemovesTaskPublicationAndRenamesExecutionDelivery(t *testing.T) {
	taskType := reflect.TypeOf(Task{})
	for _, obsolete := range []string{"Published", "PublishedAt", "PublishApprovalState", "PendingDraftArticles"} {
		if _, exists := taskType.FieldByName(obsolete); exists {
			t.Errorf("Task still has obsolete %s", obsolete)
		}
	}
	executionType := reflect.TypeOf(TaskExecution{})
	for _, obsolete := range []string{"PublishingStatus", "PublishingResult"} {
		if _, exists := executionType.FieldByName(obsolete); exists {
			t.Errorf("TaskExecution still has obsolete %s", obsolete)
		}
	}
	for _, required := range []string{"DraftDeliveryStatus", "DraftDeliveryResult"} {
		if _, exists := executionType.FieldByName(required); !exists {
			t.Errorf("TaskExecution missing %s", required)
		}
	}
}

func containsJSONKey(raw []byte, key string) bool {
	var decoded map[string]json.RawMessage
	return json.Unmarshal(raw, &decoded) == nil && decoded[key] != nil
}
