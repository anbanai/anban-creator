package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/silenceper/wechat/v2/util"
	"gorm.io/gorm"
)

type fakeWechatOpenPlatformClient struct {
	redirectURI     string
	grant           WechatOpenPlatformGrant
	identity        WechatAuthorizerIdentity
	cached          bool
	cachedToken     string
	refreshes       int
	mu              sync.Mutex
	ticket          string
	ticketCreatedAt int64
}

func (f *fakeWechatOpenPlatformClient) AuthorizationURL(_ context.Context, redirectURI string) (string, error) {
	f.redirectURI = redirectURI
	return "https://mp.weixin.qq.com/componentloginpage?redirect=" + url.QueryEscape(redirectURI), nil
}

func (f *fakeWechatOpenPlatformClient) QueryAuthorization(_ context.Context, code string) (*WechatOpenPlatformGrant, error) {
	if code != "valid-code" {
		return nil, ErrWechatOpenPlatformAuthorizationFailed
	}
	grant := f.grant
	return &grant, nil
}

func (f *fakeWechatOpenPlatformClient) GetAuthorizerInfo(_ context.Context, appID string) (*WechatAuthorizerIdentity, error) {
	if appID != f.grant.AuthorizerAppID {
		return nil, ErrWechatOpenPlatformAuthorizationFailed
	}
	identity := f.identity
	return &identity, nil
}

func (f *fakeWechatOpenPlatformClient) CacheAuthorizerAccessToken(_ context.Context, appID, token string, _ time.Duration) error {
	f.cached = appID == f.grant.AuthorizerAppID && token == f.grant.AccessToken
	f.cachedToken = token
	return nil
}

func (f *fakeWechatOpenPlatformClient) CachedAuthorizerAccessToken(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cachedToken, nil
}

func (f *fakeWechatOpenPlatformClient) DeleteAuthorizerAccessToken(context.Context, string) error {
	f.cached = false
	f.cachedToken = ""
	return nil
}

func (f *fakeWechatOpenPlatformClient) RefreshAuthorizerToken(_ context.Context, appID, refreshToken string) (*WechatOpenPlatformGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
	if appID != "wx-authorizer" || refreshToken != "old-refresh" {
		return nil, ErrWechatOpenPlatformAuthorizationFailed
	}
	return &WechatOpenPlatformGrant{AuthorizerAppID: appID, AccessToken: "refreshed-access", RefreshToken: "rotated-refresh", ExpiresIn: time.Hour}, nil
}

func (f *fakeWechatOpenPlatformClient) SetComponentVerifyTicket(_ context.Context, ticket string, createdAt int64) error {
	if createdAt > f.ticketCreatedAt {
		f.ticket, f.ticketCreatedAt = ticket, createdAt
	}
	return nil
}

func TestWechatOpenPlatformAuthorizationBindsStateAndEncryptsRefreshToken(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformWechat, Name: "Persona"}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	client := &fakeWechatOpenPlatformClient{
		grant:    WechatOpenPlatformGrant{AuthorizerAppID: "wx-authorizer", AccessToken: "short-lived-access", RefreshToken: "long-lived-refresh", ExpiresIn: time.Hour, GrantedScopes: []int{11, 7}},
		identity: WechatAuthorizerIdentity{AccountName: "公众号名称", AvatarURL: "https://example.com/avatar.png", AccountType: 2},
	}
	svc, err := newWechatOpenPlatformService(repo, client, []byte("0123456789abcdef0123456789abcdef"), "https://creator.example.com")
	if err != nil {
		t.Fatal(err)
	}

	started, err := svc.StartAuthorization(context.Background(), project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := url.Parse(client.redirectURI)
	if err != nil {
		t.Fatal(err)
	}
	state := redirect.Query().Get("state")
	if state == "" || strings.Contains(client.redirectURI, "auth_code=") {
		t.Fatalf("authorization redirect URI did not contain only state: %q", client.redirectURI)
	}
	if !strings.HasPrefix(started.AuthorizationURL, "https://mp.weixin.qq.com/") {
		t.Fatalf("authorization URL = %q", started.AuthorizationURL)
	}

	completedProjectID, err := svc.CompleteAuthorization(context.Background(), state, "valid-code")
	if err != nil {
		t.Fatal(err)
	}
	if completedProjectID != project.ID {
		t.Fatalf("completed project = %q, want %q", completedProjectID, project.ID)
	}
	authorization, err := repo.WechatOpenPlatformAuthorizations().FindByProjectID(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if authorization.UserID != project.UserID || authorization.AuthorizerAppID != "wx-authorizer" || authorization.AccountName != "公众号名称" {
		t.Fatalf("authorization = %#v", authorization)
	}
	if authorization.RefreshTokenCiphertext == "long-lived-refresh" || authorization.RefreshTokenCiphertext == "" {
		t.Fatal("refresh token was not encrypted before persistence")
	}
	refreshToken, err := svc.decryptRefreshToken(authorization.RefreshTokenCiphertext)
	if err != nil || refreshToken != "long-lived-refresh" {
		t.Fatalf("decrypted refresh token = %q, %v", refreshToken, err)
	}
	if !client.cached {
		t.Fatal("the initial access token was not placed in shared cache")
	}
	if _, err := svc.CompleteAuthorization(context.Background(), state, "valid-code"); err == nil {
		t.Fatal("authorization state replay was accepted")
	}
}

func TestWechatOpenPlatformCapabilitiesMatchOfficialPermissionScopes(t *testing.T) {
	for _, tc := range []struct {
		scopes                                             []int
		draft, publish, articleAnalytics, accountAnalytics bool
	}{
		{scopes: []int{11}, draft: true},
		{scopes: []int{100}, draft: true},
		{scopes: []int{7}, articleAnalytics: true},
		{scopes: []int{2}, accountAnalytics: true},
		{scopes: []int{11, 7, 2}, draft: true, publish: true, articleAnalytics: true, accountAnalytics: true},
		{scopes: []int{100, 7}, draft: true, publish: true, articleAnalytics: true},
	} {
		got := wechatCapabilitiesForScopes(tc.scopes)
		if got.Draft != tc.draft || got.FormalPublish != tc.publish || got.ArticleAnalytics != tc.articleAnalytics || got.AccountAnalytics != tc.accountAnalytics {
			t.Fatalf("scopes %v capabilities = %+v", tc.scopes, got)
		}
	}
}

func TestWechatOpenPlatformAuthorizationStartChecksProjectOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformWechat, Name: "Persona"}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	client := &fakeWechatOpenPlatformClient{}
	svc, err := newWechatOpenPlatformService(repository.New(db), client, []byte("0123456789abcdef0123456789abcdef"), "https://creator.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartAuthorization(context.Background(), uuid.NewString(), project.ID); err != ErrWechatOpenPlatformForbidden {
		t.Fatalf("start authorization error = %v", err)
	}
	if client.redirectURI != "" {
		t.Fatal("authorization URL was generated before checking project ownership")
	}
}

func TestWechatAuthorizerTokenRefreshIsSerializedAndRotatesRefreshToken(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformWechat, Name: "Persona"}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	client := &fakeWechatOpenPlatformClient{}
	svc, err := newWechatOpenPlatformService(repo, client, []byte("0123456789abcdef0123456789abcdef"), "https://creator.example.com")
	if err != nil {
		t.Fatal(err)
	}
	encryptedRefreshToken, err := svc.encryptRefreshToken("old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WechatOpenPlatformAuthorizations().Upsert(context.Background(), &model.WechatOpenPlatformAuthorization{
		ID: uuid.NewString(), ProjectID: project.ID, UserID: project.UserID, AuthorizerAppID: "wx-authorizer",
		Status: model.WechatAuthorizationStatusConnected, RefreshTokenCiphertext: encryptedRefreshToken,
		TokenVersion: 1, AuthorizedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan string, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			token, err := svc.AccessTokenForProject(context.Background(), project.ID)
			results <- token
			errs <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if token := <-results; token != "refreshed-access" {
			t.Fatalf("access token = %q", token)
		}
	}
	client.mu.Lock()
	refreshes := client.refreshes
	client.mu.Unlock()
	if refreshes != 1 {
		t.Fatalf("refresh calls = %d, want one serialized call", refreshes)
	}
	authorization, err := repo.WechatOpenPlatformAuthorizations().FindByProjectID(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := svc.decryptRefreshToken(authorization.RefreshTokenCiphertext)
	if err != nil || rotated != "rotated-refresh" || authorization.TokenVersion != 2 {
		t.Fatalf("rotated token/version = %q/%d, %v", rotated, authorization.TokenVersion, err)
	}
}

func TestWechatAuthorizerTokenRefreshPropagatesRotationToLinkedProjects(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	userID := uuid.NewString()
	projects := []*model.Project{
		{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformWechat, Name: "Persona one"},
		{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformWechat, Name: "Persona two"},
	}
	for _, project := range projects {
		if err := db.Create(project).Error; err != nil {
			t.Fatal(err)
		}
	}
	client := &fakeWechatOpenPlatformClient{}
	svc, err := newWechatOpenPlatformService(repo, client, []byte("0123456789abcdef0123456789abcdef"), "https://creator.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range projects {
		ciphertext, encryptErr := svc.encryptRefreshToken("old-refresh")
		if encryptErr != nil {
			t.Fatal(encryptErr)
		}
		if err := repo.WechatOpenPlatformAuthorizations().Upsert(context.Background(), &model.WechatOpenPlatformAuthorization{
			ID: uuid.NewString(), ProjectID: project.ID, UserID: userID, AuthorizerAppID: "wx-authorizer",
			Status: model.WechatAuthorizationStatusConnected, RefreshTokenCiphertext: ciphertext,
			TokenVersion: 1, AuthorizedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.AccessTokenForProject(context.Background(), projects[0].ID); err != nil {
		t.Fatal(err)
	}
	for _, project := range projects {
		authorization, findErr := repo.WechatOpenPlatformAuthorizations().FindByProjectID(context.Background(), project.ID)
		if findErr != nil {
			t.Fatal(findErr)
		}
		refreshToken, decryptErr := svc.decryptRefreshToken(authorization.RefreshTokenCiphertext)
		if decryptErr != nil || refreshToken != "rotated-refresh" || authorization.TokenVersion != 2 {
			t.Fatalf("project %s refresh token/version = %q/%d, %v", project.ID, refreshToken, authorization.TokenVersion, decryptErr)
		}
	}
}

func TestWechatOpenPlatformEventVerifiesSignaturesAndDecryptsTicket(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	client := &fakeWechatOpenPlatformClient{}
	svc, err := newWechatOpenPlatformService(repository.New(db), client, []byte("0123456789abcdef0123456789abcdef"), "https://creator.example.com")
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("12345678901234567890123456789012")
	aesKey := base64.RawStdEncoding.EncodeToString(key)
	svc.SetRuntimeConfig("component-app", "callback-token", aesKey)
	plain := "<xml><InfoType>component_verify_ticket</InfoType><CreateTime>1723600000</CreateTime><ComponentVerifyTicket>ticket-value</ComponentVerifyTicket></xml>"
	encrypted := encryptWechatPlatformEvent(t, key, "component-app", plain)
	timestamp, nonce := "1723600000", "nonce-value"
	body := []byte(fmt.Sprintf("<xml><Encrypt><![CDATA[%s]]></Encrypt></xml>", encrypted))
	urlSignature := util.Signature("callback-token", timestamp, nonce)
	messageSignature := util.Signature("callback-token", timestamp, nonce, encrypted)
	if err := svc.HandleEvent(context.Background(), urlSignature, messageSignature, timestamp, nonce, body); err != nil {
		t.Fatal(err)
	}
	if client.ticket != "ticket-value" {
		t.Fatalf("component verify ticket = %q", client.ticket)
	}
	stalePlain := "<xml><InfoType>component_verify_ticket</InfoType><CreateTime>1723599900</CreateTime><ComponentVerifyTicket>stale-ticket</ComponentVerifyTicket></xml>"
	staleEncrypted := encryptWechatPlatformEvent(t, key, "component-app", stalePlain)
	staleTimestamp, staleNonce := "1723600001", "stale-ticket-nonce"
	if err := svc.HandleEvent(
		context.Background(),
		util.Signature("callback-token", staleTimestamp, staleNonce),
		util.Signature("callback-token", staleTimestamp, staleNonce, staleEncrypted),
		staleTimestamp, staleNonce,
		[]byte(fmt.Sprintf("<xml><Encrypt><![CDATA[%s]]></Encrypt></xml>", staleEncrypted)),
	); err != nil {
		t.Fatalf("handle stale ticket event: %v", err)
	}
	if client.ticket != "ticket-value" {
		t.Fatalf("stale event replaced component verify ticket: %q", client.ticket)
	}
	if err := svc.HandleEvent(context.Background(), "wrong-signature", messageSignature, timestamp, nonce, body); err == nil {
		t.Fatal("event with an invalid URL signature was accepted")
	}
	if client.ticket != "ticket-value" {
		t.Fatal("invalid event signature changed the stored ticket")
	}
}

func TestWechatOpenPlatformUpdateAuthorizedRefreshesAuthorizerInfo(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	client := &fakeWechatOpenPlatformClient{
		grant:    WechatOpenPlatformGrant{AuthorizerAppID: "wx-authorizer"},
		identity: WechatAuthorizerIdentity{AccountName: "更新后的公众号", AvatarURL: "https://example.com/new-avatar.png", AccountType: 2, GrantedScopes: []int{11, 7}},
	}
	svc, err := newWechatOpenPlatformService(repo, client, []byte("0123456789abcdef0123456789abcdef"), "https://creator.example.com")
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("12345678901234567890123456789012")
	aesKey := base64.RawStdEncoding.EncodeToString(key)
	svc.SetRuntimeConfig("component-app", "callback-token", aesKey)
	projectID := uuid.NewString()
	if err := repo.WechatOpenPlatformAuthorizations().Upsert(context.Background(), &model.WechatOpenPlatformAuthorization{
		ID: uuid.NewString(), ProjectID: projectID, UserID: uuid.NewString(), AuthorizerAppID: "wx-authorizer",
		AccountName: "旧公众号名称", AvatarURL: "https://example.com/old-avatar.png", AccountType: 1,
		GrantedScopes: []int{1}, Status: model.WechatAuthorizationStatusConnected, RefreshTokenCiphertext: "ciphertext",
		AuthorizedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	plain := fmt.Sprintf("<xml><InfoType>updateauthorized</InfoType><CreateTime>%d</CreateTime><AuthorizerAppid>wx-authorizer</AuthorizerAppid></xml>", time.Now().Add(time.Minute).Unix())
	encrypted := encryptWechatPlatformEvent(t, key, "component-app", plain)
	timestamp, nonce := "1723600000", "nonce-update"
	body := []byte(fmt.Sprintf("<xml><Encrypt><![CDATA[%s]]></Encrypt></xml>", encrypted))
	err = svc.HandleEvent(context.Background(), util.Signature("callback-token", timestamp, nonce), util.Signature("callback-token", timestamp, nonce, encrypted), timestamp, nonce, body)
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := repo.WechatOpenPlatformAuthorizations().FindByProjectID(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if authorization.AccountName != "更新后的公众号" || authorization.AvatarURL != "https://example.com/new-avatar.png" || authorization.AccountType != 2 ||
		len(authorization.GrantedScopes) != 2 || authorization.GrantedScopes[0] != 11 || authorization.GrantedScopes[1] != 7 {
		t.Fatalf("updated authorization metadata = %#v", authorization)
	}
}

func TestWechatOpenPlatformUnauthorizedEventRevokesAndClearsTokenIdempotently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	client := &fakeWechatOpenPlatformClient{
		grant: WechatOpenPlatformGrant{AuthorizerAppID: "wx-authorizer"}, cached: true, cachedToken: "authorizer-access",
	}
	svc, err := newWechatOpenPlatformService(repo, client, []byte("0123456789abcdef0123456789abcdef"), "https://creator.example.com")
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("12345678901234567890123456789012")
	svc.SetRuntimeConfig("component-app", "callback-token", base64.RawStdEncoding.EncodeToString(key))
	projectID := uuid.NewString()
	if err := repo.WechatOpenPlatformAuthorizations().Upsert(context.Background(), &model.WechatOpenPlatformAuthorization{
		ID: uuid.NewString(), ProjectID: projectID, UserID: uuid.NewString(), AuthorizerAppID: "wx-authorizer",
		AccountName: "公众号名称", Status: model.WechatAuthorizationStatusConnected, RefreshTokenCiphertext: "ciphertext",
		TokenVersion: 1, AuthorizedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	timestamp := "1723600000"
	staleNonce := "nonce-stale-revoke"
	stalePlain := fmt.Sprintf("<xml><InfoType>unauthorized</InfoType><CreateTime>%d</CreateTime><AuthorizerAppid>wx-authorizer</AuthorizerAppid></xml>", time.Now().Add(-time.Minute).Unix())
	staleEncrypted := encryptWechatPlatformEvent(t, key, "component-app", stalePlain)
	staleBody := []byte(fmt.Sprintf("<xml><Encrypt><![CDATA[%s]]></Encrypt></xml>", staleEncrypted))
	if err := svc.HandleEvent(context.Background(), util.Signature("callback-token", timestamp, staleNonce), util.Signature("callback-token", timestamp, staleNonce, staleEncrypted), timestamp, staleNonce, staleBody); err != nil {
		t.Fatalf("handle stale unauthorized event: %v", err)
	}
	stillConnected, err := repo.WechatOpenPlatformAuthorizations().FindByProjectID(context.Background(), projectID)
	if err != nil || stillConnected.Status != model.WechatAuthorizationStatusConnected || !client.cached {
		t.Fatalf("stale revoke changed current authorization: authorization=%#v cache=%#v err=%v", stillConnected, client, err)
	}

	nonce := "nonce-revoke"
	plain := fmt.Sprintf("<xml><InfoType>unauthorized</InfoType><CreateTime>%d</CreateTime><AuthorizerAppid>wx-authorizer</AuthorizerAppid></xml>", time.Now().Add(time.Second).Unix())
	encrypted := encryptWechatPlatformEvent(t, key, "component-app", plain)
	body := []byte(fmt.Sprintf("<xml><Encrypt><![CDATA[%s]]></Encrypt></xml>", encrypted))
	urlSignature := util.Signature("callback-token", timestamp, nonce)
	messageSignature := util.Signature("callback-token", timestamp, nonce, encrypted)
	for range 2 {
		if err := svc.HandleEvent(context.Background(), urlSignature, messageSignature, timestamp, nonce, body); err != nil {
			t.Fatalf("handle repeated unauthorized event: %v", err)
		}
	}
	authorization, err := repo.WechatOpenPlatformAuthorizations().FindByProjectID(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if authorization.Status != model.WechatAuthorizationStatusRevoked || authorization.RefreshTokenCiphertext != "" || authorization.RevokedAt == nil || authorization.TokenVersion != 2 {
		t.Fatalf("revoked authorization = %#v", authorization)
	}
	if client.cached || client.cachedToken != "" {
		t.Fatalf("authorizer access token remains cached: %#v", client)
	}
}

func encryptWechatPlatformEvent(t *testing.T, key []byte, componentAppID, message string) string {
	t.Helper()
	plain := make([]byte, 16, 16+4+len(message)+len(componentAppID)+32)
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(message)))
	plain = append(plain, length...)
	plain = append(plain, message...)
	plain = append(plain, componentAppID...)
	padding := 32 - len(plain)%32
	for range padding {
		plain = append(plain, byte(padding))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(encrypted, plain)
	return base64.StdEncoding.EncodeToString(encrypted)
}
