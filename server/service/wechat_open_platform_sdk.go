package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/silenceper/wechat/v2"
	wechatcache "github.com/silenceper/wechat/v2/cache"
	"github.com/silenceper/wechat/v2/openplatform"
	opconfig "github.com/silenceper/wechat/v2/openplatform/config"
	opcontext "github.com/silenceper/wechat/v2/openplatform/context"
	"github.com/silenceper/wechat/v2/util"
)

type wechatOpenPlatformRedisCache struct{ client *redis.Client }

func newWechatOpenPlatformRedisCache(client *redis.Client) *wechatOpenPlatformRedisCache {
	return &wechatOpenPlatformRedisCache{client: client}
}

func (c *wechatOpenPlatformRedisCache) Get(key string) interface{} {
	return c.GetContext(context.Background(), key)
}

func (c *wechatOpenPlatformRedisCache) Set(key string, value interface{}, ttl time.Duration) error {
	return c.SetContext(context.Background(), key, value, ttl)
}

func (c *wechatOpenPlatformRedisCache) IsExist(key string) bool {
	return c.IsExistContext(context.Background(), key)
}

func (c *wechatOpenPlatformRedisCache) Delete(key string) error {
	return c.DeleteContext(context.Background(), key)
}

func (c *wechatOpenPlatformRedisCache) GetContext(ctx context.Context, key string) interface{} {
	if c == nil || c.client == nil || isWechatRefreshTokenCacheKey(key) {
		return nil
	}
	value, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil
	}
	return value
}

func (c *wechatOpenPlatformRedisCache) SetContext(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if isWechatRefreshTokenCacheKey(key) {
		return nil
	}
	if c == nil || c.client == nil {
		return fmt.Errorf("WeChat Open Platform shared cache is unavailable")
	}
	if ttl <= 0 {
		return fmt.Errorf("invalid WeChat cache lifetime")
	}
	return c.client.Set(ctx, key, fmt.Sprint(value), ttl).Err()
}

func (c *wechatOpenPlatformRedisCache) IsExistContext(ctx context.Context, key string) bool {
	if c == nil || c.client == nil || isWechatRefreshTokenCacheKey(key) {
		return false
	}
	count, err := c.client.Exists(ctx, key).Result()
	return err == nil && count > 0
}

func (c *wechatOpenPlatformRedisCache) DeleteContext(ctx context.Context, key string) error {
	if isWechatRefreshTokenCacheKey(key) || c == nil || c.client == nil {
		return nil
	}
	return c.client.Del(ctx, key).Err()
}

func isWechatRefreshTokenCacheKey(key string) bool {
	return strings.HasPrefix(key, "authorizer_refresh_token_")
}

type wechatSDKOpenPlatformClient struct {
	platform *openplatform.OpenPlatform
	cache    *wechatOpenPlatformRedisCache
}

func NewWechatOpenPlatformService(repo repository.Repository, settings serverconfig.WechatOpenPlatformConfig, publicBaseURL string, redisClient *redis.Client) (*WechatOpenPlatformService, error) {
	if !settings.Enabled() {
		return nil, ErrWechatOpenPlatformDisabled
	}
	if redisClient == nil {
		return nil, fmt.Errorf("WeChat Open Platform requires shared Redis cache")
	}
	encryptionKey, err := decodeWechatOpenPlatformEncryptionKey(settings.TokenEncryptionKey)
	if err != nil {
		return nil, err
	}
	cache := newWechatOpenPlatformRedisCache(redisClient)
	openConfig := &opconfig.Config{
		AppID: settings.ComponentAppID, AppSecret: settings.ComponentAppSecret,
		Token: settings.MessageToken, EncodingAESKey: settings.EncodingAESKey, Cache: cache,
	}
	client := &wechatSDKOpenPlatformClient{platform: wechat.NewWechat().GetOpenPlatform(openConfig), cache: cache}
	service, err := newWechatOpenPlatformService(repo, client, encryptionKey, publicBaseURL)
	if err != nil {
		return nil, err
	}
	service.SetRuntimeConfig(settings.ComponentAppID, settings.MessageToken, settings.EncodingAESKey)
	service.SetRedis(redisClient)
	return service, nil
}

func decodeWechatOpenPlatformEncryptionKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if len(value) == 32 {
		return []byte(value), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("wechat_open_platform.token_encryption_key must be 32 bytes or base64-encoded 32 bytes")
	}
	return decoded, nil
}

func (c *wechatSDKOpenPlatformClient) AuthorizationURL(ctx context.Context, redirectURI string) (string, error) {
	if err := c.ensureComponentToken(ctx); err != nil {
		return "", err
	}
	code, err := c.platform.GetPreCodeContext(ctx)
	if err != nil || strings.TrimSpace(code) == "" {
		return "", ErrWechatOpenPlatformAuthorizationFailed
	}
	return "https://mp.weixin.qq.com/cgi-bin/componentloginpage?" + url.Values{"component_appid": {c.platform.AppID}, "pre_auth_code": {code}, "redirect_uri": {redirectURI}, "auth_type": {"1"}}.Encode(), nil
}

func (c *wechatSDKOpenPlatformClient) QueryAuthorization(ctx context.Context, code string) (*WechatOpenPlatformGrant, error) {
	if err := c.ensureComponentToken(ctx); err != nil {
		return nil, err
	}
	info, err := c.platform.QueryAuthCodeContext(ctx, code)
	if err != nil || info == nil {
		return nil, err
	}
	return &WechatOpenPlatformGrant{
		AuthorizerAppID: info.Appid, AccessToken: info.AccessToken, RefreshToken: info.RefreshToken,
		ExpiresIn: time.Duration(info.ExpiresIn) * time.Second, GrantedScopes: wechatScopes(info.FuncInfo),
	}, nil
}

func (c *wechatSDKOpenPlatformClient) GetAuthorizerInfo(ctx context.Context, appID string) (*WechatAuthorizerIdentity, error) {
	if err := c.ensureComponentToken(ctx); err != nil {
		return nil, err
	}
	info, authorizationInfo, err := c.platform.GetAuthrInfoContext(ctx, appID)
	if err != nil || info == nil || authorizationInfo == nil {
		return nil, err
	}
	return &WechatAuthorizerIdentity{
		AccountName: info.NickName, AvatarURL: info.HeadImg,
		AccountType: info.ServiceTypeInfo.ID, GrantedScopes: wechatScopes(authorizationInfo.FuncInfo),
	}, nil
}

func (c *wechatSDKOpenPlatformClient) CacheAuthorizerAccessToken(ctx context.Context, appID, token string, expiresIn time.Duration) error {
	ttl := expiresIn - 30*time.Second
	if ttl <= 0 {
		return fmt.Errorf("invalid WeChat authorizer access-token lifetime")
	}
	return c.cache.SetContext(ctx, "authorizer_access_token_"+appID, token, ttl)
}

func (c *wechatSDKOpenPlatformClient) CachedAuthorizerAccessToken(ctx context.Context, appID string) (string, error) {
	value := c.cache.GetContext(ctx, "authorizer_access_token_"+appID)
	if value == nil {
		return "", nil
	}
	token, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("invalid cached WeChat authorizer token")
	}
	return token, nil
}

func (c *wechatSDKOpenPlatformClient) DeleteAuthorizerAccessToken(ctx context.Context, appID string) error {
	return c.cache.DeleteContext(ctx, "authorizer_access_token_"+appID)
}

func (c *wechatSDKOpenPlatformClient) RefreshAuthorizerToken(ctx context.Context, appID, refreshToken string) (*WechatOpenPlatformGrant, error) {
	if err := c.ensureComponentToken(ctx); err != nil {
		return nil, err
	}
	token, err := c.platform.GetComponentAccessTokenContext(ctx)
	if err != nil {
		return nil, err
	}
	// The SDK refresh method caches before persistence and ignores errcode.
	// Keep this exchange inside the Server's refresh-token CAS boundary.
	body, err := util.PostJSONContext(ctx, "https://api.weixin.qq.com/cgi-bin/component/api_authorizer_token?component_access_token="+url.QueryEscape(token), map[string]string{
		"component_appid": c.platform.AppID, "authorizer_appid": appID, "authorizer_refresh_token": refreshToken,
	})
	if err != nil {
		return nil, fmt.Errorf("WeChat authorizer token exchange failed")
	}
	var info struct {
		opcontext.AuthrAccessToken
		util.CommonError
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, ErrWechatOpenPlatformAuthorizationFailed
	}
	if info.ErrCode != 0 || info.AccessToken == "" || info.RefreshToken == "" || info.ExpiresIn <= 30 || (info.Appid != "" && info.Appid != appID) {
		return nil, fmt.Errorf("WeChat authorizer token exchange rejected (code %d)", info.ErrCode)
	}
	return &WechatOpenPlatformGrant{
		AuthorizerAppID: appID, AccessToken: info.AccessToken, RefreshToken: info.RefreshToken,
		ExpiresIn: time.Duration(info.ExpiresIn) * time.Second,
	}, nil
}

func (c *wechatSDKOpenPlatformClient) SetComponentVerifyTicket(ctx context.Context, ticket string, createdAt int64) error {
	if createdAt <= 0 || strings.TrimSpace(ticket) == "" {
		return ErrWechatOpenPlatformEventInvalid
	}
	key := wechatComponentTicketCacheKey(c.platform.AppID)
	_, err := c.cache.client.Eval(ctx, `
local current = redis.call('hget', KEYS[1], 'created_at')
if current and tonumber(current) >= tonumber(ARGV[1]) then
  return 0
end
redis.call('hset', KEYS[1], 'ticket', ARGV[2], 'created_at', ARGV[1])
return 1
`, []string{key}, createdAt, ticket).Result()
	return err
}

func wechatComponentTicketCacheKey(appID string) string {
	return "wechat:component:ticket:v2:" + appID
}

func wechatLegacyComponentTicketCacheKey(appID string) string {
	return "wechat:component:ticket:" + appID
}

func (c *wechatSDKOpenPlatformClient) componentVerifyTicket(ctx context.Context) (string, error) {
	key := wechatComponentTicketCacheKey(c.platform.AppID)
	ticket, err := c.cache.client.HGet(ctx, key, "ticket").Result()
	if err == nil && strings.TrimSpace(ticket) != "" {
		return ticket, nil
	}
	if err != nil && err != redis.Nil {
		return "", err
	}
	legacyTicket, legacyErr := c.cache.client.Get(ctx, wechatLegacyComponentTicketCacheKey(c.platform.AppID)).Result()
	if legacyErr != nil {
		if legacyErr == redis.Nil {
			return "", fmt.Errorf("WeChat component verification ticket is unavailable")
		}
		return "", legacyErr
	}
	if strings.TrimSpace(legacyTicket) == "" {
		return "", fmt.Errorf("WeChat component verification ticket is unavailable")
	}
	migrated, migrateErr := c.cache.client.HSetNX(ctx, key, "ticket", legacyTicket).Result()
	if migrateErr != nil {
		return "", migrateErr
	}
	if migrated {
		if _, migrateErr = c.cache.client.HSet(ctx, key, "created_at", time.Now().Unix()).Result(); migrateErr != nil {
			return "", migrateErr
		}
		return legacyTicket, nil
	}
	ticket, err = c.cache.client.HGet(ctx, key, "ticket").Result()
	if err != nil || strings.TrimSpace(ticket) == "" {
		return "", fmt.Errorf("WeChat component verification ticket is unavailable")
	}
	return ticket, nil
}

func (c *wechatSDKOpenPlatformClient) ensureComponentToken(ctx context.Context) error {
	deadline := time.Now().Add(5 * time.Second)
	key, owner := "wechat:component:refresh:"+c.platform.AppID, uuid.NewString()
	for {
		if token, err := c.platform.GetComponentAccessTokenContext(ctx); err == nil && token != "" {
			return nil
		}
		locked, err := c.cache.client.SetNX(ctx, key, owner, 30*time.Second).Result()
		if err != nil {
			return err
		}
		if locked {
			defer c.cache.client.Eval(context.WithoutCancel(ctx), "if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('del', KEYS[1]) else return 0 end", []string{key}, owner)
			if token, err := c.platform.GetComponentAccessTokenContext(ctx); err == nil && token != "" {
				return nil
			}
			ticket, err := c.componentVerifyTicket(ctx)
			if err != nil {
				return err
			}
			token, err := c.platform.SetComponentAccessTokenContext(ctx, ticket)
			if err != nil || token == nil || token.AccessToken == "" {
				return fmt.Errorf("WeChat component token exchange failed")
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("WeChat component token refresh is busy")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func wechatScopes(items []opcontext.AuthFuncInfo) []int {
	scopes := make([]int, 0, len(items))
	for _, item := range items {
		scopes = append(scopes, item.FuncscopeCategory.ID)
	}
	return scopes
}

var _ wechatcache.ContextCache = (*wechatOpenPlatformRedisCache)(nil)
