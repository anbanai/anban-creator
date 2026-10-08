package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func (p *searchProviderStub) Search(context.Context, SearchRequest) (*SearchResponse, error) {
	call := p.calls
	p.calls++
	if call < len(p.errors) && p.errors[call] != nil {
		return nil, p.errors[call]
	}
	if call < len(p.responses) {
		return p.responses[call], nil
	}
	return p.response, p.err
}
func (p *searchProviderStub) Name() string  { return "stub" }
func (p *searchProviderStub) Model() string { return "stub-model" }

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

func TestDoubaoSearchProviderParsesWebSearchResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v3/responses" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("ark-beta-web-search"); got != "true" {
			t.Fatalf("ark-beta-web-search = %q", got)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["model"] != "doubao-test" {
			t.Fatalf("model = %#v", request["model"])
		}
		tools, ok := request["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("tools = %#v", request["tools"])
		}
		webTool := tools[0].(map[string]any)
		if webTool["limit"] != float64(3) {
			t.Fatalf("web search limit = %#v", webTool["limit"])
		}
		if _, ok := webTool["sources"].([]any); !ok {
			t.Fatalf("web search sources = %#v", webTool["sources"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp-123",
			"output": []any{
				map[string]any{
					"type":         "web_search_result",
					"title":        "Anban",
					"url":          "https://example.com/anban",
					"snippet":      "Server-owned search",
					"published_at": "2026-10-01T00:00:00Z",
				},
			},
			"usage": map[string]any{"tool_usage": map[string]any{"web_search_requests": 1}},
		})
	}))
	defer server.Close()

	provider := NewDoubaoSearchProvider(serverconfig.SearchDoubaoConfig{
		BaseURL:    server.URL + "/api/v3",
		APIKey:     "secret",
		Model:      "doubao-test",
		Timeout:    time.Second,
		MaxResults: 10,
	})
	result, err := provider.Search(context.Background(), SearchRequest{Query: "Anban", Limit: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.ProviderRequestID != "resp-123" || len(result.Results) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Results[0].Title != "Anban" || result.Results[0].URL != "https://example.com/anban" {
		t.Fatalf("result item = %#v", result.Results[0])
	}
	if result.Results[0].Domain != "example.com" {
		t.Fatalf("domain = %q", result.Results[0].Domain)
	}
}

func TestDoubaoSearchProviderParsesNestedCitationsAndConstraints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		tool := request["tools"].([]any)[0].(map[string]any)
		if _, ok := tool["sources"].([]any); !ok {
			t.Fatalf("sources = %#v", tool["sources"])
		}
		var requestInput []any
		for _, item := range request["input"].([]any) {
			message := item.(map[string]any)
			for _, content := range message["content"].([]any) {
				requestInput = append(requestInput, content)
			}
		}
		if len(requestInput) != 1 || !strings.Contains(requestInput[0].(map[string]any)["text"].(string), "site:example.com") || !strings.Contains(requestInput[0].(map[string]any)["text"].(string), "时间范围:week") {
			t.Fatalf("constrained query = %#v", requestInput)
		}
		response := map[string]any{}
		response["id"] = "resp-nested"
		response["output"] = []any{
			map[string]any{"type": "web_search_call", "results": []any{
				map[string]any{"title": "Nested title", "url": "https://example.com/a", "snippet": "summary", "published_at": "2026-10-01", "citation_id": "cite-a"},
			}},
			map[string]any{"type": "message", "content": []any{
				map[string]any{"type": "output_text", "annotations": []any{
					map[string]any{"type": "url_citation", "title": "Annotated", "url": "https://example.org/b", "citation_id": "cite-b"},
				}},
			}},
		}
		response["usage"] = map[string]any{"tool_usage": map[string]any{"web_search_requests": 1}}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	provider := NewDoubaoSearchProvider(serverconfig.SearchDoubaoConfig{BaseURL: server.URL, APIKey: "secret", Model: "doubao-test", Timeout: time.Second, MaxResults: 10})
	result, err := provider.Search(context.Background(), SearchRequest{Query: "q", Limit: 2, TimeRange: "week", Domains: []string{"example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProviderRequestID != "resp-nested" || len(result.Results) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Results[1].CitationID != "cite-b" || result.Results[1].Domain != "example.org" {
		t.Fatalf("citation = %#v", result.Results[1])
	}
}

func TestDoubaoSearchProviderReturnsEmptyResultsAndHidesAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "empty", "output": []any{}})
	}))
	defer server.Close()
	provider := NewDoubaoSearchProvider(serverconfig.SearchDoubaoConfig{BaseURL: server.URL, APIKey: "super-secret", Model: "doubao-test", Timeout: time.Second})
	result, err := provider.Search(context.Background(), SearchRequest{Query: "q"})
	if err != nil || len(result.Results) != 0 {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
	if strings.Contains(fmt.Sprintf("%#v", result), "super-secret") {
		t.Fatal("API key leaked in result")
	}
}

func TestDoubaoSearchProviderRejectsHTTPFailuresAndInvalidResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "client error", statusCode: http.StatusUnauthorized, body: `{"error":"unauthorized"}`},
		{name: "server error", statusCode: http.StatusBadGateway, body: `{"error":"upstream failed"}`},
		{name: "invalid JSON", statusCode: http.StatusOK, body: `{"id":`},
		{name: "missing output", statusCode: http.StatusOK, body: `{"id":"response-without-output"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			provider := NewDoubaoSearchProvider(serverconfig.SearchDoubaoConfig{BaseURL: server.URL, APIKey: "secret", Model: "doubao-test", Timeout: time.Second})
			_, err := provider.Search(context.Background(), SearchRequest{Query: "test"})
			if err == nil || !IsSearchProviderUnavailable(err) {
				t.Fatalf("error = %v, want provider unavailable", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaked API key: %v", err)
			}
		})
	}
}

func TestDoubaoSearchProviderTreatsTimeoutAsUnavailable(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(release)

	provider := NewDoubaoSearchProvider(serverconfig.SearchDoubaoConfig{BaseURL: server.URL, APIKey: "secret", Model: "doubao-test", Timeout: 30 * time.Millisecond})
	_, err := provider.Search(context.Background(), SearchRequest{Query: "test"})
	select {
	case <-started:
	default:
		t.Fatal("provider request did not reach the HTTP test server")
	}
	if err == nil || !IsSearchProviderUnavailable(err) {
		t.Fatalf("error = %v, want provider unavailable", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked API key: %v", err)
	}
}

func TestSearchServiceCreatesZeroPriceChargeAndReplaysItIdempotently(t *testing.T) {
	repo, db, catalog, wallet := newSearchBillingFixture(t)
	defer repo.Close()
	provider := &searchProviderStub{response: &SearchResponse{Provider: "stub", Model: "stub-model", ProviderRequestID: "request-1", Query: "anban", Results: []SearchResultItem{}}}
	cfg := &serverconfig.SearchConfig{Enabled: true, Provider: "stub"}
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
	if first.ProviderRequestID != second.ProviderRequestID || first.Query != second.Query || provider.calls != 1 {
		t.Fatalf("replayed results/calls = %#v/%d, want cached response and one provider call", second, provider.calls)
	}
	var charges int64
	if err := db.Model(&model.BillingCharge{}).Where("user_id = ? AND sku_id = ?", billingWalletUserID, "mcp.search_web").Count(&charges).Error; err != nil {
		t.Fatal(err)
	}
	if charges != 1 {
		t.Fatalf("zero-price charge count = %d, want one", charges)
	}
	var account model.BillingWalletAccount
	if err := db.Where("user_id = ?", billingWalletUserID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.PaidCredits != 0 || account.PromotionalCredits != 0 || account.DebtCredits != 0 {
		t.Fatalf("wallet changed for zero-price search: %+v", account)
	}
}

func TestSearchServiceOperationLeaseFollowsConfiguredProviderTimeout(t *testing.T) {
	tests := []struct {
		name      string
		timeout   time.Duration
		wantLease time.Duration
	}{
		{name: "default safety lease", timeout: 30 * time.Second, wantLease: 5 * time.Minute},
		{name: "long provider timeout includes margin", timeout: 10 * time.Minute, wantLease: 11 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &SearchService{config: &serverconfig.SearchConfig{Doubao: serverconfig.SearchDoubaoConfig{Timeout: tt.timeout}}}
			if got := svc.operationLease(); got != tt.wantLease {
				t.Fatalf("operationLease() = %s, want %s", got, tt.wantLease)
			}
		})
	}
	var nilService *SearchService
	if got := nilService.operationLease(); got != 5*time.Minute {
		t.Fatalf("nil operationLease() = %s, want 5m", got)
	}
}

func TestSearchServiceCostRecordingFailureDoesNotDiscardResults(t *testing.T) {
	repo, _, catalog, wallet := newSearchBillingFixture(t)
	defer repo.Close()
	provider := &searchProviderStub{response: &SearchResponse{Provider: "stub", Model: "stub-model", ProviderRequestID: "request-2", Query: "anban"}}
	cfg := &serverconfig.SearchConfig{Enabled: true, Provider: "stub"}
	// An unconfigured ProviderCostService exercises the unreconciled logging path.
	svc := NewSearchService(cfg, provider, catalog, wallet, &ProviderCostService{}, nil)
	result, err := svc.Search(context.Background(), SearchOperationRequest{UserID: billingWalletUserID, ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-1", Request: SearchRequest{Query: "anban"}})
	if err != nil || result == nil || result.Query != "anban" {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
}

func TestSearchServiceRecordsFailedProviderAttemptAsUnreconciled(t *testing.T) {
	repo, db, catalog, wallet := newSearchBillingFixture(t)
	defer repo.Close()
	costs := NewProviderCostService(repository.NewBillingCostRepository(db), providerCostBundle())
	provider := &searchProviderStub{
		responses: []*SearchResponse{nil, {Provider: "stub", Model: "stub-model", Query: "failed search"}},
		errors:    []error{ErrSearchProviderUnavailable, nil},
	}
	svc := NewSearchService(&serverconfig.SearchConfig{Enabled: true, Provider: "stub"}, provider, catalog, wallet, costs, repo.SearchOperations())
	req := SearchOperationRequest{UserID: billingWalletUserID, ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-1", Request: SearchRequest{Query: "failed search", Limit: 3}}
	if _, err := svc.Search(context.Background(), req); !IsSearchProviderUnavailable(err) {
		t.Fatalf("Search error = %v, want provider unavailable", err)
	}

	var events []model.BillingProviderCostEvent
	if err := db.Where("task_id = ?", req.TaskID).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Status != model.BillingProviderCostStatusUnreconciled || events[0].Provider != "stub" || events[0].Model != "stub-model" {
		t.Fatalf("failed search cost events = %#v", events)
	}
	var evidence map[string]any
	if err := json.Unmarshal(events[0].UsageEvidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence["kind"] != "search_request" || evidence["query"] != req.Request.Query || evidence["result_count"] != float64(0) || evidence["search_request_count"] != float64(1) {
		t.Fatalf("failed search evidence = %#v", evidence)
	}
	if evidence["outcome"] != "provider_error" || evidence["request_id_source"] != "search_attempt" {
		t.Fatalf("failed search outcome/request ID source = %#v/%#v", evidence["outcome"], evidence["request_id_source"])
	}
	failedRequestID := events[0].ProviderRequestID
	if !strings.HasPrefix(failedRequestID, "search-attempt-") {
		t.Fatalf("failed request ID = %q, want synthetic attempt identity", failedRequestID)
	}
	if result, err := svc.Search(context.Background(), req); err != nil || result == nil {
		t.Fatalf("retry result/error = %#v/%v", result, err)
	}
	if err := db.Where("task_id = ?", req.TaskID).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].ProviderRequestID == events[1].ProviderRequestID {
		t.Fatalf("provider retry cost events = %#v, want one distinct event per attempt", events)
	}
}

func TestSearchServiceSyntheticCostIdentityIncludesRequestAndExecution(t *testing.T) {
	repo, db, catalog, wallet := newSearchBillingFixture(t)
	defer repo.Close()
	costBundle := providerCostBundle()
	costs := NewProviderCostService(repository.NewBillingCostRepository(db), costBundle)
	provider := &searchProviderStub{response: &SearchResponse{Provider: "stub", Model: "stub-model", Query: "provider-query"}}
	svc := NewSearchService(&serverconfig.SearchConfig{Enabled: true, Provider: "stub"}, provider, catalog, wallet, costs, nil)
	base := SearchOperationRequest{UserID: billingWalletUserID, ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-1", Request: SearchRequest{Query: "requested-query", Limit: 3, TimeRange: "week", Domains: []string{"example.com"}}}
	first, err := svc.Search(context.Background(), base)
	if err != nil || first == nil {
		t.Fatalf("first search = %#v, %v", first, err)
	}
	secondReq := base
	secondReq.Request.Limit = 4
	if _, err := svc.Search(context.Background(), secondReq); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Search(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	thirdExecution := base
	thirdExecution.ExecutionID = "execution-2"
	if _, err := svc.Search(context.Background(), thirdExecution); err != nil {
		t.Fatal(err)
	}
	var events []model.BillingProviderCostEvent
	if err := db.Where("provider = ? AND model = ?", "stub", "stub-model").Order("created_at asc").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("search cost event count = %d, want 4 actual provider attempts", len(events))
	}
	seenRequestIDs := make(map[string]struct{}, len(events))
	for _, event := range events {
		if !strings.HasPrefix(event.ProviderRequestID, "search-attempt-") {
			t.Fatalf("synthetic request ID = %q", event.ProviderRequestID)
		}
		if _, seen := seenRequestIDs[event.ProviderRequestID]; seen {
			t.Fatalf("synthetic request ID repeated: %q", event.ProviderRequestID)
		}
		seenRequestIDs[event.ProviderRequestID] = struct{}{}
	}
	var evidence map[string]any
	if err := json.Unmarshal(events[0].UsageEvidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence["query"] != "requested-query" {
		t.Fatalf("cost evidence query = %#v, want request query", evidence["query"])
	}
}
