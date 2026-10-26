package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/silenceper/wechat/v2/util"
	"gorm.io/gorm"
)

var (
	ErrWechatOpenPlatformDisabled            = errors.New("WeChat Open Platform is not configured")
	ErrWechatOpenPlatformForbidden           = errors.New("WeChat account is not owned by user")
	ErrWechatOpenPlatformProjectUnsupported  = errors.New("project does not support WeChat authorization")
	ErrWechatOpenPlatformStateInvalid        = errors.New("WeChat authorization state is invalid, expired, or already used")
	ErrWechatOpenPlatformAuthorizationFailed = errors.New("WeChat authorization failed")
	ErrWechatOpenPlatformNotConnected        = errors.New("no active WeChat Open Platform authorization")
	ErrWechatOpenPlatformCapabilityMissing   = errors.New("WeChat Open Platform authorization lacks draft capability")
	ErrWechatOpenPlatformTokenConflict       = errors.New("WeChat authorization token changed concurrently")
	ErrWechatOpenPlatformEventInvalid        = errors.New("invalid WeChat Open Platform event")
)

const (
	wechatOpenPlatformStateTTL              = 10 * time.Minute
	wechatAuthorizerTokenRefreshLock        = 30 * time.Second
	wechatAuthorizerRefreshWait             = 5 * time.Second
	wechatOpenPlatformDraftScope            = 11
	wechatOpenPlatformDraftScopeLegacy      = 100
	wechatOpenPlatformFormalPublishScope    = 7
	wechatOpenPlatformArticleAnalyticsScope = 7
	wechatOpenPlatformAccountAnalyticsScope = 2
	wechatOpenPlatformCallbackPath          = "/api/v1/wechat/open-platform/authorization/callback"
	wechatOpenPlatformEventPath             = "/api/v1/wechat/open-platform/events"
)

type WechatOpenPlatformGrant struct {
	AuthorizerAppID string
	AccessToken     string
	RefreshToken    string
	ExpiresIn       time.Duration
	GrantedScopes   []int
}

type WechatAuthorizerIdentity struct {
	AccountName   string
	AvatarURL     string
	AccountType   int
	GrantedScopes []int
}

type wechatOpenPlatformClient interface {
	AuthorizationURL(context.Context, string) (string, error)
	QueryAuthorization(context.Context, string) (*WechatOpenPlatformGrant, error)
	GetAuthorizerInfo(context.Context, string) (*WechatAuthorizerIdentity, error)
	CacheAuthorizerAccessToken(context.Context, string, string, time.Duration) error
	CachedAuthorizerAccessToken(context.Context, string) (string, error)
	DeleteAuthorizerAccessToken(context.Context, string) error
	RefreshAuthorizerToken(context.Context, string, string) (*WechatOpenPlatformGrant, error)
	SetComponentVerifyTicket(context.Context, string, int64) error
}

type WechatAuthorizationStart struct {
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type WechatOpenPlatformConnection struct {
	Enabled                 bool     `json:"authorization_enabled"`
	AuthorizationRecord     bool     `json:"authorization_record_exists"`
	Connected               bool     `json:"connected"`
	Status                  string   `json:"status"`
	AuthorizerAppID         string   `json:"authorizer_app_id,omitempty"`
	AccountName             string   `json:"account_name,omitempty"`
	AvatarURL               string   `json:"avatar_url,omitempty"`
	AccountType             int      `json:"account_type,omitempty"`
	GrantedScopes           []int    `json:"granted_scopes,omitempty"`
	CanCreateDraft          bool     `json:"can_create_draft"`
	CanUploadMaterial       bool     `json:"can_upload_material"`
	CanFormallyPublish      bool     `json:"can_formally_publish"`
	CanReadArticleAnalytics bool     `json:"can_read_article_analytics"`
	CanReadAccountAnalytics bool     `json:"can_read_account_analytics"`
	MissingCapabilities     []string `json:"missing_capabilities,omitempty"`
}

type WechatOpenPlatformService struct {
	repo           repository.Repository
	client         wechatOpenPlatformClient
	redis          *redis.Client
	componentAppID string
	messageToken   string
	encodingAESKey string
	publicBaseURL  string
	tokenCipher    cipher.AEAD
	now            func() time.Time
	refreshMu      sync.Mutex
}

func newWechatOpenPlatformService(repo repository.Repository, client wechatOpenPlatformClient, encryptionKey []byte, publicBaseURL string) (*WechatOpenPlatformService, error) {
	if repo == nil || client == nil {
		return nil, ErrWechatOpenPlatformDisabled
	}
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("WeChat refresh-token encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(strings.TrimSpace(publicBaseURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("WeChat Open Platform requires an HTTPS public base URL")
	}
	return &WechatOpenPlatformService{repo: repo, client: client, publicBaseURL: strings.TrimRight(publicBaseURL, "/"), tokenCipher: aead, now: time.Now}, nil
}

func (s *WechatOpenPlatformService) SetRuntimeConfig(componentAppID, messageToken, encodingAESKey string) {
	if s == nil {
		return
	}
	s.componentAppID = strings.TrimSpace(componentAppID)
	s.messageToken = strings.TrimSpace(messageToken)
	s.encodingAESKey = strings.TrimSpace(encodingAESKey)
}

func (s *WechatOpenPlatformService) SetRedis(client *redis.Client) {
	if s != nil {
		s.redis = client
	}
}

func (s *WechatOpenPlatformService) StartAuthorization(ctx context.Context, userID, projectID string) (*WechatAuthorizationStart, error) {
	if s == nil || s.client == nil {
		return nil, ErrWechatOpenPlatformDisabled
	}
	if err := s.requireProjectOwner(ctx, userID, projectID); err != nil {
		return nil, err
	}
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	supported, err := supportsWechatPlatformForProject(ctx, s.repo, project)
	if err != nil {
		return nil, err
	}
	if !supported {
		return nil, ErrWechatOpenPlatformProjectUnsupported
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("generate WeChat authorization state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	now := s.now().UTC()
	stateRecord := &model.WechatOpenPlatformAuthorizationState{
		StateHash: hashWechatAuthorizationState(state), ProjectID: projectID, UserID: userID,
		ExpiresAt: now.Add(wechatOpenPlatformStateTTL),
	}
	if err := s.repo.WechatOpenPlatformAuthorizations().CreateState(ctx, stateRecord); err != nil {
		return nil, fmt.Errorf("persist WeChat authorization state: %w", err)
	}
	callbackURL, err := url.Parse(s.publicBaseURL + wechatOpenPlatformCallbackPath)
	if err != nil {
		return nil, err
	}
	query := callbackURL.Query()
	query.Set("state", state)
	callbackURL.RawQuery = query.Encode()
	authorizationURL, err := s.client.AuthorizationURL(ctx, callbackURL.String())
	if err != nil {
		return nil, fmt.Errorf("create WeChat authorization URL: %w", err)
	}
	return &WechatAuthorizationStart{AuthorizationURL: authorizationURL, ExpiresAt: stateRecord.ExpiresAt}, nil
}

func (s *WechatOpenPlatformService) CompleteAuthorization(ctx context.Context, state, authorizationCode string) (string, error) {
	state = strings.TrimSpace(state)
	authorizationCode = strings.TrimSpace(authorizationCode)
	if state == "" || authorizationCode == "" {
		return "", ErrWechatOpenPlatformStateInvalid
	}
	stateRecord, ok, err := s.repo.WechatOpenPlatformAuthorizations().ConsumeState(ctx, hashWechatAuthorizationState(state), s.now().UTC())
	if err != nil {
		return "", fmt.Errorf("consume WeChat authorization state: %w", err)
	}
	if !ok {
		return "", ErrWechatOpenPlatformStateInvalid
	}
	if err := s.requireProjectOwner(ctx, stateRecord.UserID, stateRecord.ProjectID); err != nil {
		return "", err
	}
	grant, err := s.client.QueryAuthorization(ctx, authorizationCode)
	if err != nil {
		return "", fmt.Errorf("exchange WeChat authorization code: %w", err)
	}
	if grant == nil || strings.TrimSpace(grant.AuthorizerAppID) == "" || strings.TrimSpace(grant.AccessToken) == "" || strings.TrimSpace(grant.RefreshToken) == "" {
		return "", ErrWechatOpenPlatformAuthorizationFailed
	}
	identity, identityErr := s.client.GetAuthorizerInfo(ctx, grant.AuthorizerAppID)
	if identityErr != nil || identity == nil {
		identity = &WechatAuthorizerIdentity{}
	}
	if grant.ExpiresIn <= 0 {
		grant.ExpiresIn = 2 * time.Hour
	}
	ciphertext, err := s.encryptRefreshToken(grant.RefreshToken)
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	row, getErr := s.repo.WechatOpenPlatformAuthorizations().FindByProjectID(ctx, stateRecord.ProjectID)
	if getErr != nil && !errors.Is(getErr, gorm.ErrRecordNotFound) {
		return "", getErr
	}
	if row == nil || getErr != nil {
		row = &model.WechatOpenPlatformAuthorization{ID: uuid.NewString(), ProjectID: stateRecord.ProjectID}
	}
	row.UserID = stateRecord.UserID
	row.AuthorizerAppID = grant.AuthorizerAppID
	row.AccountName = identity.AccountName
	row.AvatarURL = identity.AvatarURL
	row.AccountType = identity.AccountType
	row.GrantedScopes = append([]int(nil), grant.GrantedScopes...)
	row.Status = model.WechatAuthorizationStatusConnected
	row.RefreshTokenCiphertext = ciphertext
	row.TokenVersion++
	if row.TokenVersion == 0 {
		row.TokenVersion = 1
	}
	row.AuthorizedAt = now
	row.RevokedAt = nil
	row.LastEventAt = nil
	if err := s.client.CacheAuthorizerAccessToken(ctx, grant.AuthorizerAppID, grant.AccessToken, grant.ExpiresIn); err != nil {
		return "", fmt.Errorf("cache WeChat authorizer access token: %w", err)
	}
	if err := s.repo.WechatOpenPlatformAuthorizations().Upsert(ctx, row); err != nil {
		_ = s.client.DeleteAuthorizerAccessToken(context.WithoutCancel(ctx), grant.AuthorizerAppID)
		return "", fmt.Errorf("persist WeChat authorizer: %w", err)
	}
	return stateRecord.ProjectID, nil
}

func (s *WechatOpenPlatformService) AuthorizationReturnURL(projectID, outcome string) string {
	if s == nil {
		return "/projects"
	}
	page, err := url.Parse(s.publicBaseURL + "/projects")
	if err != nil {
		return s.publicBaseURL + "/projects"
	}
	query := page.Query()
	query.Set("wechat_authorization", outcome)
	if projectID != "" {
		query.Set("project_id", projectID)
	}
	page.RawQuery = query.Encode()
	return page.String()
}

func (s *WechatOpenPlatformService) GetConnection(ctx context.Context, userID, projectID string) (*WechatOpenPlatformConnection, error) {
	if err := s.requireProjectOwner(ctx, userID, projectID); err != nil {
		return nil, err
	}
	connection := &WechatOpenPlatformConnection{Enabled: true, Status: model.WechatAuthorizationStatusDisconnected}
	authorization, err := s.repo.WechatOpenPlatformAuthorizations().FindByProjectID(ctx, projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return connection, nil
	}
	if err != nil {
		return nil, err
	}
	connection.AuthorizationRecord = true
	connection.Status = authorization.Status
	if authorization.Status != model.WechatAuthorizationStatusConnected {
		return connection, nil
	}
	connection.Connected = true
	connection.AuthorizerAppID = authorization.AuthorizerAppID
	connection.AccountName = authorization.AccountName
	connection.AvatarURL = authorization.AvatarURL
	connection.AccountType = authorization.AccountType
	connection.GrantedScopes = append([]int(nil), authorization.GrantedScopes...)
	capabilities := wechatCapabilitiesForScopes(authorization.GrantedScopes)
	connection.CanUploadMaterial = capabilities.Draft
	connection.CanCreateDraft = connection.CanUploadMaterial
	connection.CanFormallyPublish = capabilities.FormalPublish
	connection.CanReadArticleAnalytics = capabilities.ArticleAnalytics
	connection.CanReadAccountAnalytics = capabilities.AccountAnalytics
	if !connection.CanUploadMaterial {
		connection.MissingCapabilities = []string{"素材管理"}
	}
	return connection, nil
}

func (s *WechatOpenPlatformService) Disconnect(ctx context.Context, userID, projectID string) error {
	if err := s.requireProjectOwner(ctx, userID, projectID); err != nil {
		return err
	}
	authorization, err := s.repo.WechatOpenPlatformAuthorizations().FindByProjectID(ctx, projectID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if authorization == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err := s.repo.WechatOpenPlatformAuthorizations().DisconnectProject(ctx, projectID, s.now().UTC()); err != nil {
		return err
	}
	return s.client.DeleteAuthorizerAccessToken(ctx, authorization.AuthorizerAppID)
}

func (s *WechatOpenPlatformService) AccessTokenForProject(ctx context.Context, projectID string) (string, error) {
	authorization, err := s.repo.WechatOpenPlatformAuthorizations().FindByProjectID(ctx, projectID)
	if err != nil {
		return "", err
	}
	if authorization.Status != model.WechatAuthorizationStatusConnected {
		return "", ErrWechatOpenPlatformNotConnected
	}
	if token, err := s.client.CachedAuthorizerAccessToken(ctx, authorization.AuthorizerAppID); err == nil && token != "" {
		return token, nil
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if token, cacheErr := s.client.CachedAuthorizerAccessToken(ctx, authorization.AuthorizerAppID); cacheErr == nil && token != "" {
		return token, nil
	}
	if s.redis != nil {
		return s.accessTokenWithRedisLease(ctx, projectID, authorization)
	}
	return s.refreshAuthorizerToken(ctx, projectID)
}

func (s *WechatOpenPlatformService) accessTokenWithRedisLease(ctx context.Context, projectID string, authorization *model.WechatOpenPlatformAuthorization) (string, error) {
	lockKey := "wechat:authorizer:refresh:" + authorization.AuthorizerAppID
	lockValue := uuid.NewString()
	deadline := time.Now().Add(wechatAuthorizerRefreshWait)
	for {
		locked, err := s.redis.SetNX(ctx, lockKey, lockValue, wechatAuthorizerTokenRefreshLock).Result()
		if err != nil {
			return "", fmt.Errorf("acquire WeChat authorizer refresh lease: %w", err)
		}
		if locked {
			defer s.redis.Eval(context.WithoutCancel(ctx), "if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('del', KEYS[1]) else return 0 end", []string{lockKey}, lockValue)
			return s.refreshAuthorizerToken(ctx, projectID)
		}
		if token, tokenErr := s.client.CachedAuthorizerAccessToken(ctx, authorization.AuthorizerAppID); tokenErr == nil && token != "" {
			return token, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for WeChat authorizer token refresh")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *WechatOpenPlatformService) refreshAuthorizerToken(ctx context.Context, projectID string) (string, error) {
	authorization, err := s.repo.WechatOpenPlatformAuthorizations().FindByProjectID(ctx, projectID)
	if err != nil {
		return "", err
	}
	if authorization.Status != model.WechatAuthorizationStatusConnected || authorization.RefreshTokenCiphertext == "" {
		return "", ErrWechatOpenPlatformNotConnected
	}
	refreshToken, err := s.decryptRefreshToken(authorization.RefreshTokenCiphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt WeChat authorizer refresh token: %w", err)
	}
	grant, err := s.client.RefreshAuthorizerToken(ctx, authorization.AuthorizerAppID, refreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh WeChat authorizer token: %w", err)
	}
	if grant == nil || grant.AuthorizerAppID != authorization.AuthorizerAppID || grant.AccessToken == "" || grant.RefreshToken == "" {
		return "", ErrWechatOpenPlatformAuthorizationFailed
	}
	ciphertext, err := s.encryptRefreshToken(grant.RefreshToken)
	if err != nil {
		return "", err
	}
	if err := s.persistRotatedRefreshToken(ctx, projectID, authorization, refreshToken, grant.RefreshToken, ciphertext); err != nil {
		_ = s.client.DeleteAuthorizerAccessToken(context.WithoutCancel(ctx), authorization.AuthorizerAppID)
		return "", err
	}
	if grant.ExpiresIn <= 0 {
		grant.ExpiresIn = 2 * time.Hour
	}
	if err := s.client.CacheAuthorizerAccessToken(ctx, grant.AuthorizerAppID, grant.AccessToken, grant.ExpiresIn); err != nil {
		return "", err
	}
	return grant.AccessToken, nil
}

// persistRotatedRefreshToken propagates a provider-issued refresh-token
// rotation to every project that still points at the same authorizer token.
// A project that was re-authorized or refreshed independently is left alone;
// its ciphertext no longer decrypts to previousRefreshToken and is newer state.
func (s *WechatOpenPlatformService) persistRotatedRefreshToken(ctx context.Context, projectID string, current *model.WechatOpenPlatformAuthorization, previousRefreshToken, nextRefreshToken, currentCiphertext string) error {
	authorizations, err := s.repo.WechatOpenPlatformAuthorizations().FindByAuthorizerAppID(ctx, current.AuthorizerAppID)
	if err != nil {
		return fmt.Errorf("find linked WeChat authorizations: %w", err)
	}
	updatedAt := s.now().UTC()
	currentUpdated := false
	for _, authorization := range authorizations {
		if authorization == nil || authorization.Status != model.WechatAuthorizationStatusConnected || authorization.RefreshTokenCiphertext == "" {
			continue
		}
		storedRefreshToken, decryptErr := s.decryptRefreshToken(authorization.RefreshTokenCiphertext)
		if decryptErr != nil {
			return fmt.Errorf("decrypt linked WeChat authorizer refresh token: %w", decryptErr)
		}
		if storedRefreshToken != previousRefreshToken {
			continue
		}
		ciphertext := currentCiphertext
		if authorization.ProjectID != projectID {
			ciphertext, err = s.encryptRefreshToken(nextRefreshToken)
			if err != nil {
				return fmt.Errorf("encrypt linked WeChat authorizer refresh token: %w", err)
			}
		}
		won, updateErr := s.repo.WechatOpenPlatformAuthorizations().UpdateRefreshToken(ctx, authorization.ProjectID, authorization.TokenVersion, ciphertext, updatedAt)
		if updateErr != nil {
			return fmt.Errorf("persist WeChat authorizer refresh token: %w", updateErr)
		}
		if !won {
			latest, findErr := s.repo.WechatOpenPlatformAuthorizations().FindByProjectID(ctx, authorization.ProjectID)
			if findErr != nil {
				return fmt.Errorf("verify linked WeChat authorizer refresh token update: %w", findErr)
			}
			if latest.Status == model.WechatAuthorizationStatusConnected && latest.RefreshTokenCiphertext != "" {
				latestRefreshToken, latestErr := s.decryptRefreshToken(latest.RefreshTokenCiphertext)
				if latestErr != nil {
					return fmt.Errorf("decrypt current WeChat authorizer refresh token: %w", latestErr)
				}
				if latestRefreshToken == previousRefreshToken {
					return ErrWechatOpenPlatformTokenConflict
				}
			}
			continue
		}
		if authorization.ProjectID == projectID {
			currentUpdated = true
		}
	}
	if !currentUpdated {
		return ErrWechatOpenPlatformTokenConflict
	}
	return nil
}

func (s *WechatOpenPlatformService) requireProjectOwner(ctx context.Context, userID, projectID string) error {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrProjectNotFound
	}
	if err != nil {
		return err
	}
	if project.UserID != userID {
		return ErrWechatOpenPlatformForbidden
	}
	return nil
}

func (s *WechatOpenPlatformService) encryptRefreshToken(refreshToken string) (string, error) {
	nonce := make([]byte, s.tokenCipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := s.tokenCipher.Seal(nonce, nonce, []byte(refreshToken), []byte("wechat-authorizer-refresh-v1"))
	return base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func (s *WechatOpenPlatformService) decryptRefreshToken(encoded string) (string, error) {
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	nonceSize := s.tokenCipher.NonceSize()
	if len(data) <= nonceSize {
		return "", fmt.Errorf("invalid encrypted WeChat refresh token")
	}
	plain, err := s.tokenCipher.Open(nil, data[:nonceSize], data[nonceSize:], []byte("wechat-authorizer-refresh-v1"))
	if err != nil {
		return "", fmt.Errorf("invalid encrypted WeChat refresh token")
	}
	return string(plain), nil
}

func hashWechatAuthorizationState(state string) string {
	hash := sha256.Sum256([]byte(state))
	return fmt.Sprintf("%x", hash)
}

func hasWechatScope(scopes []int, scope int) bool {
	for _, granted := range scopes {
		if granted == scope {
			return true
		}
	}
	return false
}

type wechatScopeCapabilities struct{ Draft, FormalPublish, ArticleAnalytics, AccountAnalytics bool }

func wechatCapabilitiesForScopes(scopes []int) wechatScopeCapabilities {
	draft := hasWechatScope(scopes, wechatOpenPlatformDraftScope) || hasWechatScope(scopes, wechatOpenPlatformDraftScopeLegacy)
	return wechatScopeCapabilities{
		Draft:            draft,
		FormalPublish:    draft && hasWechatScope(scopes, wechatOpenPlatformFormalPublishScope),
		ArticleAnalytics: hasWechatScope(scopes, wechatOpenPlatformArticleAnalyticsScope),
		AccountAnalytics: hasWechatScope(scopes, wechatOpenPlatformAccountAnalyticsScope),
	}
}

func (s *WechatOpenPlatformService) VerifyCallbackURL(signature, timestamp, nonce string) bool {
	return s != nil && s.messageToken != "" && signature == util.Signature(s.messageToken, timestamp, nonce)
}

func (s *WechatOpenPlatformService) HandleEvent(ctx context.Context, signature, messageSignature, timestamp, nonce string, body []byte) error {
	if s == nil || s.messageToken == "" || s.componentAppID == "" || s.encodingAESKey == "" {
		return ErrWechatOpenPlatformDisabled
	}
	var envelope struct {
		Encrypt string `xml:"Encrypt"`
	}
	if err := xml.Unmarshal(body, &envelope); err != nil || strings.TrimSpace(envelope.Encrypt) == "" {
		return ErrWechatOpenPlatformEventInvalid
	}
	if signature != util.Signature(s.messageToken, timestamp, nonce) {
		return ErrWechatOpenPlatformEventInvalid
	}
	if messageSignature != util.Signature(s.messageToken, timestamp, nonce, envelope.Encrypt) {
		return ErrWechatOpenPlatformEventInvalid
	}
	_, plain, err := util.DecryptMsg(s.componentAppID, envelope.Encrypt, s.encodingAESKey)
	if err != nil {
		return ErrWechatOpenPlatformEventInvalid
	}
	var event struct {
		InfoType              string `xml:"InfoType"`
		Event                 string `xml:"Event"`
		CreateTime            int64  `xml:"CreateTime"`
		ComponentVerifyTicket string `xml:"ComponentVerifyTicket"`
		AuthorizerAppID       string `xml:"AuthorizerAppid"`
	}
	if err := xml.Unmarshal(plain, &event); err != nil {
		return ErrWechatOpenPlatformEventInvalid
	}
	infoType := strings.ToLower(strings.TrimSpace(event.InfoType))
	if infoType == "" {
		infoType = strings.ToLower(strings.TrimSpace(event.Event))
	}
	switch infoType {
	case "component_verify_ticket":
		if event.ComponentVerifyTicket == "" || event.CreateTime <= 0 {
			return ErrWechatOpenPlatformEventInvalid
		}
		return s.client.SetComponentVerifyTicket(ctx, event.ComponentVerifyTicket, event.CreateTime)
	case "unauthorized":
		if event.AuthorizerAppID == "" || event.CreateTime <= 0 {
			return ErrWechatOpenPlatformEventInvalid
		}
		eventAt := time.Unix(event.CreateTime, 0).UTC()
		authorizations, err := s.repo.WechatOpenPlatformAuthorizations().FindByAuthorizerAppID(ctx, event.AuthorizerAppID)
		if err != nil {
			return err
		}
		for _, authorization := range authorizations {
			if authorization.AuthorizedAt.After(eventAt) {
				return nil
			}
		}
		revoked := false
		for _, authorization := range authorizations {
			if authorization.Status != model.WechatAuthorizationStatusConnected || !wechatAuthorizationEventIsNewer(authorization, eventAt) {
				continue
			}
			won, revokeErr := s.repo.WechatOpenPlatformAuthorizations().RevokeProjectForEvent(
				ctx, authorization.ProjectID, event.AuthorizerAppID, authorization.AuthorizedAt, authorization.LastEventAt, eventAt, s.now().UTC(),
			)
			if revokeErr != nil {
				return revokeErr
			}
			revoked = revoked || won
		}
		if !revoked {
			return nil
		}
		currentAuthorizations, err := s.repo.WechatOpenPlatformAuthorizations().FindByAuthorizerAppID(ctx, event.AuthorizerAppID)
		if err != nil {
			return err
		}
		for _, authorization := range currentAuthorizations {
			if authorization.Status == model.WechatAuthorizationStatusConnected && !eventAt.After(authorization.AuthorizedAt) {
				return nil
			}
		}
		return s.client.DeleteAuthorizerAccessToken(ctx, event.AuthorizerAppID)
	case "authorized":
		// The browser callback carries state that binds a new account to a project.
		return nil
	case "updateauthorized":
		if event.AuthorizerAppID == "" || event.CreateTime <= 0 {
			return ErrWechatOpenPlatformEventInvalid
		}
		return s.refreshAuthorizerInfo(ctx, event.AuthorizerAppID, time.Unix(event.CreateTime, 0).UTC())
	default:
		return nil
	}
}

func (s *WechatOpenPlatformService) refreshAuthorizerInfo(ctx context.Context, authorizerAppID string, eventAt time.Time) error {
	authorizations, err := s.repo.WechatOpenPlatformAuthorizations().FindByAuthorizerAppID(ctx, authorizerAppID)
	if err != nil {
		return err
	}
	if len(authorizations) == 0 {
		return nil
	}
	identity, err := s.client.GetAuthorizerInfo(ctx, authorizerAppID)
	if err != nil {
		return fmt.Errorf("refresh WeChat authorizer info: %w", err)
	}
	if identity == nil {
		return ErrWechatOpenPlatformAuthorizationFailed
	}
	now := s.now().UTC()
	for _, authorization := range authorizations {
		if authorization.Status != model.WechatAuthorizationStatusConnected || !wechatAuthorizationEventIsNewer(authorization, eventAt) {
			continue
		}
		if err := s.repo.WechatOpenPlatformAuthorizations().UpdateAuthorizerInfo(
			ctx, authorization.ProjectID, authorizerAppID,
			identity.AccountName, identity.AvatarURL, identity.AccountType, identity.GrantedScopes,
			authorization.AuthorizedAt, authorization.LastEventAt, eventAt, now,
		); err != nil {
			return fmt.Errorf("persist updated WeChat authorizer info: %w", err)
		}
	}
	return nil
}

func wechatAuthorizationEventIsNewer(authorization *model.WechatOpenPlatformAuthorization, eventAt time.Time) bool {
	return authorization != nil && !eventAt.Before(authorization.AuthorizedAt) &&
		(authorization.LastEventAt == nil || eventAt.After(*authorization.LastEventAt))
}
