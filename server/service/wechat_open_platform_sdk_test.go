package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/silenceper/wechat/v2"
	opconfig "github.com/silenceper/wechat/v2/openplatform/config"
	"github.com/silenceper/wechat/v2/util"
)

type openPlatformTransport func(*http.Request) (*http.Response, error)

func (f openPlatformTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenPlatformSDKRefreshDoesNotCacheBeforePersistence(t *testing.T) {
	mr := miniredis.RunT(t)
	cache := newWechatOpenPlatformRedisCache(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	client := &wechatSDKOpenPlatformClient{platform: wechat.NewWechat().GetOpenPlatform(&opconfig.Config{AppID: "component", Cache: cache}), cache: cache}
	if err := cache.Set("component_access_token_component", "component-token", time.Hour); err != nil {
		t.Fatal(err)
	}
	previous := util.DefaultHTTPClient
	t.Cleanup(func() { util.DefaultHTTPClient = previous })
	util.DefaultHTTPClient = &http.Client{Transport: openPlatformTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"authorizer_access_token":"new-token","authorizer_refresh_token":"new-refresh","expires_in":7200}`)), Header: make(http.Header)}, nil
	})}
	grant, err := client.RefreshAuthorizerToken(context.Background(), "wx-account", "refresh")
	if err != nil {
		t.Fatal(err)
	}
	if grant.AuthorizerAppID != "wx-account" {
		t.Fatalf("missing response appid must use requested identity: %+v", grant)
	}
	if mr.Exists("authorizer_access_token_wx-account") {
		t.Fatal("SDK exposed token before durable refresh-token update")
	}
}

func TestOpenPlatformSDKTicketSurvivesComponentTokenExpiration(t *testing.T) {
	mr := miniredis.RunT(t)
	cache := newWechatOpenPlatformRedisCache(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	client := &wechatSDKOpenPlatformClient{platform: wechat.NewWechat().GetOpenPlatform(&opconfig.Config{AppID: "component", Cache: cache}), cache: cache}
	if err := cache.client.Set(context.Background(), "wechat:component:ticket:component", "legacy-string-ticket", 0).Err(); err != nil {
		t.Fatal(err)
	}
	exchanges := 0
	previous := util.DefaultHTTPClient
	t.Cleanup(func() { util.DefaultHTTPClient = previous })
	util.DefaultHTTPClient = &http.Client{Transport: openPlatformTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"pre_auth_code":"pre-code"}`
		if strings.Contains(r.URL.Path, "api_component_token") {
			exchanges++
			body = `{"component_access_token":"component-token","expires_in":7200}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	ctx := context.Background()
	if err := client.SetComponentVerifyTicket(ctx, "ticket", 200); err != nil {
		t.Fatal(err)
	}
	if err := client.SetComponentVerifyTicket(ctx, "stale-ticket", 199); err != nil {
		t.Fatal(err)
	}
	if ticket, err := cache.client.HGet(ctx, wechatComponentTicketCacheKey("component"), "ticket").Result(); err != nil || ticket != "ticket" {
		t.Fatalf("ticket after stale update = %q, %v", ticket, err)
	}
	if createdAt, err := cache.client.HGet(ctx, wechatComponentTicketCacheKey("component"), "created_at").Result(); err != nil || createdAt != "200" {
		t.Fatalf("ticket timestamp after stale update = %q, %v", createdAt, err)
	}
	if legacy, err := cache.client.Get(ctx, "wechat:component:ticket:component").Result(); err != nil || legacy != "ticket" {
		t.Fatalf("legacy ticket cache key was not synchronized = %q, %v", legacy, err)
	}
	if _, err := client.AuthorizationURL(context.Background(), "https://example.com/callback"); err != nil {
		t.Fatal(err)
	}
	mr.FastForward(3 * time.Hour)
	if _, err := client.AuthorizationURL(context.Background(), "https://example.com/callback"); err != nil {
		t.Fatalf("ticket must support refresh without another event: %v", err)
	}
	if exchanges != 2 {
		t.Fatalf("component exchanges = %d", exchanges)
	}
}

func TestOpenPlatformSDKMigratesLegacyTicketBeforeComponentRefresh(t *testing.T) {
	mr := miniredis.RunT(t)
	cache := newWechatOpenPlatformRedisCache(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	client := &wechatSDKOpenPlatformClient{platform: wechat.NewWechat().GetOpenPlatform(&opconfig.Config{AppID: "component", Cache: cache}), cache: cache}
	if err := cache.client.Set(context.Background(), wechatLegacyComponentTicketCacheKey("component"), "legacy-ticket", 0).Err(); err != nil {
		t.Fatal(err)
	}
	previous := util.DefaultHTTPClient
	t.Cleanup(func() { util.DefaultHTTPClient = previous })
	util.DefaultHTTPClient = &http.Client{Transport: openPlatformTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "api_component_token") {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"component_access_token":"component-token","expires_in":7200}`)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"pre_auth_code":"pre-code"}`)), Header: make(http.Header)}, nil
	})}
	if _, err := client.AuthorizationURL(context.Background(), "https://example.com/callback"); err != nil {
		t.Fatalf("legacy ticket should support component token refresh: %v", err)
	}
	if ticket, err := cache.client.HGet(context.Background(), wechatComponentTicketCacheKey("component"), "ticket").Result(); err != nil || ticket != "legacy-ticket" {
		t.Fatalf("migrated ticket = %q, %v", ticket, err)
	}
}
