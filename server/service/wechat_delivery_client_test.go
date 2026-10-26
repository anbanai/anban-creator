package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

func TestWechatAccountClientFactoryUsesAuthorizerTokenAndRejectsRevokedFallback(t *testing.T) {
	f := newPublicationFixture(t)
	ctx := context.Background()
	client := &fakeWechatOpenPlatformClient{cachedToken: "authorizer-access"}
	openPlatform, err := newWechatOpenPlatformService(f.repo, client, []byte("0123456789abcdef0123456789abcdef"), "https://creator.example.com")
	if err != nil {
		t.Fatal(err)
	}
	factory := NewWechatAccountClientFactory(f.repo, NewPublishingService(f.repo, nil), openPlatform, nil)
	project, err := f.repo.Projects().FindByID(ctx, f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	project.RuntimeChannelConfig = map[string]any{"wechat_app_id": "wx-legacy", "wechat_secret": "legacy-secret"}
	authorization := &model.WechatOpenPlatformAuthorization{
		ID: uuid.NewString(), ProjectID: project.ID, UserID: project.UserID,
		AuthorizerAppID: "wx-authorizer", Status: model.WechatAuthorizationStatusConnected,
		GrantedScopes: []int{wechatOpenPlatformDraftScope}, RefreshTokenCiphertext: "encrypted", AuthorizedAt: time.Now(),
	}
	if err := f.repo.WechatOpenPlatformAuthorizations().Upsert(ctx, authorization); err != nil {
		t.Fatal(err)
	}

	account, err := factory.ForProject(project)
	if err != nil {
		t.Fatal(err)
	}
	token, err := account.GetAccessToken()
	if err != nil || token.AccessToken != "authorizer-access" {
		t.Fatalf("resolved access token = %#v, %v", token, err)
	}

	authorization.Status = model.WechatAuthorizationStatusRevoked
	authorization.RefreshTokenCiphertext = ""
	if err := f.repo.WechatOpenPlatformAuthorizations().Upsert(ctx, authorization); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.ForProject(project); !errors.Is(err, ErrWechatOpenPlatformNotConnected) {
		t.Fatalf("revoked authorization error = %v, want no legacy fallback", err)
	}
}

func TestWechatAccountClientFactoryRejectsAuthorizationsWithoutDraftScope(t *testing.T) {
	f := newPublicationFixture(t)
	ctx := context.Background()
	client := &fakeWechatOpenPlatformClient{}
	openPlatform, err := newWechatOpenPlatformService(f.repo, client, []byte("0123456789abcdef0123456789abcdef"), "https://creator.example.com")
	if err != nil {
		t.Fatal(err)
	}
	factory := NewWechatAccountClientFactory(f.repo, NewPublishingService(f.repo, nil), openPlatform, nil)
	project, err := f.repo.Projects().FindByID(ctx, f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	project.RuntimeChannelConfig = map[string]any{"wechat_app_id": "wx-legacy", "wechat_secret": "legacy-secret"}
	if err := f.repo.WechatOpenPlatformAuthorizations().Upsert(ctx, &model.WechatOpenPlatformAuthorization{
		ID: uuid.NewString(), ProjectID: project.ID, UserID: project.UserID,
		AuthorizerAppID: "wx-authorizer", Status: model.WechatAuthorizationStatusConnected,
		GrantedScopes: []int{3}, RefreshTokenCiphertext: "encrypted", AuthorizedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.ForProject(project); !errors.Is(err, ErrWechatOpenPlatformCapabilityMissing) {
		t.Fatalf("missing draft capability error = %v", err)
	}
}

func TestWechatAccountClientFactoryAcceptsLegacyDraftScope(t *testing.T) {
	f := newPublicationFixture(t)
	ctx := context.Background()
	client := &fakeWechatOpenPlatformClient{cachedToken: "authorizer-access"}
	openPlatform, err := newWechatOpenPlatformService(f.repo, client, []byte("0123456789abcdef0123456789abcdef"), "https://creator.example.com")
	if err != nil {
		t.Fatal(err)
	}
	factory := NewWechatAccountClientFactory(f.repo, NewPublishingService(f.repo, nil), openPlatform, nil)
	project, err := f.repo.Projects().FindByID(ctx, f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repo.WechatOpenPlatformAuthorizations().Upsert(ctx, &model.WechatOpenPlatformAuthorization{
		ID: uuid.NewString(), ProjectID: project.ID, UserID: project.UserID,
		AuthorizerAppID: "wx-authorizer", Status: model.WechatAuthorizationStatusConnected,
		GrantedScopes: []int{wechatOpenPlatformDraftScopeLegacy}, RefreshTokenCiphertext: "encrypted", AuthorizedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.ForProject(project); err != nil {
		t.Fatalf("legacy draft scope did not resolve an authorized client: %v", err)
	}
}
