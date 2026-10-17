package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

type searchProviderStub struct {
	response  *SearchResponse
	err       error
	responses []*SearchResponse
	errors    []error
	calls     int
}

func (p *searchProviderStub) Search(_ context.Context, req SearchRequest) (*SearchResponse, error) {
	call := p.calls
	p.calls++
	if call < len(p.errors) && p.errors[call] != nil {
		return nil, p.errors[call]
	}
	if call < len(p.responses) {
		return p.responses[call], nil
	}
	if p.response != nil {
		return p.response, p.err
	}
	return &SearchResponse{Provider: "stub", Query: req.Query}, p.err
}
func (p *searchProviderStub) Name() string                        { return "stub" }
func (p *searchProviderStub) ValidateRequest(SearchRequest) error { return nil }

func newSearchBillingFixture(t *testing.T) (repository.Repository, *gorm.DB, *BillingCatalogService, *BillingWalletService) {
	t.Helper()
	repo, db := newBillingServiceRepositoryWithDB(t)
	bundle := testBillingBundle()
	bundle.Products.SKUs = append(bundle.Products.SKUs, billing.SKUConfig{ID: "mcp.search_web", Operation: "mcp.search_web", ChargePolicy: "standalone_operation", PriceCredits: 0, Route: "search.web", Delivery: "search_results"})
	if err := repo.Users().Create(context.Background(), &model.User{ID: billingWalletUserID, Email: "search@example.com", Password: "fixture", InviteCode: "SEARCH01"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	catalog := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }, QuoteTTL: time.Hour})
	if _, err := catalog.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{UserID: billingWalletUserID}); err != nil {
		t.Fatal(err)
	}
	return repo, db, catalog, NewBillingWalletService(repo, &bundle, BillingWalletOptions{})
}

func TestDoubaoCustomSearchProviderUsesOfficialWebContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/search_api/web_search" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected headers")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["Query"] != "Anban" || request["SearchType"] != "web" || request["Count"] != float64(3) {
			t.Fatalf("custom request = %#v", request)
		}
		filter := request["Filter"].(map[string]any)
		if filter["NeedUrl"] != true || filter["Sites"] != "example.com" || filter["TimeRange"] != "OneWeek" {
			t.Fatalf("custom filter = %#v", filter)
		}
		if request["QueryControl"].(map[string]any)["QueryRewrite"] != true {
			t.Fatalf("custom query control = %#v", request["QueryControl"])
		}
		for _, field := range []string{"model", "input", "tools", "DocCount"} {
			if _, ok := request[field]; ok {
				t.Fatalf("legacy/unrelated field %q in request", field)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ResponseMetadata": map[string]any{"RequestId": "custom-req-123"},
			"Result": map[string]any{"ResultCount": 1, "WebResults": []any{map[string]any{
				"Id": "cite-1", "Title": "Anban", "Url": "https://example.com/anban", "Summary": "Relevant summary", "PublishTime": "2026-10-01T00:00:00Z", "AuthInfoDes": "非常权威", "RankScore": 0.91,
			}}},
		})
	}))
	defer server.Close()

	provider := NewDoubaoCustomSearchProvider(serverconfig.SearchDoubaoConfig{Endpoint: server.URL + "/search_api/web_search", APIKey: "secret", Timeout: time.Second, MaxWebResults: 10})
	needURL := true
	rewrite := true
	result, err := provider.Search(context.Background(), SearchRequest{Query: "Anban", Limit: 3, TimeRange: "OneWeek", Domains: []string{"example.com"}, NeedURL: &needURL, QueryRewrite: &rewrite})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Provider != "doubao_custom" || result.ProviderRequestID != "custom-req-123" || len(result.Results) != 1 {
		t.Fatalf("result = %#v", result)
	}
	item := result.Results[0]
	if item.Title != "Anban" || item.URL != "https://example.com/anban" || item.Domain != "example.com" || item.CitationID != "cite-1" || item.Snippet != "Relevant summary" || item.Authority != "非常权威" || item.RankScore != 0.91 {
		t.Fatalf("normalized result = %#v", item)
	}
}

func TestDoubaoGlobalSearchProviderUsesOfficialGlobalContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/search_api/global_search" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer global-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["Query"] != "Anban" || request["SearchType"] != "web" || request["DocCount"] != float64(4) {
			t.Fatalf("global request = %#v", request)
		}
		for _, field := range []string{"model", "input", "tools", "Count", "Filter", "QueryControl"} {
			if _, ok := request[field]; ok {
				t.Fatalf("unrelated field %q in Global request", field)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ResponseMetadata": map[string]any{"RequestId": "global-req-456"},
			"Result": map[string]any{"ErrorCode": 0, "ErrorMsg": "", "TotalDocCount": 17, "Documents": []any{map[string]any{
				"Rank": 2, "Url": "https://example.org/article", "Title": "Global result", "Snippet": []any{map[string]any{"Type": "text", "Text": "Global summary"}},
				"DocumentInfo": map[string]any{"Filetype": "webpage", "PublishTime": "2026-10-02T00:00:00Z"}, "HostInfo": map[string]any{"Hostname": "Example Org", "AuthorityLevel": "very_high"},
			}}},
		})
	}))
	defer server.Close()

	provider := NewDoubaoGlobalSearchProvider(serverconfig.SearchDoubaoConfig{Endpoint: server.URL + "/search_api/global_search", APIKey: "global-secret", Timeout: time.Second, MaxWebResults: 10})
	result, err := provider.Search(context.Background(), SearchRequest{Query: "Anban", Limit: 4})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Provider != "doubao_global" || result.ProviderRequestID != "global-req-456" || result.TotalCount != 17 || len(result.Results) != 1 {
		t.Fatalf("result = %#v", result)
	}
	item := result.Results[0]
	if item.Title != "Global result" || item.URL != "https://example.org/article" || item.Domain != "example.org" || item.Snippet != "Global summary" || item.PublishedAt != "2026-10-02T00:00:00Z" || item.Authority != "very_high" || item.Rank != 2 {
		t.Fatalf("normalized result = %#v", item)
	}
}

func TestDeepSeekSearchProviderUsesOfficialSearchContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/tools/search" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer deepseek-secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected headers")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["query"] != "DeepSeek Search" || request["count"] != float64(3) || request["need_content"] != true || request["ori_question"] != "What is the latest DeepSeek Search API?" || request["localization"] != "203.0.113.10" {
			t.Fatalf("deepseek request = %#v", request)
		}
		for _, field := range []string{"model", "input", "tools", "SearchType", "Count", "DocCount"} {
			if _, ok := request[field]; ok {
				t.Fatalf("unrelated field %q in DeepSeek request", field)
			}
		}
		w.Header().Set("X-Request-ID", "ds-req-123")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{
			"rerank_score": 2.4,
			"title":        "DeepSeek Search",
			"url":          "https://example.com/deepseek",
			"snippet":      "Short summary",
			"summary":      "Long summary",
			"content":      "Markdown content",
			"site_name":    "Example",
			"date":         "2026-10-17T00:00:00+00:00",
			"authority":    3,
		}}})
	}))
	defer server.Close()

	needContent := true
	provider := NewDeepSeekSearchProvider(serverconfig.SearchDeepSeekConfig{Endpoint: server.URL + "/v1/tools/search", APIKey: "deepseek-secret", Timeout: time.Second, MaxResults: 10})
	result, err := provider.Search(context.Background(), SearchRequest{Query: "DeepSeek Search", Limit: 3, NeedContent: &needContent, OriginalQuestion: "What is the latest DeepSeek Search API?", Localization: "203.0.113.10"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Provider != "deepseek" || result.ProviderRequestID != "ds-req-123" || result.TotalCount != 1 || len(result.Results) != 1 {
		t.Fatalf("result = %#v", result)
	}
	item := result.Results[0]
	if item.Title != "DeepSeek Search" || item.URL != "https://example.com/deepseek" || item.Domain != "example.com" || item.Snippet != "Short summary" || item.Summary != "Long summary" || item.Content != "Markdown content" || item.PublishedAt != "2026-10-17T00:00:00+00:00" || item.Authority != "3" || item.RankScore != 2.4 || item.CitationID != item.URL {
		t.Fatalf("normalized result = %#v", item)
	}
}

func TestDeepSeekSearchProviderRejectsUnsupportedFiltersAndInvalidQueries(t *testing.T) {
	provider := NewDeepSeekSearchProvider(serverconfig.SearchDeepSeekConfig{Endpoint: "https://api.deepseek.com/v1/tools/search", APIKey: "secret", Timeout: time.Second, MaxResults: 50})
	tests := []SearchRequest{
		{Query: "q", Domains: []string{"example.com"}},
		{Query: "q", TimeRange: "OneWeek"},
		{Query: strings.Repeat("q", 129)},
		{Query: "q", Limit: 51},
		{Query: "q", Localization: "CN"},
	}
	for _, req := range tests {
		if err := provider.ValidateRequest(req); err == nil {
			t.Fatalf("ValidateRequest accepted %#v", req)
		}
	}
}

func TestDeepSeekSearchProviderSanitizesFailuresAndAcceptsEmptyResults(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/empty":
			_, _ = w.Write([]byte(`{"results":[]}`))
		case "/unauthorized":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"secret upstream detail"}}`))
		case "/server-error":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"secret upstream detail"}}`))
		case "/invalid":
			_, _ = w.Write([]byte(`{"results":[`))
		case "/missing-results":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/oversized":
			_, _ = io.WriteString(w, `{"results":[]}`+strings.Repeat(" ", (4<<20)+1))
		case "/redirect":
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		case "/timeout":
			time.Sleep(100 * time.Millisecond)
			_, _ = w.Write([]byte(`{"results":[]}`))
		}
	}))
	defer server.Close()

	for _, tc := range []struct {
		path       string
		timeout    time.Duration
		wantResult bool
	}{
		{path: "/empty", timeout: time.Second, wantResult: true},
		{path: "/unauthorized", timeout: time.Second},
		{path: "/server-error", timeout: time.Second},
		{path: "/invalid", timeout: time.Second},
		{path: "/missing-results", timeout: time.Second},
		{path: "/oversized", timeout: time.Second},
		{path: "/redirect", timeout: time.Second},
		{path: "/timeout", timeout: 20 * time.Millisecond},
	} {
		t.Run(tc.path, func(t *testing.T) {
			provider := NewDeepSeekSearchProvider(serverconfig.SearchDeepSeekConfig{Endpoint: server.URL + tc.path, APIKey: "secret", Timeout: tc.timeout, MaxResults: 10})
			result, err := provider.Search(context.Background(), SearchRequest{Query: "test"})
			if tc.wantResult {
				if err != nil || result == nil || len(result.Results) != 0 {
					t.Fatalf("Search() = %#v, %v; want valid empty result", result, err)
				}
				return
			}
			if err == nil || !IsSearchProviderUnavailable(err) || strings.Contains(err.Error(), "secret upstream detail") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error = %v, want sanitized provider unavailable", err)
			}
		})
	}
	if got := redirected.Load(); got != 0 {
		t.Fatalf("redirect target received %d request(s), want zero", got)
	}
}

func TestDoubaoGlobalSearchRejectsCustomOnlyFilters(t *testing.T) {
	provider := NewDoubaoGlobalSearchProvider(serverconfig.SearchDoubaoConfig{Endpoint: "https://example.com/search_api/global_search", APIKey: "secret", Timeout: time.Second})
	if err := provider.ValidateRequest(SearchRequest{Query: "q", TimeRange: "OneWeek"}); err == nil {
		t.Fatal("Global provider accepted a Custom-only filter")
	}
}

func TestDoubaoCustomSearchAcceptsExplicitDateRange(t *testing.T) {
	provider := NewDoubaoCustomSearchProvider(serverconfig.SearchDoubaoConfig{Endpoint: "https://example.com/search_api/web_search", APIKey: "secret", Timeout: time.Second})
	if err := provider.ValidateRequest(SearchRequest{Query: "q", TimeRange: "2026-09-01..2026-10-01"}); err != nil {
		t.Fatalf("ValidateRequest() rejected a valid date range: %v", err)
	}
}

func TestDoubaoGlobalSearchRejectsProviderResultError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ResponseMetadata": map[string]any{"RequestId": "global-error"},
			"Result":           map[string]any{"ErrorCode": 10001, "ErrorMsg": "upstream details must not escape"},
		})
	}))
	defer server.Close()
	provider := NewDoubaoGlobalSearchProvider(serverconfig.SearchDoubaoConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second})
	_, err := provider.Search(context.Background(), SearchRequest{Query: "test"})
	if err == nil || !IsSearchProviderUnavailable(err) || strings.Contains(err.Error(), "upstream details") {
		t.Fatalf("error = %v, want sanitized provider_unavailable", err)
	}
}

func TestDoubaoSearchProviderRejectsMalformedResultPayloads(t *testing.T) {
	for _, tc := range []struct {
		name, edition, result string
	}{
		{name: "custom malformed scalar", edition: "custom", result: `"not-an-object"`},
		{name: "custom missing fields", edition: "custom", result: `{}`},
		{name: "global malformed scalar", edition: "global", result: `"not-an-object"`},
		{name: "global missing fields", edition: "global", result: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"malformed"},"Result":` + tc.result + `}`))
			}))
			defer server.Close()
			var provider *DoubaoSearchProvider
			if tc.edition == "custom" {
				provider = NewDoubaoCustomSearchProvider(serverconfig.SearchDoubaoConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second})
			} else {
				provider = NewDoubaoGlobalSearchProvider(serverconfig.SearchDoubaoConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second})
			}
			_, err := provider.Search(context.Background(), SearchRequest{Query: "test"})
			if err == nil || !IsSearchProviderUnavailable(err) {
				t.Fatalf("error = %v, want provider_unavailable", err)
			}
		})
	}
}

func TestDoubaoSearchProviderAcceptsExplicitEmptyResults(t *testing.T) {
	for _, tc := range []struct {
		name, edition, result string
	}{
		{name: "custom", edition: "custom", result: `{"ResultCount":0,"WebResults":[]}`},
		{name: "global", edition: "global", result: `{"ErrorCode":0,"TotalDocCount":0,"Documents":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"empty"},"Result":` + tc.result + `}`))
			}))
			defer server.Close()
			var provider *DoubaoSearchProvider
			if tc.edition == "custom" {
				provider = NewDoubaoCustomSearchProvider(serverconfig.SearchDoubaoConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second})
			} else {
				provider = NewDoubaoGlobalSearchProvider(serverconfig.SearchDoubaoConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second})
			}
			result, err := provider.Search(context.Background(), SearchRequest{Query: "test"})
			if err != nil || result == nil || len(result.Results) != 0 {
				t.Fatalf("Search() = %#v, %v; want a valid empty response", result, err)
			}
		})
	}
}

func TestSearchProviderCostFiltersPreserveSearchConstraints(t *testing.T) {
	needContent := true
	rewrite := false
	filters := searchProviderCostFilters(SearchRequest{
		TimeRange: "OneWeek", Domains: []string{" example.com ", "example.com"}, BlockHosts: []string{"ads.example.com"},
		NeedContent: &needContent, AuthInfoLevel: 1, ContentFormats: "markdown", Industry: "finance", QueryRewrite: &rewrite,
	})
	if filters["time_range"] != "OneWeek" || filters["auth_info_level"] != 1 || filters["content_formats"] != "markdown" || filters["industry"] != "finance" || filters["need_content"] != true || filters["query_rewrite"] != false {
		t.Fatalf("filters = %#v", filters)
	}
	sites, ok := filters["sites"].([]string)
	if !ok || len(sites) != 1 || sites[0] != "example.com" {
		t.Fatalf("sites = %#v", filters["sites"])
	}
}

func TestDoubaoSearchProviderRejectsFailuresAndHidesSecrets(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "http error", status: http.StatusUnauthorized, body: `{"error":"unauthorized"}`},
		{name: "invalid json", status: http.StatusOK, body: `{"ResponseMetadata":`},
		{name: "missing result", status: http.StatusOK, body: `{"ResponseMetadata":{"RequestId":"missing"}}`},
		{name: "provider error", status: http.StatusOK, body: `{"ResponseMetadata":{"RequestId":"bad","Error":{"Code":"10403","Message":"permission denied"}},"Result":null}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			provider := NewDoubaoCustomSearchProvider(serverconfig.SearchDoubaoConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second})
			_, err := provider.Search(context.Background(), SearchRequest{Query: "test"})
			if err == nil || !IsSearchProviderUnavailable(err) {
				t.Fatalf("error = %v, want provider unavailable", err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "permission denied") {
				t.Fatalf("secret/upstream details leaked: %v", err)
			}
		})
	}
}

func TestSearchServiceCreatesZeroPriceChargeAndReplaysIdempotently(t *testing.T) {
	repo, db, catalog, wallet := newSearchBillingFixture(t)
	defer repo.Close()
	provider := &searchProviderStub{response: &SearchResponse{Provider: "stub", ProviderRequestID: "request-1", Query: "anban", Results: []SearchResultItem{}}}
	cfg := &serverconfig.SearchConfig{Enabled: true, Provider: "stub", Custom: serverconfig.SearchDoubaoConfig{Timeout: 30 * time.Second}}
	svc := NewSearchService(cfg, provider, catalog, wallet, nil, repo.SearchOperations())
	req := SearchOperationRequest{UserID: billingWalletUserID, ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-1", Request: SearchRequest{Query: "anban", Limit: 3}}
	first, err := svc.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProviderRequestID != second.ProviderRequestID || provider.calls != 1 {
		t.Fatalf("replay = %#v calls=%d", second, provider.calls)
	}
	var charges int64
	if err := db.Model(&model.BillingCharge{}).Where("user_id = ? AND sku_id = ?", billingWalletUserID, "mcp.search_web").Count(&charges).Error; err != nil {
		t.Fatal(err)
	}
	if charges != 1 {
		t.Fatalf("charge count = %d, want one", charges)
	}
	var account model.BillingWalletAccount
	if err := db.Where("user_id = ?", billingWalletUserID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.PaidCredits != 0 || account.PromotionalCredits != 0 || account.DebtCredits != 0 {
		t.Fatalf("wallet changed: %+v", account)
	}
}

func TestSearchServiceOperationLeaseFollowsConfiguredProviderTimeout(t *testing.T) {
	svc := &SearchService{config: &serverconfig.SearchConfig{Provider: "custom", Custom: serverconfig.SearchDoubaoConfig{Timeout: 10 * time.Minute}}}
	if got := svc.operationLease(); got != 11*time.Minute {
		t.Fatalf("operationLease() = %s", got)
	}
	if got := (&SearchService{config: &serverconfig.SearchConfig{Provider: "global", Global: serverconfig.SearchDoubaoConfig{Timeout: 10 * time.Minute}}}).operationLease(); got != 11*time.Minute {
		t.Fatalf("global operationLease() = %s", got)
	}
	if got := (&SearchService{config: &serverconfig.SearchConfig{Provider: "deepseek", DeepSeek: serverconfig.SearchDeepSeekConfig{Timeout: 10 * time.Minute}}}).operationLease(); got != 11*time.Minute {
		t.Fatalf("DeepSeek operationLease() = %s", got)
	}
	var nilService *SearchService
	if got := nilService.operationLease(); got != 5*time.Minute {
		t.Fatalf("nil operationLease() = %s", got)
	}
}
