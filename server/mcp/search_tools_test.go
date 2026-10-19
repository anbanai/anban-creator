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
		{name: "non-string query", args: map[string]any{"query": 42}, want: "invalid_request"},
		{name: "non-string after", args: map[string]any{"query": "q", "after": 20260101}, want: "invalid_request"},
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

func TestFetchAndImageSearchHandlersRejectWrongStringTypes(t *testing.T) {
	old := svcs
	svcs = nil
	t.Cleanup(func() { svcs = old })
	ctx := withMCPExecutionIdentity(context.Background(), "user-1", "project-1", "task-1", "execution-1")
	for _, tc := range []struct {
		name    string
		handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
	}{
		{name: "fetch url", handler: fetchWebPageHandler, args: map[string]any{"url": 42}},
		{name: "image query", handler: searchImagesHandler, args: map[string]any{"query": 42}},
		{name: "image url", handler: searchImagesHandler, args: map[string]any{"image_url": 42}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.handler(ctx, searchToolRequest(t, tc.args))
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.IsError || !strings.Contains(toolResultText(result), "invalid_request") {
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

func TestSearchCapabilityToolsExposeProviderNeutralContracts(t *testing.T) {
	tools := listRegisteredTools(t, registerSearchTools)
	want := map[string]bool{"search_web": false, "fetch_web_page": false, "search_images": false}
	for _, tool := range tools {
		if _, ok := want[tool.Name]; !ok {
			continue
		}
		want[tool.Name] = true
		properties := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		for _, forbidden := range []string{"provider", "model", "endpoint", "api_key", "base_url", "ori_question", "localization", "auth_info_level", "query_rewrite"} {
			if _, ok := properties[forbidden]; ok {
				t.Fatalf("%s schema exposes provider-specific field %q", tool.Name, forbidden)
			}
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("%s tool not registered", name)
		}
	}
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
		{name: "DeepSeek neutral fields", edition: "deepseek", args: map[string]any{"query": "q", "limit": 2, "sites": []string{"example.com"}, "after": "2026-01-01", "include_content": true}, wantField: "count", wantCount: 2},
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
			cfg := &serverconfig.SearchConfig{Web: serverconfig.SearchCapabilityConfig{Enabled: true, Provider: map[string]string{"custom": "doubao_custom", "global": "doubao_global", "deepseek": "deepseek"}[tc.edition]}, Providers: serverconfig.SearchProvidersConfig{DeepSeek: serverconfig.SearchDeepSeekConfig{SearchEndpoint: upstream.URL + "/v1/tools/search", APIKey: "fixture", Timeout: time.Second, MaxResults: 3}, Doubao: serverconfig.SearchDoubaoConfig{CustomEndpoint: upstream.URL, GlobalEndpoint: upstream.URL, APIKey: "fixture", Timeout: time.Second, MaxResults: 3}}}
			providerConfig := serverconfig.SearchDoubaoConfig{CustomEndpoint: upstream.URL, GlobalEndpoint: upstream.URL, APIKey: "fixture", Timeout: time.Second, MaxResults: 3}
			var provider service.WebSearchProvider = service.NewDoubaoCustomSearchProvider(providerConfig)
			if tc.edition == "global" {
				provider = service.NewDoubaoGlobalSearchProvider(providerConfig)
			} else if tc.edition == "deepseek" {
				provider = service.NewDeepSeekSearchProvider(serverconfig.SearchDeepSeekConfig{SearchEndpoint: upstream.URL + "/v1/tools/search", APIKey: "fixture", Timeout: time.Second, MaxResults: 3})
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
				if len(requests) != 0 || charges != 0 {
					t.Fatalf("invalid request reached billing/provider: result=%s requests=%d charges=%d", toolResultText(result), len(requests), charges)
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
