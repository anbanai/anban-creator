package service

import (
	"context"
	"errors"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestWechatPlatformConfigUnifiesArticleAndPictureChannels(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	ensureTestUser(t, repo, userID)
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	svc := NewProjectService(repo, nil)

	initial, err := svc.GetWechatPlatformConfig(ctx, userID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if initial.AppID != "" || initial.SecretConfigured || initial.CredentialsConflict || initial.AutomaticDeliveryEnabled {
		t.Fatalf("empty config = %#v", initial)
	}

	saved, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, "wx-shared", "secret-shared")
	if err != nil {
		t.Fatal(err)
	}
	if saved.AppID != "wx-shared" || !saved.SecretConfigured || saved.CredentialsConflict {
		t.Fatalf("saved config = %#v", saved)
	}
	if _, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, "wx-replaced", ""); !errors.Is(err, ErrInvalidWechatPlatformConfig) {
		t.Fatalf("AppID change without a replacement secret error = %v", err)
	}
	for _, channel := range []string{model.ChannelArticle, model.ChannelWechatPicture} {
		config, getErr := repo.ProjectChannelConfigs().Get(ctx, projectID, channel)
		if getErr != nil {
			t.Fatalf("read %s config: %v", channel, getErr)
		}
		values := config.Config.Data()
		if values["wechat_app_id"] != "wx-shared" || values["wechat_secret"] != "secret-shared" {
			t.Fatalf("%s config = %#v", channel, values)
		}
	}
	if _, err := svc.UpsertChannelConfig(ctx, userID, projectID, model.ChannelArticle, map[string]any{"wechat_app_id": "wx-legacy", "wechat_secret": "secret-legacy"}); err != nil {
		t.Fatal(err)
	}
	for _, channel := range []string{model.ChannelArticle, model.ChannelWechatPicture} {
		config, getErr := repo.ProjectChannelConfigs().Get(ctx, projectID, channel)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if config.Config.Data()["wechat_app_id"] != "wx-legacy" || config.Config.Data()["wechat_secret"] != "secret-legacy" {
			t.Fatalf("legacy %s write was not synchronized: %#v", channel, config.Config.Data())
		}
	}
	if err := svc.DeleteChannelConfig(ctx, userID, projectID, model.ChannelWechatPicture); err != nil {
		t.Fatal(err)
	}
	for _, channel := range []string{model.ChannelArticle, model.ChannelWechatPicture} {
		if _, err := repo.ProjectChannelConfigs().Get(ctx, projectID, channel); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("delete through %s left a shared credential row: %v", channel, err)
		}
	}
}

func TestWechatPlatformConfigCanDisableAutomaticDelivery(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	ensureTestUser(t, repo, userID)
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	svc := NewProjectService(repo, nil)
	if _, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, "wx-shared", "secret-shared"); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.SetWechatAutomaticDelivery(ctx, userID, projectID, false)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AutomaticDeliveryEnabled {
		t.Fatalf("automatic delivery remained enabled: %#v", updated)
	}
	for _, channel := range []string{model.ChannelArticle, model.ChannelWechatPicture} {
		config, getErr := repo.ProjectChannelConfigs().Get(ctx, projectID, channel)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if config.Config.Data()["automatic_delivery_enabled"] != false {
			t.Fatalf("%s config = %#v", channel, config.Config.Data())
		}
	}
}

func TestWechatPlatformConfigStoresDeliveryModeForBothWechatChannels(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	ensureTestUser(t, repo, userID)
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	svc := NewProjectService(repo, nil)
	updated, err := svc.SetWechatDeliveryMode(ctx, userID, projectID, model.WechatDeliveryModeDirectPublish)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DeliveryMode != model.WechatDeliveryModeDirectPublish {
		t.Fatalf("delivery mode = %q", updated.DeliveryMode)
	}
	for _, channel := range []string{model.ChannelArticle, model.ChannelWechatPicture} {
		config, err := repo.ProjectChannelConfigs().Get(ctx, projectID, channel)
		if err != nil || config.Config.Data()["delivery_mode"] != model.WechatDeliveryModeDirectPublish {
			t.Fatalf("%s mode = %#v, %v", channel, config, err)
		}
	}
	if _, err := svc.SetWechatDeliveryMode(ctx, userID, projectID, "publish_without_confirmation"); !errors.Is(err, ErrInvalidWechatPlatformConfig) {
		t.Fatalf("invalid delivery mode error = %v", err)
	}
}

func TestWechatPlatformConfigRequiresSecretToResolveLegacyConflict(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	ensureTestUser(t, repo, userID)
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	for _, config := range []*model.ProjectChannelConfig{
		{ID: uuid.NewString(), ProjectID: projectID, Channel: model.ChannelArticle, Config: datatypes.NewJSONType(map[string]any{"wechat_app_id": "wx-article", "wechat_secret": "secret-article"})},
		{ID: uuid.NewString(), ProjectID: projectID, Channel: model.ChannelWechatPicture, Config: datatypes.NewJSONType(map[string]any{"wechat_app_id": "wx-picture", "wechat_secret": "secret-picture"})},
	} {
		if err := repo.ProjectChannelConfigs().Upsert(ctx, config); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewProjectService(repo, nil)

	view, err := svc.GetWechatPlatformConfig(ctx, userID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.CredentialsConflict || view.AppID != "" {
		t.Fatalf("conflicting config = %#v", view)
	}
	if _, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, "wx-unified", ""); !errors.Is(err, ErrProjectChannelCredentialsConflict) {
		t.Fatalf("save without secret error = %v", err)
	}
	for _, channel := range []string{model.ChannelArticle, model.ChannelWechatPicture} {
		config, getErr := repo.ProjectChannelConfigs().Get(ctx, projectID, channel)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if config.Config.Data()["wechat_app_id"] == "wx-unified" {
			t.Fatalf("conflict save partially changed %s config", channel)
		}
	}

	resolved, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, "wx-unified", "secret-unified")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.CredentialsConflict || resolved.AppID != "wx-unified" {
		t.Fatalf("resolved config = %#v", resolved)
	}
}

func TestWechatPlatformConfigRejectsSplitLegacyCredentialPair(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	ensureTestUser(t, repo, userID)
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	for _, config := range []*model.ProjectChannelConfig{
		{ID: uuid.NewString(), ProjectID: projectID, Channel: model.ChannelArticle, Config: datatypes.NewJSONType(map[string]any{"wechat_app_id": "wx-legacy"})},
		{ID: uuid.NewString(), ProjectID: projectID, Channel: model.ChannelWechatPicture, Config: datatypes.NewJSONType(map[string]any{"wechat_secret": "secret-legacy"})},
	} {
		if err := repo.ProjectChannelConfigs().Upsert(ctx, config); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewProjectService(repo, nil)

	view, err := svc.GetWechatPlatformConfig(ctx, userID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.CredentialsConflict || view.AppID != "" {
		t.Fatalf("split legacy credentials = %#v, want conflict without a usable AppID", view)
	}
	if _, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, "wx-unified", ""); !errors.Is(err, ErrProjectChannelCredentialsConflict) {
		t.Fatalf("save split credentials without a fresh secret error = %v", err)
	}
	if _, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, "wx-unified", "secret-new"); err != nil {
		t.Fatalf("save split credentials with a fresh secret: %v", err)
	}
}

func TestWechatPlatformConfigRejectsMissingCredentialFields(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	ensureTestUser(t, repo, userID)
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	svc := NewProjectService(repo, nil)
	for _, tc := range []struct {
		name   string
		appID  string
		secret string
	}{
		{name: "missing AppID", secret: "secret"},
		{name: "missing AppSecret", appID: "wx-app"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, tc.appID, tc.secret); !errors.Is(err, ErrInvalidWechatPlatformConfig) {
				t.Fatalf("upsert error = %v, want invalid WeChat platform config", err)
			}
		})
	}
}

func TestWechatPlatformConfigRejectsNonWechatProject(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	ensureTestUser(t, repo, userID)
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	svc := NewProjectService(repo, nil)
	if _, err := svc.GetWechatPlatformConfig(ctx, userID, projectID); !errors.Is(err, ErrInvalidWechatPlatformConfig) {
		t.Fatalf("get WeChat config error = %v", err)
	}
	if _, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, "wx-test", "secret-test"); !errors.Is(err, ErrInvalidWechatPlatformConfig) {
		t.Fatalf("upsert WeChat config error = %v", err)
	}
}

func TestWechatPlatformConfigSupportsLegacyProjectDerivedFromTask(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	ensureTestUser(t, repo, userID)
	projectID := createTestProject(t, repo, userID, "")
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        uuid.NewString(),
		UserID:    userID,
		ProjectID: projectID,
		AgentID:   model.AgentIDArticle,
		Channel:   model.ChannelArticle,
		TaskKind:  model.TaskKindContentGeneration,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewProjectService(repo, nil)
	if _, err := svc.GetWechatPlatformConfig(ctx, userID, projectID); err != nil {
		t.Fatalf("get config for a legacy article project: %v", err)
	}
	config, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, "wx-legacy", "secret-legacy")
	if err != nil {
		t.Fatalf("save config for a legacy article project: %v", err)
	}
	if config.AppID != "wx-legacy" || !config.SecretConfigured {
		t.Fatalf("saved legacy project config = %#v", config)
	}
}

func TestWechatPlatformConfigSupportsLegacyArticlePlatform(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	ensureTestUser(t, repo, userID)
	projectID := createTestProject(t, repo, userID, "article")
	svc := NewProjectService(repo, nil)

	config, err := svc.GetWechatPlatformConfig(ctx, userID, projectID)
	if err != nil {
		t.Fatalf("get config for a legacy article platform: %v", err)
	}
	if config.ProjectID != projectID || config.Platform != model.PlatformWechat {
		t.Fatalf("legacy article platform config = %#v", config)
	}

	if _, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, "wx-legacy", "secret-legacy"); err != nil {
		t.Fatalf("save config for a legacy article platform: %v", err)
	}
}

func TestWechatPlatformConfigSupportsLegacyArticleTaskIdentity(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	ensureTestUser(t, repo, userID)
	projectID := createTestProject(t, repo, userID, "")
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID,
		Type: "article", Status: model.TaskStatusCompleted,
	}); err != nil {
		t.Fatalf("create legacy article task: %v", err)
	}
	svc := NewProjectService(repo, nil)

	if _, err := svc.GetWechatPlatformConfig(ctx, userID, projectID); err != nil {
		t.Fatalf("get config for a legacy article task: %v", err)
	}
}

func TestProjectUpdateAndWechatCredentialsRollBackTogether(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	ensureTestUser(t, repo, userID)
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	svc := NewProjectService(repo, nil)
	if _, err := svc.UpsertWechatPlatformConfig(ctx, userID, projectID, "wx-original", "secret-original"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateWithWechatPlatformConfig(ctx, userID, projectID, &model.Project{Name: "partially changed"}, "wx-replacement", "", "", false); !errors.Is(err, ErrInvalidWechatPlatformConfig) {
		t.Fatalf("update error = %v, want replacement secret validation", err)
	}
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if project.Name == "partially changed" {
		t.Fatal("project fields were changed despite the rejected credential update")
	}
	config, err := svc.GetWechatPlatformConfig(ctx, userID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if config.AppID != "wx-original" {
		t.Fatalf("AppID after rollback = %q", config.AppID)
	}
}
