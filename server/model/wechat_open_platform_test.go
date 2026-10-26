package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWechatOpenPlatformAuthorizationPersistsOneAccountPerProject(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:wechat-open-platform-model?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&WechatOpenPlatformAuthorization{}); err != nil {
		t.Fatal(err)
	}

	projectID := uuid.NewString()
	first := &WechatOpenPlatformAuthorization{
		ID: uuid.NewString(), ProjectID: projectID, UserID: uuid.NewString(),
		AuthorizerAppID: "wx-first", AccountName: "First account", Status: WechatAuthorizationStatusConnected,
		RefreshTokenCiphertext: "ciphertext", GrantedScopes: []int{1, 2}, AuthorizedAt: time.Now(),
	}
	if err := db.Create(first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&WechatOpenPlatformAuthorization{
		ID: uuid.NewString(), ProjectID: projectID, UserID: first.UserID,
		AuthorizerAppID: "wx-second", Status: WechatAuthorizationStatusConnected,
	}).Error; err == nil {
		t.Fatal("a project accepted a second connected authorizer")
	}

	var loaded WechatOpenPlatformAuthorization
	if err := db.First(&loaded, "project_id = ?", projectID).Error; err != nil {
		t.Fatal(err)
	}
	if loaded.AuthorizerAppID != first.AuthorizerAppID || loaded.RefreshTokenCiphertext != "ciphertext" || len(loaded.GrantedScopes) != 2 {
		t.Fatalf("loaded authorization = %#v", loaded)
	}
	if db.Migrator().HasColumn(&loaded, "authorizer_access_token") {
		t.Fatal("short-lived access tokens must not be persisted in the authorization record")
	}
}

func TestWechatOpenPlatformAuthorizationStateStoresOnlyHashAndExpiry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:wechat-open-platform-state-model?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&WechatOpenPlatformAuthorizationState{}); err != nil {
		t.Fatal(err)
	}

	state := &WechatOpenPlatformAuthorizationState{
		StateHash: "sha256-hash", ProjectID: uuid.NewString(), UserID: uuid.NewString(), ExpiresAt: time.Now().Add(time.Minute),
	}
	if err := db.Create(state).Error; err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasColumn(state, "state") {
		t.Fatal("the raw OAuth state must not be persisted")
	}
}
