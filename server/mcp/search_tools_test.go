package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func searchToolRequest(t *testing.T, args map[string]any) *mcp.CallToolRequest {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: raw}}
}

func TestSearchWebHandlerRequiresExecutionIdentityBeforeProviderLookup(t *testing.T) {
	old := svcs
	svcs = nil
	t.Cleanup(func() { svcs = old })
	result, err := searchWebHandler(context.Background(), searchToolRequest(t, map[string]any{"query": "anban"}))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError || !strings.Contains(toolResultText(result), "execution_identity_required") {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchWebHandlerRequiresCompleteExecutionIdentity(t *testing.T) {
	old := svcs
	svcs = nil
	t.Cleanup(func() { svcs = old })
	for name, ctx := range map[string]context.Context{
		"missing project and task": withMCPExecutionIdentity(context.Background(), "user-1", "", "", "execution-1"),
		"missing user":             withMCPExecutionIdentity(context.Background(), "", "project-1", "task-1", "execution-1"),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := searchWebHandler(ctx, searchToolRequest(t, map[string]any{"query": "anban"}))
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.IsError || !strings.Contains(toolResultText(result), "execution_identity_required") {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestSearchWebHandlerRejectsProviderOverrideAndInvalidInput(t *testing.T) {
	old := svcs
	svcs = nil
	t.Cleanup(func() { svcs = old })
	ctx := withMCPExecutionIdentity(context.Background(), "user-1", "project-1", "task-1", "execution-1")
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "provider override", args: map[string]any{"query": "q", "provider": "other"}, want: "provider_selection_forbidden"},
		{name: "empty query", args: map[string]any{"query": "  "}, want: "invalid_request"},
		{name: "non-string ori_question", args: map[string]any{"query": "q", "ori_question": 42}, want: "invalid_request"},
		{name: "empty localization", args: map[string]any{"query": "q", "localization": "  "}, want: "invalid_request"},
		{name: "oversized ori_question", args: map[string]any{"query": "q", "ori_question": strings.Repeat("x", 129)}, want: "invalid_request"},
		{name: "fractional limit", args: map[string]any{"query": "q", "limit": 1.5}, want: "invalid_request"},
		{name: "large limit", args: map[string]any{"query": "q", "limit": 51}, want: "invalid_request"},
		{name: "sites scalar", args: map[string]any{"query": "q", "sites": "example.com"}, want: "invalid_request"},
		{name: "sites non-string item", args: map[string]any{"query": "q", "sites": []any{"example.com", 1}}, want: "invalid_request"},
		{name: "block hosts non-string item", args: map[string]any{"query": "q", "block_hosts": []any{false}}, want: "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := searchWebHandler(ctx, searchToolRequest(t, tt.args))
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.IsError || !strings.Contains(toolResultText(result), tt.want) {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestParseSearchStringArrayRejectsInvalidShapes(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"scalar":       {"sites": "example.com"},
		"non-string":   {"sites": []any{"example.com", 1}},
		"empty-string": {"sites": []any{"  "}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSearchStringArray(args, "sites"); err == nil {
				t.Fatal("parseSearchStringArray accepted invalid input")
			}
		})
	}
}

func TestSearchWebHandlerReturnsProviderUnavailableForConfiguredGap(t *testing.T) {
	old := svcs
	svcs = &Services{}
	t.Cleanup(func() { svcs = old })
	ctx := withMCPExecutionIdentity(context.Background(), "user-1", "project-1", "task-1", "execution-1")
	result, err := searchWebHandler(ctx, searchToolRequest(t, map[string]any{"query": "anban"}))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError || !strings.Contains(toolResultText(result), "provider_unavailable") {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchWebSchemaExposesSitesConstraint(t *testing.T) {
	for _, tool := range listRegisteredTools(t, registerSearchTools) {
		if tool.Name != "search_web" {
			continue
		}
		properties := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		if _, ok := properties["sites"]; !ok {
			t.Fatal("search_web schema does not allow the sites constraint")
		}
		if _, ok := properties["domains"]; ok {
			t.Fatal("search_web schema still exposes the obsolete domains field")
		}
		return
	}
	t.Fatal("search_web tool not registered")
}

func TestSearchWebHandlerPreservesDefaultsAndExplicitFilters(t *testing.T) {
	for _, tc := range []struct {
		name, edition string
		args          map[string]any
		wantFilter    string
		wantValue     any
		wantField     string
		wantCount     float64
		wantDeepSeek  map[string]any
		wantError     bool
	}{
		{name: "configured default limit", edition: "custom", args: map[string]any{"query": "q"}, wantCount: 3, wantField: "Count"},
		{name: "sites constraint", edition: "custom", args: map[string]any{"query": "q", "limit": 2, "sites": []string{"example.com"}}, wantCount: 2, wantField: "Count", wantFilter: "Sites", wantValue: "example.com"},
		{name: "explicit zero auth filter", edition: "custom", args: map[string]any{"query": "q", "limit": 2, "auth_info_level": 0}, wantCount: 2, wantField: "Count", wantFilter: "AuthInfoLevel", wantValue: float64(0)},
		{name: "global rejects zero auth filter", edition: "global", args: map[string]any{"query": "q", "limit": 2, "auth_info_level": 0}, wantError: true},
		{name: "DeepSeek fields", edition: "deepseek", args: map[string]any{"query": "q", "limit": 2, "ori_question": "original question", "localization": "203.0.113.10", "need_content": true}, wantField: "count", wantCount: 2, wantDeepSeek: map[string]any{"ori_question": "original question", "localization": "203.0.113.10", "need_content": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan map[string]any, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				requests <- request
				result := map[string]any{"ResultCount": 0, "WebResults": []any{}}
				if tc.edition == "global" {
					result = map[string]any{"ErrorCode": 0, "TotalDocCount": 0, "Documents": []any{}}
				} else if tc.edition == "deepseek" {
					_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ResponseMetadata": map[string]any{"RequestId": "search-request"}, "Result": result})
			}))
			defer upstream.Close()

			db := repositoryTestDB(t)
			repo := repository.New(db)
			defer repo.Close()
			ctx := withMCPExecutionIdentity(context.Background(), "10000000-0000-4000-8000-000000000001", "project-1", "task-1", "execution-1")
			if err := repo.Users().Create(ctx, &model.User{ID: "10000000-0000-4000-8000-000000000001", Email: "search@example.com", Password: "fixture", InviteCode: "SEARCH"}); err != nil {
				t.Fatal(err)
			}
			if err := repo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: "10000000-0000-4000-8000-000000000001"}); err != nil {
				t.Fatal(err)
			}
			bundle, err := billing.LoadBundle(filepath.Join("..", "billing"))
			if err != nil {
				t.Fatal(err)
			}
			catalog := service.NewBillingCatalogService(repo, bundle, service.BillingCatalogOptions{})
			if _, err := catalog.Publish(ctx); err != nil {
				t.Fatal(err)
			}
			cfg := &serverconfig.SearchConfig{Enabled: true, Provider: tc.edition, Custom: serverconfig.SearchDoubaoConfig{MaxWebResults: 3}, Global: serverconfig.SearchDoubaoConfig{MaxWebResults: 3}, DeepSeek: serverconfig.SearchDeepSeekConfig{MaxResults: 3}}
			providerConfig := serverconfig.SearchDoubaoConfig{Endpoint: upstream.URL, APIKey: "fixture", Timeout: time.Second, MaxWebResults: 3}
			var provider service.SearchProvider = service.NewDoubaoCustomSearchProvider(providerConfig)
			if tc.edition == "global" {
				provider = service.NewDoubaoGlobalSearchProvider(providerConfig)
			} else if tc.edition == "deepseek" {
				provider = service.NewDeepSeekSearchProvider(serverconfig.SearchDeepSeekConfig{Endpoint: upstream.URL + "/v1/tools/search", APIKey: "fixture", Timeout: time.Second, MaxResults: 3})
			}
			old := svcs
			svcs = &Services{SearchSvc: service.NewSearchService(cfg, provider, catalog, service.NewBillingWalletService(repo, bundle, service.BillingWalletOptions{}), nil, repo.SearchOperations())}
			defer func() { svcs = old }()
			result, err := searchWebHandler(ctx, searchToolRequest(t, tc.args))
			if err != nil || result == nil || result.IsError != tc.wantError {
				t.Fatalf("search_web result = %#v, %v: %s", result, err, toolResultText(result))
			}
			var charges int64
			if err := db.Model(&model.BillingCharge{}).Count(&charges).Error; err != nil {
				t.Fatal(err)
			}
			if tc.wantError {
				if !strings.Contains(toolResultText(result), "Custom filters") || len(requests) != 0 || charges != 0 {
					t.Fatalf("invalid Global filter reached billing/provider: result=%s requests=%d charges=%d", toolResultText(result), len(requests), charges)
				}
				return
			}
			if len(requests) != 1 || charges != 1 {
				t.Fatalf("requests=%d charges=%d, want one of each", len(requests), charges)
			}
			request := <-requests
			if tc.wantField != "" && request[tc.wantField] != tc.wantCount {
				t.Fatalf("%s = %#v, want %v", tc.wantField, request[tc.wantField], tc.wantCount)
			}
			for key, want := range tc.wantDeepSeek {
				if request[key] != want {
					t.Fatalf("DeepSeek request %s = %#v, want %#v", key, request[key], want)
				}
			}
			if tc.wantFilter != "" {
				filter, _ := request["Filter"].(map[string]any)
				if value, exists := filter[tc.wantFilter]; !exists || value != tc.wantValue {
					t.Fatalf("Filter = %#v, want %s=%v", filter, tc.wantFilter, tc.wantValue)
				}
			}
		})
	}
}
